package api

import (
	"errors"
	"net/http"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func validApproverRole(role string) bool {
	return role == "owner" || role == "deployer" || role == "viewer"
}

func normalizeApprovalRule(rule *store.ApprovalRule) {
	if rule.ApproverRoles == nil {
		rule.ApproverRoles = []string{}
	}
	if rule.ApproverUserIDs == nil {
		rule.ApproverUserIDs = []string{}
	}
}

func (s *Server) validateApprovalRule(r *http.Request, workspaceID string, rule store.ApprovalRule, deletion bool) error {
	minimum, maximum := 0, 5
	if deletion {
		minimum = 1
	}
	if rule.RequiredApprovals < minimum || rule.RequiredApprovals > maximum {
		if deletion {
			return errors.New("deletion approvals must be between 1 and 5")
		}
		return errors.New("sync approvals must be between 0 and 5")
	}
	if len(rule.ApproverRoles) > 3 || len(rule.ApproverUserIDs) > 50 {
		return errors.New("approval rules contain too many approvers")
	}
	seenRoles := make(map[string]bool, len(rule.ApproverRoles))
	for _, role := range rule.ApproverRoles {
		if !validApproverRole(role) || seenRoles[role] {
			return errors.New("approver roles must be unique workspace roles")
		}
		seenRoles[role] = true
	}
	var members []map[string]any
	if rule.RequiredApprovals > 0 || len(rule.ApproverUserIDs) > 0 {
		var err error
		members, err = s.Store.ListWorkspaceMembers(r.Context(), workspaceID)
		if err != nil {
			return errors.New("could not validate workspace approvers")
		}
	}
	memberIDs := make(map[string]bool, len(members))
	for _, member := range members {
		disabled, _ := member["disabled"].(bool)
		if userID, ok := member["id"].(string); ok && !disabled {
			memberIDs[userID] = true
		}
	}
	seenUsers := make(map[string]bool, len(rule.ApproverUserIDs))
	for _, userID := range rule.ApproverUserIDs {
		if userID == "" || seenUsers[userID] || !memberIDs[userID] {
			return errors.New("selected approvers must be unique workspace members")
		}
		seenUsers[userID] = true
	}
	if rule.RequiredApprovals > 0 && len(rule.ApproverRoles) == 0 && len(rule.ApproverUserIDs) == 0 {
		return errors.New("choose at least one workspace role or member who can approve")
	}
	if rule.RequiredApprovals > 0 {
		eligible := 0
		for _, member := range members {
			userID, _ := member["id"].(string)
			role, _ := member["role"].(string)
			disabled, _ := member["disabled"].(bool)
			if !disabled && store.ApprovalRuleAllows(rule, role, userID) {
				eligible++
			}
		}
		if eligible < rule.RequiredApprovals {
			return errors.New("approval count exceeds the number of eligible workspace members")
		}
	}
	return nil
}

func (s *Server) updateWorkspaceApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspaceID, "owner") {
		return
	}
	var policy store.ApprovalPolicy
	if err := decodeJSON(w, r, &policy); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalizeApprovalRule(&policy.Sync)
	normalizeApprovalRule(&policy.Deletion)
	if err := s.validateApprovalRule(r, workspaceID, policy.Sync, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.validateApprovalRule(r, workspaceID, policy.Deletion, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.UpdateWorkspaceApprovalPolicy(r.Context(), workspaceID, policy); err != nil {
		writeError(w, http.StatusConflict, "could not update workspace approval rules")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "workspace.approval_policy_updated", "workspace", workspaceID, policy)
	writeJSON(w, http.StatusOK, map[string]any{"approvalPolicy": policy})
}

func (s *Server) updateApplicationApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireWorkspaceRole(w, r, app.WorkspaceID, "owner") {
		return
	}
	var input struct {
		Override *store.ApprovalPolicyOverride `json:"override"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Override != nil {
		if input.Override.Sync != nil {
			normalizeApprovalRule(input.Override.Sync)
			if err := s.validateApprovalRule(r, app.WorkspaceID, *input.Override.Sync, false); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if input.Override.Deletion != nil {
			normalizeApprovalRule(input.Override.Deletion)
			if err := s.validateApprovalRule(r, app.WorkspaceID, *input.Override.Deletion, true); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	if err := s.Store.UpdateApplicationApprovalPolicyOverride(r.Context(), app.ID, input.Override); err != nil {
		writeError(w, http.StatusConflict, "could not update application approval overrides")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.approval_policy_updated", "application", app.ID, map[string]any{"override": input.Override})
	writeJSON(w, http.StatusOK, map[string]any{"approvalPolicyOverride": input.Override})
}
