package api

import (
	"net/http"
	"strconv"
)

func (s *Server) applicationHealthHistory(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "viewer") {
		return
	}
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "limit must be an integer between 1 and 100")
			return
		}
		limit = parsed
	}
	items, err := s.Store.ApplicationHealthTransitions(r.Context(), app.ID, limit)
	if err != nil {
		writeStoreError(w, "could not load application health history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
