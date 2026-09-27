package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
)

type approvalInboxItem struct {
	PlanID             string    `json:"planId"`
	PlanDigest         string    `json:"planDigest"`
	ApplicationID      string    `json:"applicationId"`
	ApplicationName    string    `json:"applicationName"`
	ApplicationGroupID *string   `json:"applicationGroupId,omitempty"`
	WorkspaceID        string    `json:"workspaceId"`
	WorkspaceName      string    `json:"workspaceName"`
	Kind               string    `json:"kind"`
	CreatedAt          time.Time `json:"createdAt"`
	ExpiresAt          time.Time `json:"expiresAt"`
	RequiredApprovals  int       `json:"requiredApprovals"`
	ApprovedApprovals  int       `json:"approvedApprovals"`
	Deletions          []string  `json:"deletions"`
	ClusterScoped      []string  `json:"clusterScoped"`
	Takeovers          []string  `json:"takeovers"`
	ChangeCount        int       `json:"changeCount"`
}

func (s *Server) listApprovalInbox(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := 1
	if q.Get("page") != "" {
		var err error
		page, err = strconv.Atoi(q.Get("page"))
		if err != nil || page < 1 || page > 100000 {
			writeError(w, 400, "invalid page")
			return
		}
	}
	limit := 20
	if q.Get("limit") != "" {
		var err error
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > 100 {
			writeError(w, 400, "limit must be between 1 and 100")
			return
		}
	}
	risk, age, expiry := q.Get("risk"), q.Get("age"), q.Get("expiry")
	if risk != "" && risk != "deletion" && risk != "cluster" && risk != "sync" && risk != "takeover" {
		writeError(w, 400, "invalid risk filter")
		return
	}
	if age != "" && age != "24h" && age != "7d" && age != "older7d" {
		writeError(w, 400, "invalid age filter")
		return
	}
	if expiry != "" && expiry != "1h" && expiry != "24h" && expiry != "later" {
		writeError(w, 400, "invalid expiry filter")
		return
	}
	candidates, err := s.Store.ListApprovalCandidates(r.Context(), currentUser(r), q.Get("workspaceId"), "")
	if err != nil {
		writeStoreError(w, "could not load approval inbox")
		return
	}
	now := time.Now()
	items := make([]approvalInboxItem, 0)
	applicationOptions := make(map[string]string)
	roleCache := map[string]string{}
	for _, candidate := range candidates {
		record, err := s.Store.PlanByID(r.Context(), candidate.PlanID)
		if err != nil {
			writeStoreError(w, "could not load pending plan")
			return
		}
		required := core.RequiredApprovalCount(record.Plan)
		if required < 1 {
			continue
		}
		role, ok := roleCache[candidate.WorkspaceID]
		if !ok {
			role, err = s.Store.WorkspaceRole(r.Context(), currentUser(r), candidate.WorkspaceID)
			if err != nil {
				writeStoreError(w, "could not check approval access")
				return
			}
			roleCache[candidate.WorkspaceID] = role
		}
		if role == "" || !core.ApprovalRoleAllows(record.Plan, role, currentUser(r).ID) {
			continue
		}
		approvals, err := s.Store.ListPlanApprovals(r.Context(), record.ID, record.Plan.Digest)
		if err != nil {
			writeStoreError(w, "could not load approvals")
			return
		}
		approved := map[string]bool{}
		for _, approval := range approvals {
			if approval.Approval.ActorID == currentUser(r).ID {
				approved[approval.Approval.ActorID] = true
				continue
			}
			actor, err := s.Store.UserByID(r.Context(), approval.Approval.ActorID)
			if err != nil || actor.Disabled || actor.DeletedAt != nil {
				continue
			}
			actorRole, err := s.Store.WorkspaceRole(r.Context(), actor, candidate.WorkspaceID)
			if err == nil && core.ApprovalRoleAllows(record.Plan, actorRole, actor.ID) {
				approved[actor.ID] = true
			}
		}
		if approved[currentUser(r).ID] || len(approved) >= required {
			continue
		}
		applicationOptions[candidate.ApplicationID] = candidate.ApplicationName
		if q.Get("applicationId") != "" && candidate.ApplicationID != q.Get("applicationId") {
			continue
		}
		item := approvalInboxItem{PlanID: record.ID, PlanDigest: record.Plan.Digest, ApplicationID: candidate.ApplicationID, ApplicationName: candidate.ApplicationName, ApplicationGroupID: candidate.GroupID, WorkspaceID: candidate.WorkspaceID, WorkspaceName: candidate.WorkspaceName, Kind: record.Plan.ApprovalKind, CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt, RequiredApprovals: required, ApprovedApprovals: len(approved), Deletions: []string{}, ClusterScoped: []string{}, Takeovers: []string{}, ChangeCount: len(record.Plan.Changes)}
		if item.Kind == "" {
			item.Kind = "sync"
		}
		for _, change := range record.Plan.Changes {
			name := fmt.Sprintf("%s %s/%s", change.Identity.Kind, change.Identity.Namespace, change.Identity.Name)
			if change.Kind == core.Delete {
				item.Deletions = append(item.Deletions, name)
			}
			if change.Identity.ClusterScoped {
				item.ClusterScoped = append(item.ClusterScoped, name)
			}
			if change.Takeover {
				item.Takeovers = append(item.Takeovers, name)
			}
		}
		if risk == "deletion" && len(item.Deletions) == 0 {
			continue
		}
		if risk == "cluster" && len(item.ClusterScoped) == 0 {
			continue
		}
		if risk == "takeover" && len(item.Takeovers) == 0 {
			continue
		}
		if risk == "sync" && (len(item.Deletions) > 0 || len(item.ClusterScoped) > 0 || len(item.Takeovers) > 0) {
			continue
		}
		ageDuration := now.Sub(item.CreatedAt)
		if age == "24h" && ageDuration > 24*time.Hour || age == "7d" && ageDuration > 7*24*time.Hour || age == "older7d" && ageDuration < 7*24*time.Hour {
			continue
		}
		remaining := item.ExpiresAt.Sub(now)
		if expiry == "1h" && remaining > time.Hour || expiry == "24h" && remaining > 24*time.Hour || expiry == "later" && remaining <= 24*time.Hour {
			continue
		}
		items = append(items, item)
	}
	total := len(items)
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	options := make([]map[string]string, 0, len(applicationOptions))
	for id, name := range applicationOptions {
		options = append(options, map[string]string{"id": id, "name": name})
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i]["name"] == options[j]["name"] {
			return options[i]["id"] < options[j]["id"]
		}
		return options[i]["name"] < options[j]["name"]
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": items[start:end], "applications": options, "page": page, "limit": limit, "total": total, "hasMore": end < total})
}

