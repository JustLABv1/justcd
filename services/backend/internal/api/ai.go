package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/ai"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) aiRoutes() {
	s.Mux.Handle("GET /api/v1/admin/ai", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.getAISettings))))
	s.Mux.Handle("PUT /api/v1/admin/ai", s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.putAISettings)))))
	s.Mux.Handle("POST /api/v1/admin/ai/test", s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.testAIConnection)))))
	s.Mux.Handle("GET /api/v1/workspaces/{workspaceID}/ai", s.requireAuth(http.HandlerFunc(s.workspaceAI)))
	s.Mux.Handle("PUT /api/v1/workspaces/{workspaceID}/ai", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.workspaceAI))))
	s.Mux.Handle("POST /api/v1/workspaces/{workspaceID}/ai/analyze", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.analyzeAI))))
}
func (s *Server) getAISettings(w http.ResponseWriter, r *http.Request) {
	settings, cipher, err := s.Store.AISettings(r.Context())
	if err != nil {
		writeStoreError(w, "could not load AI settings")
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": settings.Enabled, "baseUrl": settings.BaseURL, "model": settings.Model, "maxTokens": settings.MaxTokens, "hasApiKey": len(cipher) > 0})
}
func (s *Server) putAISettings(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ai.Settings
		APIKey      *string `json:"apiKey"`
		ClearAPIKey bool    `json:"clearApiKey"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		writeError(w, 400, "invalid AI settings")
		return
	}
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.Model = strings.TrimSpace(input.Model)
	if input.MaxTokens == 0 {
		input.MaxTokens = 2048
	}
	if input.APIKey != nil {
		input.Settings.APIKey = *input.APIKey
	}
	if err := ai.Validate(input.Settings); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	previous, cipher, err := s.Store.AISettings(r.Context())
	if err != nil {
		writeStoreError(w, "could not load AI settings")
		return
	}
	if previous.BaseURL != input.BaseURL {
		cipher = nil
	}
	if input.ClearAPIKey {
		cipher = nil
	} else if input.APIKey != nil && *input.APIKey != "" {
		cipher, err = security.Encrypt(s.EncryptionKey, []byte(*input.APIKey), "ai:instance")
		if err != nil {
			writeStoreError(w, "could not encrypt AI key")
			return
		}
	}
	if err = s.Store.SaveAISettings(r.Context(), input.Settings, cipher, currentUser(r).ID); err != nil {
		writeStoreError(w, "could not save AI settings")
		return
	}
	s.getAISettings(w, r)
}
func (s *Server) loadAI(ctx context.Context) (ai.Settings, error) {
	settings, cipher, err := s.Store.AISettings(ctx)
	if err != nil {
		return settings, err
	}
	if len(cipher) > 0 {
		plain, e := security.Decrypt(s.EncryptionKey, cipher, "ai:instance")
		if e != nil {
			return settings, e
		}
		settings.APIKey = string(plain)
	}
	return settings, nil
}
func (s *Server) testAIConnection(w http.ResponseWriter, r *http.Request) {
	settings, err := s.loadAI(r.Context())
	if err != nil {
		writeStoreError(w, "could not load AI connection")
		return
	}
	if !s.takeAISlot(w, r) {
		return
	}
	defer s.releaseAISlot(currentUser(r).ID)
	_, err = ai.Analyze(r.Context(), settings, "Confirm the connection works using the supplied test evidence.", []ai.Evidence{{ID: "test", Label: "Connection test", Data: map[string]bool{"connectionTest": true}}}, nil)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) workspaceAI(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspaceID")
	role := "viewer"
	if r.Method == http.MethodPut {
		role = "owner"
	}
	if !s.requireWorkspaceRole(w, r, id, role) {
		return
	}
	if r.Method == http.MethodPut {
		var input struct {
			Enabled bool `json:"enabled"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			writeError(w, 400, "invalid workspace AI settings")
			return
		}
		if err := s.Store.SetWorkspaceAI(r.Context(), id, currentUser(r).ID, input.Enabled); err != nil {
			writeStoreError(w, "could not update workspace AI settings")
			return
		}
	}
	enabled, err := s.Store.WorkspaceAIEnabled(r.Context(), id)
	if err != nil {
		writeStoreError(w, "could not load workspace AI settings")
		return
	}
	settings, _, err := s.Store.AISettings(r.Context())
	if err != nil {
		writeStoreError(w, "could not load AI availability")
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": enabled, "available": settings.Enabled, "model": settings.Model})
}
func (s *Server) takeAISlot(w http.ResponseWriter, r *http.Request) bool {
	s.aiMu.Lock()
	defer s.aiMu.Unlock()
	if s.aiRequests == nil {
		s.aiRequests = map[string]time.Time{}
		s.aiActive = map[string]bool{}
	}
	id := currentUser(r).ID
	now := time.Now()
	if s.aiActive[id] || now.Sub(s.aiRequests[id]) < 5*time.Second {
		w.Header().Set("Retry-After", "5")
		writeError(w, 429, "wait a few seconds before starting another AI request")
		return false
	}
	active := 0
	for _, running := range s.aiActive {
		if running {
			active++
		}
	}
	if active >= 8 {
		writeError(w, 429, "AI is busy; try again shortly")
		return false
	}
	for key, last := range s.aiRequests {
		if now.Sub(last) > time.Hour && !s.aiActive[key] {
			delete(s.aiRequests, key)
			delete(s.aiActive, key)
		}
	}
	s.aiRequests[id] = now
	s.aiActive[id] = true
	return true
}
func (s *Server) releaseAISlot(id string) {
	s.aiMu.Lock()
	defer s.aiMu.Unlock()
	delete(s.aiActive, id)
}

