package api

import (
	"database/sql"
	"errors"
	"net/http"
	"path"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("projectID")
	if !s.requireProjectRole(w, r, id, "owner") {
		return
	}
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name, input.Description = strings.TrimSpace(input.Name), strings.TrimSpace(input.Description)
	if input.Name == "" || len(input.Name) > 100 || len(input.Description) > 500 {
		writeError(w, http.StatusBadRequest, "project name must be 1–100 characters and description at most 500 characters")
		return
	}
	if err := s.Store.UpdateProject(r.Context(), id, input.Name, input.Description); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		writeError(w, http.StatusConflict, "could not update project")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "project.updated", "project", id, map[string]string{"name": input.Name})
	project, err := s.Store.ProjectByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "project updated but could not reload it")
		return
	}
	project.Role = "owner"
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("projectID")
	if !s.requireProjectRole(w, r, id, "owner") {
		return
	}
	policy := r.URL.Query().Get("resources")
	if policy != "keep" && policy != "delete" {
		writeError(w, http.StatusBadRequest, "choose resources=keep or resources=delete")
		return
	}
	var managed int
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM managed_resources m JOIN applications a ON a.id=m.application_id WHERE a.project_id=$1`, id).Scan(&managed); err != nil {
		writeError(w, http.StatusInternalServerError, "could not inspect project resources")
		return
	}
	if policy == "delete" && managed > 0 {
		apps, err := s.Store.ListApplications(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list project applications")
			return
		}
		plans := make([]map[string]any, 0)
		for _, app := range apps {
			var count int
			if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM managed_resources WHERE application_id=$1`, app.ID).Scan(&count); err != nil {
				writeError(w, http.StatusInternalServerError, "could not inspect application resources")
				return
			}
			if count == 0 {
				continue
			}
			record, err := s.Syncer.BuildDecommissionPlan(r.Context(), app, currentUser(r).ID)
			if err != nil {
				s.Logger.Warn("project decommission plan failed", "projectId", id, "applicationId", app.ID, "error", err)
				writeError(w, http.StatusUnprocessableEntity, "could not prepare all application deletion plans; inspect cluster permissions and try again")
				return
			}
			plans = append(plans, map[string]any{"applicationId": app.ID, "applicationName": app.Name, "plan": toPlanView(record), "managedResources": count})
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"deleted": false, "plans": plans})
		return
	}
	apps, orphaned, err := s.Store.DeleteProjectKeepingResources(r.Context(), id, policy == "delete")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		writeError(w, http.StatusConflict, "project has an active sync or connections still in use")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "project.deleted", "project", id, map[string]any{"resourcePolicy": policy, "applications": apps, "orphanedResources": orphaned})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "applications": apps, "orphanedResources": orphaned})
}

