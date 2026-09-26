package render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

const (
	maxManifestFileBytes = 2 << 20
	maxRenderOutputBytes = 64 << 20
	maxRenderedResources = 10000
)

type Options struct {
	RepositoryRoot             string
	ManifestPath               string
	Renderer                   string
	KustomizeHelmEnabled       bool
	KustomizeNamespaceOverride bool
	HelmValuesFiles            []string
	HelmValuesYAML             string
	TargetHelmValuesFiles      []string
	TargetHelmValuesYAML       string
	NamespaceHelmValues        map[string]core.HelmValuesOverride
	TargetManifestPath         string
	NamespaceManifestPaths     map[string]string
	ApplicationID              string
	ClusterID                  string
	Namespaces                 []core.Binding
	Mapper                     meta.RESTMapper
	IgnoredResources           []core.Identity
	IgnoredSelectors           []core.IgnoreSelector
}

type RendererError struct {
	renderer  string
	code      string
	message   string
	retryable bool
}

func (e *RendererError) Error() string     { return e.message }
func (e *RendererError) Retryable() bool   { return e.retryable }
func (e *RendererError) ErrorCode() string { return e.code }

func Render(ctx context.Context, opts Options) ([]core.Resource, error) {
	ctx, span := otel.Tracer("justcd/renderer").Start(ctx, "renderer.render")
	defer span.End()
	span.SetAttributes(attribute.String("renderer", opts.Renderer), attribute.String("application.id", opts.ApplicationID))
	if opts.RepositoryRoot == "" || opts.ManifestPath == "" || opts.ApplicationID == "" || opts.ClusterID == "" || opts.Mapper == nil {
		return nil, errors.New("render target and discovery mapper are required")
	}
	manifestPath, err := safeRepositoryPath(opts.RepositoryRoot, opts.ManifestPath)
	if err != nil {
		return nil, err
	}
	var output []byte
	switch opts.Renderer {
	case "yaml":
		output, err = readManifestFiles(manifestPath)
	case "kustomize":
		if len(opts.Namespaces) > 1 && len(opts.NamespaceManifestPaths) > 0 {
			return renderKustomizeNamespaces(ctx, opts)
		}
		if opts.KustomizeNamespaceOverride && len(opts.Namespaces) > 1 {
			return renderKustomizeNamespaces(ctx, opts)
		}
		selectedPath := opts.ManifestPath
		if len(opts.Namespaces) == 1 {
			selectedPath = kustomizePathForNamespace(opts, opts.Namespaces[0].Namespace)
			manifestPath, err = safeRepositoryPath(opts.RepositoryRoot, selectedPath)
			if err != nil {
				return nil, err
			}
		}
		if err := validateKustomizeSources(ctx, manifestPath, opts.RepositoryRoot, opts.KustomizeHelmEnabled); err != nil {
			return nil, err
		}
		buildPath := manifestPath
		if opts.KustomizeNamespaceOverride {
			if len(opts.Namespaces) != 1 {
				return nil, errors.New("Kustomize namespace transform requires exactly one bound namespace in a single render")
			}
			var cleanup func()
			buildPath, cleanup, err = namespaceOverrideOverlay(opts.RepositoryRoot, manifestPath, opts.Namespaces[0].Namespace)
			if err != nil {
				return nil, err
			}
			defer cleanup()
		}
		output, err = runKustomizeRenderer(ctx, buildPath, opts.RepositoryRoot, opts.KustomizeHelmEnabled)
	case "helm":
		return renderHelm(ctx, opts, manifestPath)
	default:
		return nil, fmt.Errorf("unsupported renderer %q", opts.Renderer)
	}
	if err != nil {
		return nil, err
	}
	return parseAndNormalize(output, opts)
}

func renderHelm(ctx context.Context, opts Options, chartPath string) ([]core.Resource, error) {
	if len(opts.Namespaces) == 0 {
		return nil, errors.New("Helm applications require at least one bound namespace")
	}
	resources := make([]core.Resource, 0)
	byIdentity := make(map[string]core.Resource)
	for _, binding := range opts.Namespaces {
		output, err := renderHelmNamespace(ctx, opts, chartPath, binding)
		if err != nil {
			return nil, err
		}
		namespaceOptions := opts
		namespaceOptions.Namespaces = []core.Binding{binding}
		rendered, err := parseAndNormalize(output, namespaceOptions)
		if err != nil {
			return nil, fmt.Errorf("render Helm chart for namespace %q: %w", binding.Namespace, err)
		}
		for _, resource := range rendered {
			key := resource.Identity.Key()
			if previous, exists := byIdentity[key]; exists {
				if previous.Fingerprint != resource.Fingerprint {
					return nil, fmt.Errorf("Helm chart renders different versions of %s %s/%s for different namespaces", resource.Identity.Kind, resource.Identity.Namespace, resource.Identity.Name)
				}
				continue
			}
			byIdentity[key] = resource
			resources = append(resources, resource)
			if len(resources) > maxRenderedResources {
				return nil, errors.New("rendered output exceeds 10000 resources")
			}
		}
	}
	return resources, nil
}

