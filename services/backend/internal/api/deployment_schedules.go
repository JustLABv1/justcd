package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/justlab/justcd/services/backend/internal/schedule"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) deploymentScheduleScope(w http.ResponseWriter, r *http.Request, minimum string) (string, string, bool) {
	if id := r.PathValue("applicationID"); id != "" {
		app, err := s.Store.ApplicationByID(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "application not found")
			return "", "", false
		}
		return "application", id, s.requireWorkspaceRole(w, r, app.WorkspaceID, minimum)
	}
	id := r.PathValue("workspaceID")
	return "workspace", id, s.requireWorkspaceRole(w, r, id, minimum)
}
func (s *Server) getDeploymentSchedule(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := s.deploymentScheduleScope(w, r, "viewer")
	if !ok {
		return
	}
	p, err := s.Store.DeploymentPolicy(r.Context(), scope, id)
	if err != nil {
		writeStoreError(w, "could not load deployment schedule")
		return
	}
	decision := schedule.Evaluate([]schedule.ScopedPolicy{{Scope: "Workspace", Policy: p}}, time.Now())
	managed := false
	if scope == "application" {
		decision, err = s.Store.ApplicationSchedule(r.Context(), id)
		if err != nil {
			writeStoreError(w, "could not evaluate deployment schedule")
			return
		}
		app, err := s.Store.ApplicationByID(r.Context(), id)
		if err != nil {
			writeStoreError(w, "could not load application")
			return
		}
		managed = app.RepositoryConfigurationID != "" || app.ConfigurationPath != ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"policy": p, "decision": decision, "managedByGit": managed})
}
func (s *Server) putDeploymentSchedule(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := s.deploymentScheduleScope(w, r, "owner")
	if !ok {
		return
	}
	var p schedule.Policy
	if err := decodeJSON(w, r, &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := p.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.SetDeploymentPolicy(r.Context(), scope, id, currentUser(r).ID, p); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.getDeploymentSchedule(w, r)
}
func (s *Server) overrideOperationSchedule(w http.ResponseWriter, r *http.Request) {
	op, err := s.Store.OperationByID(r.Context(), r.PathValue("operationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "operation not found")
		return
	}
	app, err := s.Store.ApplicationByID(r.Context(), op.ApplicationID)
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.OverrideOperationSchedule(r.Context(), op.ID, currentUser(r).ID, input.Reason); err != nil {
		status := http.StatusConflict
		if errors.Is(err, store.ErrScheduleOverrideForbidden) {
			status = http.StatusForbidden
		}
		writeError(w, status, err.Error())
		return
	}
	op, err = s.Store.OperationByID(r.Context(), op.ID)
	if err != nil {
		writeStoreError(w, "could not load operation")
		return
	}
	writeJSON(w, http.StatusOK, op)
}

func (s *Server) cancelQueuedOperation(w http.ResponseWriter, r *http.Request) {
	op, err := s.Store.OperationByID(r.Context(), r.PathValue("operationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "operation not found")
		return
	}
	app, err := s.Store.ApplicationByID(r.Context(), op.ApplicationID)
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "deployer") {
		return
	}
	cancelled, err := s.Store.RequestOperationCancellation(r.Context(), op.ID, currentUser(r).ID)
	if err != nil {
		writeStoreError(w, "could not request operation cancellation")
		return
	}
	if !cancelled {
		writeError(w, http.StatusConflict, "operation has finished or does not support cancellation")
		return
	}
	op, err = s.Store.OperationByID(r.Context(), op.ID)
	if err != nil {
		writeStoreError(w, "could not load operation after cancellation request")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": op.Status == "cancelled", "cancellationRequested": op.Progress.CancelRequested, "operation": op})
}
