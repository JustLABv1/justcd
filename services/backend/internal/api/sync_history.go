package api

import (
	"net/http"
	"time"
)

func (s *Server) applicationSyncHistory(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "viewer") {
		return
	}
	period, width := 24*time.Hour, time.Hour
	switch r.URL.Query().Get("range") {
	case "", "24h":
	case "7d":
		period, width = 7*24*time.Hour, 6*time.Hour
	default:
		writeError(w, http.StatusBadRequest, "range must be 24h or 7d")
		return
	}
	end := time.Now().UTC()
	history, err := s.Store.SyncHistory(r.Context(), app.ID, end.Add(-period), end, width)
	if err != nil {
		writeStoreError(w, "could not load sync history")
		return
	}
	writeJSON(w, http.StatusOK, history)
}
