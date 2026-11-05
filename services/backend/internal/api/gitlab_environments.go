package api

import (
	"net/http"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) repositoryEnvironmentSettings(w http.ResponseWriter, r *http.Request) {
	repo, err := s.Store.RepositoryConfigurationByID(r.Context(), r.PathValue("repositoryID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, repo.WorkspaceID, "viewer") {
		return
	}
	settings, err := s.Store.EnvironmentSettings(r.Context(), repo.ID)
	if err != nil {
		writeStoreError(w, "could not load environment settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) saveRepositoryEnvironmentSettings(w http.ResponseWriter, r *http.Request) {
	repo, err := s.Store.RepositoryConfigurationByID(r.Context(), r.PathValue("repositoryID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, repo.WorkspaceID, "owner") {
		return
	}
	var input store.EnvironmentSettings
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.SaveEnvironmentSettings(r.Context(), repo.ID, input); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "repository.environments.updated", "repository_configuration", repo.ID, map[string]any{"enabled": input.Enabled})
	settings, err := s.Store.EnvironmentSettings(r.Context(), repo.ID)
	if err != nil {
		writeStoreError(w, "could not reload environment settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// applicationEnvironment describes where a repository-managed application is
// reported in GitLab and the outcome of the latest report.
func (s *Server) applicationEnvironment(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "viewer") {
		return
	}
	result := map[string]any{"reporting": false}
	if app.RepositoryConfigurationID == "" || len(app.Namespaces) == 0 {
		writeJSON(w, http.StatusOK, result)
		return
	}
	settings, err := s.Store.EnvironmentSettings(r.Context(), app.RepositoryConfigurationID)
	if err != nil {
		writeStoreError(w, "could not load environment settings")
		return
	}
	override, err := s.Store.ApplicationEnvironmentOverride(r.Context(), app.ID)
	if err != nil {
		writeStoreError(w, "could not load environment")
		return
	}
	result["reporting"] = settings.Enabled
	result["environment"] = store.ResolveEnvironment(override, app.ClusterName, app.Namespaces[0].Namespace)
	result["declared"] = !override.IsZero()
	if settings.Enabled {
		if latest, err := s.Store.LatestEnvironmentDeployment(r.Context(), app.ID); err == nil && latest != nil {
			result["latestDeployment"] = latest
		}
	}
	writeJSON(w, http.StatusOK, result)
}
