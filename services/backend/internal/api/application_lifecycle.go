package api

import (
	"database/sql"
	"errors"
	"net/http"
	"path"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	"github.com/justlab/justcd/services/backend/internal/syncer"
)

func (s *Server) retryApplication(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "deployer") {
		return
	}
	if _, _, err := s.Store.ReviewByPreviewApplication(r.Context(), app.ID); err == nil {
		writeError(w, http.StatusConflict, "preview retries must be handled through pull request review")
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeStoreError(w, "could not verify preview policy")
		return
	}
	if app.Decommissioning {
		writeError(w, http.StatusConflict, "application decommissioning must be resumed or cancelled through its reviewed deletion plan")
		return
	}
	var input struct {
		OperationID string `json:"operationId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.OperationID != "" {
		operation, err := s.Store.OperationByID(r.Context(), input.OperationID)
		if err != nil || operation.ApplicationID != app.ID {
			writeError(w, http.StatusNotFound, "failed sync operation not found")
			return
		}
		if operation.Status != "failed" || operation.Type != "sync" {
			writeError(w, http.StatusConflict, "only a failed sync operation can be retried; rebuild rollback plans separately")
			return
		}
		if operation.PlanID == nil {
			writeError(w, http.StatusConflict, "failed operation has no plan to verify")
			return
		}
		failedPlan, err := s.Store.PlanByID(r.Context(), *operation.PlanID)
		if err != nil {
			writeError(w, http.StatusConflict, "failed operation plan is unavailable; review the application before retrying")
			return
		}
		if failedPlan.Plan.Decommission {
			writeError(w, http.StatusConflict, "decommissioning must be resumed or cancelled through its reviewed deletion plan")
			return
		}
	} else if app.RetryTerminalReason == "" && app.RetryNextAt == nil {
		writeError(w, http.StatusConflict, "there is no failed reconciliation waiting for retry")
		return
	}
	active, err := s.Store.ApplicationHasActiveOperation(r.Context(), app.ID)
	if err != nil {
		writeStoreError(w, "could not check active application operations")
		return
	}
	if active {
		writeError(w, http.StatusConflict, "application already has an active operation")
		return
	}
	if err := s.Store.ResetApplicationRetry(r.Context(), app.ID); err != nil {
		writeStoreError(w, "could not reset reconciliation retry state")
		return
	}
	record, err := s.Syncer.BuildPlan(r.Context(), app.ID, currentUser(r).ID)
	if err != nil {
		classification := syncer.ClassifyRetryError(err)
		terminalReason := classification.TerminalReason
		if terminalReason == "" {
			terminalReason = "manual_retry_failed"
		}
		_ = s.Store.RecordApplicationRetry(r.Context(), app.ID, 0, classification.ErrorCode, nil, terminalReason)
		if app.SyncPolicy == "auto-safe" {
			_ = s.Store.PauseAutoSync(r.Context(), app.ID)
		}
		writePlanFailure(w, http.StatusUnprocessableEntity, "could not rebuild a fresh reconciliation plan", app.ID, err)
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "sync.retry_requested", "application", app.ID, map[string]any{"planId": record.ID, "replacesFailedOperationId": input.OperationID})
	if len(record.Plan.Changes) == 0 {
		if err := s.Store.SetPlanStatus(r.Context(), record.ID, "applied"); err != nil {
			writeStoreError(w, "could not record the up-to-date retry plan")
			return
		}
		if err := s.Store.MarkApplicationSynced(r.Context(), app.ID, record.Plan.Revision, "synced"); err != nil {
			writeStoreError(w, "could not mark the application synced")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "up_to_date", "plan": toPlanView(record)})
		return
	}
	if record.Plan.RequiresApproval {
		if app.SyncPolicy == "auto-safe" {
			_ = s.Store.RecordApplicationRetry(r.Context(), app.ID, 0, "plan.approval_required", nil, "approval_required")
			_ = s.Store.PauseAutoSync(r.Context(), app.ID)
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "review_required", "plan": toPlanView(record)})
		return
	}
	operation, err := s.Syncer.Apply(r.Context(), record.ID, currentUser(r).ID, "")
	if err != nil {
		writeError(w, http.StatusConflict, "fresh plan could not be queued; review the current plan before retrying")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "operation": operation, "plan": toPlanView(record)})
}

func (s *Server) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, id, "owner") {
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
		writeError(w, http.StatusBadRequest, "workspace name must be 1–100 characters and description at most 500 characters")
		return
	}
	if err := s.Store.UpdateWorkspace(r.Context(), id, input.Name, input.Description); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		writeError(w, http.StatusConflict, "could not update workspace")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "workspace.updated", "workspace", id, map[string]string{"name": input.Name, "description": input.Description})
	workspace, err := s.Store.WorkspaceByID(r.Context(), id)
	if err != nil {
		writeStoreError(w, "workspace updated but could not reload it")
		return
	}
	workspace.Role = "owner"
	writeJSON(w, http.StatusOK, workspace)
}

func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, id, "owner") {
		return
	}
	policy := r.URL.Query().Get("resources")
	if policy != "keep" && policy != "delete" {
		writeError(w, http.StatusBadRequest, "choose resources=keep or resources=delete")
		return
	}
	var managed int
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM managed_resources m JOIN applications a ON a.id=m.application_id WHERE a.workspace_id=$1`, id).Scan(&managed); err != nil {
		writeStoreError(w, "could not inspect workspace resources")
		return
	}
	if policy == "delete" && managed > 0 {
		apps, err := s.Store.ListApplications(r.Context(), id)
		if err != nil {
			writeStoreError(w, "could not list workspace applications")
			return
		}
		plans := make([]map[string]any, 0)
		for _, app := range apps {
			var count int
			if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM managed_resources WHERE application_id=$1`, app.ID).Scan(&count); err != nil {
				writeStoreError(w, "could not inspect application resources")
				return
			}
			if count == 0 {
				continue
			}
			record, err := s.Syncer.BuildDecommissionPlan(r.Context(), app, currentUser(r).ID)
			if err != nil {
				s.Logger.Warn("workspace decommission plan failed", "workspaceId", id, "applicationId", app.ID, "error", err)
				writeError(w, http.StatusUnprocessableEntity, "could not prepare all application deletion plans; inspect cluster permissions and try again")
				return
			}
			plans = append(plans, map[string]any{"applicationId": app.ID, "applicationName": app.Name, "plan": toPlanView(record), "managedResources": count})
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"deleted": false, "plans": plans})
		return
	}
	apps, orphaned, err := s.Store.DeleteWorkspaceKeepingResources(r.Context(), id, policy == "delete")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		writeError(w, http.StatusConflict, "workspace has an active sync or connections still in use")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "workspace.deleted", "workspace", id, map[string]any{"resourcePolicy": policy, "applications": apps, "orphanedResources": orphaned})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "applications": apps, "orphanedResources": orphaned})
}

func (s *Server) updateApplication(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	if app.RepositoryConfigurationID != "" {
		writeError(w, http.StatusConflict, "application configuration is managed by Git; edit "+app.ConfigurationPath+" in the repository")
		return
	}

	if _, _, err := s.Store.ReviewByPreviewApplication(r.Context(), app.ID); err == nil {
		writeError(w, http.StatusConflict, "preview applications are managed by their pull request profile")
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeStoreError(w, "could not verify preview policy")
		return
	}
	var input struct {
		Name                       string                             `json:"name"`
		SourceID                   string                             `json:"sourceId"`
		Revision                   string                             `json:"revision"`
		ManifestPath               string                             `json:"manifestPath"`
		TargetManifestPath         string                             `json:"targetManifestPath"`
		NamespaceManifestPaths     map[string]string                  `json:"namespaceManifestPaths"`
		Renderer                   string                             `json:"renderer"`
		KustomizeHelmEnabled       bool                               `json:"kustomizeHelmEnabled"`
		CreateNamespaces           bool                               `json:"createNamespaces"`
		KustomizeNamespaceOverride bool                               `json:"kustomizeNamespaceOverride"`
		HelmValuesFiles            []string                           `json:"helmValuesFiles"`
		HelmValuesYAML             string                             `json:"helmValuesYaml"`
		TargetHelmValuesFiles      []string                           `json:"targetHelmValuesFiles"`
		TargetHelmValuesYAML       string                             `json:"targetHelmValuesYaml"`
		NamespaceHelmValues        map[string]core.HelmValuesOverride `json:"namespaceHelmValues"`
		ClusterID                  string                             `json:"clusterId"`
		Namespaces                 []string                           `json:"namespaces"`
		SyncPolicy                 string                             `json:"syncPolicy"`
		PollSeconds                int                                `json:"pollSeconds"`
		RetryPolicy                *store.RetryPolicy                 `json:"retryPolicy"`
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
	input.HelmValuesFiles, input.HelmValuesYAML, err = validateHelmValuesInput(input.Renderer, input.HelmValuesFiles, input.HelmValuesYAML)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.TargetHelmValuesFiles, input.TargetHelmValuesYAML, err = validateHelmValuesInput(input.Renderer, input.TargetHelmValuesFiles, input.TargetHelmValuesYAML)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	if input.RetryPolicy != nil {
		if err := input.RetryPolicy.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		app.RetryPolicy = *input.RetryPolicy
	}
	if app.ApplicationGroupID != "" && (input.SourceID != app.SourceID || input.Revision != app.Revision || input.ManifestPath != app.ManifestPath || input.Renderer != app.Renderer || input.KustomizeHelmEnabled != app.KustomizeHelmEnabled || input.KustomizeNamespaceOverride != app.KustomizeNamespaceOverride || strings.Join(input.HelmValuesFiles, "\x00") != strings.Join(app.HelmValuesFiles, "\x00") || input.HelmValuesYAML != app.HelmValuesYAML || input.SyncPolicy != app.SyncPolicy || input.PollSeconds != app.PollSeconds) {
		writeError(w, http.StatusBadRequest, "edit shared source and values through the deployment group")
		return
	}
	canUseSource, sourceErr := s.Store.WorkspaceCanUseGitSource(r.Context(), app.WorkspaceID, input.SourceID)
	if sourceErr != nil || !canUseSource {
		writeError(w, http.StatusBadRequest, "Git source is not available to this workspace")
		return
	}
	canUseCluster, clusterErr := s.Store.WorkspaceCanUseCluster(r.Context(), app.WorkspaceID, input.ClusterID)
	if clusterErr != nil || !canUseCluster {
		writeError(w, http.StatusBadRequest, "cluster not found")
		return
	}
	if len(input.Namespaces) == 0 {
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
		binding, err := s.Store.NamespaceBinding(r.Context(), app.WorkspaceID, input.ClusterID, namespace)
		if err != nil {
			writeError(w, http.StatusBadRequest, "every namespace must have a workspace binding")
			return
		}
		bindings = append(bindings, binding)
	}
	input.NamespaceHelmValues, err = validateNamespaceHelmValuesInput(input.Renderer, input.NamespaceHelmValues, input.Namespaces)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.TargetManifestPath, input.NamespaceManifestPaths, err = validateKustomizeManifestPaths(input.Renderer, input.TargetManifestPath, input.NamespaceManifestPaths, input.Namespaces)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	app.CreateNamespaces = input.CreateNamespaces
	app.Name, app.SourceID, app.Revision, app.ManifestPath, app.Renderer = input.Name, input.SourceID, input.Revision, input.ManifestPath, input.Renderer
	app.TargetManifestPath, app.NamespaceManifestPaths = input.TargetManifestPath, input.NamespaceManifestPaths
	app.KustomizeHelmEnabled, app.KustomizeNamespaceOverride, app.ClusterID, app.Namespaces = input.KustomizeHelmEnabled, input.KustomizeNamespaceOverride, input.ClusterID, bindings
	app.HelmValuesFiles, app.HelmValuesYAML = input.HelmValuesFiles, input.HelmValuesYAML
	app.TargetHelmValuesFiles, app.TargetHelmValuesYAML = input.TargetHelmValuesFiles, input.TargetHelmValuesYAML
	app.NamespaceHelmValues = input.NamespaceHelmValues
	app.SyncPolicy, app.PollSeconds = input.SyncPolicy, input.PollSeconds
	if err := s.Store.UpdateApplication(r.Context(), app); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.updated", "application", app.ID, applicationAuditDetails(app))
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) deleteApplication(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
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
			writeStoreError(w, "could not inspect managed resources")
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
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	if err := s.Store.CancelApplicationDecommission(r.Context(), app.ID); err != nil {
		writeError(w, http.StatusConflict, "cannot cancel while a sync is running")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.decommission_cancelled", "application", app.ID, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}
