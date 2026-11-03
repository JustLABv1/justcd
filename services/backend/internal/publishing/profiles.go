package publishing

import (
	"errors"
	"strings"
)

type ProfileSettings struct {
	TemplateID     string `json:"templateId"`
	GitLabURL      string `json:"gitlabUrl"`
	Group          string `json:"group"`
	ClusterID      string `json:"clusterId"`
	Namespace      string `json:"namespace"`
	BaseImage      string `json:"baseImage"`
	BuilderImage   string `json:"builderImage"`
	RegistryPrefix string `json:"registryPrefix"`
	HostSuffix     string `json:"hostSuffix"`
	PullSecret     string `json:"pullSecret"`
	RunnerTag      string `json:"runnerTag"`
	SecretMode     string `json:"secretMode"`
	VaultAddress   string `json:"vaultAddress"`
	VaultRole      string `json:"vaultRole"`
	VaultPath      string `json:"vaultPath"`
	SyncPolicy     string `json:"syncPolicy"`
}

func (s ProfileSettings) Resolve(name, entry string) (Config, error) {
	var template *Template
	for _, t := range Templates() {
		if t.ID == s.TemplateID {
			copy := t
			template = &copy
			break
		}
	}
	if template == nil {
		return Config{}, errors.New("select a supported application template")
	}
	if entry == "" {
		entry = template.EntryPoint
	}
	host := ""
	if s.HostSuffix != "" {
		host = name + "." + s.HostSuffix
	}
	c := Config{TemplateID: s.TemplateID, Name: name, Runtime: template.Runtime, EntryPoint: entry, BaseImage: s.BaseImage, BuilderImage: s.BuilderImage, RegistryImage: s.RegistryPrefix + "/" + name, Namespace: s.Namespace, Host: host, PullSecret: s.PullSecret, RunnerTag: s.RunnerTag, SecretMode: s.SecretMode, VaultAddress: s.VaultAddress, VaultRole: s.VaultRole, VaultPath: s.VaultPath}
	return c, c.Validate()
}
func (s ProfileSettings) Validate() error {
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
	_, err := s.Resolve("profile-check", "")
	return err
}
