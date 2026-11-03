package api

import (
	"net/http"

	"github.com/justlab/justcd/services/backend/internal/ai"
)

// aiGate applies the checks shared by every AI feature: workspace role, workspace
// policy, instance connection and the per-user request slot. The returned function
// releases the slot and must be called exactly once when ok is true.
func (s *Server) aiGate(w http.ResponseWriter, r *http.Request, workspace string) (ai.Settings, func(), bool) {
	if !s.requireWorkspaceRole(w, r, workspace, "viewer") {
		return ai.Settings{}, nil, false
	}
	enabled, err := s.Store.WorkspaceAIEnabled(r.Context(), workspace)
	if err != nil {
		writeStoreError(w, "could not load workspace AI policy")
		return ai.Settings{}, nil, false
	}
	if !enabled {
		writeError(w, 409, "a workspace owner must enable AI in workspace settings first")
		return ai.Settings{}, nil, false
	}
	settings, err := s.loadAI(r.Context())
	if err != nil {
		writeStoreError(w, "could not load AI connection")
		return ai.Settings{}, nil, false
	}
	if !settings.Enabled {
		writeError(w, 409, "an administrator must configure and enable the AI connection first")
		return ai.Settings{}, nil, false
	}
	if !s.takeAISlot(w, r) {
		return ai.Settings{}, nil, false
	}
	user := currentUser(r).ID
	return settings, func() { s.releaseAISlot(user) }, true
}

// aiAvailable reports whether AI can be used for the workspace without writing a response.
func (s *Server) aiAvailable(r *http.Request, workspace string) bool {
	enabled, err := s.Store.WorkspaceAIEnabled(r.Context(), workspace)
	if err != nil || !enabled {
		return false
	}
	settings, _, err := s.Store.AISettings(r.Context())
	return err == nil && settings.Enabled
}

func redactResult(result *ai.Result) {
	result.Summary = redactAPIErrorMessage(result.Summary)
	for i := range result.Findings {
		result.Findings[i].Title = redactAPIErrorMessage(result.Findings[i].Title)
		result.Findings[i].Detail = redactAPIErrorMessage(result.Findings[i].Detail)
		result.Findings[i].Impact = redactAPIErrorMessage(result.Findings[i].Impact)
		result.Findings[i].Action = redactAPIErrorMessage(result.Findings[i].Action)
	}
	for i := range result.NextSteps {
		result.NextSteps[i] = redactAPIErrorMessage(result.NextSteps[i])
	}
	for i := range result.Limitations {
		result.Limitations[i] = redactAPIErrorMessage(result.Limitations[i])
	}
}