type aiInput struct {
	IncludeLogs   bool         `json:"includeLogs"`
	Feature       string       `json:"feature"`
	ApplicationID string       `json:"applicationId"`
	GroupID       string       `json:"groupId"`
	PlanID        string       `json:"planId"`
	OperationID   string       `json:"operationId"`
	Question      string       `json:"question"`
	History       []ai.Message `json:"history"`
}

var aiTasks = map[string]string{
	"plan":     "Explain this exact deployment plan: intent, material changes, availability/security/storage impact, destructive changes, and approval requirements. Do not assert safety from missing information.",
	"diagnose": "Diagnose application health or failed deployment from observed conditions, operation progress, and recent changes. Rank likely causes, distinguish hypotheses from facts, and suggest specific read-only checks.",
	"drift":    "Explain live-versus-desired drift, ignored changes and field ownership. Treat possible controllers/defaults as hypotheses unless confirmed. Explain tradeoffs of any proposed ignore rule.",
	"summary":  "Summarize the selected operation (or recent deployment history), revision, outcome, timing, retries, resource failures, and observed recovery. Do not infer recovery without evidence.",
	"compare":  "Compare deployment-group targets: revisions, effective rendered configuration, resource health, and operations. Explain observed differences and possible causes of different behavior. Raw Helm values are excluded; use rendered projections.",
	"ask":      "Answer the user's workspace question using only the supplied authorized context: ",
}

