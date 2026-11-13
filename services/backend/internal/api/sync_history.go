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
	s.writeSyncHistory(w, r, app.ID, "")
}

func (s *Server) workspaceSyncHistory(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	s.writeSyncHistory(w, r, "", workspaceID)
}

func (s *Server) writeSyncHistory(w http.ResponseWriter, r *http.Request, applicationID, workspaceID string) {
	period, width := 24*time.Hour, time.Hour
	switch r.URL.Query().Get("range") {
	case "", "24h":
	case "7d":
		period, width = 7*24*time.Hour, 6*time.Hour
	default:
		writeError(w, http.StatusBadRequest, "range must be 24h or 7d")
		return
	}
	// Buckets start on whole hours (or six-hour marks), so the axis reads 10:00, 11:00…
	// instead of the minute the page happened to load. The last bucket is the one in progress.
	end := time.Now().UTC().Truncate(width).Add(width)
	var history any
	var err error
	if workspaceID != "" {
		history, err = s.Store.WorkspaceSyncHistory(r.Context(), workspaceID, end.Add(-period), end, width)
	} else {
		history, err = s.Store.SyncHistory(r.Context(), applicationID, end.Add(-period), end, width)
	}
	if err != nil {
		writeStoreError(w, "could not load sync history")
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) workspaceDeliveryMetrics(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspaceID, "viewer") {
		return
	}
	period := 7 * 24 * time.Hour
	switch r.URL.Query().Get("range") {
	case "", "7d":
	case "30d":
		period = 30 * 24 * time.Hour
	default:
		writeError(w, http.StatusBadRequest, "range must be 7d or 30d")
		return
	}
	metrics, err := s.Store.WorkspaceDeliveryMetrics(r.Context(), workspaceID, time.Now().UTC(), period)
	if err != nil {
		writeStoreError(w, "could not load delivery metrics")
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}