func renderHelmNamespace(ctx context.Context, opts Options, chartPath string, binding core.Binding) ([]byte, error) {
	args := []string{"template", "justcd", chartPath, "--include-crds", "--namespace", binding.Namespace}
	appendValuesFile := func(filename string) error {
		filename = strings.TrimSpace(filename)
		if filename == "" {
			return errors.New("Helm values file path cannot be empty")
		}
		valuesPath, pathErr := safeRepositoryPath(opts.RepositoryRoot, filename)
		if pathErr != nil {
			return fmt.Errorf("Helm values file %q: %w", filename, pathErr)
		}
		info, statErr := os.Stat(valuesPath)
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > maxManifestFileBytes {
			return fmt.Errorf("Helm values file %q must be a regular file no larger than 2 MiB", filename)
		}
		args = append(args, "--values", valuesPath)
		return nil
	}
	valuesDir := ""
	cleanupValues := func() {}
	defer func() { cleanupValues() }()
	appendInlineValues := func(layer string) error {
		layer = strings.TrimSpace(layer)
		if layer == "" {
			return nil
		}
		if len(layer) > maxManifestFileBytes {
			return errors.New("Helm values YAML exceeds 2 MiB")
		}
		var values map[string]any
		if err := yaml.Unmarshal([]byte(layer), &values); err != nil || values == nil {
			return errors.New("Helm values must be a YAML mapping")
		}
		if valuesDir == "" {
			var dirErr error
			valuesDir, dirErr = os.MkdirTemp("", "justcd-helm-values-")
			if dirErr != nil {
				return dirErr
			}
			cleanupValues = func() { _ = os.RemoveAll(valuesDir) }
		}
		valuesPath := filepath.Join(valuesDir, fmt.Sprintf("values-%d.yaml", len(args)))
		if err := os.WriteFile(valuesPath, []byte(layer), 0600); err != nil {
			return err
		}
		args = append(args, "--values", valuesPath)
		return nil
	}
	if err := appendValuesFiles(appendValuesFile, opts.HelmValuesFiles); err != nil {
		return nil, err
	}
	if err := appendInlineValues(opts.HelmValuesYAML); err != nil {
		return nil, err
	}
	if err := appendValuesFiles(appendValuesFile, opts.TargetHelmValuesFiles); err != nil {
		return nil, err
	}
	if err := appendInlineValues(opts.TargetHelmValuesYAML); err != nil {
		return nil, err
	}
	if namespaceValues, ok := opts.NamespaceHelmValues[binding.Namespace]; ok {
		if err := appendValuesFiles(appendValuesFile, namespaceValues.Files); err != nil {
			return nil, err
		}
		if err := appendInlineValues(namespaceValues.YAML); err != nil {
			return nil, err
		}
	}
	return runRenderer(ctx, "helm", args, opts.RepositoryRoot)
}

func appendValuesFiles(appendFile func(string) error, files []string) error {
	for _, filename := range files {
		if err := appendFile(filename); err != nil {
			return err
		}
	}
	return nil
}

// KustomizationNamespace reads only the selected application's overlay. Referenced
// components may have their own namespace transformers, but they do not define
// the top-level namespace that the user can override here.
func KustomizationNamespace(repositoryRoot, manifestPath string) (string, error) {
	root, err := safeRepositoryPath(repositoryRoot, manifestPath)
	if err != nil {
		return "", err
	}
	for _, filename := range []string{"kustomization.yaml", "kustomization.yml", "Kustomization", "kustomization.json"} {
		path := filepath.Join(root, filename)
		if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("application kustomization file must not be a symlink")
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return "", statErr
		}
		content, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if len(content) > maxManifestFileBytes {
			return "", errors.New("kustomization file exceeds 2 MiB")
		}
		var data struct {
			Namespace string `json:"namespace"`
		}
		if err := yaml.Unmarshal(content, &data); err != nil {
			return "", errors.New("invalid kustomization file")
		}
		return strings.TrimSpace(data.Namespace), nil
	}
	return "", errors.New("application kustomization file not found")
}

// A wrapper overlay applies the target namespace to rendered resource metadata.
// Explicit namespace references inside resources remain as defined in Git.
// The Git checkout stays unchanged after rendering.
func namespaceOverrideOverlay(repositoryRoot, manifestPath, namespace string) (string, func(), error) {
	resolvedRoot, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(resolvedRoot, ".justcd-namespace-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	relative, err := filepath.Rel(dir, manifestPath)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	content, err := yaml.Marshal(map[string]any{
		"apiVersion": "kustomize.config.k8s.io/v1beta1",
		"kind":       "Kustomization",
		"namespace":  namespace,
		"resources":  []string{filepath.ToSlash(relative)},
	})
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"), content, 0600); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}

func runKustomizeRenderer(ctx context.Context, manifestPath, repositoryRoot string, enableHelm bool) ([]byte, error) {
	name := "kustomize"
	args := []string{"build", "--load-restrictor", "LoadRestrictionsNone", manifestPath}
	if _, err := exec.LookPath(name); err != nil {
		if _, err := exec.LookPath("kubectl"); err != nil {
			return nil, errors.New("kustomize or kubectl is required to render Kustomize applications")
		}
		name = "kubectl"
		args = []string{"kustomize", "--load-restrictor", "LoadRestrictionsNone", manifestPath}
	}
	if enableHelm {
		if _, err := exec.LookPath("helm"); err != nil {
			return nil, errors.New("helm is required for Kustomize Helm charts")
		}
		args = append(args, "--enable-helm")
	}
	return runRenderer(ctx, name, args, repositoryRoot)
}

func safeRepositoryPath(root, relative string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("repository root cannot be resolved: %w", err)
	}
	requested := filepath.Join(rootAbs, filepath.FromSlash(relative))
	rel, err := filepath.Rel(rootAbs, requested)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("manifestPath escapes the repository")
	}
	resolved, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return "", fmt.Errorf("manifestPath cannot be resolved: %w", err)
	}
	resolvedAbs, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedAbs)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
		return "", errors.New("manifestPath resolves outside the repository")
	}
	return resolvedAbs, nil
}

func readManifestFiles(root string) ([]byte, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	files := []string{}
	if !info.IsDir() {
		ext := strings.ToLower(filepath.Ext(root))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil, errors.New("manifest file must end in .yaml, .yml, or .json")
		}
		files = append(files, root)
	} else {
		err = filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.Type()&os.ModeSymlink != 0 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext == ".yaml" || ext == ".yml" || ext == ".json" {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	if len(files) > maxRenderedResources {
		return nil, errors.New("manifest contains too many files")
	}
	var output bytes.Buffer
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			return nil, err
		}
		if info.Size() > maxManifestFileBytes {
			return nil, fmt.Errorf("manifest file %q exceeds 2 MiB", filepath.Base(file))
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if output.Len()+len(content)+4 > maxRenderOutputBytes {
			return nil, errors.New("manifest output exceeds configured size limit")
		}
		output.Write(content)
		output.WriteString("\n---\n")
	}
	return output.Bytes(), nil
}

type cappedBuffer struct {
	bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("renderer output exceeds configured size limit")
	}
	return b.Buffer.Write(p)
}

