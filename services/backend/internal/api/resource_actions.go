package api

import (
	"github.com/justlab/justcd/services/backend/internal/syncer"
	"net/http"
)

func (s *Server) pauseApplication(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	if err := s.Store.PauseApplication(r.Context(), app.ID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.sync_paused", "application", app.ID, nil)
	app.AutoSyncPaused = true
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) applicationResourceAction(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	var input syncer.ResourceAction
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Syncer.ResourceAction(r.Context(), app, currentUser(r).ID, input); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"autoSyncPaused": true})
}
