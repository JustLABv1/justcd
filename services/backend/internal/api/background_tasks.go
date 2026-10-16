package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) createPlan(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, 404, "application not found")
		return
	}
	role := "deployer"
	if app.Decommissioning || app.ConfigurationMissing {
		role = "owner"
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, role) {
		return
	}
	s.enqueueBackgroundTask(w, r, app.ID, "plan", "", json.RawMessage(`{}`))
}

func (s *Server) approvePlan(w http.ResponseWriter, r *http.Request) {
	record, app, ok := s.authorizedPlan(w, r, "viewer")
	if !ok {
		return
	}
	var input struct {
		Comment    string `json:"comment"`
		PlanDigest string `json:"planDigest"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		return
	}
	input.Comment = strings.TrimSpace(input.Comment)
	if len(input.Comment) > 1000 {
		writeError(w, 400, "approval comment must be at most 1000 characters")
		return
	}
	if input.PlanDigest == "" || input.PlanDigest != record.Plan.Digest {
		writeError(w, 409, "plan has changed; review it again before approving")
		return
	}
	if core.RequiredApprovalCount(record.Plan) == 0 {
		writeError(w, 409, "this plan does not require approval")
		return
	}
	role, err := s.Store.WorkspaceRole(r.Context(), currentUser(r), app.WorkspaceID)
	if err != nil || !core.ApprovalRoleAllows(record.Plan, role, currentUser(r).ID) {
		writeError(w, 403, "you are not an eligible approver for this plan")
		return
	}
	payload, _ := json.Marshal(input)
	s.enqueueBackgroundTask(w, r, app.ID, "approval", record.ID+":"+record.Plan.Digest, payload)
}

func (s *Server) enqueueBackgroundTask(w http.ResponseWriter, r *http.Request, applicationID, kind, target string, input json.RawMessage) {
	requestID := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(requestID) > 128 {
		writeError(w, 400, "idempotency key must be at most 128 characters")
		return
	}
	if requestID == "" {
		requestID = store.NewID()
	}
	task, err := s.Store.EnqueueBackgroundTask(r.Context(), store.BackgroundTask{ApplicationID: applicationID, ActorID: currentUser(r).ID, Kind: kind, Target: target, RequestID: requestID, Input: input})
	if err != nil {
		if errors.Is(err, store.ErrTaskRequestMismatch) {
			writeError(w, 409, err.Error())
			return
		}
		s.Logger.ErrorContext(r.Context(), "could not enqueue background task", "applicationId", applicationID, "kind", kind, "error", err)
		writeStoreError(w, "could not queue task")
		return
	}
	w.Header().Set("Location", "/api/v1/tasks/"+task.ID)
	w.Header().Set("Retry-After", "1")
	writeJSON(w, http.StatusAccepted, task)
}

func (s *Server) getBackgroundTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.Store.BackgroundTaskByID(r.Context(), r.PathValue("taskID"))
	if err != nil || task.ActorID != currentUser(r).ID {
		writeError(w, 404, "task not found")
		return
	}
	app, err := s.Store.ApplicationByID(r.Context(), task.ApplicationID)
	if err != nil {
		writeError(w, 404, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "viewer") {
		return
	}
	writeJSON(w, 200, task)
}

func (s *Server) listBackgroundTasks(w http.ResponseWriter, r *http.Request) {
	applicationID, workspaceID := r.URL.Query().Get("applicationId"), r.URL.Query().Get("workspaceId")
	if applicationID == "" && workspaceID == "" {
		writeError(w, 400, "applicationId or workspaceId is required")
		return
	}
	if applicationID != "" {
		app, err := s.Store.ApplicationByID(r.Context(), applicationID)
		if err != nil {
			writeError(w, 404, "application not found")
			return
		}
		workspaceID = app.WorkspaceID
	}
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	tasks, err := s.Store.ListBackgroundTasks(r.Context(), currentUser(r).ID, applicationID, workspaceID)
	if err != nil {
		writeStoreError(w, "could not load tasks")
		return
	}
	writeJSON(w, 200, map[string]any{"items": tasks})
}

// Execute the existing handlers with the current account and current workspace
// permissions. Their structured responses become the durable task outcome.
func (s *Server) executeBackgroundTask(ctx context.Context, task store.BackgroundTask) (recorder *httptest.ResponseRecorder) {
	recorder = httptest.NewRecorder()
	defer func() {
		if recover() != nil {
			s.Logger.ErrorContext(ctx, "background task panicked", "taskId", task.ID)
			recorder = httptest.NewRecorder()
			writeError(recorder, 500, "Background work failed unexpectedly. Refresh the current state before retrying.")
		}
	}()
	actor, err := s.Store.UserByID(ctx, task.ActorID)
	if err != nil || actor.Disabled || actor.DeletedAt != nil {
		writeError(recorder, 403, "task requester no longer has an active account")
		return recorder
	}
	ctx = context.WithValue(ctx, userContextKey, actor)
	r, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/", bytes.NewReader(task.Input))
	if task.Kind == "plan" {
		r.SetPathValue("applicationID", task.ApplicationID)
		s.createPlanNow(recorder, r)
	} else if task.Kind == "approval" {
		planID, _, _ := strings.Cut(task.Target, ":")
		r.SetPathValue("planID", planID)
		s.approvePlanNow(recorder, r)
	} else {
		writeError(recorder, 500, "unknown background task kind")
	}
	return recorder
}
