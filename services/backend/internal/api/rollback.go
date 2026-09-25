package api

import (
	"net/http"
	"strings"
)

func (s *Server) listRollbackTargets(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "viewer") {
		return
	}
	items, err := s.Syncer.RollbackTargets(r.Context(), app.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load rollback targets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createRollbackPlan(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "owner") {
		return
	}
	var input struct {
		Kind     string `json:"kind"`
		TargetID string `json:"targetId"`
		Revision string `json:"revision"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	record, err := s.Syncer.BuildRollbackPlan(r.Context(), app, currentUser(r).ID, input.Kind, input.TargetID, input.Revision)
	if err != nil {
		s.Logger.Warn("rollback plan calculation failed", "applicationId", app.ID, "targetKind", input.Kind, "error", err)
		if strings.Contains(err.Error(), "no longer available") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "could not calculate a safe rollback plan"})
		return
	}
	writeJSON(w, http.StatusCreated, toPlanView(record))
}

func (s *Server) updateRollbackState(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "owner") {
		return
	}
	var input struct {
		Action   string `json:"action"`
		Revision string `json:"revision"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch input.Action {
	case "keep":
		err = s.Store.KeepRollbackPin(r.Context(), app.ID)
	case "resume":
		resolvedRevision := ""
		if strings.TrimSpace(input.Revision) != "" {
			resolvedRevision, err = s.Syncer.ValidateResumeRevision(r.Context(), app, input.Revision)
		}
		if err == nil {
			err = s.Store.ResumeRollbackTracking(r.Context(), app.ID, resolvedRevision)
		}
	default:
		writeError(w, http.StatusBadRequest, "action must be keep or resume")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "select and verify") {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "Git") || strings.Contains(err.Error(), "revision") {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "could not verify the selected Git revision"})
			return
		}
		writeError(w, http.StatusInternalServerError, "could not update rollback tracking state")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.rollback_tracking_updated", "application", app.ID, map[string]any{"action": input.Action, "revisionSelected": strings.TrimSpace(input.Revision) != ""})
	updated, err := s.Store.ApplicationByID(r.Context(), app.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rollback tracking state was saved but application could not be reloaded")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
