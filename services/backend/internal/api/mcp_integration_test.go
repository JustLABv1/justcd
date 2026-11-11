//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationMCPCredentialBoundaries(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email,display_name) VALUES('mcp-user','mcp@example.invalid','MCP');
 INSERT INTO workspaces(id,name) VALUES('mcp-a','A'),('mcp-b','B');
 INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('mcp-a','mcp-user','deployer'),('mcp-b','mcp-user','owner');
 INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('mcp-source','mcp-b','Source','https://example.invalid/repo.git');
 INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('mcp-cluster','mcp-b','Cluster','https://example.invalid');
 INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id) VALUES('mcp-other-app','mcp-b','Other','mcp-source','main','deploy','yaml','mcp-cluster')`)
	if err != nil {
		t.Fatal(err)
	}
	c := store.MCPCredential{ID: "mcp-credential", UserID: "mcp-user", WorkspaceID: "mcp-a", Name: "Test", ExpiresAt: new(time.Now().Add(30 * 24 * time.Hour))}
	hash := security.HashToken("test-mcp-token")
	if err := db.CreateMCPCredential(ctx, c, hash, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MCPCredentialByHash(ctx, hash); err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.ExecContext(ctx, `INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id) VALUES('mcp-own-app','mcp-a','Own','mcp-source','main','deploy','yaml','mcp-cluster')`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db}
	ctx = context.WithValue(ctx, userContextKey, store.User{ID: c.UserID})
	if _, err := s.callMCPTool(ctx, c, "get_application", mcpInput{ApplicationID: "mcp-own-app"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.callMCPTool(ctx, c, "create_plan", mcpInput{ApplicationID: "mcp-own-app", RequestID: "read-only"}); err == nil {
		t.Fatal("read-only credential generated a plan")
	}
	if _, err := s.callMCPTool(ctx, c, "get_application", mcpInput{ApplicationID: "mcp-other-app"}); err == nil {
		t.Fatal("cross-workspace application exposed")
	}
	if _, err := s.callMCPTool(ctx, c, "create_plan", mcpInput{ApplicationID: "mcp-other-app", RequestID: "retry"}); err == nil {
		t.Fatal("cross-workspace plan allowed")
	}
	if ok, err := db.RevokeMCPCredential(ctx, c.ID, "other-user"); err != nil || ok {
		t.Fatalf("other user revoked credential: %v %v", ok, err)
	}
	if ok, err := db.RevokeMCPCredential(ctx, c.ID, c.UserID); err != nil || !ok {
		t.Fatalf("revoke failed: %v %v", ok, err)
	}
	if _, err := db.MCPCredentialByHash(ctx, hash); err == nil {
		t.Fatal("revoked credential accepted")
	}
	c.ID = "expired"
	c.ExpiresAt = new(time.Now().Add(-time.Hour))
	if err := db.CreateMCPCredential(ctx, c, security.HashToken("expired"), 30); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MCPCredentialByHash(ctx, security.HashToken("expired")); err == nil {
		t.Fatal("expired credential accepted")
	}
}

func TestIntegrationMCPLifetimes(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email,display_name,is_admin) VALUES('lifetime-user','lifetime@example.invalid','Owner',TRUE); INSERT INTO workspaces(id,name) VALUES('lifetime-ws','Lifetimes'); INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('lifetime-ws','lifetime-user','owner')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db}
	create := func(days string) *httptest.ResponseRecorder {
		body := `{"name":"Test","workspaceId":"lifetime-ws"` + days + `}`
		r := httptest.NewRequest("POST", "/api/v1/mcp-credentials", strings.NewReader(body))
		r = r.WithContext(context.WithValue(ctx, userContextKey, store.User{ID: "lifetime-user", IsAdmin: true}))
		w := httptest.NewRecorder()
		s.createMCPCredential(w, r)
		return w
	}
	for _, days := range []string{"", `,"expiresInDays":7`, `,"expiresInDays":30`, `,"expiresInDays":90`, `,"expiresInDays":365`, `,"expiresInDays":0`} {
		w := create(days)
		if w.Code != 201 {
			t.Fatalf("%s: %d %s", days, w.Code, w.Body.String())
		}
		var result struct {
			Credential store.MCPCredential `json:"credential"`
			Token      string              `json:"token"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		stored, err := db.MCPCredentialByHash(ctx, security.HashToken(result.Token))
		if err != nil {
			t.Fatal(err)
		}
		if days == `,"expiresInDays":0` {
			if stored.ExpiresAt != nil {
				t.Fatal("non-expiring key has expiry")
			}
			if ok, err := db.RevokeMCPCredential(ctx, stored.ID, "lifetime-user"); err != nil || !ok {
				t.Fatalf("revoke: %v %v", ok, err)
			}
			if _, err := db.MCPCredentialByHash(ctx, security.HashToken(result.Token)); err == nil {
				t.Fatal("revoked non-expiring key accepted")
			}
		} else {
			want := 30
			if days != "" {
				fmt.Sscanf(days, `,"expiresInDays":%d`, &want)
			}
			if stored.ExpiresAt == nil || stored.ExpiresAt.Sub(result.Credential.CreatedAt).Round(time.Millisecond) != time.Duration(want)*24*time.Hour {
				t.Fatalf("incorrect expiry for %s", days)
			}
		}
	}
	if err := db.UpdateMCPPolicy(ctx, store.MCPPolicy{AllowedLifetimes: []int{7, 30}}, "lifetime-user"); err != nil {
		t.Fatal(err)
	}
	for _, days := range []string{`,"expiresInDays":0`, `,"expiresInDays":90`, `,"expiresInDays":365`, `,"expiresInDays":-1`, `,"expiresInDays":999999999`} {
		if w := create(days); w.Code != 400 {
			t.Fatalf("disallowed lifetime %s accepted: %d", days, w.Code)
		}
	}
	if w := create(""); w.Code != 201 {
		t.Fatalf("default no longer works: %s", w.Body.String())
	}
	items, err := db.ListMCPCredentials(ctx, "lifetime-user")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range items {
		if c.ExpiresAt != nil && c.ExpiresAt.After(time.Now().Add(364*24*time.Hour)) { // Restriction leaves existing one-year keys valid.
			var count int
			if err := db.DB.QueryRowContext(ctx, `SELECT count(*) FROM mcp_credentials WHERE id=$1 AND revoked_at IS NULL`, c.ID).Scan(&count); err != nil || count != 1 {
				t.Fatal("existing key changed")
			}
		}
	}
	if err := db.UpdateMCPPolicy(ctx, store.MCPPolicy{AllowedLifetimes: []int{}}, "lifetime-user"); err == nil {
		t.Fatal("empty policy accepted")
	}
	policy, err := db.MCPPolicy(ctx)
	if err != nil || !slices.Equal(policy.AllowedLifetimes, []int{7, 30}) {
		t.Fatalf("policy mismatch: %v %v", policy, err)
	}
}
