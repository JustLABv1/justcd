package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

type publishingSecrets struct {
	GitLabToken      string `json:"gitlabToken"`
	GitReadToken     string `json:"gitReadToken"`
	RegistryUser     string `json:"registryUser"`
	RegistryPassword string `json:"registryPassword"`
}

// Read-only profile summaries deliberately exclude infrastructure settings and
// credential references. Analysts choose an approved destination, not its tokens.
type publishingProfileSummary struct {
	Services        publishing.ServiceOptions `json:"services"`
	CurrentRevision int64                     `json:"currentRevision"`
	NamespaceMode   string                    `json:"namespaceMode"`
	NamespacePrefix string                    `json:"namespacePrefix"`
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	Description     string                    `json:"description"`
	TemplateID      string                    `json:"templateId"`
	ClusterID       string                    `json:"clusterId"`
	ClusterName     string                    `json:"clusterName"`
	Namespace       string                    `json:"namespace"`
	HostSuffix      string                    `json:"hostSuffix"`
	SyncPolicy      string                    `json:"syncPolicy"`
}

func (s *Server) listApprovedPublishingProfiles(w http.ResponseWriter, r *http.Request) {
	workspace := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspace, "deployer") {
		return
	}
	profiles, err := s.Store.PublishingProfiles(r.Context(), workspace)
	if err != nil {
		writeStoreError(w, "could not list publishing profiles")
		return
	}
	items := []publishingProfileSummary{}
	for _, p := range profiles {
		if !p.Enabled {
			continue
		}
		allowed, e := s.Store.WorkspaceCanUseCluster(r.Context(), workspace, p.Settings.ClusterID)
		if e != nil {
			writeStoreError(w, "could not check profile destination access")
			return
		}
		if !allowed {
			continue
		}
		if p.Settings.NamespaceMode != "per-application" {
			if _, e = s.Store.NamespaceBinding(r.Context(), workspace, p.Settings.ClusterID, p.Settings.Namespace); e != nil {
				continue
			}
		}
		cluster, e := s.Store.ClusterByID(r.Context(), p.Settings.ClusterID)
		if e != nil {
			continue
		}
		catalog, err := publishing.ParseServiceCatalog(p.Settings.ServiceCatalog)
		if err != nil {
			writeStoreError(w, "invalid publishing service catalog")
			return
		}
		items = append(items, publishingProfileSummary{Services: catalog.Options(), CurrentRevision: p.CurrentRevision, ID: p.ID, Name: p.Name, Description: p.Description, TemplateID: p.Settings.TemplateID, ClusterID: p.Settings.ClusterID, ClusterName: cluster.Name, Namespace: p.Settings.Namespace, HostSuffix: p.Settings.HostSuffix, SyncPolicy: p.Settings.SyncPolicy, NamespaceMode: p.Settings.NamespaceMode, NamespacePrefix: p.Settings.NamespacePrefix})
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) listPublishingProfiles(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	items, err := s.Store.PublishingProfiles(r.Context(), workspace)
	if err != nil {
		writeStoreError(w, "could not list publishing profiles")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) loadPublishingSecrets(r *http.Request, p store.PublishingProfile) (publishingSecrets, error) {
	var result publishingSecrets
	for _, id := range []string{p.CredentialID, p.GitReadCredentialID} {
		c, err := s.Store.CredentialByID(r.Context(), id)
		if err != nil || c.WorkspaceID == nil || *c.WorkspaceID != p.WorkspaceID || (c.ExpiresAt != nil && !c.ExpiresAt.After(time.Now())) {
			return result, errors.New("profile credential not available in this workspace")
		}
		plain, err := security.Decrypt(s.EncryptionKey, c.Cipher, "credential:"+c.ID)
		if err != nil {
			return result, errors.New("could not decrypt profile credential")
		}
		if id == p.CredentialID {
			if c.Kind != "publishing" {
				return result, errors.New("invalid profile credential type")
			}
			if err = json.Unmarshal(plain, &result); err != nil {
				return result, errors.New("invalid profile credential")
			}
		} else {
			if c.Kind != "git-https" {
				return result, errors.New("invalid profile Git credential type")
			}
			var read struct {
				Token string `json:"token"`
			}
			if err = json.Unmarshal(plain, &read); err != nil {
				return result, errors.New("invalid profile Git credential")
			}
			result.GitReadToken = read.Token
		}
	}
	return result, nil
}
func validatePublishingSecrets(settings publishing.ProfileSettings, secrets publishingSecrets) error {
	if err := (publishing.GitLab{URL: settings.GitLabURL, Token: secrets.GitLabToken}).Validate(); err != nil {
		return err
	}
	if len(secrets.GitReadToken) < 8 || len(secrets.GitReadToken) > 64<<10 || strings.ContainsAny(secrets.GitReadToken, "\r\n\x00") {
		return errors.New("a valid Git repository read token is required")
	}
	if len(secrets.GitLabToken) > 64<<10 {
		return errors.New("GitLab token exceeds 64 KiB")
	}
	if settings.SecretMode == "variables" && (secrets.RegistryUser == "" || len(secrets.RegistryUser) > 256 || len(secrets.RegistryPassword) < 8 || len(secrets.RegistryPassword) > 64<<10 || strings.ContainsAny(secrets.RegistryPassword, " \r\n\t")) {
		return errors.New("registry username and a maskable token of at least eight characters are required")
	}
	return nil
}
func (s *Server) savePublishingProfile(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	var input struct {
		CopyFromProfileID string                     `json:"copyFromProfileId"`
		PublishRevision   bool                       `json:"publishRevision"`
		Name              string                     `json:"name"`
		Description       string                     `json:"description"`
		Enabled           bool                       `json:"enabled"`
		Settings          publishing.ProfileSettings `json:"settings"`
		Secrets           publishingSecrets          `json:"secrets"`
		Version           int64                      `json:"version"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, 400, "invalid publishing profile")
		return
	}
	var copySource *store.PublishingProfile
	if input.CopyFromProfileID != "" {
		if r.Method != "POST" {
			writeError(w, 400, "copyFromProfileId is only supported when creating a profile")
			return
		}
		source, err := s.Store.PublishingProfile(r.Context(), input.CopyFromProfileID)
		if err != nil || source.WorkspaceID != workspace {
			writeError(w, 404, "source publishing profile not found")
			return
		}
		copySource = &source
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 100 || len(input.Description) > 1000 {
		writeError(w, 400, "profile name is required (maximum 100 characters); description must be at most 1000 characters")
		return
	}
	if input.Settings.SyncPolicy == "" {
		input.Settings.SyncPolicy = "manual"
	}
	if err := input.Settings.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if !s.requireWorkspaceCluster(w, r, workspace, input.Settings.ClusterID) {
		return
	}
	checkConfig, _ := input.Settings.Resolve("profile-check", "")
	if input.Settings.TemplateVersion > 0 {
		if err := s.attachTemplate(r.Context(), workspace, &checkConfig); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		for _, renv := range []bool{false, true} {
			if _, err := publishing.RenderConfig(checkConfig, publishing.SourceFacts{HasRenvLock: renv}); err != nil {
				writeError(w, 400, "the selected template version does not render with these settings: "+err.Error())
				return
			}
		}
	}
	if input.Settings.ServiceCatalog != "" {
		catalog, _ := publishing.ParseServiceCatalog(input.Settings.ServiceCatalog)
		all := publishing.ServiceSelection{Database: catalog.Database != nil}
		for _, v := range catalog.VaultSecrets {
			all.VaultSecrets = append(all.VaultSecrets, v.ID)
		}
		resolvedServices, err := catalog.Resolve(all)
		checkConfig.Services = resolvedServices
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		for _, renv := range []bool{false, true} {
			if _, err := publishing.RenderConfig(checkConfig, publishing.SourceFacts{HasRenvLock: renv}); err != nil {
				writeError(w, 400, "application services: "+err.Error())
				return
			}
		}
	}
	if _, err := s.Store.PublishingNamespaceBinding(r.Context(), workspace, input.Settings.ClusterID, checkConfig.Namespace, checkConfig.CreateNamespace); err != nil {
		writeError(w, 400, "profile namespace destination is unavailable: "+err.Error())
		return
	}
	create := r.Method == "POST"
	p := store.PublishingProfile{ID: store.NewID(), WorkspaceID: workspace, CredentialID: store.NewID(), GitReadCredentialID: store.NewID(), Version: 1}
	secrets := input.Secrets
	if copySource != nil {
		previous, err := s.loadPublishingSecrets(r, *copySource)
		if err != nil {
			writeStoreError(w, "could not copy profile credentials")
			return
		}
		if secrets.GitLabToken == "" {
			secrets.GitLabToken = previous.GitLabToken
		}
		if secrets.GitReadToken == "" {
			secrets.GitReadToken = previous.GitReadToken
		}
		if secrets.RegistryUser == "" {
			secrets.RegistryUser = previous.RegistryUser
		}
		if secrets.RegistryPassword == "" {
			secrets.RegistryPassword = previous.RegistryPassword
		}
	}
	if !create {
		var err error
		p, err = s.Store.PublishingProfile(r.Context(), r.PathValue("profileID"))
		if err != nil || p.WorkspaceID != workspace {
			writeError(w, 404, "publishing profile not found")
			return
		}
		previous, e := s.loadPublishingSecrets(r, p)
		if e != nil {
			writeError(w, 500, e.Error())
			return
		}
		if secrets.GitLabToken == "" {
			secrets.GitLabToken = previous.GitLabToken
		}
		if secrets.GitReadToken == "" {
			secrets.GitReadToken = previous.GitReadToken
		}
		if secrets.RegistryUser == "" {
			secrets.RegistryUser = previous.RegistryUser
		}
		if secrets.RegistryPassword == "" {
			secrets.RegistryPassword = previous.RegistryPassword
		}
		p.Version = input.Version
	}
	p.PublishRevision = input.PublishRevision
	p.Name, p.Description, p.Enabled, p.Settings = input.Name, input.Description, input.Enabled, input.Settings
	if p.Settings.SecretMode == "vault" {
		secrets.RegistryUser = ""
		secrets.RegistryPassword = ""
	}
	if err := validatePublishingSecrets(p.Settings, secrets); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	// Reuse the encrypted credential store. A dedicated bundle cannot be used as
	// a Kubernetes/Git credential; the separately scoped read token can be rotated
	// in place without cloning credentials for every published application.
	bundle := secrets
	bundle.GitReadToken = ""
	values := []struct {
		id, kind string
		payload  any
	}{{p.CredentialID, "publishing", bundle}, {p.GitReadCredentialID, "git-https", map[string]string{"username": "oauth2", "token": secrets.GitReadToken}}}
	credentials := []store.Credential{}
	for _, v := range values {
		plain, _ := json.Marshal(v.payload)
		cipher, err := security.Encrypt(s.EncryptionKey, plain, "credential:"+v.id)
		if err != nil {
			writeStoreError(w, "could not protect profile credentials")
			return
		}
		credentials = append(credentials, store.Credential{ID: v.id, WorkspaceID: &workspace, Name: p.Name + " publishing", Kind: v.kind, Cipher: cipher})
	}
	if err := s.Store.SavePublishingProfile(r.Context(), p, credentials, create); err != nil {
		if strings.HasPrefix(err.Error(), "profile is in use;") {
			writeError(w, 409, err.Error())
			return
		}
		writeError(w, 409, "could not save profile; reload to check its version, unique name, and whether infrastructure settings are already in use")
		return
	}
	action := "publishing_profile.updated"
	status := 200
	if create {
		action = "publishing_profile.created"
		status = 201
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, action, "publishing_profile", p.ID, map[string]any{"workspaceId": workspace, "name": p.Name, "enabled": p.Enabled, "credentialsUpdated": input.Secrets.GitLabToken != "" || input.Secrets.GitReadToken != "" || input.Secrets.RegistryPassword != ""})
	saved, err := s.Store.PublishingProfile(r.Context(), p.ID)
	if err != nil {
		writeStoreError(w, "profile saved but could not be reloaded; refresh the profile list")
		return
	}
	writeJSON(w, status, saved)
}
func (s *Server) deletePublishingProfile(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "owner") {
		return
	}
	p, err := s.Store.PublishingProfile(r.Context(), r.PathValue("profileID"))
	if err != nil || p.WorkspaceID != workspace {
		writeError(w, 404, "publishing profile not found")
		return
	}
	if err = s.Store.DeletePublishingProfile(r.Context(), p); err != nil {
		writeError(w, 409, "profile is in use or has changed; disable it to stop publishing without removing existing applications")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "publishing_profile.deleted", "publishing_profile", p.ID, map[string]string{"workspaceId": workspace})
	w.WriteHeader(204)
}

func (s *Server) resolvePublishingProfile(w http.ResponseWriter, r *http.Request, in *publishInput) (*store.PublishingProfile, bool) {
	if !s.requireWorkspaceRole(w, r, in.WorkspaceID, "deployer") {
		return nil, false
	}
	// A profile request contains only analyst-owned fields. In particular, raw
	// credentials, image references and destinations are never accepted as overrides.
	if in.Config != (publishing.Config{}) || in.ClusterID != "" || in.GitLabURL != "" || in.Group != "" || in.GitLabToken != "" || in.GitReadToken != "" || in.RegistryUser != "" || in.RegistryPassword != "" {
		writeError(w, 400, "profile publishing accepts only profileId, workspaceId, name, entryPoint, buildCommand, outputDir, expectedProfileRevision, and updateApplicationId")
		return nil, false
	}
	p, err := s.Store.PublishingProfile(r.Context(), in.ProfileID)
	if err != nil || p.WorkspaceID != in.WorkspaceID {
		writeError(w, 404, "publishing profile not found in workspace")
		return nil, false
	}
	if !p.Enabled {
		writeError(w, 403, "this publishing profile is disabled; ask a workspace administrator")
		return nil, false
	}
	if in.UpdateApplicationID == "" && in.ExpectedProfileRevision != 0 && in.ExpectedProfileRevision != p.CurrentRevision {
		writeError(w, 409, "profile has a newer published version; reload the profile selection before publishing")
		return nil, false
	}
	in.ProfileRevision = p.CurrentRevision
	if in.UpdateApplicationID != "" {
		app, e := s.Store.ApplicationByID(r.Context(), in.UpdateApplicationID)
		if e != nil || app.WorkspaceID != in.WorkspaceID || app.Decommissioning {
			writeError(w, 409, "application is unavailable for source updates")
			return nil, false
		}
		source, e := s.Store.GitSourceByID(r.Context(), app.SourceID)
		if e != nil {
			writeError(w, 404, "application source not found")
			return nil, false
		}
		previous, e := s.Store.PublishedApplication(r.Context(), in.WorkspaceID, source.RepositoryURL)
		if e != nil || previous.ProfileID == nil || *previous.ProfileID != p.ID {
			writeError(w, 400, "source updates must use the application's original publishing profile")
			return nil, false
		}
		in.ProfileRevision = previous.ProfileRevision
		var config publishing.Config
		if e = json.Unmarshal(previous.Config, &config); e != nil {
			writeStoreError(w, "could not read publication configuration")
			return nil, false
		}
		if (in.Name != "" && in.Name != app.Name) || (in.EntryPoint != "" && in.EntryPoint != config.EntryPoint) || (in.BuildCommand != "" && in.BuildCommand != config.BuildCommand) || (in.OutputDir != "" && in.OutputDir != config.OutputDir) {
			writeError(w, 400, "source updates must retain the application name, entry point, and build settings")
			return nil, false
		}
		in.Name, in.EntryPoint = app.Name, config.EntryPoint
		in.BuildCommand, in.OutputDir = config.BuildCommand, config.OutputDir
		in.Services, e = publishing.ServiceChoices(config.Services)
		if e != nil {
			writeStoreError(w, "invalid application services")
			return nil, false
		}
	}
	version, e := s.Store.PublishingProfileVersion(r.Context(), p.ID, in.ProfileRevision)
	if e != nil {
		writeError(w, 409, "pinned profile revision is unavailable")
		return nil, false
	}
	if version.TemplateVersion != publishing.Version {
		writeError(w, 409, "pinned recipe version is unsupported by this server; upgrade the application profile before publishing source")
		return nil, false
	}
	cfg, err := version.Settings.Resolve(in.Name, in.EntryPoint)
	if err != nil {
		writeError(w, 400, err.Error())
		return nil, false
	}
	if cfg.Runtime == "python-static" {
		if in.BuildCommand != "" {
			cfg.BuildCommand = in.BuildCommand
		}
		if in.OutputDir != "" {
			cfg.OutputDir = in.OutputDir
		}
		if e := cfg.Validate(); e != nil {
			writeError(w, 400, e.Error())
			return nil, false
		}
	} else if in.BuildCommand != "" || in.OutputDir != "" {
		writeError(w, 400, "static build settings require a Python static website profile")
		return nil, false
	}
	catalog, err := publishing.ParseServiceCatalog(version.Settings.ServiceCatalog)
	if err != nil {
		writeError(w, 400, err.Error())
		return nil, false
	}
	cfg.Services, err = catalog.Resolve(in.Services)
	if err != nil {
		writeError(w, 400, err.Error())
		return nil, false
	}
	secrets, err := s.loadPublishingSecrets(r, p)
	if err != nil {
		writeError(w, 500, err.Error())
		return nil, false
	}
	if err = validatePublishingSecrets(p.Settings, secrets); err != nil {
		writeError(w, 409, "profile credentials need administrator attention")
		return nil, false
	}
	in.Config, in.ClusterID, in.GitLabURL, in.Group = cfg, p.Settings.ClusterID, p.Settings.GitLabURL, p.Settings.Group
	in.GitLabToken, in.GitReadToken, in.RegistryUser, in.RegistryPassword = secrets.GitLabToken, secrets.GitReadToken, secrets.RegistryUser, secrets.RegistryPassword
	return &p, true
}
func publicationResult(p store.PublishedApplication, syncPolicy string) map[string]any {
	var cfg publishing.Config
	_ = json.Unmarshal(p.Config, &cfg)
	url := ""
	if cfg.Host != "" {
		url = "https://" + cfg.Host
	}
	result := map[string]any{"applicationId": p.ApplicationID, "projectUrl": p.ProjectURL, "repositoryUrl": p.RepositoryURL, "pipelineUrl": p.PipelineURL, "syncPolicy": syncPolicy, "applicationUrl": url, "managedCompletion": cfg.ManagedCompletion}
	if p.ProfileID != nil {
		result["profileId"] = *p.ProfileID
		result["profileRevision"] = p.ProfileRevision
		result["config"] = map[string]any{"createNamespace": cfg.CreateNamespace, "namespace": cfg.Namespace, "name": cfg.Name, "runtime": cfg.Runtime, "entryPoint": cfg.EntryPoint, "templateId": cfg.TemplateID, "buildCommand": cfg.BuildCommand, "outputDir": cfg.OutputDir}
	} else {
		result["config"] = cfg
		result["gitlabUrl"] = p.GitLabURL
		result["group"] = p.Group
	}
	return result
}