func (s *Server) batchApprovePlans(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Items []struct {
			PlanID     string `json:"planId"`
			PlanDigest string `json:"planDigest"`
		} `json:"items"`
		Comment string `json:"comment"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		return
	}
	if len(input.Items) < 1 || len(input.Items) > 20 {
		writeError(w, 400, "select between 1 and 20 plans")
		return
	}
	if len(strings.TrimSpace(input.Comment)) > 1000 {
		writeError(w, 400, "approval comment must be at most 1000 characters")
		return
	}
	seen := map[string]bool{}
	results := make([]map[string]any, 0, len(input.Items))
	succeeded := 0
	for _, item := range input.Items {
		if item.PlanID == "" || item.PlanDigest == "" || seen[item.PlanID] {
			writeError(w, 400, "each plan must have a unique ID and reviewed digest")
			return
		}
		seen[item.PlanID] = true
	}
	for _, item := range input.Items {
		payload, _ := json.Marshal(map[string]string{"comment": input.Comment, "planDigest": item.PlanDigest})
		request := r.Clone(r.Context())
		request.Body = ioNopCloser{bytes.NewReader(payload)}
		request.SetPathValue("planID", item.PlanID)
		recorder := httptest.NewRecorder()
		s.approvePlan(recorder, request)
		result := map[string]any{"planId": item.PlanID, "status": recorder.Code, "success": recorder.Code == http.StatusCreated}
		if recorder.Code == http.StatusCreated {
			succeeded++
		} else {
			var failure map[string]any
			if json.Unmarshal(recorder.Body.Bytes(), &failure) == nil {
				result["error"] = failure["error"]
			} else {
				result["error"] = "approval failed"
			}
		}
		results = append(results, result)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "succeeded": succeeded, "failed": len(results) - succeeded})
}

type ioNopCloser struct{ *bytes.Reader }

func (ioNopCloser) Close() error { return nil }
