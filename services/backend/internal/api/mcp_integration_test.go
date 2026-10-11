//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/config"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpAuthTransport struct{ token, csrf string }

func (a mcpAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header = r.Header.Clone()
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: a.token})
	r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: a.csrf})
	r.Header.Set("X-CSRF-Token", a.csrf)
	return http.DefaultTransport.RoundTrip(r)
}

func TestIntegrationMCPReadOnly(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('mcp-user','mcp@example.invalid');
 INSERT INTO workspaces(id,name) VALUES('mcp-w','Visible'),('mcp-other','Hidden');
 INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('mcp-w','mcp-user','viewer');
 INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('mcp-source','mcp-w','Git','https://user:credential-secret@example.invalid/repo');
 INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('mcp-cluster','mcp-w','Cluster','https://unused.invalid');
 INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id,helm_values_yaml) VALUES('mcp-app','mcp-w','Visible app','mcp-source','main','deploy','yaml','mcp-cluster','password: helm-secret'),('mcp-app2','mcp-w','Second','mcp-source','main','deploy','yaml','mcp-cluster',''),('mcp-hidden','mcp-other','Hidden app','mcp-source','main','deploy','yaml','mcp-cluster','');
 INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at) VALUES('mcp-plan','mcp-app','main','digest','[{"diff":"diff-secret"}]','[{"kind":"Secret","data":{"password":"secret-data"},"metadata":{"annotations":{"sensitive":"annotation-secret"}}}]','mcp-user',NOW()+INTERVAL '1 hour');
 INSERT INTO operations(id,application_id,plan_id,status,message) VALUES('mcp-op','mcp-app','mcp-plan','failed','operation-secret');
 INSERT INTO audit_events(action,resource_type,resource_id,details) VALUES('plan.created','application','mcp-app','{"token":"audit-secret"}'),('hidden','application','mcp-hidden','{}');`)
	if err != nil {
		t.Fatal(err)
	}
	token, csrf := "test-session", "test-csrf"
	if err := db.CreateSession(ctx, security.HashToken(token), security.HashToken(csrf), "mcp-user", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	server, err := New(db, config.Config{PublicURL: "http://localhost:8080"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "integration", Version: "1"}, nil)
	transport := &http.Client{Transport: mcpAuthTransport{token, csrf}}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL + "/api/v1/mcp", HTTPClient: transport}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.InitializeResult().ProtocolVersion != "2025-11-25" {
		t.Fatalf("unexpected protocol: %s", session.InitializeResult().ProtocolVersion)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 5 {
		t.Fatalf("unexpected tools: %+v", tools.Tools)
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool not read only: %s", tool.Name)
		}
	}
	call := func(name string, args map[string]any, wantError bool) string {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			if wantError {
				return err.Error()
			}
			t.Fatal(err)
		}
		if result.IsError != wantError {
			t.Fatalf("%s error=%v want=%v: %+v", name, result.IsError, wantError, result)
		}
		raw, _ := json.Marshal(result)
		return string(raw)
	}
	visible := call("list_workspaces", map[string]any{}, false)
	if strings.Contains(visible, "Hidden") || !strings.Contains(visible, "Visible") {
		t.Fatalf("workspace leakage: %s", visible)
	}
	for _, name := range []string{"list_applications", "list_plans", "list_operations", "list_application_audit"} {
		result := call(name, map[string]any{"workspaceId": "mcp-w", "applicationId": "mcp-app"}, false)
		for _, secret := range []string{"helm-secret", "diff-secret", "secret-data", "annotation-secret", "operation-secret", "audit-secret", "credential-secret", "Hidden"} {
			if strings.Contains(result, secret) {
				t.Fatalf("%s leaked %s", name, secret)
			}
		}
		call(name, map[string]any{"workspaceId": "mcp-other", "applicationId": "mcp-hidden"}, true)
		call(name, map[string]any{"workspaceId": "mcp-w", "applicationId": "mcp-hidden", "offset": 1}, true)
	}
	page := call("list_applications", map[string]any{"workspaceId": "mcp-w", "limit": 1}, false)
	if !strings.Contains(page, "nextOffset") {
		t.Fatal("pagination continuation missing")
	}
	page = call("list_applications", map[string]any{"workspaceId": "mcp-w", "limit": 1, "offset": 1}, false)
	if strings.Contains(page, "nextOffset") {
		t.Fatal("unexpected continuation")
	}
	call("list_applications", map[string]any{"workspaceId": "mcp-w", "limit": 101}, true)
	call("list_plans", map[string]any{"workspaceId": "mcp-w"}, true)
	call("apply_plan", map[string]any{"planId": "mcp-plan"}, true)
	call("list_applications", map[string]any{"workspaceId": "mcp-w", "limit": "wrong"}, true)
	if os.Getenv("JUSTCD_E2E_MCP_PROXY") == "1" {
		verifyMCPProxy(t, ctx, httpServer.URL, token, csrf, server)
	}
	// Response size is enforced even for malformed historical metadata.
	oversizedID := strings.Repeat("z", 128<<10)
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO workspaces(id,name) VALUES($1,'Oversized')`, oversizedID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES($1,'mcp-user','viewer')`, oversizedID); err != nil {
		t.Fatal(err)
	}
	call("list_workspaces", map[string]any{}, true)
	if _, err := db.DB.ExecContext(ctx, `DELETE FROM workspaces WHERE id=$1`, oversizedID); err != nil {
		t.Fatal(err)
	}
	// Revocation takes effect on the same MCP client; no cached authorization.
	if _, err := db.DB.ExecContext(ctx, `DELETE FROM workspace_memberships WHERE user_id='mcp-user'`); err != nil {
		t.Fatal(err)
	}
	call("list_operations", map[string]any{"workspaceId": "mcp-w", "applicationId": "mcp-app"}, true)
	// Raw protocol requests exercise HTTP protections, parser and size limits.
	for _, tc := range []struct {
		name, body, csrf, token, version string
		want                             int
	}{
		{"missing csrf", `{}`, "", token, "", 403},
		{"malformed", `{`, csrf, token, "", 400},
		{"oversize", strings.Repeat(" ", 65<<10), csrf, token, "", 413},
		{"anonymous tools", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, csrf, "", "", 401},
		{"unsupported protocol", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, csrf, token, "1900-01-01", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := http.NewRequestWithContext(ctx, "POST", httpServer.URL+"/api/v1/mcp", strings.NewReader(tc.body))
			r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tc.token})
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf})
			r.Header.Set("X-CSRF-Token", tc.csrf)
			r.Header.Set("MCP-Protocol-Version", tc.version)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			resp, err := http.DefaultClient.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("HTTP %d want %d: %s", resp.StatusCode, tc.want, raw)
			}
		})
	}
	var status string
	if err := db.DB.QueryRowContext(ctx, `SELECT status FROM plans WHERE id='mcp-plan'`).Scan(&status); err != nil || status != "current" {
		t.Fatalf("MCP mutated plan: %s %v", status, err)
	}
	var count int
	if err := db.DB.QueryRowContext(ctx, `SELECT count(*) FROM operations`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("MCP mutated operations: %d %v", count, err)
	}
	if err := db.DeleteSession(ctx, security.HashToken(token)); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ListTools(ctx, nil); err == nil {
		t.Fatal("revoked session still accepted")
	}
}

// Opt-in in the integration runner when frontend dependencies are installed.
// Uses the real Next server and Undici fetch, not a mock of the proxy handler.
func verifyMCPProxy(t *testing.T, ctx context.Context, backend, token, csrf string, server *Server) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	frontend, err := filepath.Abs("../../../frontend")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	server.Config.FrontendURL = endpoint
	logpath := filepath.Join(t.TempDir(), "next.log")
	logfile, err := os.Create(logpath)
	if err != nil {
		t.Fatal(err)
	}
	defer logfile.Close()
	cmd := exec.CommandContext(ctx, "node", "node_modules/next/dist/bin/next", "dev", "--webpack", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port))
	cmd.Dir = frontend
	cmd.Env = append(os.Environ(), "JUSTCD_API_URL="+backend, "NEXT_TELEMETRY_DISABLED=1")
	cmd.Stdout = logfile
	cmd.Stderr = logfile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			raw, _ := os.ReadFile(logpath)
			t.Log(string(raw))
		}
	}()
	ready := false
	httpClient := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		res, err := httpClient.Get(endpoint + "/api/v1/health")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		t.Fatal("Next proxy did not become ready")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "proxy-integration", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint + "/api/v1/mcp", HTTPClient: &http.Client{Transport: mcpAuthTransport{token, csrf}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 5 {
		t.Fatalf("proxy listtools: %+v %v", tools, err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_applications", Arguments: map[string]any{"workspaceId": "mcp-w"}})
	if err != nil || result.IsError {
		t.Fatalf("proxy call: %+v %v", result, err)
	}
	// A malicious Origin must reach the backend unchanged and fail.
	request, _ := http.NewRequestWithContext(ctx, "POST", endpoint+"/api/v1/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	request.Header.Set("Origin", "https://evil.test")
	response, err := (&http.Client{Transport: mcpAuthTransport{token, csrf}}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatalf("proxy dropped Origin: HTTP %d", response.StatusCode)
	}
}
