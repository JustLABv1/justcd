//go:build integration

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationWorkspaceOwnersCanCreatePrivateConnections(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}

	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES
		('workspace-auth-owner','owner@example.invalid'),
		('workspace-auth-deployer','deployer@example.invalid'),
		('workspace-auth-viewer','viewer@example.invalid')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO workspaces(id,name) VALUES('workspace-auth-test','Authorization test')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES
		('workspace-auth-test','workspace-auth-owner','owner'),
		('workspace-auth-test','workspace-auth-deployer','deployer'),
		('workspace-auth-test','workspace-auth-viewer','viewer')`); err != nil {
		t.Fatal(err)
	}
	var ownerIsAdmin bool
	if err := db.DB.QueryRowContext(ctx, `SELECT is_admin FROM users WHERE id='workspace-auth-owner'`).Scan(&ownerIsAdmin); err != nil || ownerIsAdmin {
		t.Fatalf("fixture owner must be a non-admin: admin=%v err=%v", ownerIsAdmin, err)
	}

	server := &Server{Store: db, EncryptionKey: []byte("0123456789abcdef0123456789abcdef")}
	credentialBody := `{"workspaceId":"workspace-auth-test","name":"Workspace token","kind":"kubernetes-token","secret":{"token":"private-test-secret"}}`
	for _, userID := range []string{"workspace-auth-deployer", "workspace-auth-viewer"} {
		if got := workspaceAuthorizationRequest(server.createCredential, userID, credentialBody).Code; got != http.StatusForbidden {
			t.Errorf("%s credential creation: got HTTP %d, want %d", userID, got, http.StatusForbidden)
		}
	}
	credentialResponse := workspaceAuthorizationRequest(server.createCredential, "workspace-auth-owner", credentialBody)
	if credentialResponse.Code != http.StatusCreated {
		t.Fatalf("non-admin workspace owner credential creation: got HTTP %d, body=%s", credentialResponse.Code, credentialResponse.Body.String())
	}
	var credential store.Credential
	if err := json.Unmarshal(credentialResponse.Body.Bytes(), &credential); err != nil {
		t.Fatal(err)
	}
	if credential.WorkspaceID == nil || *credential.WorkspaceID != "workspace-auth-test" || credential.Cipher != nil {
		t.Fatalf("created credential must be workspace-owned and never return ciphertext: %+v", credential)
	}

	clusterBody := `{"workspaceId":"workspace-auth-test","name":"Workspace cluster","apiServer":"https://api.example.invalid:6443"}`
	for _, userID := range []string{"workspace-auth-deployer", "workspace-auth-viewer"} {
		if got := workspaceAuthorizationRequest(server.createCluster, userID, clusterBody).Code; got != http.StatusForbidden {
			t.Errorf("%s cluster creation: got HTTP %d, want %d", userID, got, http.StatusForbidden)
		}
	}
	if got := workspaceAuthorizationRequest(server.createCluster, "workspace-auth-owner", clusterBody).Code; got != http.StatusCreated {
		t.Errorf("workspace owner cluster creation: got HTTP %d, want %d", got, http.StatusCreated)
	}

	gitSourceBody := `{"workspaceId":"workspace-auth-test","name":"Workspace source","repositoryUrl":"https://example.invalid/repository.git"}`
	for _, userID := range []string{"workspace-auth-deployer", "workspace-auth-viewer"} {
		if got := workspaceAuthorizationRequest(server.createGitSource, userID, gitSourceBody).Code; got != http.StatusForbidden {
			t.Errorf("%s Git source creation: got HTTP %d, want %d", userID, got, http.StatusForbidden)
		}
	}
	if got := workspaceAuthorizationRequest(server.createGitSource, "workspace-auth-owner", gitSourceBody).Code; got != http.StatusCreated {
		t.Errorf("workspace owner Git source creation: got HTTP %d, want %d", got, http.StatusCreated)
	}
}

func workspaceAuthorizationRequest(handler http.HandlerFunc, userID, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, store.User{ID: userID}))
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func openWorkspaceAuthorizationDB(t *testing.T, dsn string) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "justcd_e2e_workspace_auth_" + store.NewID()[:8]
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		_ = admin.Close()
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		_ = admin.Close()
		t.Fatal(err)
	}
	storeDB := &store.Store{DB: db}
	if err := storeDB.Migrate(ctx); err != nil {
		_ = db.Close()
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		_ = admin.Close()
		t.Fatalf("migrate isolated workspace authorization schema: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		_ = admin.Close()
	})
	return storeDB
}