func runRenderer(parent context.Context, name string, args []string, repositoryRoot string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	home, err := os.MkdirTemp("", "justcd-render-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(home)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = repositoryRoot
	cmd.Env = cleanEnvironment(os.Environ(), home)
	stdout := &cappedBuffer{max: maxRenderOutputBytes}
	stderr := &cappedBuffer{max: 1 << 20}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, &RendererError{renderer: name, code: "render.timeout", message: fmt.Sprintf("%s renderer timed out", name), retryable: true}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		output := strings.ToLower(stderr.String())
		transient := false
		for _, marker := range []string{"connection timed out", "connection reset", "connection refused", "connection aborted", "network is unreachable", "temporary failure", "temporarily unavailable", "could not resolve host", "tls handshake timeout", "i/o timeout", "service unavailable", "unexpected eof", "returned error: 502", "returned error: 503", "returned error: 504"} {
			if strings.Contains(output, marker) {
				transient = true
				break
			}
		}
		return nil, &RendererError{renderer: name, code: "render.command_failed", message: fmt.Sprintf("%s renderer failed", name), retryable: transient}
	}
	return stdout.Bytes(), nil
}

func cleanEnvironment(in []string, home string) []string {
	allowed := map[string]bool{"PATH": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true, "SYSTEMROOT": true, "WINDIR": true, "HTTPS_PROXY": true, "HTTP_PROXY": true, "NO_PROXY": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true}
	out := make([]string, 0, len(allowed)+12)
	for _, pair := range in {
		key, _, ok := strings.Cut(pair, "=")
		if !ok || !allowed[strings.ToUpper(key)] {
			continue
		}
		out = append(out, pair)
	}
	return append(out, "HOME="+home, "TMPDIR="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "HELM_CONFIG_HOME="+filepath.Join(home, ".config", "helm"), "HELM_CACHE_HOME="+filepath.Join(home, ".cache", "helm"), "HELM_DATA_HOME="+filepath.Join(home, ".local", "share", "helm"), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
}

func validateKustomizeSources(ctx context.Context, root, repositoryRoot string, enableHelm bool) error {
	repositoryRoot, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return fmt.Errorf("repository root cannot be resolved: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("kustomization root cannot be resolved: %w", err)
	}
	rootRelative, err := filepath.Rel(repositoryRoot, root)
	if err != nil || rootRelative == ".." || strings.HasPrefix(rootRelative, ".."+string(filepath.Separator)) {
		return errors.New("kustomization is outside repository root")
	}
	pending := []string{root}
	visited := map[string]bool{}
	for len(pending) > 0 {
		current := pending[0]
		pending = pending[1:]
		if visited[current] {
			continue
		}
		visited[current] = true
		err := filepath.WalkDir(current, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if d.Type()&os.ModeSymlink != 0 {
				resolved, err := filepath.EvalSymlinks(p)
				if err != nil || !withinRepository(repositoryRoot, resolved) {
					return errors.New("Kustomize symlink escapes the repository")
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			name := strings.ToLower(d.Name())
			if name != "kustomization.yaml" && name != "kustomization.yml" && name != "kustomization.json" {
				return nil
			}
			content, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if len(content) > maxManifestFileBytes {
				return errors.New("kustomization file exceeds 2 MiB")
			}
			var data map[string]any
			if err := yaml.Unmarshal(content, &data); err != nil {
				return errors.New("invalid kustomization file")
			}
			base := filepath.Dir(p)
			if globals, ok := data["helmGlobals"].(map[string]any); ok {
				for _, field := range []string{"chartHome", "configHome"} {
					if value, ok := globals[field].(string); ok && value != "" {
						if err := validateKustomizePath(repositoryRoot, base, value); err != nil {
							return fmt.Errorf("helmGlobals %s: %w", field, err)
						}
					}
				}
			}
			for _, field := range []string{"resources", "bases", "components", "crds", "configurations", "transformers", "generators", "validators", "patchesStrategicMerge"} {
				if raw, ok := data[field]; ok {
					list, ok := raw.([]any)
					if !ok {
						return fmt.Errorf("kustomize %s must be a list", field)
					}
					for _, item := range list {
						value, ok := item.(string)
						if !ok {
							return fmt.Errorf("kustomize %s entries must be paths", field)
						}
						if err := validateKustomizePath(repositoryRoot, base, value); err != nil {
							return fmt.Errorf("kustomize %s: %w", field, err)
						}
						if field == "resources" || field == "bases" || field == "components" || field == "transformers" || field == "generators" || field == "validators" {
							candidate, err := filepath.EvalSymlinks(filepath.Join(base, value))
							if err == nil {
								if info, err := os.Stat(candidate); err == nil && info.IsDir() {
									pending = append(pending, candidate)
								}
							}
						}
					}
				}
			}
			for _, field := range []string{"patches", "patchesJson6902"} {
				if entries, ok := data[field].([]any); ok {
					for _, raw := range entries {
						if entry, ok := raw.(map[string]any); ok {
							if value, ok := entry["path"].(string); ok && value != "" {
								if err := validateKustomizePath(repositoryRoot, base, value); err != nil {
									return fmt.Errorf("kustomize %s: %w", field, err)
								}
							}
						}
					}
				}
			}
			if openAPI, ok := data["openapi"].(map[string]any); ok {
				if value, ok := openAPI["path"].(string); ok && value != "" {
					if err := validateKustomizePath(repositoryRoot, base, value); err != nil {
						return fmt.Errorf("kustomize openapi: %w", err)
					}
				}
			}
			for _, field := range []string{"configMapGenerator", "secretGenerator"} {
				if entries, ok := data[field].([]any); ok {
					for _, raw := range entries {
						entry, ok := raw.(map[string]any)
						if !ok {
							continue
						}
						for _, sourceField := range []string{"files", "envs"} {
							if sources, ok := entry[sourceField].([]any); ok {
								for _, source := range sources {
									value, ok := source.(string)
									if !ok {
										return fmt.Errorf("kustomize %s %s entries must be paths", field, sourceField)
									}
									if sourceField == "files" {
										if _, path, named := strings.Cut(value, "="); named {
											value = path
										}
									}
									if err := validateKustomizePath(repositoryRoot, base, value); err != nil {
										return fmt.Errorf("kustomize %s: %w", field, err)
									}
								}
							}
						}
					}
				}
			}
			if raw, ok := data["helmCharts"]; ok {
				if !enableHelm {
					return errors.New("Kustomize Helm charts are disabled for this application; enable them in Application source")
				}
				charts, ok := raw.([]any)
				if !ok || len(charts) > 32 {
					return errors.New("helmCharts must be a list of at most 32 charts")
				}
				for _, rawChart := range charts {
					chart, ok := rawChart.(map[string]any)
					if !ok {
						return errors.New("helmCharts entries must be objects")
					}
					name, _ := chart["name"].(string)
					if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
						return errors.New("helmCharts chart name must be a single directory name")
					}
					if repo, _ := chart["repo"].(string); repo != "" {
						if version, _ := chart["version"].(string); strings.TrimSpace(version) == "" {
							return errors.New("remote helmCharts require a pinned version")
						}
						if err := validateHelmRepository(ctx, repo); err != nil {
							return err
						}
					}
					for _, field := range []string{"valuesFile", "chartHome"} {
						if value, ok := chart[field].(string); ok && value != "" {
							if err := validateKustomizePath(repositoryRoot, base, value); err != nil {
								return fmt.Errorf("helmCharts %s: %w", field, err)
							}
						}
					}
					if values, ok := chart["additionalValuesFiles"].([]any); ok {
						for _, rawValue := range values {
							value, ok := rawValue.(string)
							if !ok {
								return errors.New("helmCharts additionalValuesFiles entries must be paths")
							}
							if err := validateKustomizePath(repositoryRoot, base, value); err != nil {
								return fmt.Errorf("helmCharts additionalValuesFiles: %w", err)
							}
						}
					}
				}
			}
			rel, err := filepath.Rel(repositoryRoot, p)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return errors.New("kustomization is outside repository root")
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func validateHelmRepository(parent context.Context, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Port() != "" && parsed.Port() != "443") {
		return errors.New("helmCharts repo must be a public HTTPS URL without credentials")
	}
	host := strings.ToLower(parsed.Hostname())
	if net.ParseIP(host) != nil || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return errors.New("helmCharts repo must use a public DNS hostname")
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return errors.New("helmCharts repo hostname could not be resolved")
	}
	for _, address := range addresses {
		if !address.IsGlobalUnicast() || address.IsPrivate() {
			return errors.New("helmCharts repo must resolve only to public addresses")
		}
	}
	return nil
}

func validateKustomizePath(repositoryRoot, base, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, "://") || strings.Contains(value, "\\") || strings.HasPrefix(value, "git::") || strings.HasPrefix(value, "github.com/") {
		return errors.New("only local repository-relative paths are supported")
	}
	target := filepath.Join(base, filepath.FromSlash(value))
	if !withinRepository(repositoryRoot, target) {
		return errors.New("path escapes the repository")
	}
	// Helm can create chartHome during rendering, so check the nearest existing
	// ancestor as well as the lexical path. This catches symlinked parents.
	ancestor := target
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil || !withinRepository(repositoryRoot, resolved) {
				return errors.New("path resolves outside the repository")
			}
			break
		} else if !os.IsNotExist(err) {
			return err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return errors.New("path cannot be resolved")
		}
		ancestor = parent
	}
	return nil
}

