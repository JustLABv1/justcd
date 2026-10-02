//go:build integration

package api

import (
	"context"
	"database/sql"
	"errors"
	"github.com/justlab/justcd/services/backend/internal/config"
	"github.com/justlab/justcd/services/backend/internal/security"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegrationAgentRotationAndRevocationBoundaries(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, q := range []string{`INSERT INTO workspaces(id,name) VALUES('security-w','Security')`, `INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('security-c','security-w','Security','https://unused.invalid')`} {
		if _, e := db.DB.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	old := security.HashToken("old")
	current := security.HashToken("current")
	next := security.HashToken("next")
	if _, e := db.DB.ExecContext(ctx, `INSERT INTO cluster_agents(cluster_id,token_hash,token_expires_at,last_seen_at) VALUES('security-c',$1,NOW()+INTERVAL '1 day',NOW())`, old); e != nil {
		t.Fatal(e)
	}
	if e := db.RenewClusterAgent(ctx, "security-c", old, current); e != nil {
		t.Fatal(e)
	}
	var expiry time.Time
	if e := db.DB.QueryRowContext(ctx, `UPDATE cluster_agents SET previous_token_expires_at=NOW()+INTERVAL '30 seconds' WHERE cluster_id='security-c' RETURNING previous_token_expires_at`).Scan(&expiry); e != nil {
		t.Fatal(e)
	}
	if e := db.RenewClusterAgent(ctx, "security-c", old, next); e != nil {
		t.Fatal(e)
	}
	var after time.Time
	if e := db.DB.QueryRowContext(ctx, `SELECT previous_token_expires_at FROM cluster_agents WHERE cluster_id='security-c'`).Scan(&after); e != nil {
		t.Fatal(e)
	}
	if !after.Equal(expiry) {
		t.Fatal("previous token extended its grace")
	}
	if _, e := db.DB.ExecContext(ctx, `UPDATE cluster_agents SET previous_token_expires_at=NOW()-INTERVAL '1 second' WHERE cluster_id='security-c'`); e != nil {
		t.Fatal(e)
	}
	if e := db.RenewClusterAgent(ctx, "security-c", old, current); !errors.Is(e, sql.ErrNoRows) {
		t.Fatalf("expired token accepted: %v", e)
	}
	if e := db.QueueAgentTask(ctx, "security-task", "security-c", []byte("synthetic-cipher"), time.Now().Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	if _, _, e := db.ClaimAgentTask(ctx, "security-c", "wrong", current); !errors.Is(e, sql.ErrNoRows) {
		t.Fatalf("replaced token accepted: %v", e)
	}
	// Hold the same row lock as revocation while a claim starts. Commit revocation
	// before releasing it: the waiting claim must recheck the revoked state.
	tx, e := db.DB.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE cluster_agents SET revoked=TRUE WHERE cluster_id='security-c'`); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() { close(started); _, _, err := db.ClaimAgentTask(ctx, "security-c", "lease", next); done <- err }()
	<-started
	select {
	case err := <-done:
		t.Fatalf("claim completed before revocation released its lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = <-done; !errors.Is(e, sql.ErrNoRows) {
		t.Fatalf("claim crossed revocation: %v", e)
	}
	var state string
	if e = db.DB.QueryRowContext(ctx, `SELECT state FROM cluster_agent_tasks WHERE id='security-task'`).Scan(&state); e != nil || state != "queued" {
		t.Fatalf("revoked task changed: %s %v", state, e)
	}
	if e = db.RenewClusterAgent(ctx, "security-c", next, current); !errors.Is(e, sql.ErrNoRows) {
		t.Fatalf("revoked token renewed: %v", e)
	}
}

func TestIntegrationLoginProxyDoesNotShareAccountLimits(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	hash, e := security.HashPassword("Safe-test-password-2026!")
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.DB.ExecContext(context.Background(), `INSERT INTO users(id,email,password_hash) VALUES('login-attacker','attacker@example.invalid',$1),('login-other','other@example.invalid',$1)`, hash)
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{Store: db, dummyHash: hash, Config: config.Config{SessionLifetime: time.Hour}}
	request := func(email, password string) int {
		r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"`+email+`","password":"`+password+`"}`))
		r.RemoteAddr = "127.0.0.1:8080"
		w := httptest.NewRecorder()
		s.login(w, r)
		return w.Code
	}
	for i := 0; i < 8; i++ {
		if got := request("ATTACKER@example.invalid", "wrong"); got != 401 {
			t.Fatalf("attempt %d: %d", i, got)
		}
	}
	if got := request("attacker@example.invalid", "wrong"); got != 429 {
		t.Fatalf("same account not limited: %d", got)
	}
	if got := request("other@example.invalid", "Safe-test-password-2026!"); got != 200 {
		t.Fatalf("other account blocked behind proxy: %d", got)
	}
	for i := 0; i < 9; i++ {
		request("unknown@example.invalid", "wrong")
	}
	if got := request("other@example.invalid", "Safe-test-password-2026!"); got != 200 {
		t.Fatalf("unknown accounts blocked known user: %d", got)
	}
}