func (s *Server) analyzeAI(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspaceID")
	if !s.requireWorkspaceRole(w, r, workspace, "viewer") {
		return
	}
	var input aiInput
	r.Body = http.MaxBytesReader(w, r.Body, 24000)
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		writeError(w, 400, "invalid AI request")
		return
	}
	task, ok := aiTasks[input.Feature]
	if !ok {
		writeError(w, 400, "unknown AI feature")
		return
	}
	if input.Feature != "diagnose" {
		input.IncludeLogs = false
	}
	if input.Feature == "compare" && input.ApplicationID != "" {
		writeError(w, 400, "comparison requires a group scope")
		return
	}
	if len(input.Question) > 4000 || len(input.History) > 8 {
		writeError(w, 400, "question or conversation exceeds the limit")
		return
	}
	for _, m := range input.History {
		if (m.Role != "user" && m.Role != "assistant") || len(m.Content) > 2000 {
			writeError(w, 400, "invalid conversation history")
			return
		}
	}
	if input.Feature == "ask" && strings.TrimSpace(input.Question) == "" {
		writeError(w, 400, "enter a question")
		return
	}
	if input.Feature != "ask" {
		input.History = nil
		input.Question = ""
	}
	if input.Feature == "compare" && input.GroupID == "" {
		writeError(w, 400, "choose a deployment group")
		return
	}
	if input.Feature != "ask" && input.Feature != "compare" && input.ApplicationID == "" {
		writeError(w, 400, "choose an application")
		return
	}
	enabled, err := s.Store.WorkspaceAIEnabled(r.Context(), workspace)
	if err != nil {
		writeStoreError(w, "could not load workspace AI policy")
		return
	}
	if !enabled {
		writeError(w, 409, "a workspace owner must enable AI in workspace settings first")
		return
	}
	settings, err := s.loadAI(r.Context())
	if err != nil {
		writeStoreError(w, "could not load AI connection")
		return
	}
	if !settings.Enabled {
		writeError(w, 409, "an administrator must configure and enable the AI connection first")
		return
	}
	if !s.takeAISlot(w, r) {
		return
	}
	defer s.releaseAISlot(currentUser(r).ID)
	var apps []store.Application
	if input.ApplicationID != "" {
		app, e := s.Store.ApplicationByID(r.Context(), input.ApplicationID)
		if e != nil || app.WorkspaceID != workspace {
			writeError(w, 404, "application not found in this workspace")
			return
		}
		apps = []store.Application{app}
	} else if input.GroupID != "" {
		group, e := s.Store.ApplicationGroupByID(r.Context(), input.GroupID)
		if e != nil || group.WorkspaceID != workspace {
			writeError(w, 404, "deployment group not found in this workspace")
			return
		}
		apps, err = s.Store.ApplicationsByGroupID(r.Context(), group.ID)
	} else {
		apps, err = s.Store.ListApplications(r.Context(), workspace)
	}
	if err != nil {
		writeStoreError(w, "could not load AI scope")
		return
	}
	if len(apps) > 30 {
		writeError(w, 400, "scope exceeds 30 applications; choose an application or a smaller deployment group")
		return
	}
	evidence := []ai.Evidence{{ID: "scope", Label: "Analysis scope", URL: "/workspaces/" + workspace, Data: map[string]any{"workspaceId": workspace, "applicationCount": len(apps), "collectedAt": time.Now().UTC(), "limitations": []string{"Stored observations and recent history are bounded. Diagnosis may include live events and explicitly opted-in log tails. Metrics, raw Helm values, Secret/ConfigMap contents, environment values, commands, and arbitrary custom-resource specs are excluded. Log redaction is best-effort.", "Manifest projections omit fields that may affect behavior. Missing evidence does not prove a setting is absent from Kubernetes."}}}}
	analysisCtx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	for _, app := range apps {
		if err := s.appendAIApplication(analysisCtx, &evidence, app, input); err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	if err = s.Store.Audit(r.Context(), currentUser(r).ID, "ai.analysis_requested", "workspace", workspace, map[string]any{"feature": input.Feature, "applicationId": input.ApplicationID, "groupId": input.GroupID, "model": settings.Model, "includeLogs": input.IncludeLogs}); err != nil {
		writeStoreError(w, "could not audit AI request")
		return
	}
	result, err := ai.Analyze(analysisCtx, settings, task+input.Question, evidence, input.History)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	result.Limitations = append(result.Limitations, "Analysis uses bounded snapshots and filtered manifests. Raw Helm values, Secret/ConfigMap contents, environment values, commands, and arbitrary custom-resource specs are excluded; omitted fields can affect diagnosis.")
	result.Summary = redactAPIErrorMessage(result.Summary)
	for i := range result.Findings {
		result.Findings[i].Title = redactAPIErrorMessage(result.Findings[i].Title)
		result.Findings[i].Detail = redactAPIErrorMessage(result.Findings[i].Detail)
	}
	for i := range result.NextSteps {
		result.NextSteps[i] = redactAPIErrorMessage(result.NextSteps[i])
	}
	for i := range result.Limitations {
		result.Limitations[i] = redactAPIErrorMessage(result.Limitations[i])
	}
	writeJSON(w, 200, result)
}
func (s *Server) appendAIApplication(ctx context.Context, evidence *[]ai.Evidence, app store.Application, input aiInput) error {
	base := "/applications/" + app.ID
	add := func(id, label, url string, data any) {
		*evidence = append(*evidence, ai.Evidence{ID: id, Label: label, URL: url, Data: aiScrub(data)})
	}
	namespaces := []string{}
	for _, binding := range app.Namespaces {
		namespaces = append(namespaces, binding.Namespace)
	}
	add("app:"+app.ID, app.Name, base, map[string]any{"id": app.ID, "name": app.Name, "cluster": app.ClusterName, "clusterId": app.ClusterID, "revision": app.Revision, "lastSyncedRevision": app.LastSyncedRevision, "namespaces": namespaces, "renderer": app.Renderer, "manifestPath": app.ManifestPath, "targetManifestPath": app.TargetManifestPath, "namespaceManifestPaths": app.NamespaceManifestPaths, "syncPolicy": app.SyncPolicy, "autoSyncPaused": app.AutoSyncPaused, "syncHealth": app.Health, "runtimeHealth": app.HealthCondition, "statusIssues": app.StatusIssues, "checkedAt": app.LastCheckedAt})
	if input.Feature == "diagnose" && s.Syncer != nil {
		diagnostics, e := s.Syncer.AIDiagnostics(ctx, app, input.IncludeLogs)
		if e != nil {
			add("diagnostics:"+app.ID, "Live diagnostic availability", base, map[string]string{"warning": "Live events/logs could not be read; analysis uses stored observations"})
		} else {
			add("diagnostics:"+app.ID, "Live events and optional logs", base, diagnostics)
		}
	}
	plans, err := s.Store.ListPlans(ctx, app.ID, 3)
	if err != nil {
		return fmt.Errorf("could not load plan evidence")
	}
	if input.PlanID != "" {
		selected, e := s.Store.PlanByID(ctx, input.PlanID)
		if e != nil || selected.Plan.ApplicationID != app.ID {
			return fmt.Errorf("selected plan does not belong to the application")
		}
		plans = []store.PlanRecord{selected}
	}
	if (input.Feature == "plan" || input.Feature == "drift") && len(plans) == 0 {
		return fmt.Errorf("create a plan before requesting an explanation")
	}
	for _, p := range plans {
		changes := []any{}
		for _, c := range p.Plan.Changes {
			if len(changes) >= 100 {
				break
			}
			changes = append(changes, aiChange(c))
		}
		ignored := []any{}
		for _, c := range p.Plan.Ignored {
			if len(ignored) >= 30 {
				break
			}
			ignored = append(ignored, aiChange(c))
		}
		approvals, e := s.Store.ListPlanApprovals(ctx, p.ID, p.Plan.Digest)
		if e != nil {
			return fmt.Errorf("could not load approval evidence")
		}
		desired := []any{}
		for index, resource := range p.Desired {
			if index >= 80 {
				break
			}
			desired = append(desired, map[string]any{"identity": resource.Identity, "configuration": aiManifest(resource.Manifest)})
		}
		approvalSummary := []any{}
		for _, a := range approvals {
			approvalSummary = append(approvalSummary, map[string]any{"createdAt": a.CreatedAt, "expiresAt": a.Approval.ExpiresAt})
		}
		add("plan:"+p.ID, "Plan "+p.ID, base+"?tab=changes&planId="+p.ID, map[string]any{"id": p.ID, "digest": p.Plan.Digest, "revision": p.Plan.Revision, "status": p.Status, "createdAt": p.CreatedAt, "expiresAt": p.ExpiresAt, "requiresApproval": p.Plan.RequiresApproval, "requiredApprovals": p.Plan.RequiredApprovals, "approvalRecords": approvalSummary, "renderedResources": desired, "totalRenderedResources": len(p.Desired), "renderedResourcesTruncated": len(p.Desired) > 80, "approvalReasons": p.Plan.ApprovalReasons, "approverRoles": p.Plan.ApproverRoles, "changes": changes, "ignored": ignored, "totalChanges": len(p.Plan.Changes), "totalIgnored": len(p.Plan.Ignored), "truncated": len(p.Plan.Changes) > 100 || len(p.Plan.Ignored) > 30})
	}
	operations, err := s.Store.ListOperations(ctx, app.ID, 10)
	if err != nil {
		return fmt.Errorf("could not load operation evidence")
	}
	found := input.OperationID == ""
	for _, op := range operations {
		if input.OperationID != "" && op.ID != input.OperationID {
			continue
		}
		found = true
		revision := ""
		if op.PlanID != nil {
			record, e := s.Store.PlanByID(ctx, *op.PlanID)
			if e == nil && record.Plan.ApplicationID == app.ID {
				revision = record.Plan.Revision
			}
		}
		add("operation:"+op.ID, "Operation "+op.ID, base+"?tab=activity", map[string]any{"id": op.ID, "planId": op.PlanID, "revision": revision, "type": op.Type, "status": op.Status, "message": op.Message, "progress": op.Progress, "attemptCount": op.AttemptCount, "errorCode": op.ErrorCode, "terminalReason": op.TerminalReason, "startedAt": op.StartedAt, "finishedAt": op.FinishedAt})
	}
	if !found {
		return fmt.Errorf("selected operation is not in recent application history")
	}
	observed, err := s.Store.ObservedResources(ctx, app.ID)
	if err != nil {
		return fmt.Errorf("could not load resource evidence")
	}
	for index, item := range observed {
		if index >= 80 {
			add("resource-limit:"+app.ID, "Resource evidence limit", base, map[string]any{"total": len(observed), "included": 80})
			break
		}
		add("resource:"+app.ID+":"+fmt.Sprint(index), item.Identity.Kind+" "+item.Identity.Namespace+"/"+item.Identity.Name, base, map[string]any{"identity": item.Identity, "phase": item.Phase, "readiness": item.Readiness, "health": item.HealthSummary, "observedAt": item.ObservedAt, "source": item.Source})
	}
	transitions, err := s.Store.ApplicationHealthTransitions(ctx, app.ID, 10)
	if err != nil {
		return fmt.Errorf("could not load health history")
	}
	add("health:"+app.ID, app.Name+" health history", base+"?tab=activity", transitions)
	return nil
}
func aiChange(c core.Change) any {
	return map[string]any{"kind": c.Kind, "identity": c.Identity, "changedPaths": c.ChangedPaths, "ignoredPaths": c.IgnoredPaths, "ignoreReason": c.IgnoreReason, "takeover": c.Takeover, "recreationRequired": c.RecreationRequired, "recreation": c.Recreation, "recreationBlocked": c.RecreationBlocked, "before": aiManifest(c.Before), "after": aiManifest(c.After)}
}