func withinRepository(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func parseAndNormalize(output []byte, opts Options) ([]core.Resource, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
	allowedNamespaces := map[string]bool{}
	for _, binding := range opts.Namespaces {
		allowedNamespaces[binding.Namespace] = true
	}
	resources := make([]core.Resource, 0)
	for index := 0; ; index++ {
		var object map[string]interface{}
		err := decoder.Decode(&object)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode rendered document %d: %w", index+1, err)
		}
		if len(object) == 0 {
			continue
		}
		if len(resources) >= maxRenderedResources {
			return nil, errors.New("rendered output exceeds 10000 resources")
		}
		resource := unstructured.Unstructured{Object: object}
		apiVersion := resource.GetAPIVersion()
		kind := resource.GetKind()
		name := resource.GetName()
		if apiVersion == "" || kind == "" || name == "" {
			return nil, fmt.Errorf("rendered document %d needs apiVersion, kind, and metadata.name", index+1)
		}
		groupVersion, err := schema.ParseGroupVersion(apiVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid apiVersion in rendered document %d", index+1)
		}
		candidateNamespace := resource.GetNamespace()
		if candidateNamespace == "" && len(allowedNamespaces) == 1 {
			for only := range allowedNamespaces {
				candidateNamespace = only
			}
		}
		candidate := core.Identity{ClusterID: opts.ClusterID, APIVersion: apiVersion, Kind: kind, Namespace: candidateNamespace, Name: name, ClusterScoped: candidateNamespace == ""}
		ignored := false
		for _, exact := range opts.IgnoredResources {
			// Discovery may be unavailable for the ignored kind, so check both
			// the declared scope and the single-namespace default before mapping.
			raw := candidate
			raw.Namespace = resource.GetNamespace()
			raw.ClusterScoped = raw.Namespace == ""
			if exact.Key() == candidate.Key() || exact.Key() == raw.Key() {
				ignored = true
				break
			}
		}
		if !ignored {
			for _, selector := range opts.IgnoredSelectors {
				if selector.Matches(candidate, resource.GetLabels()) {
					ignored = true
					break
				}
			}
		}
		if ignored {
			continue
		}
		mapping, err := opts.Mapper.RESTMapping(schema.GroupKind{Group: groupVersion.Group, Kind: kind}, groupVersion.Version)
		if err != nil {
			return nil, fmt.Errorf("Kubernetes API does not recognize %s %s: %w", apiVersion, kind, err)
		}
		clusterScoped := mapping.Scope.Name() != meta.RESTScopeNameNamespace
		if clusterScoped && resource.GetNamespace() != "" {
			return nil, fmt.Errorf("cluster-scoped resource %s/%s must not declare a namespace", kind, name)
		}
		if !clusterScoped {
			namespace := resource.GetNamespace()
			if namespace == "" {
				if len(allowedNamespaces) != 1 {
					return nil, fmt.Errorf("resource %s/%s must declare a namespace when the application has multiple namespace bindings", kind, name)
				}
				for only := range allowedNamespaces {
					namespace = only
				}
				resource.SetNamespace(namespace)
			}
			if !allowedNamespaces[namespace] {
				return nil, fmt.Errorf("resource %s/%s targets unbound namespace %q", kind, name, namespace)
			}
		}
		metadata, found, err := unstructured.NestedMap(resource.Object, "metadata")
		if err != nil || !found {
			return nil, fmt.Errorf("rendered document %d has invalid metadata", index+1)
		}
		stripServerMetadata(metadata)
		if err := unstructured.SetNestedMap(resource.Object, metadata, "metadata"); err != nil {
			return nil, err
		}
		delete(resource.Object, "status")
		labels := resource.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels["justcd.io/application-id"] = opts.ApplicationID
		resource.SetLabels(labels)
		canonical, err := json.Marshal(resource.Object)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(canonical)
		resources = append(resources, core.Resource{Identity: core.Identity{ClusterID: opts.ClusterID, APIVersion: apiVersion, Kind: kind, Namespace: resource.GetNamespace(), Name: name, ClusterScoped: clusterScoped}, Fingerprint: hex.EncodeToString(sum[:]), Manifest: canonical})
	}
	return resources, nil
}

