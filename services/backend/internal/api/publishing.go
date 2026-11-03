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
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

type publishInput struct {
	UpdateApplicationID string            `json:"updateApplicationId"`
	WorkspaceID         string            `json:"workspaceId"`
	ClusterID           string            `json:"clusterId"`
	GitLabURL           string            `json:"gitlabUrl"`
	Group               string            `json:"group"`
	GitLabToken         string            `json:"gitlabToken"`
	GitReadToken        string            `json:"gitReadToken"`
	RegistryUser        string            `json:"registryUser"`
	RegistryPassword    string            `json:"registryPassword"`
	Config              publishing.Config `json:"config"`
}

func (s *Server) publishApplication(w http.ResponseWriter, r *http.Request) {
	in, upload, err := readPublishingMetadata(r)
	if err != nil {
		writePublishingUploadError(w, err)
		return
	}
	if !s.requireWorkspaceRole(w, r, in.WorkspaceID, "owner") {
		return
	}
	if !s.requireWorkspaceCluster(w, r, in.WorkspaceID, in.ClusterID) {
		return
	}
	binding, err := s.Store.NamespaceBinding(r.Context(), in.WorkspaceID, in.ClusterID, in.Config.Namespace)
	if err != nil {
		writeError(w, 400, "select a namespace already bound to this workspace and cluster")
		return
	}
	if strings.Trim(in.Group, "/") != in.Group || in.Group == "" || strings.ContainsAny(in.Group, "?#\\\r\n\t ") || strings.Contains(in.Group, "..") {
		writeError(w, 400, "GitLab group must be a group/subgroup path")
		return
	}
	git := publishing.GitLab{URL: in.GitLabURL, Token: in.GitLabToken}
	if err = git.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if in.UpdateApplicationID == "" && (len(in.GitReadToken) < 8 || strings.ContainsAny(in.GitReadToken, "\r\n")) {
		writeError(w, 400, "provide a repository read token for JustCD to fetch the private repository")
		return
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
		cfg, _ := json.Marshal(in.Config)
		var oldConfig publishing.Config
		_ = json.Unmarshal(previous.Config, &oldConfig)
		oldCfg, _ := json.Marshal(oldConfig)
		if e != nil || string(cfg) != string(oldCfg) || previous.GitLabURL != in.GitLabURL || previous.Group != in.Group || app.ClusterID != in.ClusterID {
			writeError(w, 400, "source updates must reuse the original publication settings")
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
	if err == nil {
		if in.UpdateApplicationID != "" && registered.ApplicationID != in.UpdateApplicationID {
			writeError(w, 409, "publication does not match this application")
			return
		}
		if registered.Fingerprint != fingerprint {
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
			_ = s.Store.Audit(ctx, currentUser(r).ID, "application.source_uploaded", "application", registered.ApplicationID, map[string]string{"runtime": in.Config.Runtime, "templateVersion": publishing.Version})
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
			writeError(w, 502, "could not protect main before starting CI; retry setup after checking GitLab permissions")
			return
		}
		cred := store.Credential{ID: store.NewID(), WorkspaceID: &in.WorkspaceID, Name: in.Config.Name + " Git read", Kind: "git-https"}
		payload, _ := json.Marshal(map[string]string{"username": "oauth2", "token": in.GitReadToken})
		cred.Cipher, err = security.Encrypt(s.EncryptionKey, payload, "credential:"+cred.ID)
		if err != nil {
			writeStoreError(w, "could not protect Git read credential")
			return
		}
		src := store.GitSource{ID: store.NewID(), WorkspaceID: in.WorkspaceID, Name: in.Config.Name, RepositoryURL: project.RepositoryURL, CredentialID: &cred.ID}
		app := store.Application{Labels: map[string]string{"justcd.io/published": "true"}, ID: store.NewID(), WorkspaceID: in.WorkspaceID, Name: in.Config.Name, SourceID: src.ID, Revision: "main", ManifestPath: "deploy", Renderer: "yaml", ClusterID: in.ClusterID, Namespaces: []store.NamespaceBinding{binding}, SyncPolicy: "manual", PollSeconds: 30, RetryPolicy: store.DefaultRetryPolicy(), Health: "unknown"}
		safeConfig, _ := json.Marshal(in.Config)
		registered = store.PublishedApplication{Config: safeConfig, GitLabURL: in.GitLabURL, Group: in.Group, ApplicationID: app.ID, Fingerprint: fingerprint, RepositoryURL: project.RepositoryURL, ProjectURL: project.WebURL, ProjectID: project.ID}
		if err = s.Store.RegisterPublishedApplication(ctx, cred, src, app, registered); err != nil {
			writeError(w, 409, "could not register publication; the repository is retained. Retry the same upload and settings to resume")
			return
		}
		_ = s.Store.Audit(ctx, currentUser(r).ID, "application.published", "application", app.ID, map[string]string{"workspaceId": in.WorkspaceID, "runtime": in.Config.Runtime, "templateVersion": publishing.Version, "secretMode": in.Config.SecretMode})
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
	writeJSON(w, http.StatusCreated, registered)
}

func (s *Server) getPublication(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, 404, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
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
	writeJSON(w, 200, map[string]any{"publication": p, "workspaceId": app.WorkspaceID, "clusterId": app.ClusterID})
}