func (s *Server) updateApplication(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "owner") {
		return
	}
	var input struct {
		Name                       string   `json:"name"`
		SourceID                   string   `json:"sourceId"`
		Revision                   string   `json:"revision"`
		ManifestPath               string   `json:"manifestPath"`
		Renderer                   string   `json:"renderer"`
		KustomizeHelmEnabled       bool     `json:"kustomizeHelmEnabled"`
		KustomizeNamespaceOverride bool     `json:"kustomizeNamespaceOverride"`
		ClusterID                  string   `json:"clusterId"`
		Namespaces                 []string `json:"namespaces"`
		SyncPolicy                 string   `json:"syncPolicy"`
		PollSeconds                int      `json:"pollSeconds"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name, input.Revision = strings.TrimSpace(input.Name), strings.TrimSpace(input.Revision)
	if !resourceNamePattern.MatchString(input.Name) || input.Revision == "" || len(input.Revision) > 256 || strings.HasPrefix(input.Revision, "-") || strings.ContainsAny(input.Revision, " \t\r\n\x00") {
		writeError(w, http.StatusBadRequest, "invalid application name or Git revision")
		return
	}
	input.ManifestPath = path.Clean(strings.TrimSpace(input.ManifestPath))
	if input.ManifestPath == "." || input.ManifestPath == ".." || strings.HasPrefix(input.ManifestPath, "../") || path.IsAbs(input.ManifestPath) {
		writeError(w, http.StatusBadRequest, "manifestPath must be repository-relative")
		return
	}
	if input.Renderer != "yaml" && input.Renderer != "kustomize" && input.Renderer != "helm" {
		writeError(w, http.StatusBadRequest, "invalid renderer")
		return
	}
	if (input.KustomizeHelmEnabled || input.KustomizeNamespaceOverride) && input.Renderer != "kustomize" {
		writeError(w, http.StatusBadRequest, "Kustomize settings require the Kustomize renderer")
		return
	}
	if input.SyncPolicy != "manual" && input.SyncPolicy != "auto-safe" {
		writeError(w, http.StatusBadRequest, "invalid sync policy")
		return
	}
	if input.PollSeconds < 30 || input.PollSeconds > 86400 {
		writeError(w, http.StatusBadRequest, "pollSeconds must be between 30 and 86400")
		return
	}
	source, err := s.Store.GitSourceByID(r.Context(), input.SourceID)
	if err != nil || source.ProjectID != app.ProjectID {
		writeError(w, http.StatusBadRequest, "Git source is not available to this project")
		return
	}
	if _, err := s.Store.ClusterByID(r.Context(), input.ClusterID); err != nil {
		writeError(w, http.StatusBadRequest, "cluster not found")
		return
	}
	if len(input.Namespaces) == 0 || (input.Renderer == "helm" && len(input.Namespaces) != 1) || (input.KustomizeNamespaceOverride && len(input.Namespaces) != 1) {
		writeError(w, http.StatusBadRequest, "invalid namespace selection")
		return
	}
	bindings := make([]store.NamespaceBinding, 0, len(input.Namespaces))
	seen := map[string]bool{}
	for _, namespace := range input.Namespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" || seen[namespace] {
			writeError(w, http.StatusBadRequest, "namespace names must be nonempty and unique")
			return
		}
		seen[namespace] = true
		binding, err := s.Store.NamespaceBinding(r.Context(), app.ProjectID, input.ClusterID, namespace)
		if err != nil {
			writeError(w, http.StatusBadRequest, "every namespace must have a project binding")
			return
		}
		bindings = append(bindings, binding)
	}
	app.Name, app.SourceID, app.Revision, app.ManifestPath, app.Renderer = input.Name, input.SourceID, input.Revision, input.ManifestPath, input.Renderer
	app.KustomizeHelmEnabled, app.KustomizeNamespaceOverride, app.ClusterID, app.Namespaces = input.KustomizeHelmEnabled, input.KustomizeNamespaceOverride, input.ClusterID, bindings
	app.SyncPolicy, app.PollSeconds = input.SyncPolicy, input.PollSeconds
	if err := s.Store.UpdateApplication(r.Context(), app); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.updated", "application", app.ID, map[string]any{"name": app.Name, "sourceId": app.SourceID, "revision": app.Revision, "manifestPath": app.ManifestPath, "renderer": app.Renderer, "clusterId": app.ClusterID, "namespaces": input.Namespaces, "syncPolicy": app.SyncPolicy})
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) deleteApplication(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "owner") {
		return
	}
	policy := r.URL.Query().Get("resources")
	if policy != "keep" && policy != "delete" {
		writeError(w, http.StatusBadRequest, "choose resources=keep or resources=delete")
		return
	}
	if policy == "delete" {
		var count int
		if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM managed_resources WHERE application_id=$1`, app.ID).Scan(&count); err != nil {
			writeError(w, http.StatusInternalServerError, "could not inspect managed resources")
			return
		}
		if count > 0 {
			record, err := s.Syncer.BuildDecommissionPlan(r.Context(), app, currentUser(r).ID)
			if err != nil {
				s.Logger.Warn("decommission plan failed", "applicationId", app.ID, "error", err)
				writeError(w, http.StatusUnprocessableEntity, "could not build a safe deletion plan; check cluster permissions and server logs")
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]any{"deleted": false, "plan": toPlanView(record)})
			return
		}
	}
	orphaned, err := s.Store.DeleteApplicationKeepingResources(r.Context(), app.ID, policy == "delete")
	if err != nil {
		writeError(w, http.StatusConflict, "application has an active sync or could not be removed")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.deleted", "application", app.ID, map[string]any{"resourcePolicy": policy, "orphanedResources": orphaned})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "orphanedResources": orphaned})
}

func (s *Server) cancelApplicationDecommission(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "owner") {
		return
	}
	if err := s.Store.CancelApplicationDecommission(r.Context(), app.ID); err != nil {
		writeError(w, http.StatusConflict, "cannot cancel while a sync is running")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.decommission_cancelled", "application", app.ID, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}
