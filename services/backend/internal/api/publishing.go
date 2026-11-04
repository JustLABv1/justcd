package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

type publishInput struct {
	ExpectedProfileRevision int64             `json:"expectedProfileRevision,omitempty"`
	ProfileRevision         int64             `json:"-"`
	BuildCommand            string            `json:"buildCommand"`
	OutputDir               string            `json:"outputDir"`
	ProfileID               string            `json:"profileId"`
	Name                    string            `json:"name"`
	EntryPoint              string            `json:"entryPoint"`
	UpdateApplicationID     string            `json:"updateApplicationId"`
	WorkspaceID             string            `json:"workspaceId"`
	ClusterID               string            `json:"clusterId"`
	GitLabURL               string            `json:"gitlabUrl"`
	Group                   string            `json:"group"`
	GitLabToken             string            `json:"gitlabToken"`
	GitReadToken            string            `json:"gitReadToken"`
	RegistryUser            string            `json:"registryUser"`
	RegistryPassword        string            `json:"registryPassword"`
	Config                  publishing.Config `json:"config"`
}

func (s *Server) publishApplication(w http.ResponseWriter, r *http.Request) {
	in, upload, err := readPublishingMetadata(r)
	if err != nil {
		writePublishingUploadError(w, err)
		return
	}
	var profile *store.PublishingProfile
	syncPolicy := "manual"
	if in.ProfileID != "" {
		var ok bool
		profile, ok = s.resolvePublishingProfile(w, r, &in)
		if !ok {
			return
		}
		syncPolicy = profile.Settings.SyncPolicy
	} else if !s.requireWorkspaceRole(w, r, in.WorkspaceID, "owner") {
		return
	}

	if !s.requireWorkspaceCluster(w, r, in.WorkspaceID, in.ClusterID) {
		return
	}
	if err := in.Config.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	binding, err := s.Store.PublishingNamespaceBinding(r.Context(), in.WorkspaceID, in.ClusterID, in.Config.Namespace, in.Config.CreateNamespace)
	if err != nil {
		writeError(w, 400, "namespace destination is unavailable: "+err.Error())
		return
	}
	if strings.Trim(in.Group, "/") != in.Group || in.Group == "" || strings.ContainsAny(in.Group, "?#\\\r\n\t ") || strings.Contains(in.Group, "..") {
		writeError(w, 400, "GitLab group must be a group/subgroup path")
		return
	}
	git := publishing.GitLab{URL: in.GitLabURL, Token: in.GitLabToken, HTTP: s.PublishingHTTP}
	if err = git.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if in.UpdateApplicationID == "" && (len(in.GitReadToken) < 8 || strings.ContainsAny(in.GitReadToken, "\r\n")) {
		writeError(w, 400, "provide a repository read token for JustCD to fetch the private repository")
		return
	}
	// Managed publications finish in JustCD; retain the existing mode on source-only updates.
	in.Config.ManagedCompletion = true
	if in.UpdateApplicationID != "" {
		app, e := s.Store.ApplicationByID(r.Context(), in.UpdateApplicationID)
		if e != nil || app.WorkspaceID != in.WorkspaceID {
			writeError(w, 404, "application not found")
			return
		}
		src, e := s.Store.GitSourceByID(r.Context(), app.SourceID)
		if e != nil {
			writeError(w, 404, "Git source not found")
			return
		}
		previous, e := s.Store.PublishedApplication(r.Context(), in.WorkspaceID, src.RepositoryURL)
		var cfg publishing.Config
		if e != nil || json.Unmarshal(previous.Config, &cfg) != nil {
			writeError(w, 404, "publication not found")
			return
		}
		in.Config.ManagedCompletion = cfg.ManagedCompletion
	}
	if err = in.Config.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if in.Config.SecretMode == "variables" && in.UpdateApplicationID == "" && (in.RegistryUser == "" || len(in.RegistryPassword) < 8 || strings.ContainsAny(in.RegistryPassword, " \r\n\t")) {
		writeError(w, 400, "registry username and a maskable token of at least eight characters are required")
		return
	}
	if in.Config.SecretMode == "vault" && (in.RegistryUser != "" || in.RegistryPassword != "") {
		writeError(w, 400, "Vault mode accepts secret references, not registry secret values")
		return
	}
	// Publishing uploads and provider setup may exceed the normal API deadlines.
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(5 * time.Minute))
	_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Minute))
	dir, err := os.MkdirTemp(s.Config.PublishingTempDir, "justcd-publish-*")
	if err != nil {
		writeError(w, 500, "cannot allocate temporary upload storage")
		return
	}
	defer os.RemoveAll(dir)
	limits := s.Config.PublishingLimits.WithDefaults()
	archive, err := savePublishingArchive(r, upload, dir, limits)
	if err != nil {
		writePublishingUploadError(w, err)
		return
	}
	defer archive.Close()
	stat, err := archive.Stat()
	if err != nil {
		writeError(w, 500, "cannot inspect uploaded archive")
		return
	}
	source, err := publishing.ExtractArchive(r.Context(), archive, stat.Size(), dir, limits)
	if err != nil {
		writePublishingUploadError(w, err)
		return
	}
	if err = s.attachTemplate(r.Context(), in.WorkspaceID, &in.Config); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	files, err := publishing.Generate(in.Config, source)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	// Secret values are intentionally excluded. Retrying rotates CI variables only
	// before the source commit exists; no token or archive is written to the audit log.
	fingerprint, err := publishing.Fingerprint(r.Context(), in.Config, files, in.ClusterID)
	if err != nil {
		writeError(w, 500, "could not fingerprint uploaded source")
		return
	}
	if profile != nil {
		// A credential rotation must make a repeated source snapshot eligible for
		// a new build without putting secret values into release identity.
		sum := sha256.Sum256([]byte(fingerprint + "\n" + profile.ID + "\n" + strconv.FormatInt(profile.Version, 10)))
		fingerprint = hex.EncodeToString(sum[:])
	}
	git.TempDir = dir
	markerSum := sha256.Sum256([]byte(in.WorkspaceID + "\n" + strings.TrimRight(in.GitLabURL, "/") + "\n" + in.Group + "/" + in.Config.Name))
	marker := "JustCD managed publication " + hex.EncodeToString(markerSum[:])
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	// Serialize publication for this repository across all API replicas. The
	// transaction is only a bounded advisory lock; registration uses a separate atomic transaction.
	lock, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		writeStoreError(w, "could not start publishing")
		return
	}
	defer lock.Rollback()
	var acquired bool
	if err = lock.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, marker).Scan(&acquired); err != nil || !acquired {
		writeError(w, 409, "this application is already being published; wait before retrying")
		return
	}
	if in.UpdateApplicationID != "" {
		app, e := s.Store.ApplicationByID(ctx, in.UpdateApplicationID)
		if e != nil || app.WorkspaceID != in.WorkspaceID {
			writeError(w, 404, "application not found in workspace")
			return
		}
		src, e := s.Store.GitSourceByID(ctx, app.SourceID)
		if e != nil {
			writeError(w, 404, "Git source not found")
			return
		}
		previous, e := s.Store.PublishedApplication(ctx, in.WorkspaceID, src.RepositoryURL)
		if e == nil && previous.ProfileID != nil && profile == nil {
			writeError(w, 400, "this application requires its publishing profile")
			return
		}
		cfg, _ := json.Marshal(in.Config)
		var oldConfig publishing.Config
		_ = json.Unmarshal(previous.Config, &oldConfig)
		oldCfg, _ := json.Marshal(oldConfig)
		if e != nil || string(cfg) != string(oldCfg) || previous.GitLabURL != in.GitLabURL || previous.Group != in.Group || app.ClusterID != in.ClusterID {
			writeError(w, 400, "source updates must reuse the original publication settings")
			return
		}
	}
	if profile != nil {
		current, e := s.Store.PublishingProfile(ctx, profile.ID)
		if e != nil || !current.Enabled || current.Version != profile.Version {
			writeError(w, 409, "publishing profile changed during upload; reload before publishing")
			return
		}
	}
	project, err := git.EnsureProject(ctx, in.Group, in.Config.Name, marker)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	expected := strings.TrimRight(in.GitLabURL, "/") + "/" + in.Group + "/" + in.Config.Name + ".git"
	if project.RepositoryURL != expected {
		writeError(w, 502, "GitLab returned an unexpected repository URL; refusing to forward credentials")
		return
	}
	registered, err := s.Store.PublishedApplication(ctx, in.WorkspaceID, project.RepositoryURL)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeStoreError(w, "could not read publication")
		return
	}
	if err == nil && ((registered.ProfileID != nil && (profile == nil || *registered.ProfileID != profile.ID)) || (registered.ProfileID == nil && profile != nil)) {
		writeError(w, 409, "application must use its original publishing workflow and profile")
		return
	}
	if err == nil && profile != nil && in.Config.SecretMode == "variables" {
		variables := []publishing.Variable{{Key: "REGISTRY_USER", Value: in.RegistryUser, Protected: true, Raw: true}, {Key: "REGISTRY_PASSWORD", Value: in.RegistryPassword, Masked: true, Protected: true, Raw: true}}
		if e := git.SetVariables(ctx, project, variables); e != nil {
			writeError(w, 502, "could not refresh profile registry credentials in GitLab")
			return
		}
	}
	if err == nil {
		if in.UpdateApplicationID != "" && registered.ApplicationID != in.UpdateApplicationID {
			writeError(w, 409, "publication does not match this application")
			return
		}
		sourceChanged := registered.Fingerprint != fingerprint
		if !sourceChanged && in.UpdateApplicationID != "" {
			matches, checkErr := git.SourceMatches(ctx, project, files)
			if checkErr != nil {
				writeError(w, 502, "could not verify the previously uploaded source; retry when GitLab is available")
				return
			}
			sourceChanged = !matches
		}
		if sourceChanged {
			if in.UpdateApplicationID == "" {
				writeError(w, 409, "application already exists; use Upload new version")
				return
			}
			app, e := s.Store.ApplicationByID(ctx, registered.ApplicationID)
			if e != nil || app.ClusterID != in.ClusterID || len(app.Namespaces) != 1 || app.Namespaces[0].Namespace != in.Config.Namespace || app.Name != in.Config.Name {
				writeError(w, 400, "updates must retain the original application name and deployment target")
				return
			}
			if e = git.ReplaceSource(ctx, project, files, fingerprint); e != nil {
				writeError(w, 502, e.Error())
				return
			}
			registered.Fingerprint = fingerprint
			registered.Config, _ = json.Marshal(in.Config)
			registered.PipelineURL = ""
			if e = s.Store.UpdatePublishedSource(ctx, registered); e != nil {
				writeStoreError(w, "source committed but publication metadata could not be updated; retry to resume")
				return
			}
			_ = s.Store.Audit(ctx, currentUser(r).ID, "application.source_uploaded", "application", registered.ApplicationID, map[string]string{"runtime": in.Config.Runtime, "templateVersion": publishing.Version, "profileId": in.ProfileID})
		}
	} else {
		if in.UpdateApplicationID != "" {
			writeError(w, 404, "managed publication not found")
			return
		}
		variables := []publishing.Variable{}
		if in.Config.SecretMode == "variables" {
			variables = append(variables, publishing.Variable{Key: "REGISTRY_USER", Value: in.RegistryUser, Protected: true, Raw: true}, publishing.Variable{Key: "REGISTRY_PASSWORD", Value: in.RegistryPassword, Masked: true, Protected: true, Raw: true})
		}
		if err = git.Prepare(ctx, project, files, variables, fingerprint); err != nil {
			writeError(w, 502, err.Error()+". The private repository is retained; retry the same upload and settings to resume.")
			return
		}
		if err = git.ProtectPublishingBranch(ctx, project); err != nil {
			writeError(w, 502, "could not protect main before starting CI: "+err.Error()+". Check the GitLab API token’s protected-branch permissions (normally Maintainer or Owner), or ask a GitLab administrator to protect main. The repository is retained; retry the same upload and settings to resume.")
			return
		}
		cred := store.Credential{}
		credentialID := ""
		if profile != nil {
			credentialID = profile.GitReadCredentialID
		} else {
			cred = store.Credential{ID: store.NewID(), WorkspaceID: &in.WorkspaceID, Name: in.Config.Name + " Git read", Kind: "git-https"}
			payload, _ := json.Marshal(map[string]string{"username": "oauth2", "token": in.GitReadToken})
			cred.Cipher, err = security.Encrypt(s.EncryptionKey, payload, "credential:"+cred.ID)
			if err != nil {
				writeStoreError(w, "could not protect Git read credential")
				return
			}
			credentialID = cred.ID
		}
		src := store.GitSource{ID: store.NewID(), WorkspaceID: in.WorkspaceID, Name: in.Config.Name, RepositoryURL: project.RepositoryURL, CredentialID: &credentialID}
		app := store.Application{Labels: map[string]string{"justcd.io/published": "true"}, ID: store.NewID(), WorkspaceID: in.WorkspaceID, Name: in.Config.Name, SourceID: src.ID, Revision: "main", ManifestPath: "deploy", Renderer: "yaml", ClusterID: in.ClusterID, Namespaces: []store.NamespaceBinding{binding}, CreateNamespaces: in.Config.CreateNamespace, SyncPolicy: syncPolicy, PollSeconds: 30, RetryPolicy: store.DefaultRetryPolicy(), Health: "unknown"}
		safeConfig, _ := json.Marshal(in.Config)
		registered = store.PublishedApplication{Config: safeConfig, GitLabURL: in.GitLabURL, Group: in.Group, ApplicationID: app.ID, Fingerprint: fingerprint, RepositoryURL: project.RepositoryURL, ProjectURL: project.WebURL, ProjectID: project.ID}
		if profile != nil {
			app.Labels["justcd.io/publishing-profile"] = profile.ID
			registered.ProfileRevision = in.ProfileRevision
			registered.ProfileID = &profile.ID
			registered.ProfileVersion = profile.Version
		}
		if err = s.Store.RegisterPublishedApplication(ctx, cred, src, app, registered); err != nil {
			writeError(w, 409, "could not register publication; the repository is retained. Retry the same upload and settings to resume")
			return
		}
		_ = s.Store.Audit(ctx, currentUser(r).ID, "application.published", "application", app.ID, map[string]string{"workspaceId": in.WorkspaceID, "runtime": in.Config.Runtime, "templateVersion": publishing.Version, "secretMode": in.Config.SecretMode, "profileId": in.ProfileID})
	}
	if in.Config.ManagedCompletion && profile == nil {
		cipher, e := security.Encrypt(s.EncryptionKey, []byte(in.GitLabToken), "publication:"+registered.ApplicationID)
		if e != nil {
			writeStoreError(w, "could not protect publishing completion token")
			return
		}
		if _, e = s.Store.DB.ExecContext(ctx, `UPDATE published_applications SET completion_token_cipher=$2 WHERE application_id=$1`, registered.ApplicationID, cipher); e != nil {
			writeStoreError(w, "could not retain publishing completion token")
			return
		}
	}
	if registered.PipelineURL == "" {
		registered.PipelineURL, err = git.StartPipeline(ctx, project.ID)
		if err != nil {
			writeError(w, 502, "application registered but pipeline could not start; retry the same upload to resume, or start main in GitLab")
			return
		}
		if err = s.Store.SetPublishingPipeline(ctx, registered.ApplicationID, registered.PipelineURL); err != nil {
			writeStoreError(w, "pipeline started but its URL could not be recorded; inspect GitLab before retrying")
			return
		}
	}
	// Only same-origin provider links are returned to the browser.
	origin, _ := url.Parse(in.GitLabURL)
	for _, raw := range []string{registered.ProjectURL, registered.PipelineURL} {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Host != origin.Host {
			writeError(w, 502, "GitLab returned an unexpected link")
			return
		}
	}
	writeJSON(w, http.StatusCreated, publicationResult(registered, syncPolicy))
}

