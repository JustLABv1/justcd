//go:build integration

package store

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
)

func TestIntegrationOIDCUserMapping(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "justcd_oidc_" + NewID()[:8]
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	s, err := Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	if err := s.CreateOIDCProvider(ctx, OIDCProvider{ID: "oidc-test", Name: "Test", Issuer: "https://example.invalid", ClientID: "client", ClientSecret: []byte("cipher"), RedirectURL: "https://example.invalid/callback", GroupsClaim: "groups", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO workspaces(id,name) VALUES('oidc-workspace','Test')`); err != nil {
		t.Fatal(err)
	}
	if err := s.AddOIDCGroupRole(ctx, "oidc-test", "developers", "oidc-workspace", "deployer"); err != nil {
		t.Fatal(err)
	}
	user, err := s.ResolveOIDCUser(ctx, "oidc-test", "new-subject", "new@example.invalid", "New User", []string{"developers"})
	if err != nil {
		t.Fatalf("provision user: %v", err)
	}
	var role string
	if err := s.DB.QueryRowContext(ctx, `SELECT role FROM oidc_membership_grants WHERE provider_id=$1 AND user_id=$2 AND workspace_id=$3`, "oidc-test", user.ID, "oidc-workspace").Scan(&role); err != nil || role != "deployer" {
		t.Fatalf("group grant: %s %v", role, err)
	}
	again, err := s.ResolveOIDCUser(ctx, "oidc-test", "new-subject", "new@example.invalid", "New User", nil)
	if err != nil || again.ID != user.ID {
		t.Fatalf("repeat login: %+v %v", again, err)
	}
	var grants int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM oidc_membership_grants WHERE user_id=$1`, user.ID).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("revoked grants: %d %v", grants, err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('oidc-existing','existing@example.invalid')`); err != nil {
		t.Fatal(err)
	}
	linked, err := s.ResolveOIDCUser(ctx, "oidc-test", "linked-subject", "existing@example.invalid", "Existing User", []string{"developers"})
	if err != nil || linked.ID != "oidc-existing" {
		t.Fatalf("link and update profile: %+v %v", linked, err)
	}
	provider, err := s.OIDCProviderByID(ctx, "oidc-test")
	if err != nil {
		t.Fatal(err)
	}
	provider.Name = "Renamed"
	provider.ClientID = "updated-client"
	provider.Enabled = false
	if err := s.UpdateOIDCProvider(ctx, provider); err != nil {
		t.Fatal(err)
	}
	updated, err := s.OIDCProviderByID(ctx, provider.ID)
	if err != nil || updated.Name != "Renamed" || updated.Enabled || string(updated.ClientSecret) != "cipher" {
		t.Fatalf("update: %+v %v", updated, err)
	}
	provider.Issuer = "https://other.invalid"
	if err := s.UpdateOIDCProvider(ctx, provider); err != ErrOIDCIssuerInUse {
		t.Fatalf("linked issuer change should fail: %v", err)
	}

	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE action LIKE 'user.oidc_%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Fatalf("expected 6 audit events, got %d", count)
	}
	if err := s.DeleteOIDCProvider(ctx, "oidc-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOIDCProvider(ctx, "oidc-test"); err != sql.ErrNoRows {
		t.Fatalf("missing provider: %v", err)
	}
	for _, table := range []string{"oidc_identities", "oidc_group_roles", "oidc_membership_grants", "oidc_login_states"} {
		var rows int
		if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE provider_id=$1", "oidc-test").Scan(&rows); err != nil || rows != 0 {
			t.Fatalf("cascade %s: rows=%d err=%v", table, rows, err)
		}
	}
	var users int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id IN ($1,$2)`, user.ID, linked.ID).Scan(&users); err != nil || users != 2 {
		t.Fatalf("accounts must remain: %d %v", users, err)
	}

}
