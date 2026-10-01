// Package repoconfig reads JustCD application definitions from a Git checkout.
package repoconfig

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/store"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

const MaxDefinitions = 500

var namePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type Definition struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Source struct {
			Renderer                   string   `json:"renderer"`
			ReleaseName                string   `json:"releaseName,omitempty"`
			Path                       string   `json:"path"`
			ValuesFiles                []string `json:"valuesFiles,omitempty"`
			ValuesYAML                 string   `json:"valuesYaml,omitempty"`
			KustomizeHelmEnabled       bool     `json:"kustomizeHelmEnabled,omitempty"`
			KustomizeNamespaceOverride bool     `json:"kustomizeNamespaceOverride,omitempty"`
		} `json:"source"`
		Destination struct {
			CreateNamespaces bool   `json:"createNamespaces,omitempty"`
			Cluster          string `json:"cluster"`
			Namespace        string `json:"namespace"`
		} `json:"destination"`
		PullRequests    *store.RepositoryPRConfig `json:"pullRequests,omitempty"`
		IgnoreResources []IgnoreResource          `json:"ignoreResources,omitempty"`
		SyncPolicy      string                    `json:"syncPolicy,omitempty"`
		PollSeconds     int                       `json:"pollSeconds,omitempty"`
	} `json:"spec"`
	File string `json:"-"`
	Hash string `json:"-"`
}