func (s *Server) getPublication(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, 404, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "deployer") {
		return
	}
	source, err := s.Store.GitSourceByID(r.Context(), app.SourceID)
	if err != nil {
		writeError(w, 404, "Git source not found")
		return
	}
	p, err := s.Store.PublishedApplication(r.Context(), app.WorkspaceID, source.RepositoryURL)
	if err != nil {
		writeError(w, 404, "this application was not created with guided publishing")
		return
	}
	if p.ProfileID == nil && !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	result := publicationResult(p, app.SyncPolicy)
	var state, statusError string
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT completion_status,completion_error FROM published_applications WHERE application_id=$1`, app.ID).Scan(&state, &statusError); err != nil {
		writeStoreError(w, "could not read build completion state")
		return
	}
	result["completionStatus"], result["completionError"] = state, statusError
	if p.ProfileID != nil {
		profile, e := s.Store.PublishingProfile(r.Context(), *p.ProfileID)
		if e != nil || profile.WorkspaceID != app.WorkspaceID {
			writeError(w, 404, "publishing profile not found")
			return
		}
		result["profileName"] = profile.Name
		result["profileEnabled"] = profile.Enabled
		result["profileRevision"] = p.ProfileRevision
		result["latestProfileRevision"] = profile.CurrentRevision
		result["profileUpdateAvailable"] = profile.CurrentRevision > p.ProfileRevision
	}
	writeJSON(w, 200, map[string]any{"publication": result, "workspaceId": app.WorkspaceID, "clusterId": app.ClusterID})
}
