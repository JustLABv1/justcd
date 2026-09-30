package api

import (
	"net/http"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) listRepositoryConfigurations(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	items, err := s.Store.ListRepositoryConfigurations(r.Context(), workspaceID, false)
	if err != nil {
		writeStoreError(w, "could not list repository configurations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) createRepositoryConfiguration(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorkspaceID string `json:"workspaceId"`
		SourceID    string `json:"sourceId"`
		Revision    string `json:"revision"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireWorkspaceRole(w, r, input.WorkspaceID, "owner") {
		return
	}
	if !s.requireWorkspaceGitSource(w, r, input.WorkspaceID, input.SourceID) {
		return
	}
	input.Revision = strings.TrimSpace(input.Revision)
	if input.Revision == "" || len(input.Revision) > 256 || strings.HasPrefix(input.Revision, "-") || strings.ContainsAny(input.Revision, "\x00\r\n") {
		writeError(w, http.StatusBadRequest, "a valid tracked Git branch, tag, or commit is required")
		return
	}
	repository := store.RepositoryConfiguration{ID: store.NewID(), WorkspaceID: input.WorkspaceID, SourceID: input.SourceID, Revision: input.Revision, Enabled: true}
	if err := s.Store.CreateRepositoryConfiguration(r.Context(), repository); err != nil {
		writeError(w, http.StatusConflict, "this repository and revision may already be connected to the workspace")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "repository.configuration.created", "repository_configuration", repository.ID, map[string]any{"workspaceId": repository.WorkspaceID, "sourceId": repository.SourceID, "revision": repository.Revision})
	// Persist failed discovery too, so diagnostics and retries remain available.
	_ = s.Syncer.ReconcileRepository(r.Context(), repository.ID)
	repository, err := s.Store.RepositoryConfigurationByID(r.Context(), repository.ID)
	if err != nil {
		writeStoreError(w, "could not load repository configuration")
		return
	}
	writeJSON(w, http.StatusCreated, repository)
}
func (s *Server) reconcileRepositoryConfiguration(w http.ResponseWriter, r *http.Request) {
	repository, err := s.Store.RepositoryConfigurationByID(r.Context(), r.PathValue("repositoryID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "repository configuration not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, repository.WorkspaceID, "owner") {
		return
	}
	if err := s.Syncer.ReconcileRepository(r.Context(), repository.ID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	repository, err = s.Store.RepositoryConfigurationByID(r.Context(), repository.ID)
	if err != nil {
		writeStoreError(w, "could not load repository configuration")
		return
	}
	writeJSON(w, http.StatusOK, repository)
}
func (s *Server) updateRepositoryConfiguration(w http.ResponseWriter, r *http.Request) {
	repository, err := s.Store.RepositoryConfigurationByID(r.Context(), r.PathValue("repositoryID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "repository configuration not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, repository.WorkspaceID, "owner") {
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	if err := s.Store.SetRepositoryConfigurationEnabled(r.Context(), repository.ID, *input.Enabled); err != nil {
		writeStoreError(w, "could not update repository discovery")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "repository.configuration.updated", "repository_configuration", repository.ID, map[string]any{"enabled": *input.Enabled})
	repository, err = s.Store.RepositoryConfigurationByID(r.Context(), repository.ID)
	if err != nil {
		writeStoreError(w, "could not load repository configuration")
		return
	}
	writeJSON(w, http.StatusOK, repository)
}
