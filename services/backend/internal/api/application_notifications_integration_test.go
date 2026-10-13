//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/justlab/justcd/services/backend/internal/config"
	"github.com/justlab/justcd/services/backend/internal/notifications"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegrationApplicationNotificationRouting(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable database required")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('owner','owner@example.invalid'),('viewer','viewer@example.invalid'),('deployer','deployer@example.invalid');INSERT INTO workspaces(id,name) VALUES('w','Workspace'),('other','Other');INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('w','owner','owner'),('w','viewer','viewer'),('w','deployer','deployer');INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('source','w','Git','https://example.invalid');INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('cluster','w','Kubernetes','https://example.invalid');INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id) VALUES('prod','w','Prod','source','main','deploy','yaml','cluster'),('dev','w','Dev','source','main','deploy','yaml','cluster');INSERT INTO notification_destinations(id,workspace_id,name,provider,events,minimum_severity,config_cipher) VALUES('default','w','Default','slack','["sync_failed"]','error','cipher'),('pager','w','Pager','telegram','["unhealthy"]','error','cipher'),('foreign','other','Other','slack','["drift"]','info','cipher')`)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(db, config.Config{EncryptionKey: []byte(strings.Repeat("k", 32)), SessionLifetime: time.Hour}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"owner", "viewer", "deployer"} {
		if err = db.CreateSession(ctx, security.HashToken(user), security.HashToken("csrf"), user, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	req := func(method, user, app, body string, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/applications/"+app+"/notifications", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: user})
		if csrf {
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf"})
			r.Header.Set("X-CSRF-Token", "csrf")
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, code int) {
		t.Helper()
		if w.Code != code {
			t.Fatalf("got %d want %d: %s", w.Code, code, w.Body)
		}
	}
	expect(req("GET", "viewer", "prod", "", false), 403)
	expect(req("GET", "deployer", "prod", "", false), 403)
	expect(req("GET", "owner", "prod", "", false), 200)
	custom := `{"mode":"custom","destinationIds":["pager"],"events":["drift","sync_failed"],"minimumSeverity":"info"}`
	expect(req("PUT", "owner", "prod", custom, false), 403)
	expect(req("PUT", "viewer", "prod", custom, true), 403)
	expect(req("PUT", "owner", "prod", `{"mode":"custom","destinationIds":["foreign"],"events":["drift"],"minimumSeverity":"info"}`, true), 409)
	expect(req("PUT", "owner", "prod", `{"mode":"custom","destinationIds":[],"events":["drift"],"minimumSeverity":"info"}`, true), 400)
	event := func(id, app, kind, severity string) {
		t.Helper()
		if _, err = db.DB.ExecContext(ctx, `INSERT INTO notification_events(id,workspace_id,application_id,event_type,severity) VALUES($1,'w',$2,$3,$4)`, id, app, kind, severity); err != nil {
			t.Fatal(err)
		}
	}
	routes := func(id string, want ...string) {
		t.Helper()
		rows, err := db.DB.QueryContext(ctx, `SELECT destination_id FROM notification_deliveries WHERE event_id=$1 ORDER BY destination_id`, id)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		ids := []string{}
		for rows.Next() {
			var id string
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		if strings.Join(ids, ",") != strings.Join(want, ",") {
			t.Fatalf("%s routes: %v want %v", id, ids, want)
		}
	}
	event("inherited", "prod", "sync_failed", "error")
	routes("inherited", "default")
	expect(req("PUT", "owner", "prod", custom, true), 200)
	event("custom", "prod", "drift", "info")
	routes("custom", "pager") // overrides destination's events and higher threshold
	event("dev-default", "dev", "sync_failed", "error")
	routes("dev-default", "default")
	event("prod-ignored", "prod", "unhealthy", "error")
	routes("prod-ignored")
	expect(req("PUT", "owner", "dev", `{"mode":"disabled"}`, true), 200)
	event("dev-off", "dev", "sync_failed", "error")
	routes("dev-off")
	var status string
	_ = db.DB.QueryRowContext(ctx, `SELECT status FROM notification_deliveries WHERE event_id='dev-default'`).Scan(&status)
	if status != "disabled" {
		t.Fatal("app disable did not cancel queued delivery")
	}
	// A disabled destination vetoes even custom production routing.
	_, _ = db.DB.ExecContext(ctx, `UPDATE notification_destinations SET enabled=FALSE WHERE id='pager'`)
	event("pager-off", "prod", "sync_failed", "error")
	routes("pager-off")
	_, _ = db.DB.ExecContext(ctx, `UPDATE notification_destinations SET enabled=TRUE WHERE id='pager'`)
	expect(req("PUT", "owner", "prod", `{"mode":"custom","destinationIds":["pager"],"events":["sync_failed"],"minimumSeverity":"error"}`, true), 200)
	event("below-threshold", "prod", "sync_failed", "warning")
	routes("below-threshold")
	// Confirm excluded queued messages cannot be sent by the worker.
	_, _ = db.DB.ExecContext(ctx, `UPDATE notification_deliveries SET status='queued',next_at=NOW() WHERE event_id='custom'`)
	called := false
	worked, err := db.DeliverNotification(ctx, func(d store.NotificationDestination, e notifications.Event, id string) notifications.Result {
		called = true
		return notifications.Result{Code: "delivered"}
	})
	if err != nil || called || !worked {
		t.Fatalf("excluded message sent %t %v", called, err)
	}
	expect(req("PUT", "owner", "prod", `{"mode":"inherit"}`, true), 200)
	event("restored", "prod", "sync_failed", "error")
	routes("restored", "default")
	var count int
	_ = db.DB.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE action='notification.application_policy_saved'`).Scan(&count)
	if count != 4 {
		t.Fatal("policy audit", count)
	}
	// Native Telegram credentials are write-only, including in the application destination list.
	token := "123456:abcdefghijklmnopqrstuvwxyz123456789"
	body := fmt.Sprintf(`{"name":"Telegram","provider":"telegram","enabled":true,"events":["sync_failed"],"minimumSeverity":"error","config":{"botToken":%q,"chatId":"-123456","messageThreadId":42}}`, token)
	r := httptest.NewRequest("POST", "/api/v1/workspaces/w/notifications", strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "owner"})
	r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf"})
	r.Header.Set("X-CSRF-Token", "csrf")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	expect(w, 200)
	if strings.Contains(w.Body.String(), token) {
		t.Fatal("token in API response")
	}
	var d store.NotificationDestination
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	var cipher []byte
	_ = db.DB.QueryRowContext(ctx, `SELECT config_cipher FROM notification_destinations WHERE id=$1`, d.ID).Scan(&cipher)
	if strings.Contains(string(cipher), token) {
		t.Fatal("token stored in plaintext")
	}
	w = req("GET", "owner", "prod", "", false)
	if strings.Contains(w.Body.String(), token) {
		t.Fatal("token in application config")
	}
}
