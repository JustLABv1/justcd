//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationMaintenanceAdministrationAndBypass(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email,is_admin) VALUES ('maintenance-admin','admin@example.invalid',TRUE),('maintenance-member','member@example.invalid',FALSE)`); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"admin", "member"} {
		if err := db.CreateSession(ctx, security.HashToken(role), security.HashToken("csrf-"+role), "maintenance-"+role, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{Store: db, Mux: http.NewServeMux()}
	server.routes()
	request := func(method, path, role, body string, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if role != "" {
			r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: role})
		}
		if csrf {
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-" + role})
			r.Header.Set("X-CSRF-Token", "csrf-"+role)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, code int) {
		t.Helper()
		if w.Code != code {
			t.Fatalf("got HTTP %d, want %d: %s", w.Code, code, w.Body.String())
		}
	}
	status := func(role, wanted string, bypass bool) {
		t.Helper()
		w := request("GET", "/api/v1/service-status", role, "", false)
		expect(w, 200)
		var result struct {
			Status      string `json:"status"`
			AdminBypass bool   `json:"adminBypass"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != wanted || result.AdminBypass != bypass {
			t.Fatalf("unexpected status: %s", w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Vary") != "Cookie" {
			t.Fatal("session-dependent status must not be cached")
		}
	}
	status("", "online", false)
	expect(request("PUT", "/api/v1/admin/instance-settings", "", `{"maintenanceEnabled":true}`, false), 401)
	expect(request("PUT", "/api/v1/admin/instance-settings", "member", `{"maintenanceEnabled":true}`, true), 403)
	expect(request("PUT", "/api/v1/admin/instance-settings", "admin", `{"maintenanceEnabled":true}`, false), 403)
	expect(request("PUT", "/api/v1/admin/instance-settings", "admin", `{}`, true), 400)
	expect(request("PUT", "/api/v1/admin/instance-settings", "admin", `{"maintenanceEnabled":true}`, true), 200)
	// A new Store instance reads the persisted setting; it is not process-local.
	settings, err := (&store.Store{DB: db.DB}).InstanceSettings(ctx)
	if err != nil || !settings.MaintenanceEnabled {
		t.Fatalf("maintenance was not persisted: %+v %v", settings, err)
	}
	status("", "maintenance", false)
	status("member", "maintenance", false)
	status("forged-admin", "maintenance", false)
	status("admin", "online", true)
	expect(request("GET", "/api/v1/admin/instance-settings", "admin", "", false), 200)
	// Use a protected real route to prove this is API enforcement, not just UI.
	blocked := request("GET", "/api/v1/workspaces", "member", "", false)
	expect(blocked, 503)
	if !strings.Contains(blocked.Body.String(), `"code":"instance.maintenance"`) || blocked.Header().Get("Retry-After") != "15" {
		t.Fatalf("missing maintenance metadata: %s", blocked.Body.String())
	}
	expect(request("GET", "/api/v1/auth/session", "member", "", false), 200)
	expect(request("GET", "/api/v1/workspaces", "admin", "", false), 200)
	// Demotion and session expiration revoke the bypass immediately.
	if _, err := db.DB.ExecContext(ctx, `UPDATE users SET is_admin=FALSE WHERE id='maintenance-admin'`); err != nil {
		t.Fatal(err)
	}
	status("admin", "maintenance", false)
	if _, err := db.DB.ExecContext(ctx, `UPDATE users SET is_admin=TRUE WHERE id='maintenance-admin'`); err != nil {
		t.Fatal(err)
	}
	expect(request("PUT", "/api/v1/admin/instance-settings", "admin", `{"maintenanceEnabled":false}`, true), 200)
	status("member", "online", false)
	expect(request("GET", "/api/v1/workspaces", "member", "", false), 200)
	var events int
	if err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE action='instance.maintenance_updated' AND actor_id='maintenance-admin'`).Scan(&events); err != nil || events != 2 {
		t.Fatalf("expected audited enable/disable: count=%d err=%v", events, err)
	}
	if err := db.SetMaintenance(ctx, true, "maintenance-admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE sessions SET expires_at=NOW()-INTERVAL '1 second' WHERE user_id='maintenance-admin'`); err != nil {
		t.Fatal(err)
	}
	status("admin", "maintenance", false)
}
