package api

import (
	"database/sql"
	"net/http"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) listWorkspaceConnectionShares(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	items, err := s.Store.ListWorkspaceConnectionShares(r.Context(), workspaceID)
	if err != nil {
		writeStoreError(w, "could not load connection sharing")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) shareCluster(w http.ResponseWriter, r *http.Request) {
	var input struct {
		TargetWorkspaceID string `json:"targetWorkspaceId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cluster, err := s.Store.ClusterByID(r.Context(), r.PathValue("clusterID"))
	if err != nil || cluster.WorkspaceID == nil {
		writeError(w, http.StatusNotFound, "workspace-owned cluster not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, *cluster.WorkspaceID, "owner") {
		return
	}
	if input.TargetWorkspaceID == "" || input.TargetWorkspaceID == *cluster.WorkspaceID {
		writeError(w, http.StatusBadRequest, "a different target workspace is required")
		return
	}
	if _, err := s.Store.WorkspaceByID(r.Context(), input.TargetWorkspaceID); err != nil {
		writeError(w, http.StatusNotFound, "target workspace not found")
		return
	}
	share := store.WorkspaceConnectionShare{ID: store.NewID(), Kind: "cluster", ResourceID: cluster.ID, ResourceName: cluster.Name, OwnerWorkspaceID: *cluster.WorkspaceID, TargetWorkspaceID: input.TargetWorkspaceID}
	if err := s.Store.CreateWorkspaceClusterShare(r.Context(), share); err != nil {
		writeError(w, http.StatusConflict, "could not offer cluster access; an offer may already exist")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "workspace_connection_share.offered", "cluster", cluster.ID, map[string]string{"ownerWorkspaceId": share.OwnerWorkspaceID, "targetWorkspaceId": share.TargetWorkspaceID})
	writeJSON(w, http.StatusCreated, share)
}

func (s *Server) shareGitSource(w http.ResponseWriter, r *http.Request) {
	var input struct {
		TargetWorkspaceID string  `json:"targetWorkspaceId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	source, err := s.Store.GitSourceByID(r.Context(), r.PathValue("sourceID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Git source not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, source.WorkspaceID, "owner") {
		return
	}
	if input.TargetWorkspaceID == "" || input.TargetWorkspaceID == source.WorkspaceID {
		writeError(w, http.StatusBadRequest, "a different target workspace is required")
		return
	}
	if _, err := s.Store.WorkspaceByID(r.Context(), input.TargetWorkspaceID); err != nil {
		writeError(w, http.StatusNotFound, "target workspace not found")
		return
	}
	share := store.WorkspaceConnectionShare{ID: store.NewID(), Kind: "git-source", ResourceID: source.ID, ResourceName: source.Name, RepositoryURL: source.RepositoryURL, OwnerWorkspaceID: source.WorkspaceID, TargetWorkspaceID: input.TargetWorkspaceID}
	if err := s.Store.CreateWorkspaceGitSourceShare(r.Context(), share); err != nil {
		writeError(w, http.StatusConflict, "could not offer Git source access; an offer may already exist")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "workspace_connection_share.offered", "git_source", source.ID, map[string]string{"ownerWorkspaceId": share.OwnerWorkspaceID, "targetWorkspaceId": share.TargetWorkspaceID})
	writeJSON(w, http.StatusCreated, share)
}

func (s *Server) acceptWorkspaceConnectionShare(w http.ResponseWriter, r *http.Request) {
	s.decideWorkspaceConnectionShare(w, r, true)
}

func (s *Server) declineWorkspaceConnectionShare(w http.ResponseWriter, r *http.Request) {
	s.decideWorkspaceConnectionShare(w, r, false)
}

func (s *Server) decideWorkspaceConnectionShare(w http.ResponseWriter, r *http.Request, accept bool) {
	var input struct {
		WorkspaceID string `json:"workspaceId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireWorkspaceRole(w, r, input.WorkspaceID, "owner") {
		return
	}
	if err := s.Store.DecideWorkspaceConnectionShare(r.Context(), r.PathValue("shareID"), input.WorkspaceID, accept); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "pending connection offer not found for this workspace")
			return
		}
		writeStoreError(w, "could not update connection offer")
		return
	}
	status := "declined"
	if accept {
		status = "accepted"
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "workspace_connection_share."+status, "connection_share", r.PathValue("shareID"), map[string]string{"workspaceId": input.WorkspaceID})
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) revokeWorkspaceConnectionShare(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "owner") {
		return
	}
	if err := s.Store.RevokeWorkspaceConnectionShare(r.Context(), r.PathValue("shareID"), workspaceID); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "active connection share not found for this workspace")
			return
		}
		writeStoreError(w, "could not revoke connection sharing")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "workspace_connection_share.revoked", "connection_share", r.PathValue("shareID"), map[string]string{"workspaceId": workspaceID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setSharedGitCredential(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorkspaceID  string  `json:"workspaceId"`
		CredentialID *string `json:"credentialId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireWorkspaceRole(w, r, input.WorkspaceID, "owner") {
		return
	}
	if input.CredentialID != nil {
		credential, err := s.Store.CredentialByID(r.Context(), *input.CredentialID)
		if err != nil || credential.WorkspaceID == nil || *credential.WorkspaceID != input.WorkspaceID || (credential.Kind != "git-ssh" && credential.Kind != "git-https") {
			writeError(w, http.StatusBadRequest, "credential must be a Git credential owned by this workspace")
			return
		}
	}
	if err := s.Store.SetWorkspaceGitSourceShareCredential(r.Context(), r.PathValue("shareID"), input.WorkspaceID, input.CredentialID); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "accepted Git source share not found for this workspace")
			return
		}
		writeStoreError(w, "could not update the shared Git credential")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentialId": input.CredentialID})
}
