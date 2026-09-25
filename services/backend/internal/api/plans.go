package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/store"
	"github.com/justlab/justcd/services/backend/internal/syncer"
)

type planView struct {
	ID        string          `json:"id"`
	Plan      core.Plan       `json:"plan"`
	Resources []core.Identity `json:"resources"`
	CreatedBy string          `json:"createdBy"`
	CreatedAt time.Time       `json:"createdAt"`
	ExpiresAt time.Time       `json:"expiresAt"`
	Status    string          `json:"status"`
}

func toPlanView(record store.PlanRecord) planView {
	plan := record.Plan
	resources := make([]core.Identity, 0, len(record.Desired))
	for _, resource := range record.Desired {
		resources = append(resources, resource.Identity)
	}
	for index := range plan.Changes {
		plan.Changes[index].Before = redactManifest(plan.Changes[index].Before)
		plan.Changes[index].After = redactManifest(plan.Changes[index].After)
	}
	for index := range plan.Ignored {
		plan.Ignored[index].Before = redactManifest(plan.Ignored[index].Before)
		plan.Ignored[index].After = redactManifest(plan.Ignored[index].After)
	}
	return planView{ID: record.ID, Plan: plan, Resources: resources, CreatedBy: record.CreatedBy, CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt, Status: record.Status}
}

func redactManifest(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return json.RawMessage(`"[redacted]"`)
	}
	root, _ := value.(map[string]any)
	if strings.EqualFold(stringValue(root["kind"]), "Secret") {
		if _, ok := root["data"]; ok {
			root["data"] = "[redacted]"
		}
		if _, ok := root["stringData"]; ok {
			root["stringData"] = "[redacted]"
		}
	}
	encoded, err := json.Marshal(redactObject(root))
	if err != nil {
		return json.RawMessage(`"[redacted]"`)
	}
	return encoded
}

func redactObject(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "privatekey") || strings.Contains(lower, "accesskey") || strings.Contains(lower, "authorization") {
				result[key] = "[redacted]"
				continue
			}
			result[key] = redactObject(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = redactObject(item)
		}
		return result
	default:
		return value
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func (s *Server) createPlan(w http.ResponseWriter, r *http.Request) {
	applicationID := r.PathValue("applicationID")
	app, err := s.Store.ApplicationByID(r.Context(), applicationID)
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "deployer") {
		return
	}
	record, err := s.Syncer.BuildPlan(r.Context(), app.ID, currentUser(r).ID)
	if err != nil {
		var conflicts *syncer.OwnershipConflicts
		if errors.As(err, &conflicts) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": conflicts.Error(), "conflict": conflicts.Items[0], "conflicts": conflicts.Items})
			return
		}
		var conflict *syncer.OwnershipConflict
		if errors.As(err, &conflict) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": conflict.Error(), "conflict": conflict})
			return
		}
		s.Logger.Warn("plan calculation failed", "applicationId", app.ID, "error", err)
		writeError(w, http.StatusUnprocessableEntity, "could not calculate a safe plan")
		return
	}
	writeJSON(w, http.StatusCreated, toPlanView(record))
}