// Discover validates the complete snapshot before the caller makes changes.
func Discover(root string) ([]Definition, error) {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(canonicalRoot)
	if err != nil {
		return nil, err
	}
	definitions := []Definition{}
	names := map[string]bool{}
	err = filepath.WalkDir(root, func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "justcd.yaml" && entry.Name() != "justcd.yml" {
			return nil
		}
		relative, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		fail := func(err error) error { return fmt.Errorf("%s: %w", relative, err) }
		if entry.Type()&os.ModeSymlink != 0 {
			return fail(fmt.Errorf("configuration must not be a symlink"))
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 256<<10 {
			return fail(fmt.Errorf("configuration must be a regular file no larger than 256 KiB"))
		}
		if len(definitions) >= MaxDefinitions {
			return fail(fmt.Errorf("repository exceeds %d application definitions", MaxDefinitions))
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
		first, err := reader.Read()
		if err != nil {
			return fail(err)
		}
		if _, err := reader.Read(); err != io.EOF {
			return fail(fmt.Errorf("exactly one Application document is required per file"))
		}
		data = first
		var d Definition
		if err := yaml.UnmarshalStrict(data, &d); err != nil {
			return fail(err)
		}
		if d.APIVersion != "justcd.io/v1alpha1" || d.Kind != "Application" {
			return fail(fmt.Errorf("expected justcd.io/v1alpha1 Application"))
		}
		if len(d.Metadata.Name) > 63 || !namePattern.MatchString(d.Metadata.Name) {
			return fail(fmt.Errorf("name must be a lowercase Kubernetes-style name, at most 63 characters"))
		}
		if names[d.Metadata.Name] {
			return fail(fmt.Errorf("duplicate application name %q", d.Metadata.Name))
		}
		names[d.Metadata.Name] = true
		source := &d.Spec.Source
		if source.Renderer != "yaml" && source.Renderer != "kustomize" && source.Renderer != "helm" {
			return fail(fmt.Errorf("renderer must be yaml, kustomize, or helm"))
		}
		if source.Renderer != "kustomize" && (source.KustomizeHelmEnabled || source.KustomizeNamespaceOverride) {
			return fail(fmt.Errorf("Kustomize settings require the kustomize renderer"))
		}
		if source.Renderer != "helm" && (len(source.ValuesFiles) > 0 || source.ValuesYAML != "" || source.ReleaseName != "") {
			return fail(fmt.Errorf("values settings require the helm renderer"))
		}
		if source.ReleaseName != "" && (!namePattern.MatchString(source.ReleaseName) || len(source.ReleaseName) > 53) {
			return fail(fmt.Errorf("releaseName must be a lowercase Helm release name, at most 53 characters"))
		}
		if source.ValuesYAML != "" {
			var values map[string]any
			if err := yaml.UnmarshalStrict([]byte(source.ValuesYAML), &values); err != nil {
				return fail(fmt.Errorf("invalid Helm values: %w", err))
			}
			if values == nil {
				return fail(fmt.Errorf("Helm values must be a YAML mapping"))
			}
		}
		if source.Path == "" {
			source.Path = "."
		}
		source.Path, err = resolvePath(root, filepath.Dir(file), source.Path)
		if err != nil {
			return fail(err)
		}
		for i, value := range source.ValuesFiles {
			source.ValuesFiles[i], err = resolvePath(root, filepath.Dir(file), value)
			if err != nil {
				return fail(err)
			}
		}
		if source.Renderer == "yaml" && (filepath.Base(source.Path) == "justcd.yaml" || filepath.Base(source.Path) == "justcd.yml") {
			return fail(fmt.Errorf("manifest path must point to Kubernetes manifests, not a JustCD definition"))
		}
		if d.Spec.Destination.Cluster == "" || !namePattern.MatchString(d.Spec.Destination.Namespace) || len(d.Spec.Destination.Namespace) > 63 {
			return fail(fmt.Errorf("existing cluster and valid namespace are required"))
		}
		if err := validateIgnores(d.Spec.IgnoreResources, d.Spec.Destination.Namespace); err != nil {
			return fail(err)
		}
		if pr := d.Spec.PullRequests; pr != nil {
			if pr.CredentialID == "" {
				return fail(fmt.Errorf("pullRequests.credentialId must reference an existing HTTPS credential"))
			}
			if pr.Provider != "" && pr.Provider != "github" && pr.Provider != "gitlab" {
				return fail(fmt.Errorf("pullRequests.provider must be github or gitlab"))
			}
			profile := &pr.PreviewProfile
			if profile.ManifestPath != "" {
				profile.ManifestPath, err = resolvePath(root, filepath.Dir(file), profile.ManifestPath)
				if err != nil {
					return fail(err)
				}
			}
			for i, filename := range profile.HelmValuesFiles {
				profile.HelmValuesFiles[i], err = resolvePath(root, filepath.Dir(file), filename)
				if err != nil {
					return fail(err)
				}
			}
			if err := store.ValidatePreviewProfile(profile, store.Application{Renderer: source.Renderer, ManifestPath: source.Path}); err != nil {
				return fail(fmt.Errorf("pullRequests: %w", err))
			}
		}
		if d.Spec.SyncPolicy == "" {
			d.Spec.SyncPolicy = "manual"
		}
		if d.Spec.SyncPolicy != "manual" && d.Spec.SyncPolicy != "auto-safe" {
			return fail(fmt.Errorf("syncPolicy must be manual or auto-safe"))
		}
		if d.Spec.PollSeconds == 0 {
			d.Spec.PollSeconds = 300
		}
		if d.Spec.PollSeconds < 30 || d.Spec.PollSeconds > 86400 {
			return fail(fmt.Errorf("pollSeconds must be between 30 and 86400"))
		}
		d.File = relative
		encoded, _ := json.Marshal(d)
		sum := sha256.Sum256(encoded)
		d.Hash = hex.EncodeToString(sum[:])
		definitions = append(definitions, d)
		return nil
	})
	return definitions, err
}

func resolvePath(root, directory, value string) (string, error) {
	if filepath.IsAbs(value) || strings.Contains(value, "\\") {
		return "", fmt.Errorf("paths must be relative to the configuration file")
	}
	target := filepath.Join(directory, value)
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("path %q is unavailable", value)
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the repository", value)
	}
	return filepath.ToSlash(relative), nil
}
