//go:build integration

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationDeliveryMetrics(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('metrics-viewer','metrics@example.invalid');
 INSERT INTO workspaces(id,name) VALUES('metrics-workspace','Metrics'),('metrics-other','Other');
 INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('metrics-workspace','metrics-viewer','viewer');
 INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('metrics-source','metrics-workspace','Git','https://example.invalid/repo.git');
 INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('metrics-cluster','metrics-workspace','Cluster','https://example.invalid');
 INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id,namespaces,sync_policy) VALUES
  ('metrics-a','metrics-workspace','Alpha','metrics-source','main','deploy','yaml','metrics-cluster','[{"namespace":"a"}]','manual'),
  ('metrics-b','metrics-workspace','Beta','metrics-source','main','deploy','yaml','metrics-cluster','[{"namespace":"b"}]','manual');
 INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at) VALUES
  ('metrics-plan-a','metrics-a','main','digest','[]','[]','metrics-viewer',NOW()+interval '1 hour'),
  ('metrics-plan-b','metrics-b','main','digest','[]','[]','metrics-viewer',NOW()+interval '1 hour');`)
	if err != nil {
		t.Fatal(err)
	}
	end := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	at := func(offset time.Duration) time.Time { return end.Add(offset) }
	// Alpha: two failures in a row, fixed 30 minutes after the first one finished.
	// Beta: one failure fixed after 10 minutes, one still open, one cancelled run.
	// Previous week: one success. Outside both weeks: one failure.
	rows := []struct {
		id, app, plan, kind, status string
		start                       time.Time
		finish                      time.Duration
	}{
		{"a1", "metrics-a", "metrics-plan-a", "sync", "succeeded", at(-6 * day), time.Minute},
		{"a2", "metrics-a", "metrics-plan-a", "sync", "failed", at(-5 * day), time.Minute},
		{"a3", "metrics-a", "metrics-plan-a", "sync", "failed", at(-5*day + 10*time.Minute), time.Minute},
		{"a4", "metrics-a", "metrics-plan-a", "rollback", "succeeded", at(-5*day + 30*time.Minute), time.Minute},
		{"b1", "metrics-b", "metrics-plan-b", "sync", "failed", at(-3 * day), 0},
		{"b2", "metrics-b", "metrics-plan-b", "sync", "succeeded", at(-3*day + 10*time.Minute), 0},
		{"b3", "metrics-b", "metrics-plan-b", "sync", "cancelled", at(-2 * day), 0},
		{"b4", "metrics-b", "metrics-plan-b", "sync", "failed", at(-1 * day), 0},
		{"p1", "metrics-a", "metrics-plan-a", "sync", "succeeded", at(-10 * day), 0},
		{"old", "metrics-b", "metrics-plan-b", "sync", "failed", at(-20 * day), 0},
		// The old outage ends before this week, so Beta's failure below starts a new one.
		{"old-fix", "metrics-b", "metrics-plan-b", "sync", "succeeded", at(-15 * day), 0},
	}
	for _, row := range rows {
		if _, err := db.DB.ExecContext(ctx, `INSERT INTO operations(id,application_id,plan_id,operation_type,status,started_at,finished_at) VALUES($1,$2,$3,$4,$5,$6,$7)`,
			"metrics-"+row.id, row.app, row.plan, row.kind, row.status, row.start, row.start.Add(row.finish)); err != nil {
			t.Fatal(err)
		}
	}

	metrics, err := db.WorkspaceDeliveryMetrics(ctx, "metrics-workspace", end, 7*day)
	if err != nil {
		t.Fatal(err)
	}
	current := metrics.Current
	if current.Deployments != 3 || current.Failed != 4 {
		t.Fatalf("counts: %#v", current)
	}
	// Alpha restored 30 minutes after its first failure finished (the second failure does not restart the clock); Beta after 10.
	if current.Restored != 2 || current.RestoreSeconds == nil || *current.RestoreSeconds != (30*60+10*60)/2 {
		t.Fatalf("restore: %#v %v", current, current.RestoreSeconds)
	}
	if metrics.Previous.Deployments != 1 || metrics.Previous.Failed != 0 || metrics.Previous.RestoreSeconds != nil {
		t.Fatalf("previous: %#v", metrics.Previous)
	}
	if len(metrics.FailuresByApplication) != 2 || metrics.FailuresByApplication[0].Name != "Alpha" || metrics.FailuresByApplication[0].Failed != 2 || metrics.FailuresByApplication[1].Total != 4 {
		t.Fatalf("by application: %#v", metrics.FailuresByApplication)
	}

	server := &Server{Store: db}
	request := func(query, user string) int {
		r := httptest.NewRequest("GET", "/?"+query, nil)
		r.SetPathValue("workspaceID", "metrics-workspace")
		r = r.WithContext(context.WithValue(ctx, userContextKey, store.User{ID: user}))
		w := httptest.NewRecorder()
		server.workspaceDeliveryMetrics(w, r)
		return w.Code
	}
	if code := request("range=30d", "metrics-viewer"); code != http.StatusOK {
		t.Fatalf("viewer denied: %d", code)
	}
	if code := request("range=1y", "metrics-viewer"); code != http.StatusBadRequest {
		t.Fatalf("invalid range accepted: %d", code)
	}
	if code := request("range=7d", "outsider"); code != http.StatusForbidden {
		t.Fatalf("workspace isolation failed: %d", code)
	}
}