func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "viewer") {
		return
	}
	items, err := s.Store.ListPlans(r.Context(), app.ID, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load plans")
		return
	}
	views := make([]planView, 0, len(items))
	for _, item := range items {
		views = append(views, toPlanView(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (s *Server) getPlan(w http.ResponseWriter, r *http.Request) {
	record, err := s.Store.PlanByID(r.Context(), r.PathValue("planID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "plan not found")
		return
	}
	app, err := s.Store.ApplicationByID(r.Context(), record.Plan.ApplicationID)
	if err != nil || !s.requireProjectRole(w, r, app.ProjectID, "viewer") {
		if err != nil {
			writeError(w, http.StatusNotFound, "application not found")
		}
		return
	}
	writeJSON(w, http.StatusOK, toPlanView(record))
}

func (s *Server) approvePlan(w http.ResponseWriter, r *http.Request) {
	record, app, ok := s.authorizedPlan(w, r, "viewer")
	if !ok {
		return
	}
	required := core.RequiredApprovalCount(record.Plan)
	if required == 0 {
		writeError(w, http.StatusConflict, "this plan does not require approval")
		return
	}
	role, roleErr := s.Store.ProjectRole(r.Context(), currentUser(r), app.ProjectID)
	if roleErr != nil || role == "" || !core.ApprovalRoleAllows(record.Plan, role, currentUser(r).ID) {
		writeError(w, http.StatusForbidden, "you are not an eligible approver for this plan")
		return
	}
	fresh, err := s.Syncer.RecheckPlan(r.Context(), app, record)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not safely recheck the plan")
		return
	}
	if record.Status != "current" || !time.Now().Before(record.ExpiresAt) || fresh.Digest != record.Plan.Digest {
		refreshed, err := s.Syncer.RefreshPlan(r.Context(), app, currentUser(r).ID, record)
		if err != nil {
			writeError(w, http.StatusConflict, "plan is no longer current; refresh it before approval")
			return
		}
		writeJSON(w, http.StatusConflict, map[string]any{"error": "plan is stale; review the refreshed plan", "plan": toPlanView(refreshed)})
		return
	}
	activeApprovals, err := s.Store.ListPlanApprovals(r.Context(), record.ID, record.Plan.Digest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load existing approvals")
		return
	}
	approvedActors := make(map[string]bool, len(activeApprovals))
	for _, existing := range activeApprovals {
		if existing.Approval.ActorID == currentUser(r).ID {
			writeError(w, http.StatusConflict, "you have already approved this plan")
			return
		}
		approver, userErr := s.Store.UserByID(r.Context(), existing.Approval.ActorID)
		approverRole := ""
		if userErr == nil {
			approverRole, userErr = s.Store.ProjectRole(r.Context(), approver, app.ProjectID)
		}
		if userErr == nil && approverRole != "" && core.ApprovalRoleAllows(record.Plan, approverRole, existing.Approval.ActorID) {
			approvedActors[existing.Approval.ActorID] = true
		}
	}
	if len(approvedActors) >= required {
		writeError(w, http.StatusConflict, "this plan already has all required approvals")
		return
	}
	deletes := make([]core.Change, 0)
	privileged := make([]core.Change, 0)
	for _, change := range record.Plan.Changes {
		if change.Kind == core.Delete {
			deletes = append(deletes, change)
		}
		if change.Identity.ClusterScoped {
			privileged = append(privileged, change)
		}
	}
	approvalID := store.NewID()
	expires := time.Now().Add(10 * time.Minute)
	if record.ExpiresAt.Before(expires) {
		expires = record.ExpiresAt
	}
	approval := core.DeletionApproval{PlanDigest: record.Plan.Digest, ActorID: currentUser(r).ID, Deletes: deletes, Privileged: privileged, ExpiresAt: expires}
	if err := s.Store.CreateApproval(r.Context(), approvalID, record.ID, approval, expires); err != nil {
		if strings.Contains(err.Error(), "already") || strings.Contains(err.Error(), "required approvals") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if strings.Contains(err.Error(), "eligible") {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		if strings.Contains(err.Error(), "plan is no longer current") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "could not save approval")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "plan.approved", "application", app.ID, map[string]any{"planId": record.ID, "approvalId": approvalID, "digest": record.Plan.Digest, "kind": record.Plan.ApprovalKind, "deletions": len(deletes), "privilegedChanges": len(privileged)})
	writeJSON(w, http.StatusCreated, map[string]any{"id": approvalID, "planId": record.ID, "planDigest": record.Plan.Digest, "expiresAt": expires, "requiredApprovals": required, "approvedApprovals": len(approvedActors) + 1})
}

func (s *Server) listPlanApprovals(w http.ResponseWriter, r *http.Request) {
	record, app, ok := s.authorizedPlan(w, r, "viewer")
	if !ok {
		return
	}
	required := core.RequiredApprovalCount(record.Plan)
	items, err := s.Store.ListPlanApprovals(r.Context(), record.ID, record.Plan.Digest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load plan approvals")
		return
	}
	type approvalView struct {
		ID          string    `json:"id"`
		ActorID     string    `json:"actorId"`
		DisplayName string    `json:"displayName"`
		Email       string    `json:"email"`
		Role        string    `json:"role,omitempty"`
		Eligible    bool      `json:"eligible"`
		ExpiresAt   time.Time `json:"expiresAt"`
	}
	approvals := make([]approvalView, 0, len(items))
	approvedActors := make(map[string]bool, len(items))
	currentUserApproved := false
	for _, item := range items {
		user, userErr := s.Store.UserByID(r.Context(), item.Approval.ActorID)
		role := ""
		if userErr == nil {
			role, _ = s.Store.ProjectRole(r.Context(), user, app.ProjectID)
		}
		eligible := userErr == nil && role != "" && core.ApprovalRoleAllows(record.Plan, role, item.Approval.ActorID)
		name, email := "Former project member", ""
		if userErr == nil {
			name, email = user.DisplayName, user.Email
			if name == "" {
				name = email
			}
		}
		approvals = append(approvals, approvalView{ID: item.ID, ActorID: item.Approval.ActorID, DisplayName: name, Email: email, Role: role, Eligible: eligible, ExpiresAt: item.Approval.ExpiresAt})
		if eligible {
			approvedActors[item.Approval.ActorID] = true
			if item.Approval.ActorID == currentUser(r).ID {
				currentUserApproved = true
			}
		}
	}
	currentRole, roleErr := s.Store.ProjectRole(r.Context(), currentUser(r), app.ProjectID)
	canApprove := required > 0 && record.Status == "current" && time.Now().Before(record.ExpiresAt) && roleErr == nil && currentRole != "" && core.ApprovalRoleAllows(record.Plan, currentRole, currentUser(r).ID) && !currentUserApproved && len(approvedActors) < required
	writeJSON(w, http.StatusOK, map[string]any{
		"planId":              record.ID,
		"planDigest":          record.Plan.Digest,
		"approvalKind":        record.Plan.ApprovalKind,
		"requiredApprovals":   required,
		"approvedApprovals":   len(approvedActors),
		"approverRoles":       record.Plan.ApproverRoles,
		"approverUserIds":     record.Plan.ApproverUserIDs,
		"currentUserApproved": currentUserApproved,
		"canApprove":          canApprove,
		"approvals":           approvals,
	})
}

func (s *Server) applyPlan(w http.ResponseWriter, r *http.Request) {
	record, app, ok := s.authorizedPlan(w, r, "deployer")
	if !ok {
		return
	}
	if record.Plan.Decommission && !s.requireProjectRole(w, r, app.ProjectID, "owner") {
		return
	}
	var input struct {
		ApprovalIDs []string `json:"approvalIds"`
		ApprovalID  string   `json:"approvalId"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.ApprovalID != "" && len(input.ApprovalIDs) > 0 {
		writeError(w, http.StatusBadRequest, "use either approvalId or approvalIds")
		return
	}
	approvalIDs := input.ApprovalIDs
	if input.ApprovalID != "" {
		approvalIDs = []string{input.ApprovalID}
	}
	operation, err := s.Syncer.ApplyWithApprovals(r.Context(), record.ID, currentUser(r).ID, approvalIDs)
	if err != nil {
		var stale *syncer.StalePlanError
		if errors.As(err, &stale) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "plan is stale; review the refreshed plan", "plan": toPlanView(stale.Fresh)})
			return
		}
		if strings.Contains(err.Error(), "approv") || strings.Contains(err.Error(), "already has an active operation") || strings.Contains(err.Error(), "changed after approval") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		s.Logger.Error("plan apply failed", "applicationId", app.ID, "planId", record.ID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "sync failed", "operation": operation})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"operation": operation})
}

func (s *Server) createPlanSelection(w http.ResponseWriter, r *http.Request) {
	record, app, ok := s.authorizedPlan(w, r, "deployer")
	if !ok {
		return
	}
	if record.Plan.Decommission {
		writeError(w, http.StatusBadRequest, "decommission plans cannot exclude managed resource deletions")
		return
	}
	var input core.PlanSelection
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Resources) > 10000 || len(input.Fields) > 10000 {
		writeError(w, http.StatusBadRequest, "plan selection contains too many exclusions")
		return
	}
	freshSource, _, err := s.Syncer.CalculatePlanWithSelection(r.Context(), app, record.Plan.Selection)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not safely recheck the plan")
		return
	}
	if record.Status != "current" || !time.Now().Before(record.ExpiresAt) || freshSource.Digest != record.Plan.Digest {
		refreshed, buildErr := s.Syncer.BuildPlanWithSelection(r.Context(), app, currentUser(r).ID, record.Plan.Selection)
		if buildErr != nil {
			writeError(w, http.StatusConflict, "plan is stale; refresh it before changing the selection")
			return
		}
		writeJSON(w, http.StatusConflict, map[string]any{"error": "plan is stale; review the refreshed plan", "plan": toPlanView(refreshed)})
		return
	}
	baseline, _, err := s.Syncer.CalculatePlan(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not safely validate the selected changes")
		return
	}
	availableResources := map[string]bool{}
	availableFields := map[string]map[string]bool{}
	for _, change := range baseline.Changes {
		key := change.Identity.Key()
		availableResources[key] = true
		availableFields[key] = map[string]bool{}
		alreadyIgnored := map[string]bool{}
		for _, path := range change.IgnoredPaths {
			alreadyIgnored[path] = true
		}
		for _, path := range change.ChangedPaths {
			canonical, canonicalErr := render.CanonicalJSONPointer(path)
			if canonicalErr == nil && !alreadyIgnored[canonical] {
				availableFields[key][canonical] = true
			}
		}
	}
	for i := range input.Resources {
		identity := &input.Resources[i]
		if identity.ClusterID == "" {
			identity.ClusterID = app.ClusterID
		}
		identity.ClusterScoped = identity.Namespace == ""
		if !availableResources[identity.Key()] {
			writeError(w, http.StatusBadRequest, "resource exclusion must target an exact resource with a change in this plan")
			return
		}
	}
	for i := range input.Fields {
		field := &input.Fields[i]
		if field.Identity.ClusterID == "" {
			field.Identity.ClusterID = app.ClusterID
		}
		field.Identity.ClusterScoped = field.Identity.Namespace == ""
		canonical, pathErr := render.CanonicalJSONPointer(field.Path)
		if pathErr != nil {
			writeError(w, http.StatusBadRequest, pathErr.Error())
			return
		}
		field.Path = canonical
		if !availableFields[field.Identity.Key()][field.Path] {
			writeError(w, http.StatusBadRequest, "field exclusion must target a changed field in this plan; arrays can only be excluded as a whole")
			return
		}
	}
	newRecord, err := s.Syncer.BuildPlanWithSelection(r.Context(), app, currentUser(r).ID, input)
	if err != nil {
		s.Logger.Warn("selected plan calculation failed", "applicationId", app.ID, "error", err)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "could not safely create a selected plan"})
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "plan.selection_created", "application", app.ID, map[string]any{"sourcePlanId": record.ID, "planId": newRecord.ID, "digest": newRecord.Plan.Digest, "excludedResources": len(input.Resources), "excludedFields": len(input.Fields)})
	writeJSON(w, http.StatusCreated, toPlanView(newRecord))
}

func (s *Server) listIgnoreRules(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "viewer") {
		return
	}
	items, err := s.Store.IgnoreRules(r.Context(), app.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load ignore rules")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createIgnoreRule(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "owner") {
		return
	}
	var input struct {
		Identity core.Identity `json:"identity"`
		Path     string        `json:"path"`
		Reason   string        `json:"reason"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	input.Path = strings.TrimSpace(input.Path)
	if len(input.Reason) < 5 || len(input.Reason) > 500 {
		writeError(w, http.StatusBadRequest, "an ignore reason between 5 and 500 characters is required")
		return
	}
	if input.Identity.ClusterID == "" {
		input.Identity.ClusterID = app.ClusterID
	}
	input.Identity.ClusterScoped = input.Identity.Namespace == ""
	if input.Path != "" {
		canonical, pathErr := render.CanonicalJSONPointer(input.Path)
		if pathErr != nil {
			writeError(w, http.StatusBadRequest, pathErr.Error())
			return
		}
		input.Path = canonical
	}
	if err := s.Syncer.ValidateIgnoreRule(r.Context(), app, input.Identity, input.Path); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return
	}
	rule := core.IgnoreRule{ID: store.NewID(), Identity: input.Identity, Path: input.Path, Reason: input.Reason}
	if err := s.Store.CreateIgnoreRule(r.Context(), app.ID, currentUser(r).ID, rule); err != nil {
		if strings.Contains(err.Error(), "currently syncing") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusConflict, "an identical ignore rule already exists or could not be saved")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.ignore_rule_created", "application", app.ID, map[string]any{"ruleId": rule.ID, "identity": rule.Identity, "path": rule.Path, "reason": rule.Reason})
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) deleteIgnoreRule(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "owner") {
		return
	}
	rule, err := s.Store.DeleteIgnoreRule(r.Context(), app.ID, r.PathValue("ruleID"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "ignore rule not found")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "currently syncing") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "could not delete ignore rule")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "application.ignore_rule_deleted", "application", app.ID, map[string]any{"ruleId": rule.ID, "identity": rule.Identity, "path": rule.Path, "reason": rule.Reason})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authorizedPlan(w http.ResponseWriter, r *http.Request, minimum string) (store.PlanRecord, store.Application, bool) {
	record, err := s.Store.PlanByID(r.Context(), r.PathValue("planID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "plan not found")
		return store.PlanRecord{}, store.Application{}, false
	}
	app, err := s.Store.ApplicationByID(r.Context(), record.Plan.ApplicationID)
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return store.PlanRecord{}, store.Application{}, false
	}
	if !s.requireProjectRole(w, r, app.ProjectID, minimum) {
		return store.PlanRecord{}, store.Application{}, false
	}
	return record, app, true
}

func (s *Server) listApplicationResources(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "viewer") {
		return
	}
	managed, err := s.Store.ManagedResources(r.Context(), app.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load application resources")
		return
	}
	type resourceView struct {
		Identity        core.Identity `json:"identity"`
		UID             string        `json:"uid"`
		ResourceVersion string        `json:"resourceVersion"`
		Adopted         bool          `json:"adopted"`
	}
	items := make([]resourceView, 0, len(managed))
	for _, item := range managed {
		item.Identity.ClusterScoped = item.Identity.Namespace == ""
		items = append(items, resourceView{Identity: item.Identity, UID: item.UID, ResourceVersion: item.ResourceVersion, Adopted: item.Adopted})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listApplicationOperations(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.ApplicationByID(r.Context(), r.PathValue("applicationID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireProjectRole(w, r, app.ProjectID, "viewer") {
		return
	}
	items, err := s.Store.ListOperations(r.Context(), app.ID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load application operations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

var _ = errors.Is
