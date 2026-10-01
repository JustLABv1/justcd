package api

import (
	"net/http"
	"path"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (s *Server) startBranchTest(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	var input struct {
		Revision        string `json:"revision"`
		Mode            string `json:"mode"`
		Namespace       string `json:"namespace"`
		ManifestPath    string `json:"manifestPath"`
		ConfirmExisting bool   `json:"confirmExisting"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Revision = strings.TrimSpace(input.Revision)
	if input.Revision == "" || len(input.Revision) > 256 {
		writeError(w, http.StatusBadRequest, "a branch or Git revision is required")
		return
	}
	if input.Mode != "existing" && input.Mode != "isolated" {
		writeError(w, http.StatusBadRequest, "mode must be existing or isolated")
		return
	}
	if input.Mode == "existing" && !input.ConfirmExisting {
		writeError(w, http.StatusBadRequest, "confirm testing against the existing environment and its data")
		return
	}
	if input.Mode == "isolated" {
		if !namespaceNamePattern.MatchString(input.Namespace) || len(input.Namespace) > 63 {
			writeError(w, http.StatusBadRequest, "a valid dedicated preview namespace is required")
			return
		}
		input.ManifestPath = path.Clean(strings.TrimSpace(input.ManifestPath))
		if input.ManifestPath == "." || input.ManifestPath == ".." || path.IsAbs(input.ManifestPath) || strings.HasPrefix(input.ManifestPath, "../") || strings.ContainsAny(input.ManifestPath, "\x00\\") {
			writeError(w, http.StatusBadRequest, "a repository-relative preview overlay path is required")
			return
		}
		if app.Renderer != "helm" && input.ManifestPath == app.ManifestPath {
			writeError(w, http.StatusBadRequest, "choose a separate preview overlay with isolated hostnames and dependencies")
			return
		}
	}
	source, err := s.Store.GitSourceForWorkspace(r.Context(), app.SourceID, app.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusConflict, "Git source is unavailable to this workspace")
		return
	}
	checkout, err := gitops.Fetch(r.Context(), s.Store, s.EncryptionKey, source, input.Revision)
	if err != nil {
		writePlanFailure(w, http.StatusUnprocessableEntity, "could not verify the test branch", app.ID, err)
		return
	}
	checkout.Close()
	if input.Mode == "isolated" {
		cluster, err := s.Store.ClusterByID(r.Context(), app.ClusterID)
		if err != nil {
			writeStoreError(w, "could not load preview cluster")
			return
		}
		credential, err := s.Store.WorkspaceClusterCredential(r.Context(), app.WorkspaceID, app.ClusterID)
		if err != nil {
			writeStoreError(w, "could not load workspace cluster credential")
			return
		}
		clients, err := kube.ForWorkspaceBinding(r.Context(), s.Store, s.EncryptionKey, cluster, credential, false, app.WorkspaceID, input.Namespace)
		if err != nil {
			writePlanFailure(w, http.StatusUnprocessableEntity, "could not verify preview namespace", app.ID, err)
			return
		}
		_, err = clients.Dynamic.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).Get(r.Context(), input.Namespace, metav1.GetOptions{})
		if err == nil {
			writeError(w, http.StatusConflict, "preview namespace already exists in Kubernetes; choose a new dedicated namespace")
			return
		}
		if !apierrors.IsNotFound(err) {
			writePlanFailure(w, http.StatusUnprocessableEntity, "could not verify preview namespace", app.ID, err)
			return
		}
	}
	id, err := s.Store.StartBranchTest(r.Context(), app, input.Revision, input.Mode, input.Namespace, input.ManifestPath)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.branch_test_started", "application", id, map[string]any{"mode": input.Mode, "parentApplicationId": app.ID, "revision": input.Revision})
	updated, err := s.Store.ApplicationByID(r.Context(), id)
	if err != nil {
		writeStoreError(w, "branch test saved but application could not be reloaded")
		return
	}
	writeJSON(w, http.StatusCreated, updated)
}

func (s *Server) resumeBranchTest(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	if _, _, err := s.Store.ReviewByPreviewApplication(r.Context(), app.ID); err == nil {
		writeError(w, http.StatusConflict, "end this deployment through its pull request")
		return
	}
	if err = s.Store.ResumeBranchTest(r.Context(), app.ID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	// Restore current Git configuration before exposing a return plan. A failed
	// discovery leaves reconciliation paused so it cannot apply stale settings.
	if app.RepositoryConfigurationID != "" {
		if err = s.Syncer.ReconcileRepository(r.Context(), app.RepositoryConfigurationID); err != nil {
			s.Logger.ErrorContext(r.Context(), "branch test ended but repository discovery failed", "applicationId", app.ID, "error", err)
			writeError(w, http.StatusConflict, "branch test ended; repository discovery failed. Reconciliation remains paused. Retry repository discovery before creating a return plan.")
			return
		}
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.branch_test_resumed", "application", app.ID, nil)
	updated, err := s.Store.ApplicationByID(r.Context(), app.ID)
	if err != nil {
		writeStoreError(w, "tracked source restored but application could not be reloaded")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
