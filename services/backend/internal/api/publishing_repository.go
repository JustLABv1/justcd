package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/store"
	"net/http"
	"path"
	"strings"
	"time"
)

type repositoryPublishingInput struct {
	WorkspaceID  string            `json:"workspaceId"`
	SourceID     string            `json:"sourceId"`
	Revision     string            `json:"revision"`
	AppDir       string            `json:"appDir"`
	ProfileID    string            `json:"profileId"`
	Name         string            `json:"name"`
	EntryPoint   string            `json:"entryPoint"`
	BuildCommand string            `json:"buildCommand"`
	OutputDir    string            `json:"outputDir"`
	GitLabToken  string            `json:"gitlabToken"`
	ClusterID    string            `json:"clusterId"`
	Config       publishing.Config `json:"config"`
	Image        string            `json:"image"`
	PlanHash     string            `json:"planHash"`
}
type repositoryPublishingContext struct {
	input    repositoryPublishingInput
	git      publishing.GitLab
	snapshot publishing.RepositorySnapshot
	config   publishing.Config
}

func (s *Server) repositoryPublishingContext(w http.ResponseWriter, r *http.Request) (repositoryPublishingContext, bool) {
	var c repositoryPublishingContext
	in := &c.input
	if err := decodeJSON(w, r, in); err != nil {
		writeError(w, 400, err.Error())
		return c, false
	}
	minimum := "owner"
	if in.ProfileID != "" {
		minimum = "deployer"
	}
	if !s.requireWorkspaceRole(w, r, in.WorkspaceID, minimum) {
		return c, false
	}
	// Repository writes require an owned connection, never a connection shared by another workspace.
	source, err := s.Store.GitSourceByID(r.Context(), in.SourceID)
	if err != nil || source.WorkspaceID != in.WorkspaceID {
		writeError(w, 404, "select a Git source owned by this workspace")
		return c, false
	}
	origin, project, err := publishing.RepositoryLocation(source.RepositoryURL)
	if err != nil {
		writeError(w, 400, err.Error())
		return c, false
	}
	if in.ProfileID != "" {
		p := publishInput{WorkspaceID: in.WorkspaceID, ProfileID: in.ProfileID, Name: in.Name, EntryPoint: in.EntryPoint, BuildCommand: in.BuildCommand, OutputDir: in.OutputDir, Config: in.Config, ClusterID: in.ClusterID, GitLabToken: in.GitLabToken}
		profile, ok := s.resolvePublishingProfile(w, r, &p)
		if !ok {
			return c, false
		}
		if origin != strings.TrimRight(profile.Settings.GitLabURL, "/") || !strings.HasPrefix(project, profile.Settings.Group+"/") {
			writeError(w, 400, "repository must be inside the publishing profile's GitLab origin and group")
			return c, false
		}
		if in.Image != "" && !strings.HasPrefix(in.Image, profile.Settings.RegistryPrefix+"/") {
			writeError(w, 400, "published image must use the publishing profile's registry prefix")
			return c, false
		}
		c.config = p.Config
		in.ClusterID = p.ClusterID
		in.GitLabToken = p.GitLabToken
	} else {
		c.config = in.Config
	}
	c.git = publishing.GitLab{URL: origin, Token: in.GitLabToken, HTTP: s.PublishingHTTP}
	if err := c.git.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return c, false
	}
	if in.AppDir == "" {
		in.AppDir = "."
	}
	c.snapshot, err = c.git.InspectRepository(r.Context(), project, in.Revision)
	if err != nil {
		writeError(w, 400, err.Error())
		return c, false
	}
	return c, true
}
func (s *Server) inspectPublishingRepository(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	c, ok := s.repositoryPublishingContext(w, r)
	if !ok {
		return
	}
	paths := []string{}
	docker, ci := false, c.snapshot.Project.CIConfigPath != "" && c.snapshot.Project.CIConfigPath != ".gitlab-ci.yml"
	for _, e := range c.snapshot.Entries {
		regular := e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755" || e.Mode == "")
		if regular && e.Path == path.Join(c.input.AppDir, "Dockerfile") {
			docker = true
		}
		if regular && e.Path == ".gitlab-ci.yml" {
			ci = true
		}
		if regular && (strings.HasSuffix(e.Path, ".yaml") || strings.HasSuffix(e.Path, ".yml")) && (strings.HasPrefix(e.Path, "deploy/") || strings.HasPrefix(e.Path, "k8s/") || strings.HasPrefix(e.Path, "manifests/") || path.Base(e.Path) == "Chart.yaml" || path.Base(e.Path) == "kustomization.yaml") {
			paths = append(paths, e.Path)
		}
	}
	writeJSON(w, 200, map[string]any{"sha": c.snapshot.SHA, "hasDockerfile": docker, "hasPipeline": ci, "manifestPaths": paths})
}
func (s *Server) planPublishingRepository(w http.ResponseWriter, r *http.Request) {
	s.preparePublishingRepository(w, r, false)
}
func (s *Server) publishRepository(w http.ResponseWriter, r *http.Request) {
	s.preparePublishingRepository(w, r, true)
}
func (s *Server) preparePublishingRepository(w http.ResponseWriter, r *http.Request, apply bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	c, ok := s.repositoryPublishingContext(w, r)
	if !ok {
		return
	}
	in := c.input
	if !s.requireWorkspaceCluster(w, r, in.WorkspaceID, in.ClusterID) {
		return
	}
	binding, err := s.Store.NamespaceBinding(r.Context(), in.WorkspaceID, in.ClusterID, c.config.Namespace)
	if err != nil {
		writeError(w, 400, "select an existing workspace namespace binding")
		return
	}
	plan, err := publishing.PlanRepository(c.config, c.snapshot, in.AppDir, in.Image)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	sum := sha256.Sum256([]byte(plan.Hash + "\n" + in.WorkspaceID + "\n" + in.SourceID + "\n" + in.ClusterID + "\n" + in.ProfileID))
	plan.Hash = hex.EncodeToString(sum[:])
	if !apply {
		writeJSON(w, 200, plan)
		return
	}
	if plan.NeedsImage {
		writeError(w, 400, "supply the image digest produced by the existing pipeline")
		return
	}
	if in.PlanHash == "" || in.PlanHash != plan.Hash {
		writeError(w, 409, "repository, profile, or settings changed; review a fresh plan before creating the merge request")
		return
	}
	// Create an ordinary Git application, never a managed ZIP publication: subsequent
	// edits remain under the existing repository/pipeline's ownership.
	apps, err := s.Store.ListApplications(r.Context(), in.WorkspaceID)
	if err != nil {
		writeStoreError(w, "could not check existing applications")
		return
	}
	var app store.Application
	for _, existing := range apps {
		if existing.Name == c.config.Name && existing.ClusterID == in.ClusterID {
			if existing.SourceID != in.SourceID || existing.Revision != in.Revision || existing.ManifestPath != plan.ManifestPath || existing.ClusterID != in.ClusterID || len(existing.Namespaces) != 1 || existing.Namespaces[0].Namespace != c.config.Namespace || existing.Labels["justcd.io/onboarding"] != plan.Hash[:40] || existing.Decommissioning {
				writeError(w, 409, "an application with this name already exists; choose another name")
				return
			}
			app = existing
		}
	}
	mr, err := c.git.ProposeRepository(r.Context(), c.snapshot, plan)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if app.ID == "" {
		app = store.Application{ID: store.NewID(), WorkspaceID: in.WorkspaceID, Name: c.config.Name, SourceID: in.SourceID, Revision: in.Revision, ManifestPath: plan.ManifestPath, Renderer: "yaml", ClusterID: in.ClusterID, Namespaces: []store.NamespaceBinding{binding}, SyncPolicy: "manual", PollSeconds: 300, RetryPolicy: store.DefaultRetryPolicy(), Health: "unknown", Labels: map[string]string{"justcd.io/onboarding": plan.Hash[:40]}}
		if err = s.Store.CreateApplication(r.Context(), app); err != nil {
			writeJSON(w, 409, map[string]any{"error": "merge request created but application registration failed; retry with the same reviewed plan", "mergeRequestUrl": mr.WebURL})
			return
		}
		_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.repository_onboarded", "application", app.ID, map[string]string{"sourceId": in.SourceID, "revision": in.Revision, "profileId": in.ProfileID, "mergeRequestUrl": mr.WebURL})
	}
	writeJSON(w, 201, map[string]any{"applicationId": app.ID, "mergeRequestUrl": mr.WebURL, "manifestPath": plan.ManifestPath, "notes": plan.Notes})
}
