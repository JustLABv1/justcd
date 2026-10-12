package api

import (
	"github.com/justlab/justcd/services/backend/internal/security"
	"net/http"
)

func (s *Server) getInstanceSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Store.InstanceSettings(r.Context())
	if err != nil {
		writeStoreError(w, "could not load instance settings")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) updateInstanceSettings(w http.ResponseWriter, r *http.Request) {
	var input struct {
		MaintenanceEnabled *bool `json:"maintenanceEnabled"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.MaintenanceEnabled == nil {
		writeError(w, http.StatusBadRequest, "maintenanceEnabled is required")
		return
	}
	if err := s.Store.SetMaintenance(r.Context(), *input.MaintenanceEnabled, currentUser(r).ID); err != nil {
		writeStoreError(w, "could not update instance settings")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]bool{"maintenanceEnabled": *input.MaintenanceEnabled})
}

// Public availability contains no account data. Bypass is derived only from the
// existing server-validated session, never a browser flag or caller-supplied role.
func (s *Server) serviceStatus(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Store.InstanceSettings(r.Context())
	if err != nil {
		writeStoreError(w, "could not check service availability")
		return
	}
	bypass := false
	if settings.MaintenanceEnabled {
		if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
			if session, err := s.Store.SessionByTokenHash(r.Context(), security.HashToken(cookie.Value)); err == nil {
				if user, err := s.Store.UserByID(r.Context(), session.UserID); err == nil {
					bypass = user.IsAdmin
				}
			}
		}
	}
	status := "online"
	if settings.MaintenanceEnabled && !bypass {
		status = "maintenance"
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "maintenanceEnabled": settings.MaintenanceEnabled, "adminBypass": bypass})
}

func maintenanceExempt(path string) bool {
	return path == "/api/v1/auth/session" || path == "/api/v1/auth/logout"
}

func writeMaintenanceError(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "15")
	writeClassifiedError(w, http.StatusServiceUnavailable, "JustCD is in maintenance mode", apiErrorMetadata{
		code: "instance.maintenance", category: "maintenance", retryable: true,
		remediation: "Wait for maintenance to finish, or sign in with an administrator account.", remediationURL: "/maintenance",
	}, nil)
}