func CanonicalLive(object *unstructured.Unstructured, clusterID, applicationID string, clusterScoped bool) (core.Resource, error) {
	copy := object.DeepCopy()
	metadata, found, err := unstructured.NestedMap(copy.Object, "metadata")
	if err != nil || !found {
		return core.Resource{}, errors.New("live object has invalid metadata")
	}
	stripServerMetadata(metadata)
	if err := unstructured.SetNestedMap(copy.Object, metadata, "metadata"); err != nil {
		return core.Resource{}, err
	}
	delete(copy.Object, "status")
	canonical, err := json.Marshal(copy.Object)
	if err != nil {
		return core.Resource{}, err
	}
	sum := sha256.Sum256(canonical)
	identity := core.Identity{ClusterID: clusterID, APIVersion: copy.GetAPIVersion(), Kind: copy.GetKind(), Namespace: copy.GetNamespace(), Name: copy.GetName(), ClusterScoped: clusterScoped}
	return core.Resource{Identity: identity, Fingerprint: hex.EncodeToString(sum[:]), UID: string(object.GetUID()), ResourceVersion: object.GetResourceVersion(), Owner: object.GetLabels()["justcd.io/application-id"], Manifest: canonical}, nil
}

// CanonicalLiveAgainst compares only fields declared in the current or last
// applied manifest. Kubernetes defaults and fields owned by other controllers
// therefore do not create permanent drift, while fields removed from Git stay
// in the comparison set until JustCD has pruned them.
func CanonicalLiveAgainst(object *unstructured.Unstructured, clusterID, applicationID string, clusterScoped bool, desiredManifest, previousManifest []byte) (core.Resource, string, error) {
	resource, desiredFingerprint, _, err := CanonicalLiveAgainstIgnoring(object, clusterID, applicationID, clusterScoped, desiredManifest, previousManifest, nil)
	return resource, desiredFingerprint, err
}

func CanonicalLiveAgainstIgnoring(object *unstructured.Unstructured, clusterID, applicationID string, clusterScoped bool, desiredManifest, previousManifest []byte, ignoredPaths []string, reportPathSets ...[]string) (core.Resource, string, []string, error) {
	live := object.DeepCopy()
	metadata, found, err := unstructured.NestedMap(live.Object, "metadata")
	if err != nil || !found {
		return core.Resource{}, "", nil, errors.New("live object has invalid metadata")
	}
	stripServerMetadata(metadata)
	if err := unstructured.SetNestedMap(live.Object, metadata, "metadata"); err != nil {
		return core.Resource{}, "", nil, err
	}
	delete(live.Object, "status")
	var desired map[string]interface{}
	if err := json.Unmarshal(desiredManifest, &desired); err != nil {
		return core.Resource{}, "", nil, fmt.Errorf("decode desired manifest for comparison: %w", err)
	}
	var previous map[string]interface{}
	if len(previousManifest) > 0 {
		if err := json.Unmarshal(previousManifest, &previous); err != nil {
			return core.Resource{}, "", nil, fmt.Errorf("decode stored manifest for comparison: %w", err)
		}
	}
	ignoredSegments := make([][]string, 0, len(ignoredPaths))
	reportPaths := ignoredPaths
	if len(reportPathSets) > 0 {
		reportPaths = reportPathSets[0]
	}
	reportSet := make(map[string]bool, len(reportPaths))
	for _, pointer := range reportPaths {
		reportSet[pointer] = true
	}
	changedIgnored := make([]string, 0, len(reportPaths))
	for _, pointer := range ignoredPaths {
		segments, err := ParseJSONPointer(pointer)
		if err != nil {
			return core.Resource{}, "", nil, err
		}
		ignoredSegments = append(ignoredSegments, segments)
		if !reportSet[pointer] {
			continue
		}
		actual, actualExists := lookupPath(live.Object, segments)
		want, wantExists := lookupPath(desired, segments)
		if !wantExists {
			// A selected path may be a Git deletion. Keep that desired-vs-live
			// difference visible as excluded instead of comparing only against
			// the last applied value and accidentally hiding the change.
			if actualExists {
				changedIgnored = append(changedIgnored, pointer)
			}
			continue
		}
		if wantExists && (!actualExists || !reflect.DeepEqual(want, actual)) {
			changedIgnored = append(changedIgnored, pointer)
		}
	}
	paths := make([][]string, 0, 64)
	collectManagedPaths(desired, nil, &paths)
	collectManagedPaths(previous, nil, &paths)
	seen := map[string]bool{}
	unique := make([][]string, 0, len(paths))
	for _, path := range paths {
		ignored := false
		for _, excluded := range ignoredSegments {
			if hasPathPrefix(path, excluded) {
				ignored = true
				break
			}
		}
		if ignored {
			continue
		}
		key := strings.Join(path, "\x00")
		if !seen[key] {
			seen[key] = true
			unique = append(unique, path)
		}
	}
	projectedDesired := projectFields(desired, unique)
	projectedLive := projectFields(live.Object, unique)
	projectListItems(projectedDesired, projectedLive, previous)
	normalizeEmptyEquivalents(projectedDesired, projectedLive)
	desiredJSON, err := json.Marshal(projectedDesired)
	if err != nil {
		return core.Resource{}, "", nil, err
	}
	liveProjectionJSON, err := json.Marshal(projectedLive)
	if err != nil {
		return core.Resource{}, "", nil, err
	}
	canonical, err := json.Marshal(live.Object)
	if err != nil {
		return core.Resource{}, "", nil, err
	}
	identity := core.Identity{ClusterID: clusterID, APIVersion: live.GetAPIVersion(), Kind: live.GetKind(), Namespace: live.GetNamespace(), Name: live.GetName(), ClusterScoped: clusterScoped}
	resource := core.Resource{Identity: identity, Fingerprint: fingerprint(liveProjectionJSON), UID: string(object.GetUID()), ResourceVersion: object.GetResourceVersion(), Owner: object.GetLabels()["justcd.io/application-id"], Manifest: canonical}
	return resource, fingerprint(desiredJSON), changedIgnored, nil
}

