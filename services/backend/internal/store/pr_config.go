package store

import (
	"errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"path"
	"regexp"
	"sigs.k8s.io/yaml"
	"strings"
)

var prPreviewDNSName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}[a-z0-9]$`)
var prNamespaceName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type RepositoryPRConfig struct {
	PipelineStatusReporting bool           `json:"pipelineStatusReporting"`
	Enabled                 bool           `json:"enabled"`
	Provider                string         `json:"provider,omitempty"`
	APIURL                  string         `json:"apiUrl,omitempty"`
	CredentialID            string         `json:"credentialId"`
	PreviewProfile          PreviewProfile `json:"previewProfile,omitempty"`
}

func ValidatePreviewProfile(profile *PreviewProfile, app Application) error {
	p := profile
	if p.DeploymentMode != "" && p.DeploymentMode != "isolated" && p.DeploymentMode != "existing" {
		return errors.New("deploymentMode must be isolated or existing")
	}
	for providerID, userID := range p.ApprovalActors {
		if !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(providerID) || userID == "" {
			return errors.New("approval actors require a numeric provider user ID and a JustCD user ID")
		}
	}
	if !p.Enabled {
		return nil
	}
	if p.DeploymentMode == "existing" {
		if !p.ConfirmShared || p.AllowForks || app.ApplicationGroupID != "" {
			return errors.New("existing-environment PR deployments require confirmation, a standalone application, and forks disabled")
		}
		return nil
	}
	if !prPreviewDNSName.MatchString(p.NamespacePrefix) || p.MaxActive < 1 || p.MaxActive > 100 || p.MaxLifetimeHours < 1 || p.MaxLifetimeHours > 168 {
		return errors.New("preview profile needs a namespacePrefix, maxActive (1-100), and maxLifetimeHours (1-168)")
	}
	if p.QuotaCPU == "" || p.QuotaMemory == "" {
		return errors.New("preview profile needs CPU and memory quotas")
	}
	if _, err := resource.ParseQuantity(p.QuotaCPU); err != nil {
		return errors.New("quotaCpu must be a Kubernetes quantity")
	}
	if _, err := resource.ParseQuantity(p.QuotaMemory); err != nil {
		return errors.New("quotaMemory must be a Kubernetes quantity")
	}
	if p.DatabaseStrategy != "none" && p.DatabaseStrategy != "shared-preview" && p.DatabaseStrategy != "schema-per-pr" && p.DatabaseStrategy != "ephemeral" && p.DatabaseStrategy != "sanitized-snapshot" {
		return errors.New("databaseStrategy must name a preview-safe strategy")
	}
	if p.ManifestPath == "" && p.HelmValuesYAML == "" && len(p.HelmValuesFiles) == 0 {
		return errors.New("preview profile needs a preview manifest path or Helm values override")
	}
	if (p.HelmValuesYAML != "" || len(p.HelmValuesFiles) > 0) && app.Renderer != "helm" {
		return errors.New("preview Helm values require a Helm application")
	}
	if app.Renderer != "helm" && (p.ManifestPath == "" || p.ManifestPath == app.ManifestPath) {
		return errors.New("non-Helm previews need a separate preview overlay path")
	}
	if app.Renderer == "helm" {
		files, values, err := ValidateHelmValuesInput("helm", p.HelmValuesFiles, p.HelmValuesYAML)
		if err != nil {
			return err
		}
		p.HelmValuesFiles, p.HelmValuesYAML = files, values
	}
	if p.ManifestPath != "" {
		clean := path.Clean(p.ManifestPath)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.ContainsRune(clean, '\x00') {
			return errors.New("preview manifestPath must be repository-relative")
		}
		p.ManifestPath = clean
	}
	if p.HostSuffix == "" || strings.ContainsAny(p.HostSuffix, "/:@ \\?") || !strings.Contains(p.HostSuffix, ".") {
		return errors.New("preview profile needs a valid hostSuffix for ingress validation")
	}
	for _, name := range p.AllowedSecrets {
		if !prNamespaceName.MatchString(name) {
			return errors.New("invalid allowed secret name")
		}
	}
	if p.AllowForks && len(p.AllowedSecrets) > 0 {
		return errors.New("fork previews cannot use allowed secrets")
	}
	return nil
}

func ValidateHelmValuesInput(renderer string, files []string, valuesYAML string) ([]string, string, error) {
	valuesYAML = strings.TrimSpace(valuesYAML)
	if renderer != "helm" {
		if len(files) > 0 || valuesYAML != "" {
			return nil, "", errors.New("Helm values can only be set for Helm applications")
		}
		return nil, "", nil
	}
	if len(files) > 16 {
		return nil, "", errors.New("at most 16 Helm values files may be configured")
	}
	cleanFiles := make([]string, 0, len(files))
	seen := map[string]bool{}
	for _, filename := range files {
		filename = strings.TrimSpace(filename)
		clean := path.Clean(filename)
		if filename == "" || strings.ContainsRune(filename, '\x00') || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			return nil, "", errors.New("Helm values file paths must be repository-relative")
		}
		if seen[clean] {
			return nil, "", errors.New("Helm values file paths must be unique")
		}
		seen[clean] = true
		cleanFiles = append(cleanFiles, clean)
	}
	if len(valuesYAML) > 2<<20 {
		return nil, "", errors.New("Helm values YAML exceeds 2 MiB")
	}
	if valuesYAML != "" {
		var values map[string]any
		if err := yaml.Unmarshal([]byte(valuesYAML), &values); err != nil || values == nil {
			return nil, "", errors.New("Helm values must be a YAML mapping")
		}
	}
	return cleanFiles, valuesYAML, nil
}
