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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegrationNotifications(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable database required")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('owner','owner@example.invalid'),('viewer','viewer@example.invalid'); INSERT INTO workspaces(id,name) VALUES('w','Workspace'),('other','Other');INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('w','owner','owner'),('w','viewer','viewer');INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('source','w','Git','https://example.invalid');INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('cluster','w','Kubernetes','https://example.invalid');INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id) VALUES('app','w','App','source','main','deploy','yaml','cluster')`)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("k", 32))
	s, err := New(db, config.Config{EncryptionKey: key, SessionLifetime: time.Hour, FrontendURL: "https://justcd.example"}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"owner", "viewer"} {
		if err = db.CreateSession(ctx, security.HashToken(user), security.HashToken("csrf"), user, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	request := func(method, path, user, body string, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: user})
		if csrf {
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf"})
			r.Header.Set("X-CSRF-Token", "csrf")
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status %d want %d: %s", w.Code, status, w.Body)
		}
	}
	base := "/api/v1/workspaces/w/notifications"
	body := `{"name":"Slack","provider":"slack","enabled":true,"events":["drift","approval_requested","sync_failed","unhealthy","operation_completed","rollback"],"minimumSeverity":"info","config":{"url":"https://example.invalid/TOP-SECRET","signingSecret":"12345678901234567890123456789012"}}`
	expect(request("POST", base, "viewer", body, true), 403)
	expect(request("POST", base, "owner", body, false), 403)
	w := request("POST", base, "owner", body, true)
	expect(w, 200)
	if strings.Contains(w.Body.String(), "TOP-SECRET") {
		t.Fatal("secret in response")
	}
	var d store.NotificationDestination
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	expect(request("GET", base+"/activity", "viewer", "", false), 403)
	expect(request("DELETE", "/api/v1/workspaces/other/notifications/"+d.ID, "owner", "", true), 403)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at,required_approvals) VALUES('plan','app','main','digest','[{"manifest":{"kind":"Secret","data":{"password":"TOP-SECRET"}}}]','[]','owner',NOW()+INTERVAL '1 hour',1);INSERT INTO operations(id,application_id,actor_id,status,operation_type) VALUES('op','app','owner','running','rollback');UPDATE operations SET status='failed' WHERE id='op';UPDATE applications SET health_condition_status='Degraded' WHERE id='app';INSERT INTO operations(id,application_id,actor_id,status) VALUES('op2','app','owner','running');UPDATE operations SET status='succeeded' WHERE id='op2'`)
	if err != nil {
		t.Fatal(err)
	}
	// A second destination receives only selected error-level events, never another workspace's events.
	filter := strings.ReplaceAll(strings.ReplaceAll(body, `"minimumSeverity":"info"`, `"minimumSeverity":"error"`), `["drift","approval_requested","sync_failed","unhealthy","operation_completed","rollback"]`, `["sync_failed"]`)
	filteredResponse := request("POST", base, "owner", filter, true)
	expect(filteredResponse, 200)
	var filtered store.NotificationDestination
	_ = json.Unmarshal(filteredResponse.Body.Bytes(), &filtered)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO notification_events(id,workspace_id,event_type,severity) VALUES('route-warning','w','sync_failed','warning'),('route-error','w','sync_failed','error'),('route-other','other','sync_failed','error')`)
	if err != nil {
		t.Fatal(err)
	}
	var routed int
	_ = db.DB.QueryRowContext(ctx, `SELECT count(*) FROM notification_deliveries WHERE destination_id=$1`, filtered.ID).Scan(&routed)
	if routed != 1 {
		t.Fatal("SQL event/severity/workspace routing", routed)
	}
	expect(request("DELETE", base+"/"+filtered.ID, "owner", "", true), 200)
	var count int
	if err = db.DB.QueryRowContext(ctx, `SELECT count(DISTINCT event_type) FROM notification_events`).Scan(&count); err != nil || count != 6 {
		t.Fatalf("all six events: %d %v", count, err)
	}
	// Isolate a test delivery and exhaust retries. Network errors cannot roll back reconciliation.
	expect(request("POST", base+"/"+d.ID+"/test", "owner", "{}", true), 202)
	_, _ = db.DB.ExecContext(ctx, `UPDATE notification_deliveries SET status='disabled' WHERE event_id IN(SELECT id FROM notification_events WHERE event_type<>'test')`)
	identity := ""
	for i := 1; i <= 5; i++ {
		worked, err := db.DeliverNotification(ctx, func(dest store.NotificationDestination, e notifications.Event, id string) notifications.Result {
			if identity != "" && identity != id {
				t.Fatal("retry identity changed")
			}
			identity = id
			raw, _ := json.Marshal(e)
			if strings.Contains(string(raw), "TOP-SECRET") {
				t.Fatal("secret in payload")
			}
			return notifications.Result{Retry: true, StatusCode: 503, Code: "provider_rejected"}
		})
		if err != nil || !worked {
			t.Fatalf("attempt %d: %v", i, err)
		}
		if i < 5 {
			_, _ = db.DB.ExecContext(ctx, `UPDATE notification_deliveries SET next_at=NOW() WHERE id=$1`, identity)
		}
	}
	items, err := db.NotificationActivity(ctx, "w")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range items {
		if a.ID == identity {
			found = true
			if a.Status != "failed" || a.Attempts != 5 || !strings.Contains(string(a.History), "provider_rejected") {
				t.Fatalf("terminal observability: %+v", a)
			}
		}
	}
	if !found {
		t.Fatal("missing delivery")
	}
	// Break only event capture and verify operation completion still commits.
	_, err = db.DB.ExecContext(ctx, `ALTER TABLE notification_events ADD CONSTRAINT test_capture_failure CHECK(event_type<>'operation_completed') NOT VALID; INSERT INTO operations(id,application_id,actor_id,status) VALUES('capture-failure','app','owner','running'); UPDATE operations SET status='succeeded' WHERE id='capture-failure'; ALTER TABLE notification_events DROP CONSTRAINT test_capture_failure`)
	if err != nil {
		t.Fatal("notification failure blocked reconciliation", err)
	}
	update := fmt.Sprintf(`{"name":"Slack","provider":"slack","enabled":false,"events":["drift"],"minimumSeverity":"error"}`)
	expect(request("PUT", base+"/"+d.ID, "owner", update, true), 200)
	expect(request("POST", base+"/"+d.ID+"/test", "owner", "{}", true), 409)
	expect(request("DELETE", base+"/"+d.ID, "owner", "", true), 200)
	var audits int
	_ = db.DB.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE action LIKE 'notification.%'`).Scan(&audits)
	if audits < 9 {
		t.Fatal("missing audit events", audits)
	}
}