// ComparisonManifests returns the same Git-owned field projection used for
// drift checks. These are review documents only; the original desired manifest
// must be retained for server-side apply.
func ComparisonManifests(liveManifest, desiredManifest, previousManifest []byte) ([]byte, []byte, error) {
	var live, desired, previous map[string]interface{}
	if err := json.Unmarshal(liveManifest, &live); err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(desiredManifest, &desired); err != nil {
		return nil, nil, err
	}
	if len(previousManifest) > 0 {
		if err := json.Unmarshal(previousManifest, &previous); err != nil {
			return nil, nil, err
		}
	}
	paths := make([][]string, 0, 64)
	collectManagedPaths(desired, nil, &paths)
	collectManagedPaths(previous, nil, &paths)
	projectedDesired := projectFields(desired, paths)
	projectedLive := projectFields(live, paths)
	projectListItems(projectedDesired, projectedLive, previous)
	normalizeEmptyEquivalents(projectedDesired, projectedLive)
	before, err := json.Marshal(projectedLive)
	if err != nil {
		return nil, nil, err
	}
	after, err := json.Marshal(projectedDesired)
	return before, after, err
}

func projectListItems(desired, live map[string]interface{}, previous map[string]interface{}) {
	for key, value := range live {
		want, wantExists := desired[key]
		prior := previous[key]
		switch current := value.(type) {
		case map[string]interface{}:
			wantMap, _ := want.(map[string]interface{})
			priorMap, _ := prior.(map[string]interface{})
			projectListItems(wantMap, current, priorMap)
		case []interface{}:
			wantList, _ := want.([]interface{})
			priorList, _ := prior.([]interface{})
			if !wantExists && len(priorList) == 0 {
				continue
			}
			live[key] = projectList(current, wantList, priorList)
		}
	}
}

func kustomizePathForNamespace(opts Options, namespace string) string {
	if target := strings.TrimSpace(opts.NamespaceManifestPaths[namespace]); target != "" {
		return target
	}
	if target := strings.TrimSpace(opts.TargetManifestPath); target != "" {
		return target
	}
	return opts.ManifestPath
}

func renderKustomizeNamespaces(ctx context.Context, opts Options) ([]core.Resource, error) {
	if len(opts.Namespaces) == 0 {
		return nil, errors.New("Kustomize applications require at least one bound namespace")
	}
	resources := make([]core.Resource, 0)
	byIdentity := make(map[string]core.Resource)
	for _, binding := range opts.Namespaces {
		selectedPath := kustomizePathForNamespace(opts, binding.Namespace)
		manifestPath, err := safeRepositoryPath(opts.RepositoryRoot, selectedPath)
		if err != nil {
			return nil, fmt.Errorf("Kustomize path for namespace %q: %w", binding.Namespace, err)
		}
		if err := validateKustomizeSources(ctx, manifestPath, opts.RepositoryRoot, opts.KustomizeHelmEnabled); err != nil {
			return nil, fmt.Errorf("Kustomize path for namespace %q: %w", binding.Namespace, err)
		}
		buildPath := manifestPath
		if opts.KustomizeNamespaceOverride {
			var cleanup func()
			buildPath, cleanup, err = namespaceOverrideOverlay(opts.RepositoryRoot, manifestPath, binding.Namespace)
			if err != nil {
				return nil, err
			}
			output, renderErr := runKustomizeRenderer(ctx, buildPath, opts.RepositoryRoot, opts.KustomizeHelmEnabled)
			cleanup()
			if renderErr != nil {
				return nil, fmt.Errorf("render Kustomize overlay for namespace %q: %w", binding.Namespace, renderErr)
			}
			namespaceOptions := opts
			namespaceOptions.Namespaces = []core.Binding{binding}
			rendered, parseErr := parseAndNormalize(output, namespaceOptions)
			if parseErr != nil {
				return nil, fmt.Errorf("render Kustomize overlay for namespace %q: %w", binding.Namespace, parseErr)
			}
			if err := appendNamespaceRender(&resources, byIdentity, rendered); err != nil {
				return nil, err
			}
			continue
		}
		output, err := runKustomizeRenderer(ctx, buildPath, opts.RepositoryRoot, opts.KustomizeHelmEnabled)
		if err != nil {
			return nil, fmt.Errorf("render Kustomize overlay for namespace %q: %w", binding.Namespace, err)
		}
		namespaceOptions := opts
		namespaceOptions.Namespaces = []core.Binding{binding}
		rendered, err := parseAndNormalize(output, namespaceOptions)
		if err != nil {
			return nil, fmt.Errorf("render Kustomize overlay for namespace %q: %w", binding.Namespace, err)
		}
		if err := appendNamespaceRender(&resources, byIdentity, rendered); err != nil {
			return nil, err
		}
	}
	return resources, nil
}

func appendNamespaceRender(resources *[]core.Resource, byIdentity map[string]core.Resource, rendered []core.Resource) error {
	for _, resource := range rendered {
		key := resource.Identity.Key()
		if previous, exists := byIdentity[key]; exists {
			if previous.Fingerprint != resource.Fingerprint {
				return fmt.Errorf("Kustomize renders different versions of cluster resource %s/%s for different namespaces", resource.Identity.Kind, resource.Identity.Name)
			}
			continue
		}
		byIdentity[key] = resource
		*resources = append(*resources, resource)
		if len(*resources) > maxRenderedResources {
			return errors.New("rendered output exceeds 10000 resources")
		}
	}
	return nil
}

