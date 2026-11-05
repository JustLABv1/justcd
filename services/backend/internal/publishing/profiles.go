package publishing

import (
	"errors"
	"strings"
)

type ProfileSettings struct {
	ServiceCatalog      string `json:"serviceCatalog,omitempty"`
	GitImage            string `json:"gitImage,omitempty"`
	BuildEngine         string `json:"buildEngine,omitempty"`
	CAVariable          string `json:"caVariable,omitempty"`
	NamespaceMode       string `json:"namespaceMode,omitempty"`
	NamespacePrefix     string `json:"namespacePrefix,omitempty"`
	WebServerImage      string `json:"webServerImage,omitempty"`
	TemplateID          string `json:"templateId"`
	GitLabURL           string `json:"gitlabUrl"`
	Group               string `json:"group"`
	ClusterID           string `json:"clusterId"`
	Namespace           string `json:"namespace"`
	BaseImage           string `json:"baseImage"`
	BuilderImage        string `json:"builderImage"`
	RegistryPrefix      string `json:"registryPrefix"`
	HostSuffix          string `json:"hostSuffix"`
	PullSecret          string `json:"pullSecret"`
	RunnerTag           string `json:"runnerTag"`
	Exposure            string `json:"exposure,omitempty"`
	ExposureLabels      string `json:"exposureLabels,omitempty"`
	ExposureAnnotations string `json:"exposureAnnotations,omitempty"`
	IngressClass        string `json:"ingressClass,omitempty"`
	IngressTLS          string `json:"ingressTls,omitempty"`
	// PackageSources routes package managers through mirrors (JSON list); see packages.go.
	PackageSources string `json:"packageSources,omitempty"`
	PackagePolicy  string `json:"packagePolicy,omitempty"`
	// TemplateVersion pins a workspace template version (0 = built-in).
	TemplateVersion      int    `json:"templateVersion,omitempty"`
	TemplateParameters   string `json:"templateParameters,omitempty"`
	RunnerServiceAccount string `json:"runnerServiceAccount,omitempty"`
	RunnerSCC            string `json:"runnerScc,omitempty"`
	BuilderSetup         string `json:"builderSetup,omitempty"`
	SecretMode           string `json:"secretMode"`
	VaultAddress         string `json:"vaultAddress"`
	VaultRole            string `json:"vaultRole"`
	VaultPath            string `json:"vaultPath"`
	SyncPolicy           string `json:"syncPolicy"`
}

// Resolve builds the configuration of an application using a built-in template.
func (s ProfileSettings) Resolve(name, entry string) (Config, error) {
	return s.ResolveWith(BuiltinLookup, name, entry)
}

// ResolveWith builds the configuration of an application; lookup finds the
// built-in and workspace templates.
func (s ProfileSettings) ResolveWith(lookup TemplateLookup, name, entry string) (Config, error) {
	found, ok := lookup(s.TemplateID)
	if !ok {
		return Config{}, errors.New("select a supported application template")
	}
	template := &found
	if template.Custom && s.TemplateVersion <= 0 {
		return Config{}, errors.New("select a version of the custom template " + template.Name)
	}
	if entry == "" {
		entry = template.EntryPoint
	}
	host := ""
	if s.HostSuffix != "" {
		host = name + "." + s.HostSuffix
	}
	c := Config{PackageSources: s.PackageSources, PackagePolicy: s.PackagePolicy, TemplateParameters: s.TemplateParameters, GitImage: s.GitImage, BuildEngine: s.BuildEngine, CAVariable: s.CAVariable, TemplateID: s.TemplateID, Name: name, Runtime: template.Runtime, EntryPoint: entry, BaseImage: s.BaseImage, BuilderImage: s.BuilderImage, RegistryImage: s.RegistryPrefix + "/" + name, Namespace: s.Namespace, Host: host, PullSecret: s.PullSecret, RunnerTag: s.RunnerTag, Exposure: s.Exposure, ExposureLabels: s.ExposureLabels, ExposureAnnotations: s.ExposureAnnotations, IngressClass: s.IngressClass, IngressTLS: s.IngressTLS, TemplateVersion: s.TemplateVersion, RunnerServiceAccount: s.RunnerServiceAccount, RunnerSCC: s.RunnerSCC, BuilderSetup: s.BuilderSetup, SecretMode: s.SecretMode, VaultAddress: s.VaultAddress, VaultRole: s.VaultRole, VaultPath: s.VaultPath}
	c.Port = template.Port
	if len(template.Layout) > 0 {
		c.Layout = &TemplateLayout{Rules: template.Layout}
	}
	if s.NamespaceMode == "per-application" {
		c.Namespace = s.NamespacePrefix + name
		c.CreateNamespace = true
	}
	if IsStaticRuntime(c.Runtime) {
		c.WebServerImage = s.WebServerImage
	}
	if c.Runtime == "python-static" {
		c.BuildCommand = "python " + template.EntryPoint
		c.OutputDir = "dist"
	}
	return c, c.Validate()
}

// Validate checks settings against the built-in templates.
func (s ProfileSettings) Validate() error { return s.ValidateWith(BuiltinLookup) }

// ValidateWith checks settings; lookup finds the built-in and workspace templates.
func (s ProfileSettings) ValidateWith(lookup TemplateLookup) error {
	if _, err := ParseServiceCatalog(s.ServiceCatalog); err != nil {
		return err
	}
	if s.NamespaceMode != "" && s.NamespaceMode != "fixed" && s.NamespaceMode != "per-application" {
		return errors.New("namespace mode must be fixed or per-application")
	}
	if s.NamespaceMode == "per-application" {
		if s.Namespace != "" {
			return errors.New("per-application namespace profiles must not specify a fixed namespace")
		}
		if s.NamespacePrefix == "" || !strings.HasSuffix(s.NamespacePrefix, "-") {
			return errors.New("namespace prefix must be non-empty and end with a hyphen")
		}
		if err := ValidateNewNamespace(s.NamespacePrefix + "profile-check"); err != nil {
			return err
		}
	} else if s.NamespacePrefix != "" {
		return errors.New("namespace prefix requires per-application namespace mode")
	}

	if s.ClusterID == "" {
		return errors.New("select a deployment cluster")
	}
	if s.SyncPolicy != "manual" && s.SyncPolicy != "auto-safe" {
		return errors.New("publishing sync policy must be manual or auto-safe")
	}
	if s.Group == "" || strings.Trim(s.Group, "/") != s.Group || strings.ContainsAny(s.Group, "?#\\\r\n\t ") || strings.Contains(s.Group, "..") {
		return errors.New("GitLab group must be a group/subgroup path")
	}
	host, repository, ok := strings.Cut(s.RegistryPrefix, "/")
	if !ok || host == "" || !safePath(repository) || strings.ContainsAny(repository, ":@") || strings.ContainsAny(host, "@ \t\r\n") {
		return errors.New("registry prefix must be a registry/repository path without an image tag")
	}
	if s.HostSuffix != "" && (len(s.HostSuffix) > 189 || !hostPattern.MatchString(s.HostSuffix) || strings.Contains(s.HostSuffix, "..") || !strings.Contains(s.HostSuffix, ".")) {
		return errors.New("application domain must be a valid DNS suffix")
	}
	_, err := s.ResolveWith(lookup, "profile-check", "")
	return err
}
