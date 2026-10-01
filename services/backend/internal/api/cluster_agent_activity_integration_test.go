//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationAgentActivityLifecycleAndIsolation(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('activity-user','activity@example.invalid'); INSERT INTO workspaces(id,name) VALUES('activity-w','Activity'),('activity-other','Other'); INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('activity-w','activity-user','viewer'); INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('activity-cluster','activity-w','Activity','https://unused.invalid'); INSERT INTO cluster_agents(cluster_id,token_expires_at,last_seen_at,profiles) VALUES('activity-cluster',NOW()+INTERVAL '1 day',NOW(),'[{"name":"local","workspaceIds":["activity-w","activity-other"],"namespaces":["dev"]},{"name":"hidden-profile","workspaceIds":["activity-other"],"namespaces":["hidden-ns"]}]')`)
	if err != nil {
		t.Fatal(err)
	}
	request := agentprotocol.Request{WorkspaceID: "activity-w", Profile: "local", Namespace: "dev", Method: "PATCH", URI: "/api/v1/namespaces/dev/secrets/secret-canary?token=query-canary", Body: []byte("body-canary")}
	deadline := time.Now().Add(time.Minute)
	if err = db.QueueAgentTask(ctx, "success", "activity-cluster", []byte("encrypted"), deadline, request); err != nil {
		t.Fatal(err)
	}
	items, summary, err := db.ListAgentActivity(ctx, "activity-cluster", "activity-w")
	if err != nil || len(items) != 1 || summary.Queued != 1 || items[0].Resource != "secrets" {
		t.Fatalf("queued: %#v %#v %v", items, summary, err)
	}
	if _, _, err = db.ClaimAgentTask(ctx, "activity-cluster", "lease"); err != nil {
		t.Fatal(err)
	}
	items, summary, err = db.ListAgentActivity(ctx, "activity-cluster", "activity-w")
	if err != nil || summary.Running != 1 || summary.Queued != 0 || items[0].StartedAt == nil {
		t.Fatalf("running: %#v %#v %v", items, summary, err)
	}
	if err = db.CompleteAgentTask(ctx, "activity-cluster", "success", "wrong-lease", []byte("response"), agentprotocol.Result{Status: 200}); err == nil {
		t.Fatal("accepted wrong lease")
	}
	if err = db.CompleteAgentTask(ctx, "activity-cluster", "success", "lease", []byte("response"), agentprotocol.Result{Status: 200, Body: []byte("response-canary")}); err != nil {
		t.Fatal(err)
	}
	db.DeleteAgentTask(ctx, "success")
	items, summary, err = db.ListAgentActivity(ctx, "activity-cluster", "activity-w")
	if err != nil || len(items) != 1 || items[0].State != "succeeded" || summary.LastSuccessAt == nil {
		t.Fatalf("history lost after payload deletion: %#v %#v %v", items, summary, err)
	}
	if _, err = db.AgentTaskResult(ctx, "success"); err == nil {
		t.Fatal("payload retained")
	}
	for _, test := range []struct {
		id          string
		result      agentprotocol.Result
		state, code string
	}{
		{"rbac", agentprotocol.Result{Status: 403, Body: []byte("response-canary")}, "failed", "kubernetes.access_denied"},
		{"scope", agentprotocol.Result{Error: "secret-canary", ErrorCode: "cluster.agent_scope_denied"}, "failed", "cluster.agent_scope_denied"},
		{"unknown", agentprotocol.Result{Error: "secret-canary", ErrorCode: "cluster.agent_unknown_outcome"}, "unknown", "cluster.agent_unknown_outcome"},
		{"untrusted", agentprotocol.Result{Error: "secret-canary", ErrorCode: "error-code-canary"}, "failed", "cluster.agent_request_failed"},
	} {
		if err = db.QueueAgentTask(ctx, test.id, "activity-cluster", []byte("encrypted"), deadline, request); err != nil {
			t.Fatal(err)
		}
		if _, _, err = db.ClaimAgentTask(ctx, "activity-cluster", "lease"); err != nil {
			t.Fatal(err)
		}
		if err = db.CompleteAgentTask(ctx, "activity-cluster", test.id, "lease", []byte("response"), test.result); err != nil {
			t.Fatal(err)
		}
		db.DeleteAgentTask(ctx, test.id)
		items, _, err = db.ListAgentActivity(ctx, "activity-cluster", "activity-w")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range items {
			if item.ID == test.id {
				found = item.State == test.state && item.ErrorCode == test.code
			}
		}
		if !found {
			t.Fatalf("missing result %s: %#v", test.id, items)
		}
	}
	for _, id := range []string{"expired", "lost"} {
		if err = db.QueueAgentTask(ctx, id, "activity-cluster", []byte("encrypted"), deadline, request); err != nil {
			t.Fatal(err)
		}
		if id == "lost" {
			if _, _, err = db.ClaimAgentTask(ctx, "activity-cluster", "lease"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = db.DB.ExecContext(ctx, `UPDATE cluster_agent_tasks SET deadline=NOW()-INTERVAL '6 minutes' WHERE id=$1;`, id); err != nil {
			t.Fatal(err)
		}
		if _, err = db.DB.ExecContext(ctx, `UPDATE cluster_agent_activity SET deadline=NOW()-INTERVAL '6 minutes' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		// Remove unclaimed expired task before claiming the next one.
		if id == "expired" {
			db.DeleteAgentTask(ctx, id)
		}
	}
	if err = db.PruneAgentTasks(ctx); err != nil {
		t.Fatal(err)
	}
	request.WorkspaceID = "activity-other"
	if err = db.QueueAgentTask(ctx, "other-task", "activity-cluster", []byte("encrypted"), deadline, request); err != nil {
		t.Fatal(err)
	}
	server := &Server{Store: db}
	r := httptest.NewRequest("GET", "/api/v1/clusters/activity-cluster/agent?workspaceId=activity-w", nil)
	r.SetPathValue("clusterID", "activity-cluster")
	r = r.WithContext(context.WithValue(r.Context(), userContextKey, store.User{ID: "activity-user"}))
	response := httptest.NewRecorder()
	server.getClusterAgent(response, r)
	if response.Code != 200 {
		t.Fatalf("status: %d %s", response.Code, response.Body.String())
	}
	for _, canary := range []string{"secret-canary", "query-canary", "body-canary", "response-canary", "error-code-canary", "other-task", "activity-other", "hidden-profile", "hidden-ns"} {
		if strings.Contains(response.Body.String(), canary) {
			t.Fatalf("leaked %s", canary)
		}
	}
	var payload struct {
		Activity []store.AgentActivity
		Summary  store.AgentActivitySummary
	}
	if err = json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, item := range payload.Activity {
		states[item.ID] = item.State
	}
	if states["expired"] != "expired" || states["lost"] != "unknown" || payload.Summary.LastFailureAt == nil || payload.Summary.Queued != 0 {
		t.Fatalf("incorrect expiry or isolation: %#v", payload)
	}
	r = httptest.NewRequest("GET", "/api/v1/clusters/activity-cluster/agent?workspaceId=activity-other", nil)
	r.SetPathValue("clusterID", "activity-cluster")
	r = r.WithContext(context.WithValue(r.Context(), userContextKey, store.User{ID: "activity-user"}))
	response = httptest.NewRecorder()
	server.getClusterAgent(response, r)
	if response.Code != 403 {
		t.Fatalf("other workspace status: %d", response.Code)
	}
	if _, err = db.DB.ExecContext(ctx, `UPDATE cluster_agent_activity SET created_at=NOW()-INTERVAL '8 days' WHERE id='success'`); err != nil {
		t.Fatal(err)
	}
	if err = db.PruneAgentTasks(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM cluster_agent_activity WHERE id='success'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("retention: %d %v", count, err)
	}
}
