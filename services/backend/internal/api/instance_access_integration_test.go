//go:build integration

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/config"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationInstanceAccessControls(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email,is_admin) VALUES ('access-admin','admin@corp.example.com',TRUE),('access-member','member@corp.example.com',FALSE)`); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"admin", "member"} {
		if err := db.CreateSession(ctx, security.HashToken(role), security.HashToken("csrf-"+role), "access-"+role, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{Store: db, Mux: http.NewServeMux()}
	server.routes()
	call := func(method, path, role, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if role != "" {
			r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: role})
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-" + role})
			r.Header.Set("X-CSRF-Token", "csrf-"+role)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	expect := func(step string, w *httptest.ResponseRecorder, code int) {
		t.Helper()
		if w.Code != code {
			t.Fatalf("%s: status = %d (%s), want %d", step, w.Code, w.Body.String(), code)
		}
	}

	// Only administrators reach the controls.
	expect("member cannot read", call("GET", "/api/v1/admin/access-settings", "member", ""), 403)
	expect("member cannot write", call("PUT", "/api/v1/admin/access-settings", "member", `{"localLoginEnabled":false}`), 403)

	// Defaults keep earlier behaviour.
	var current accessSettingsResponse
	response := call("GET", "/api/v1/admin/access-settings", "admin", "")
	expect("read defaults", response, 200)
	_ = json.Unmarshal(response.Body.Bytes(), &current)
	if !current.LocalLoginEnabled || !current.OIDCAutoProvision || current.WorkspaceCreation != "anyone" || len(current.OIDCAllowedDomains) != 0 {
		t.Fatalf("defaults = %#v", current)
	}

	// Lockout protection: no provider, then a provider nobody has used, then a working SSO administrator.
	expect("no provider", call("PUT", "/api/v1/admin/access-settings", "admin", `{"localLoginEnabled":false}`), 409)
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO oidc_providers(id,name,issuer,client_id,client_secret_cipher,redirect_url,enabled) VALUES ('idp','IdP','https://idp.example.invalid','c','\x00','https://x.invalid/cb',TRUE)`); err != nil {
		t.Fatal(err)
	}
	expect("provider without an SSO administrator", call("PUT", "/api/v1/admin/access-settings", "admin", `{"localLoginEnabled":false}`), 409)
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO oidc_identities(provider_id,subject,user_id) VALUES ('idp','sub-admin','access-admin')`); err != nil {
		t.Fatal(err)
	}
	expect("disable password sign-in", call("PUT", "/api/v1/admin/access-settings", "admin", `{"localLoginEnabled":false}`), 200)

	// The login endpoint and the public provider list follow the setting.
	expect("password login refused", call("POST", "/api/v1/auth/login", "", `{"email":"admin@corp.example.com","password":"whatever-it-is"}`), 403)
	var providers struct {
		LocalLoginEnabled bool `json:"localLoginEnabled"`
	}
	response = call("GET", "/api/v1/auth/providers", "", "")
	expect("providers", response, 200)
	_ = json.Unmarshal(response.Body.Bytes(), &providers)
	if providers.LocalLoginEnabled {
		t.Fatal("the login page would still offer a password form")
	}
	// The recovery override keeps the door open.
	forced := &Server{Store: db, Mux: http.NewServeMux(), Config: config.Config{ForceLocalLogin: true}}
	settings, err := db.InstanceSettings(ctx)
	if err != nil || !forced.localLoginAllowed(settings) || server.localLoginAllowed(settings) {
		t.Fatalf("override: forced=%v plain=%v err=%v", forced.localLoginAllowed(settings), server.localLoginAllowed(settings), err)
	}

	// Registration: nobody new may be created while auto-provisioning is off; domains narrow it when on.
	expect("turn auto-provisioning off", call("PUT", "/api/v1/admin/access-settings", "admin", `{"oidcAutoProvision":false}`), 200)
	if _, err := db.ResolveOIDCUser(ctx, "idp", "sub-new", "new@corp.example.com", "New", nil); !errors.Is(err, store.ErrOIDCSignupDisabled) {
		t.Fatalf("a new identity was provisioned while registration is off: %v", err)
	}
	if user, err := db.ResolveOIDCUser(ctx, "idp", "sub-member", "member@corp.example.com", "Member", nil); err != nil || user.ID != "access-member" {
		t.Fatalf("an account an administrator already created must still link: %v %v", user, err)
	}
	expect("on, limited to a domain", call("PUT", "/api/v1/admin/access-settings", "admin", `{"oidcAutoProvision":true,"oidcAllowedDomains":["Corp.Example.com","@corp.example.com"]}`), 200)
	if _, err := db.ResolveOIDCUser(ctx, "idp", "sub-out", "x@elsewhere.test", "X", nil); !errors.Is(err, store.ErrOIDCDomainNotAllowed) {
		t.Fatalf("a foreign domain was provisioned: %v", err)
	}
	if user, err := db.ResolveOIDCUser(ctx, "idp", "sub-in", "new@corp.example.com", "New", nil); err != nil || user.Email != "new@corp.example.com" {
		t.Fatalf("an allowed domain must be provisioned: %v %v", user, err)
	}
	expect("bad domain", call("PUT", "/api/v1/admin/access-settings", "admin", `{"oidcAllowedDomains":["not a domain"]}`), 409)

	// Workspace creation.
	expect("anyone creates", call("POST", "/api/v1/workspaces", "member", `{"name":"Team A"}`), 201)
	expect("restrict", call("PUT", "/api/v1/admin/access-settings", "admin", `{"workspaceCreation":"admins"}`), 200)
	expect("member refused", call("POST", "/api/v1/workspaces", "member", `{"name":"Team B"}`), 403)
	expect("admin still creates", call("POST", "/api/v1/workspaces", "admin", `{"name":"Team C"}`), 201)
	var session struct {
		CanCreateWorkspace bool `json:"canCreateWorkspace"`
	}
	response = call("GET", "/api/v1/auth/session", "member", "")
	_ = json.Unmarshal(response.Body.Bytes(), &session)
	if session.CanCreateWorkspace {
		t.Fatal("the page would still show a create button to a member")
	}

	// Every change is audited with what changed.
	var changes int
	if err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE action='instance.access_updated' AND actor_id='access-admin'`).Scan(&changes); err != nil || changes < 4 {
		t.Fatalf("audited changes = %d, err = %v", changes, err)
	}
	// An unchanged save writes nothing.
	before := changes
	expect("no-op", call("PUT", "/api/v1/admin/access-settings", "admin", `{"workspaceCreation":"admins"}`), 200)
	_ = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE action='instance.access_updated'`).Scan(&changes)
	if changes != before {
		t.Fatalf("a no-op save was audited: %d -> %d", before, changes)
	}
}