// Kubernetes commonly omits explicitly empty optional maps and lists from
// live objects. An absent value and an empty value are equivalent only when
// the other side is absent; a non-empty live list versus an empty desired list
// remains a real deletion diff.
func normalizeEmptyEquivalents(desired, live map[string]interface{}) {
	for key, wanted := range desired {
		actual, exists := live[key]
		if !exists {
			if isEmptyCollection(wanted) {
				delete(desired, key)
			}
			continue
		}
		wantMap, wantIsMap := wanted.(map[string]interface{})
		liveMap, liveIsMap := actual.(map[string]interface{})
		if wantIsMap && liveIsMap {
			normalizeEmptyEquivalents(wantMap, liveMap)
			if len(wantMap) == 0 && len(liveMap) == 0 {
				delete(desired, key)
				delete(live, key)
			}
		}
	}
	for key, actual := range live {
		if _, exists := desired[key]; !exists && isEmptyCollection(actual) {
			delete(live, key)
		}
	}
}

func isEmptyCollection(value interface{}) bool {
	switch typed := value.(type) {
	case map[string]interface{}:
		return len(typed) == 0
	case []interface{}:
		return len(typed) == 0
	default:
		return false
	}
}

func projectList(live, desired, previous []interface{}) []interface{} {
	if len(desired) == 0 && len(previous) == 0 {
		return live
	}
	result := make([]interface{}, 0, len(live))
	for index, item := range live {
		itemMap, isMap := item.(map[string]interface{})
		if !isMap {
			result = append(result, item)
			continue
		}
		var shape map[string]interface{}
		if name, ok := itemMap["name"].(string); ok {
			shape = mergeListItemShape(desired, previous, name)
		} else if index < len(desired) || index < len(previous) {
			shape = map[string]interface{}{}
			if index < len(previous) {
				mergeShape(shape, previous[index])
			}
			if index < len(desired) {
				mergeShape(shape, desired[index])
			}
		}
		if shape == nil {
			continue
		}
		projected := projectByShape(itemMap, shape)
		result = append(result, projected)
	}
	return result
}

func mergeListItemShape(desired, previous []interface{}, name string) map[string]interface{} {
	shape := map[string]interface{}{}
	for _, list := range [][]interface{}{previous, desired} {
		for _, item := range list {
			entry, ok := item.(map[string]interface{})
			if ok && entry["name"] == name {
				mergeShape(shape, entry)
			}
		}
	}
	if len(shape) == 0 {
		return nil
	}
	return shape
}

func mergeShape(target map[string]interface{}, value interface{}) {
	source, ok := value.(map[string]interface{})
	if !ok {
		return
	}
	for key, field := range source {
		if nested, ok := field.(map[string]interface{}); ok {
			current, _ := target[key].(map[string]interface{})
			if current == nil {
				current = map[string]interface{}{}
				target[key] = current
			}
			mergeShape(current, nested)
		} else {
			target[key] = field
		}
	}
}

func projectByShape(source, shape map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{}
	for key, shapeValue := range shape {
		value, exists := source[key]
		if !exists {
			continue
		}
		switch nested := shapeValue.(type) {
		case map[string]interface{}:
			if sourceMap, ok := value.(map[string]interface{}); ok {
				result[key] = projectByShape(sourceMap, nested)
			}
		case []interface{}:
			if sourceList, ok := value.([]interface{}); ok {
				result[key] = projectList(sourceList, nested, nil)
			}
		default:
			result[key] = value
		}
	}
	return result
}

func ParseJSONPointer(pointer string) ([]string, error) {
	if !strings.HasPrefix(pointer, "/") || len(pointer) > 1024 {
		return nil, errors.New("field path must be a JSON Pointer beginning with /")
	}
	raw := strings.Split(pointer[1:], "/")
	segments := make([]string, len(raw))
	for i, segment := range raw {
		var decoded strings.Builder
		for j := 0; j < len(segment); j++ {
			if segment[j] != '~' {
				decoded.WriteByte(segment[j])
				continue
			}
			if j+1 >= len(segment) || (segment[j+1] != '0' && segment[j+1] != '1') {
				return nil, errors.New("field path contains an invalid JSON Pointer escape")
			}
			j++
			if segment[j] == '0' {
				decoded.WriteByte('~')
			} else {
				decoded.WriteByte('/')
			}
		}
		segments[i] = decoded.String()
		if segments[i] == "" || isArrayIndex(segments[i]) {
			return nil, errors.New("field paths must name object fields; array indexes are not supported")
		}
	}
	if len(segments) == 0 || segments[0] == "apiVersion" || segments[0] == "kind" {
		return nil, errors.New("API version and kind cannot be ignored")
	}
	if segments[0] == "metadata" {
		if len(segments) == 1 || (len(segments) > 1 && (segments[1] == "name" || segments[1] == "namespace" || segments[1] == "uid" || segments[1] == "managedFields")) {
			return nil, errors.New("resource identity metadata cannot be ignored")
		}
		if len(segments) <= 3 && (segments[1] == "labels" || segments[1] == "annotations") {
			return nil, errors.New("metadata maps cannot be ignored as a whole")
		}
		if len(segments) >= 3 && segments[1] == "labels" && segments[2] == "justcd.io/application-id" {
			return nil, errors.New("the JustCD ownership label cannot be ignored")
		}
	}
	return segments, nil
}

func RemoveJSONPointer(manifest []byte, pointer string) ([]byte, error) {
	segments, err := ParseJSONPointer(pointer)
	if err != nil {
		return nil, err
	}
	var object map[string]interface{}
	if err := json.Unmarshal(manifest, &object); err != nil {
		return nil, err
	}
	current := object
	for _, segment := range segments[:len(segments)-1] {
		next, ok := current[segment].(map[string]interface{})
		if !ok {
			return manifest, nil
		}
		current = next
	}
	delete(current, segments[len(segments)-1])
	return json.Marshal(object)
}

func JSONPointerExists(manifest []byte, pointer string) bool {
	segments, err := ParseJSONPointer(pointer)
	if err != nil {
		return false
	}
	var object map[string]interface{}
	if json.Unmarshal(manifest, &object) != nil {
		return false
	}
	_, exists := lookupPath(object, segments)
	return exists
}

func CanonicalJSONPointer(pointer string) (string, error) {
	segments, err := ParseJSONPointer(pointer)
	if err != nil {
		return "", err
	}
	for i, segment := range segments {
		segment = strings.ReplaceAll(segment, "~", "~0")
		segments[i] = strings.ReplaceAll(segment, "/", "~1")
	}
	return "/" + strings.Join(segments, "/"), nil
}

