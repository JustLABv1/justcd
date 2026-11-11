package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) mcpRoutes() {
	s.Mux.Handle("GET /api/v1/mcp-policy", s.requireAuth(http.HandlerFunc(s.getMCPPolicy)))
	s.Mux.Handle("PUT /api/v1/admin/mcp-policy", s.requireAuth(s.requireAdmin(s.requireCSRF(http.HandlerFunc(s.updateMCPPolicy)))))
	s.Mux.Handle("GET /api/v1/mcp-credentials", s.requireAuth(http.HandlerFunc(s.listMCPCredentials)))
	s.Mux.Handle("POST /api/v1/mcp-credentials", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.createMCPCredential))))
	s.Mux.Handle("DELETE /api/v1/mcp-credentials/{credentialID}", s.requireAuth(s.requireCSRF(http.HandlerFunc(s.revokeMCPCredential))))
	s.Mux.HandleFunc("/api/v1/mcp", s.serveMCP)
}
func (s *Server) listMCPCredentials(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListMCPCredentials(r.Context(), currentUser(r).ID)
	if err != nil {
		writeStoreError(w, "could not list MCP credentials")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) createMCPCredential(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name          string `json:"name"`
		WorkspaceID   string `json:"workspaceId"`
		AllowPlan     bool   `json:"allowPlan"`
		ExpiresInDays *int   `json:"expiresInDays"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 100 || input.WorkspaceID == "" {
		writeError(w, 400, "name and workspaceId are required")
		return
	}
	days := 30
	if input.ExpiresInDays != nil {
		days = *input.ExpiresInDays
	}
	if !slices.Contains([]int{7, 30, 90, 365, 0}, days) {
		writeError(w, 400, "expiresInDays must be 7, 30, 90, 365 or 0 (no expiry)")
		return
	}
	role := "viewer"
	if input.AllowPlan {
		role = "deployer"
	}
	if !s.requireWorkspaceRole(w, r, input.WorkspaceID, role) {
		return
	}
	token, _, err := security.RandomToken(32)
	if err != nil {
		writeStoreError(w, "could not create MCP credential")
		return
	}
	token = "justcd_mcp_" + token
	now := time.Now()
	var expires *time.Time
	if days != 0 {
		value := now.Add(time.Duration(days) * 24 * time.Hour)
		expires = &value
	}
	c := store.MCPCredential{ID: store.NewID(), UserID: currentUser(r).ID, WorkspaceID: input.WorkspaceID, Name: input.Name, AllowPlan: input.AllowPlan, CreatedAt: now, ExpiresAt: expires}
	if err := s.Store.CreateMCPCredential(r.Context(), c, security.HashToken(token), days); err != nil {
		var rejected *store.AccessError
		if errors.As(err, &rejected) {
			writeError(w, 400, rejected.Message)
			return
		}
		writeStoreError(w, "could not save MCP credential")
		return
	}
	_ = s.Store.Audit(r.Context(), c.UserID, "mcp.credential.created", "workspace", c.WorkspaceID, map[string]any{"credentialId": c.ID, "allowPlan": c.AllowPlan})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 201, map[string]any{"credential": c, "token": token})
}
func (s *Server) revokeMCPCredential(w http.ResponseWriter, r *http.Request) {
	ok, err := s.Store.RevokeMCPCredential(r.Context(), r.PathValue("credentialID"), currentUser(r).ID)
	if err != nil {
		writeStoreError(w, "could not revoke MCP credential")
		return
	}
	if !ok {
		writeError(w, 404, "credential not found")
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "mcp.credential.revoked", "mcp_credential", r.PathValue("credentialID"), nil)
	w.WriteHeader(204)
}
func (s *Server) serveMCP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// Reject browser cross-origin requests; authentication is exclusively bearer based.
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || (u.Scheme != "https" && u.Scheme != "http") {
			writeError(w, 403, "origin is not permitted")
			return
		}
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !strings.HasPrefix(token, "justcd_mcp_") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, 401, "MCP bearer credential required")
		return
	}
	c, err := s.Store.MCPCredentialByHash(r.Context(), security.HashToken(token))
	if err != nil {
		writeError(w, 401, "MCP credential expired or revoked")
		return
	}
	user, err := s.Store.UserByID(r.Context(), c.UserID)
	if err != nil {
		writeError(w, 401, "account is unavailable")
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), userContextKey, user))
	if !s.requireWorkspaceRole(w, r, c.WorkspaceID, "viewer") {
		return
	}
	settings, err := s.Store.InstanceSettings(r.Context())
	if err != nil {
		writeStoreError(w, "could not check service availability")
		return
	}
	if settings.MaintenanceEnabled && !user.IsAdmin {
		writeMaintenanceError(w)
		return
	}
	if !s.securityLimit(w, r, "mcp", c.ID, 120, time.Minute) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	server := s.newMCPServer(c, user)
	mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}).ServeHTTP(w, r)
}

type mcpInput struct {
	ApplicationID string `json:"applicationId,omitempty"`
	PlanID        string `json:"planId,omitempty"`
	TaskID        string `json:"taskId,omitempty"`
	RequestID     string `json:"requestId,omitempty"`
}

func (s *Server) newMCPServer(c store.MCPCredential, user store.User) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "justcd", Version: "1.0.0"}, nil)
	for _, tool := range []struct {
		name, description string
		write             bool
	}{
		{"list_applications", "List application summaries in the credential's workspace.", false},
		{"get_application", "Read application health and revision summary. Requires applicationId.", false},
		{"list_plans", "Read redacted drift plans. Requires applicationId.", false},
		{"get_plan", "Read a redacted deployment plan. Requires planId.", false},
		{"get_task", "Read your plan-generation task status. Requires taskId. Use list_plans after completion.", false},
		{"create_plan", "Queue a reviewable deployment plan; never approves or applies it. Requires applicationId and requestId (reuse for retries).", true},
	} {
		if tool.write && !c.AllowPlan {
			continue
		}
		mcp.AddTool(server, &mcp.Tool{Name: tool.name, Description: tool.description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: !tool.write, DestructiveHint: new(false), OpenWorldHint: new(tool.write)}}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpInput) (*mcp.CallToolResult, any, error) {
			ctx = context.WithValue(ctx, userContextKey, user)
			value, err := s.callMCPTool(ctx, c, tool.name, in)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return nil, nil, err
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil, nil
		})
	}
	return server
}
func mcpApplication(app store.Application) map[string]any {
	return map[string]any{"id": app.ID, "workspaceId": app.WorkspaceID, "name": app.Name, "health": app.Health, "revision": app.Revision, "lastSyncedRevision": app.LastSyncedRevision, "lastCheckedAt": app.LastCheckedAt, "syncPolicy": app.SyncPolicy, "autoSyncPaused": app.AutoSyncPaused, "decommissioning": app.Decommissioning, "configurationMissing": app.ConfigurationMissing}
}
func (s *Server) callMCPTool(ctx context.Context, c store.MCPCredential, name string, in mcpInput) (any, error) {
	denied := errors.New("resource unavailable or outside credential scope")
	r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	if !s.requireWorkspaceRole(w, r, c.WorkspaceID, "viewer") {
		return nil, denied
	}
	if name == "list_applications" {
		apps, err := s.Store.ListApplications(ctx, c.WorkspaceID)
		if err != nil {
			return nil, errors.New("could not load applications")
		}
		items := []any{}
		for _, app := range apps {
			items = append(items, mcpApplication(app))
		}
		return map[string]any{"items": items}, nil
	}
	if name == "get_plan" {
		p, err := s.Store.PlanByID(ctx, in.PlanID)
		if err != nil {
			return nil, denied
		}
		in.ApplicationID = p.Plan.ApplicationID
	}
	var task store.BackgroundTask
	if name == "get_task" {
		var err error
		task, err = s.Store.BackgroundTaskByID(ctx, in.TaskID)
		if err != nil || task.ActorID != c.UserID || task.Kind != "plan" {
			return nil, denied
		}
		in.ApplicationID = task.ApplicationID
	}
	app, err := s.Store.ApplicationByID(ctx, in.ApplicationID)
	if err != nil || app.WorkspaceID != c.WorkspaceID {
		return nil, denied
	}
	r.SetPathValue("applicationID", app.ID)
	switch name {
	case "get_application":
		return mcpApplication(app), nil
	case "get_task":
		return map[string]any{"id": task.ID, "applicationId": task.ApplicationID, "status": task.Status, "createdAt": task.CreatedAt, "finishedAt": task.FinishedAt}, nil
	case "list_plans":
		s.listPlans(w, r)
	case "get_plan":
		r.SetPathValue("planID", in.PlanID)
		s.getPlan(w, r)
	case "create_plan":
		if !c.AllowPlan {
			return nil, denied
		}
		if in.RequestID == "" || len(in.RequestID) > 80 {
			return nil, errors.New("requestId is required and must be at most 80 characters")
		}
		// Record the credential before queuing; fail closed if the audit cannot be saved.
		if err := s.Store.Audit(ctx, c.UserID, "mcp.plan.requested", "application", app.ID, map[string]any{"credentialId": c.ID, "requestId": in.RequestID}); err != nil {
			return nil, errors.New("could not record MCP request")
		}
		r.Method = "POST"
		r.Header.Set("Idempotency-Key", c.ID+":"+in.RequestID)
		s.createPlan(w, r)
	default:
		return nil, errors.New("unknown tool")
	}
	if w.Code >= 400 {
		return nil, errors.New("request failed; check workspace permissions and application state in justcd")
	}
	var value any
	if json.Unmarshal(w.Body.Bytes(), &value) != nil {
		return nil, errors.New("could not read result")
	}
	return mcpRedact(value), nil
}

// Preserve the existing manifest redaction and also hide credential references
// and values in Kubernetes name/value entries that identify sensitive variables.
func mcpRedact(value any) any {
	switch v := value.(type) {
	case map[string]any:
		sensitive := func(key string) bool {
			key = strings.ToLower(key)
			for _, part := range []string{"password", "token", "secret", "privatekey", "accesskey", "authorization", "credential"} {
				if strings.Contains(key, part) {
					return true
				}
			}
			return false
		}
		result := make(map[string]any, len(v))
		for key, item := range v {
			if sensitive(key) || (key == "value" && sensitive(stringValue(v["name"]))) {
				result[key] = "[redacted]"
			} else {
				result[key] = mcpRedact(item)
			}
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = mcpRedact(item)
		}
		return result
	default:
		return value
	}
}

func (s *Server) getMCPPolicy(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.MCPPolicy(r.Context())
	if err != nil {
		writeStoreError(w, "could not load MCP policy")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, p)
}
func (s *Server) updateMCPPolicy(w http.ResponseWriter, r *http.Request) {
	var p store.MCPPolicy
	if decodeJSON(w, r, &p) != nil {
		return
	}
	if err := s.Store.UpdateMCPPolicy(r.Context(), p, currentUser(r).ID); err != nil {
		var rejected *store.AccessError
		if errors.As(err, &rejected) {
			writeError(w, 400, rejected.Message)
			return
		}
		writeStoreError(w, "could not save MCP policy")
		return
	}
	writeJSON(w, 200, p)
}
