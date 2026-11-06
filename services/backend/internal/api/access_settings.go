package api

import (
	"errors"
	"net/http"

	"github.com/justlab/justcd/services/backend/internal/store"
)

// localLoginAllowed is whether password sign-in is available right now: the instance setting, or the
// operator's recovery override.
func (s *Server) localLoginAllowed(settings store.InstanceSettings) bool {
	return settings.LocalLoginEnabled || s.Config.ForceLocalLogin
}

type accessSettingsResponse struct {
	store.AccessSettings
	// LocalLoginForced is true when JUSTCD_FORCE_LOCAL_LOGIN keeps password sign-in on regardless of the setting.
	LocalLoginForced bool                  `json:"localLoginForced"`
	Readiness        store.AccessReadiness `json:"readiness"`
}

func (s *Server) getAccessSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Store.InstanceSettings(r.Context())
	if err != nil {
		writeStoreError(w, "could not load access settings")
		return
	}
	readiness, err := s.Store.AccessReadiness(r.Context())
	if err != nil {
		writeStoreError(w, "could not load access settings")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, accessSettingsResponse{AccessSettings: settings.AccessSettings, LocalLoginForced: s.Config.ForceLocalLogin, Readiness: readiness})
}

// updateAccessSettings accepts any subset of the settings and keeps the rest as they are.
func (s *Server) updateAccessSettings(w http.ResponseWriter, r *http.Request) {
	var input struct {
		LocalLoginEnabled        *bool     `json:"localLoginEnabled"`
		LocalRegistrationEnabled *bool     `json:"localRegistrationEnabled"`
		OIDCAutoProvision        *bool     `json:"oidcAutoProvision"`
		OIDCAllowedDomains       *[]string `json:"oidcAllowedDomains"`
		WorkspaceCreation        *string   `json:"workspaceCreation"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := s.Store.InstanceSettings(r.Context())
	if err != nil {
		writeStoreError(w, "could not load access settings")
		return
	}
	next := current.AccessSettings
	if input.LocalLoginEnabled != nil {
		next.LocalLoginEnabled = *input.LocalLoginEnabled
		// Turning password sign-in off closes registration with it, unless the same request insists otherwise.
		if !next.LocalLoginEnabled && input.LocalRegistrationEnabled == nil {
			next.LocalRegistrationEnabled = false
		}
	}
	if input.LocalRegistrationEnabled != nil {
		next.LocalRegistrationEnabled = *input.LocalRegistrationEnabled
	}
	if input.OIDCAutoProvision != nil {
		next.OIDCAutoProvision = *input.OIDCAutoProvision
	}
	if input.OIDCAllowedDomains != nil {
		next.OIDCAllowedDomains = *input.OIDCAllowedDomains
	}
	if input.WorkspaceCreation != nil {
		next.WorkspaceCreation = *input.WorkspaceCreation
	}
	saved, err := s.Store.UpdateAccessSettings(r.Context(), next, currentUser(r).ID)
	if err != nil {
		var rejected *store.AccessError
		if errors.As(err, &rejected) {
			writeError(w, http.StatusConflict, rejected.Message)
			return
		}
		writeStoreError(w, "could not update access settings")
		return
	}
	readiness, err := s.Store.AccessReadiness(r.Context())
	if err != nil {
		writeStoreError(w, "could not load access settings")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, accessSettingsResponse{AccessSettings: saved, LocalLoginForced: s.Config.ForceLocalLogin, Readiness: readiness})
}