// ChangedJSONPointers returns the leaf paths whose values differ. Arrays are
// represented by their containing field so callers cannot target list indexes.
func ChangedJSONPointers(before, after []byte) ([]string, error) {
	var left, right map[string]interface{}
	if len(before) > 0 {
		if err := json.Unmarshal(before, &left); err != nil {
			return nil, err
		}
	}
	if len(after) > 0 {
		if err := json.Unmarshal(after, &right); err != nil {
			return nil, err
		}
	}
	if left == nil {
		left = map[string]interface{}{}
	}
	if right == nil {
		right = map[string]interface{}{}
	}
	paths := make([]string, 0)
	var visit func(any, any, []string)
	visit = func(a, b any, prefix []string) {
		am, aMap := a.(map[string]interface{})
		bm, bMap := b.(map[string]interface{})
		if aMap && bMap {
			keys := map[string]bool{}
			for key := range am {
				keys[key] = true
			}
			for key := range bm {
				keys[key] = true
			}
			ordered := make([]string, 0, len(keys))
			for key := range keys {
				ordered = append(ordered, key)
			}
			sort.Strings(ordered)
			if len(ordered) == 0 && !reflect.DeepEqual(a, b) && len(prefix) > 0 {
				paths = append(paths, pointerFor(prefix))
				return
			}
			for _, key := range ordered {
				av, aok := am[key]
				bv, bok := bm[key]
				if !aok {
					visit(nil, bv, append(append([]string(nil), prefix...), key))
					continue
				}
				if !bok {
					visit(av, nil, append(append([]string(nil), prefix...), key))
					continue
				}
				visit(av, bv, append(append([]string(nil), prefix...), key))
			}
			return
		}
		if !reflect.DeepEqual(a, b) && len(prefix) > 0 {
			paths = append(paths, pointerFor(prefix))
		}
	}
	visit(left, right, nil)
	sort.Strings(paths)
	return paths, nil
}

func pointerFor(segments []string) string {
	encoded := make([]string, len(segments))
	for i, segment := range segments {
		segment = strings.ReplaceAll(segment, "~", "~0")
		encoded[i] = strings.ReplaceAll(segment, "/", "~1")
	}
	return "/" + strings.Join(encoded, "/")
}

func HasOtherManagerFieldOwnership(object *unstructured.Unstructured, applicationID, pointer string) bool {
	segments, err := ParseJSONPointer(pointer)
	if err != nil {
		return false
	}
	value, exists := lookupPath(object.Object, segments)
	if !exists {
		return false
	}
	paths := make([][]string, 0)
	if mapping, ok := value.(map[string]interface{}); ok {
		collectObjectLeafPaths(mapping, segments, &paths)
	} else {
		// Lists are selectable only as whole fields. A structured-list owner
		// for one item is not enough to safely hand off the list as a whole.
		paths = append(paths, segments)
	}
	if len(paths) == 0 {
		paths = append(paths, segments)
	}
	justCDManager := "justcd/" + applicationID
	otherManagers := make([]map[string]json.RawMessage, 0)
	for _, entry := range object.GetManagedFields() {
		if entry.Manager == justCDManager || entry.FieldsV1 == nil {
			continue
		}
		var tree map[string]json.RawMessage
		if json.Unmarshal(entry.FieldsV1.Raw, &tree) != nil {
			continue
		}
		otherManagers = append(otherManagers, tree)
	}
	for _, path := range paths {
		owned := false
		for _, tree := range otherManagers {
			if ownsWholePath(tree, path) {
				owned = true
				break
			}
		}
		if !owned {
			return false
		}
	}
	return true
}

func collectObjectLeafPaths(value map[string]interface{}, prefix []string, paths *[][]string) {
	if len(value) == 0 {
		*paths = append(*paths, append([]string(nil), prefix...))
		return
	}
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		next := append(append([]string(nil), prefix...), key)
		if nested, ok := value[key].(map[string]interface{}); ok {
			collectObjectLeafPaths(nested, next, paths)
			continue
		}
		*paths = append(*paths, next)
	}
}

func ownsWholePath(tree map[string]json.RawMessage, segments []string) bool {
	for _, segment := range segments {
		if len(tree) == 0 {
			return true
		}
		if _, whole := tree["."]; whole {
			return true
		}
		raw, ok := tree["f:"+segment]
		if !ok {
			return false
		}
		tree = nil
		if json.Unmarshal(raw, &tree) != nil {
			return false
		}
	}
	// A non-empty child tree records ownership of only some descendants, not the
	// whole selected field. Treating that as a handoff could relinquish fields
	// that are still exclusively managed by JustCD.
	_, atomic := tree["."]
	return len(tree) == 0 || atomic
}

func isArrayIndex(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func hasPathPrefix(path, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	for i := range prefix {
		if path[i] != prefix[i] {
			return false
		}
	}
	return true
}

func stripServerMetadata(metadata map[string]interface{}) {
	for _, field := range []string{"uid", "resourceVersion", "generation", "creationTimestamp", "managedFields", "selfLink"} {
		delete(metadata, field)
	}
}
func fingerprint(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func collectManagedPaths(value interface{}, prefix []string, out *[][]string) {
	object, ok := value.(map[string]interface{})
	if !ok {
		if len(prefix) > 0 {
			*out = append(*out, append([]string(nil), prefix...))
		}
		return
	}
	if len(object) == 0 {
		return
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		if len(prefix) == 0 && key == "status" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		collectManagedPaths(object[key], append(prefix, key), out)
	}
}
func projectFields(source map[string]interface{}, paths [][]string) map[string]interface{} {
	projected := map[string]interface{}{}
	for _, fieldPath := range paths {
		value, ok := lookupPath(source, fieldPath)
		if ok {
			setPath(projected, fieldPath, value)
		}
	}
	return projected
}
func lookupPath(source map[string]interface{}, fieldPath []string) (interface{}, bool) {
	var current interface{} = source
	for _, part := range fieldPath {
		object, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
func setPath(target map[string]interface{}, fieldPath []string, value interface{}) {
	current := target
	for _, part := range fieldPath[:len(fieldPath)-1] {
		next, ok := current[part].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			current[part] = next
		}
		current = next
	}
	current[fieldPath[len(fieldPath)-1]] = value
}

func NormalizedObject(resource core.Resource) (*unstructured.Unstructured, error) {
	var object map[string]interface{}
	if err := json.Unmarshal(resource.Manifest, &object); err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: object}, nil
}
