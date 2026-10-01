package api

import (
	"net/http"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/scm"
)

func (s *Server) adoptBranchPreview(w http.ResponseWriter, r *http.Request) {
	parent, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, parent.WorkspaceID, "owner") {
		return
	}
	connection, err := s.Store.SourceControlConnectionByApplication(r.Context(), parent.ID)
	if err != nil || !connection.Enabled || !connection.PreviewProfile.Enabled || connection.PreviewProfile.DeploymentMode == "existing" {
		writeError(w, http.StatusConflict, "enable isolated PR previews first")
		return
	}
	review, err := s.Store.ReviewByID(r.Context(), r.PathValue("reviewID"))
	if err != nil || review.ConnectionID != connection.ID {
		writeError(w, http.StatusNotFound, "PR review not found")
		return
	}
	var input struct {
		ApplicationID string `json:"applicationId"`
	}
	if err = decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	token, err := s.sourceControlToken(r.Context(), connection)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	current, err := (scm.Client{}).Current(r.Context(), connection.Provider, connection.APIURL, connection.Repository, string(token), review.Number)
	if err != nil || current.Closed || current.Fork || current.HeadSHA != review.HeadSHA || current.HeadBranch != review.HeadBranch {
		writeError(w, http.StatusConflict, "PR changed; refresh before choosing a preview")
		return
	}
	if input.ApplicationID != "" {
		preview, err := s.Store.ApplicationByID(r.Context(), input.ApplicationID)
		if err != nil || preview.BranchTest == nil || preview.BranchTest.ParentApplicationID != parent.ID {
			writeError(w, http.StatusConflict, "branch preview does not match")
			return
		}
		preview.Revision = pullRequestRef(connection.Provider, review.Number)
		plan, desired, err := s.Syncer.CalculatePlanWithSelection(r.Context(), preview, core.PlanSelection{})
		if err != nil || plan.Revision != review.HeadSHA || len(preview.Namespaces) != 1 {
			writeError(w, http.StatusConflict, "could not verify the branch preview against the current PR commit")
			return
		}
		if err = validatePreviewResources(desired, connection.PreviewProfile, preview.Namespaces[0].Namespace, false); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}
	if err = s.Store.AdoptBranchPreview(r.Context(), review, parent, input.ApplicationID, pullRequestRef(connection.Provider, review.Number), connection.PreviewProfile.MaxActive); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "pull_request.branch_preview_selected", "application", parent.ID, map[string]any{"reviewId": review.ID, "previewApplicationId": input.ApplicationID, "headSha": review.HeadSHA})
	writeJSON(w, http.StatusOK, map[string]any{"selected": true})
}
