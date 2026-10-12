//go:build integration

package api

import (
	"context"
	"encoding/json"
	"github.com/justlab/justcd/services/backend/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestIntegrationSyncHistory(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('history-viewer','history@example.invalid');
 INSERT INTO workspaces(id,name) VALUES('history-workspace','History');
 INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('history-workspace','history-viewer','viewer');
 INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('history-source','history-workspace','Git','https://example.invalid/repo.git');
 INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('history-cluster','history-workspace','Cluster','https://example.invalid');
 INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id,namespaces,sync_policy) VALUES('history-app','history-workspace','App','history-source','main','deploy','yaml','history-cluster','[{"namespace":"dev"}]','manual');
 INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at) VALUES('history-plan','history-app','main','digest','[]','[]','history-viewer',NOW()+interval '1 hour');`)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO operations(id,application_id,plan_id,status,started_at)
 SELECT 'run-' || i,'history-app','history-plan','succeeded',$1::timestamptz FROM generate_series(1,125) i`, start)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.ExecContext(ctx, `INSERT INTO operations(id,application_id,plan_id,operation_type,status,started_at) VALUES
 ('failed','history-app','history-plan','sync','failed',$1::timestamptz + interval '1 hour'),
 ('cancelled','history-app','history-plan','rollback','cancelled',$1::timestamptz + interval '1 hour'),
 ('queued','history-app','history-plan','sync','queued',$1::timestamptz + interval '2 hours'),
 ('running','history-app','history-plan','sync','running',$1::timestamptz + interval '2 hours'),
 ('before','history-app','history-plan','sync','failed',$1::timestamptz - interval '1 microsecond'),
 ('at-end','history-app','history-plan','sync','failed',$2),
 ('resource','history-app','history-plan','resource_action','failed',$1)`, start, end)
	if err != nil {
		t.Fatal(err)
	}
	history, err := db.SyncHistory(ctx, "history-app", start, end, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Buckets) != 24 || history.BucketSeconds != 3600 {
		t.Fatalf("invalid range: %#v", history)
	}
	if history.Buckets[0].Succeeded != 125 || history.Buckets[1].Failed != 1 || history.Buckets[1].Cancelled != 1 || history.Buckets[2].Active != 2 {
		t.Fatalf("incorrect outcomes: %#v", history.Buckets[:3])
	}
	for i, bucket := range history.Buckets {
		if !bucket.StartedAt.Equal(start.Add(time.Duration(i) * time.Hour)) {
			t.Fatal("bucket timestamp misaligned")
		}
		if i > 2 && (bucket.Succeeded+bucket.Failed+bucket.Cancelled+bucket.Active != 0) {
			t.Fatalf("empty bucket contains data: %#v", bucket)
		}
	}
	empty, err := db.SyncHistory(ctx, "missing-app", start, end, time.Hour)
	if err != nil || len(empty.Buckets) != 24 || empty.Buckets[0].Succeeded != 0 {
		t.Fatalf("invalid empty history: %#v %v", empty, err)
	}
	server := &Server{Store: db}
	request := func(query, user string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/?"+query, nil)
		r.SetPathValue("applicationID", "history-app")
		r = r.WithContext(context.WithValue(ctx, userContextKey, store.User{ID: user}))
		w := httptest.NewRecorder()
		server.applicationSyncHistory(w, r)
		return w
	}
	for _, rangeName := range []string{"24h", "7d"} {
		w := request("range="+rangeName, "history-viewer")
		if w.Code != http.StatusOK {
			t.Fatalf("viewer denied: %d %s", w.Code, w.Body.String())
		}
		var response store.SyncHistory
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		wantBuckets, wantSeconds := 24, 3600
		if rangeName == "7d" {
			wantBuckets, wantSeconds = 28, 21600
		}
		if len(response.Buckets) != wantBuckets || response.BucketSeconds != wantSeconds {
			t.Fatalf("wrong %s range: %#v", rangeName, response)
		}
	}
	if w := request("range=365d", "history-viewer"); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid range accepted: %d", w.Code)
	}
	if w := request("range=24h", "outsider"); w.Code != http.StatusForbidden {
		t.Fatalf("workspace isolation failed: %d", w.Code)
	}
}
