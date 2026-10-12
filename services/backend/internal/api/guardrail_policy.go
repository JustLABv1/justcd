package api

import (
	"github.com/justlab/justcd/services/backend/internal/core"
	"net/http"
)

func (s *Server) getGuardrailPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, id, "viewer") {
		return
	}
	config, err := s.Store.GuardrailPolicy(r.Context(), id)
	if err != nil {
		writeStoreError(w, "could not load deployment guardrails")
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) putGuardrailPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, id, "owner") {
		return
	}
	var config core.PolicyConfig
	if err := decodeJSON(w, r, &config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	config, err := core.NormalizePolicyConfig(config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.SetGuardrailPolicy(r.Context(), id, currentUser(r).ID, config); err != nil {
		writeStoreError(w, "could not save deployment guardrails")
		return
	}
	writeJSON(w, http.StatusOK, config)
}