// Only known structural fields leave the backend. Arbitrary manifests never do.
func aiManifest(raw json.RawMessage) any {
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	kind, _ := root["kind"].(string)
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob", "Pod", "Service", "Ingress", "PersistentVolumeClaim", "HorizontalPodAutoscaler":
	default:
		return map[string]any{"kind": kind, "specOmitted": true}
	}
	return aiProject(root)
}

var aiAllowed = map[string]bool{"kind": true, "apiVersion": true, "spec": true, "template": true, "jobTemplate": true, "replicas": true, "strategy": true, "type": true, "rollingUpdate": true, "maxSurge": true, "maxUnavailable": true, "containers": true, "initContainers": true, "image": true, "imagePullPolicy": true, "resources": true, "requests": true, "limits": true, "cpu": true, "memory": true, "storage": true, "ports": true, "containerPort": true, "port": true, "targetPort": true, "protocol": true, "readinessProbe": true, "livenessProbe": true, "startupProbe": true, "initialDelaySeconds": true, "periodSeconds": true, "timeoutSeconds": true, "failureThreshold": true, "successThreshold": true, "httpGet": true, "tcpSocket": true, "path": true, "scheme": true, "minReplicas": true, "maxReplicas": true, "restartPolicy": true, "storageClassName": true, "accessModes": true, "serviceAccountName": true, "securityContext": true, "runAsNonRoot": true, "runAsUser": true, "privileged": true, "allowPrivilegeEscalation": true, "readOnlyRootFilesystem": true, "automountServiceAccountToken": true, "schedule": true, "suspend": true}

func aiProject(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, item := range value {
			if aiAllowed[k] {
				out[k] = aiProject(item)
			}
		}
		return out
	case []any:
		out := []any{}
		for i, item := range value {
			if i >= 30 {
				break
			}
			out = append(out, aiProject(item))
		}
		return out
	default:
		return v
	}
}
func aiScrub(data any) any {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return aiScrubValue(value)
}
func aiScrubValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for k, item := range v {
			v[k] = aiScrubValue(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = aiScrubValue(item)
		}
		return v
	case string:
		return redactAPIErrorMessage(v)
	default:
		return value
	}
}
