package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) deleteCluster(w http.ResponseWriter, r *http.Request) {
	cluster, err := s.Store.ClusterByID(r.Context(), r.PathValue("clusterID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "cluster not found")
		return
	}
	if cluster.WorkspaceID != nil {
		if !s.requireWorkspaceRole(w, r, *cluster.WorkspaceID, "owner") {
			return
		}
	} else if !currentUser(r).IsAdmin {
		writeError(w, http.StatusForbidden, "instance administrator role required to delete a legacy cluster")
		return
	}
	s.deleteConnection(w, r, "cluster", cluster.ID, "Cluster is still used by applications, managed resources, or active shares. Remove these references before deleting the connection.")
}

func (s *Server) deleteGitSource(w http.ResponseWriter, r *http.Request) {
	source, err := s.Store.GitSourceByID(r.Context(), r.PathValue("sourceID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Git source not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, source.WorkspaceID, "owner") {
		return
	}
	s.deleteConnection(w, r, "git_source", source.ID, "Git source is still used by applications, application groups, repository configurations, or active shares. Remove these references before deleting the source.")
}

func (s *Server) deleteCredential(w http.ResponseWriter, r *http.Request) {
	credential, err := s.Store.CredentialByID(r.Context(), r.PathValue("credentialID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "credential not found")
		return
	}
	if credential.WorkspaceID != nil {
		if !s.requireWorkspaceRole(w, r, *credential.WorkspaceID, "owner") {
			return
		}
	} else if !currentUser(r).IsAdmin {
		writeError(w, http.StatusForbidden, "instance administrator role required")
		return
	}
	s.deleteConnection(w, r, "credential", credential.ID, "Credential is still assigned to a cluster, namespace, Git source, or shared connection. Replace or remove its assignments before deleting it.")
}

func (s *Server) deleteConnection(w http.ResponseWriter, r *http.Request, kind, id, conflict string) {
	if err := s.Store.DeleteConnection(r.Context(), kind, id); err != nil {
		writeConnectionDeletionError(w, err, conflict)
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, kind+".deleted", kind, id, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteNamespaceBinding(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	if !s.requireWorkspaceRole(w, r, workspaceID, "owner") {
		return
	}
	clusterID, namespace := r.PathValue("clusterID"), r.PathValue("namespace")
	if err := s.Store.DeleteNamespaceBinding(r.Context(), workspaceID, clusterID, namespace); err != nil {
		writeConnectionDeletionError(w, err, "Namespace access is still used by an application or managed resources. Remove these references before removing the binding.")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "namespace_binding.deleted", "cluster", clusterID, map[string]string{"workspaceId": workspaceID, "namespace": namespace})
	w.WriteHeader(http.StatusNoContent)
}

func writeConnectionDeletionError(w http.ResponseWriter, err error, conflict string) {
	switch {
	case errors.Is(err, store.ErrConnectionInUse):
		writeError(w, http.StatusConflict, conflict)
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "connection not found")
	default:
		writeStoreError(w, "could not delete connection")
	}
}

func (s *Server) deleteRepositoryConfiguration(w http.ResponseWriter, r *http.Request) {
	repository, err := s.Store.RepositoryConfigurationByID(r.Context(), r.PathValue("repositoryID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "repository configuration not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, repository.WorkspaceID, "owner") {
		return
	}
	s.deleteConnection(w, r, "repository_configuration", repository.ID, "Repository discovery still manages applications. Pause discovery and remove those applications before removing this connection.")
}
