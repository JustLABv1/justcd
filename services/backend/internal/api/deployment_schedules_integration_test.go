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

	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationDeploymentScheduleAuthorization(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('schedule-owner','owner@example.invalid'),('schedule-deployer','deploy@example.invalid'),('schedule-viewer','view@example.invalid'),('schedule-outsider','out@example.invalid');
 INSERT INTO workspaces(id,name) VALUES('schedule-workspace','Schedules');
 INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('schedule-workspace','schedule-owner','owner'),('schedule-workspace','schedule-deployer','deployer'),('schedule-workspace','schedule-viewer','viewer');
 INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('schedule-source','schedule-workspace','Git','https://example.invalid/repo.git');
 INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('schedule-cluster','schedule-workspace','Cluster','https://example.invalid');
 INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id,namespaces,sync_policy) VALUES('schedule-app','schedule-workspace','App','schedule-source','main','deploy','yaml','schedule-cluster','[]','manual');
 INSERT INTO operations(id,application_id,actor_id,status) VALUES('schedule-op','schedule-app','schedule-deployer','queued')`)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Store: db}
	request := func(handler http.HandlerFunc, user, key, id, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.SetPathValue(key, id)
		r = r.WithContext(context.WithValue(ctx, userContextKey, store.User{ID: user}))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	policy := `{"windows":[{"timeZone":"UTC","days":[1],"start":"09:00","end":"10:00"}],"freezes":[{"reason":"Incident"}]}`
	for _, user := range []string{"schedule-deployer", "schedule-viewer", "schedule-outsider"} {
		w := request(server.putDeploymentSchedule, user, "workspaceID", "schedule-workspace", policy)
		if w.Code != 403 {
			t.Fatalf("%s wrote workspace policy: %d %s", user, w.Code, w.Body)
		}
	}
	if w := request(server.putDeploymentSchedule, "schedule-owner", "workspaceID", "schedule-workspace", policy); w.Code != 200 {
		t.Fatalf("owner denied: %d %s", w.Code, w.Body)
	}
	for _, user := range []string{"schedule-owner", "schedule-deployer", "schedule-viewer"} {
		w := request(server.getDeploymentSchedule, user, "applicationID", "schedule-app", "")
		if w.Code != 200 {
			t.Fatalf("%s cannot view %d", user, w.Code)
		}
		var response struct {
			Decision struct {
				Allowed bool
				Reasons []string
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Decision.Allowed || len(response.Decision.Reasons) == 0 {
			t.Fatalf("workspace freeze not inherited: %s %v", w.Body, err)
		}
	}
	if w := request(server.getDeploymentSchedule, "schedule-outsider", "applicationID", "schedule-app", ""); w.Code != 403 {
		t.Fatalf("cross workspace policy exposed: %d", w.Code)
	}
	for _, user := range []string{"schedule-deployer", "schedule-viewer", "schedule-outsider"} {
		if w := request(server.overrideOperationSchedule, user, "operationID", "schedule-op", `{"reason":"Incident response"}`); w.Code != 403 {
			t.Fatalf("%s overrode: %d", user, w.Code)
		}
	}
	if w := request(server.overrideOperationSchedule, "schedule-owner", "operationID", "schedule-op", `{"reason":" "}`); w.Code != 409 {
		t.Fatalf("empty reason: %d", w.Code)
	}
	if w := request(server.overrideOperationSchedule, "schedule-owner", "operationID", "schedule-op", `{"reason":"Incident response"}`); w.Code != 200 {
		t.Fatalf("owner override failed: %d %s", w.Code, w.Body)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE workspace_memberships SET role='deployer' WHERE workspace_id='schedule-workspace' AND user_id='schedule-owner'`); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckScheduleOverrideOwner(ctx, "schedule-workspace", "schedule-owner"); err == nil {
		t.Fatal("revoked override owner still authorized")
	}
	if w := request(server.cancelQueuedOperation, "schedule-viewer", "operationID", "schedule-op", `{}`); w.Code != 403 {
		t.Fatalf("viewer cancelled %d", w.Code)
	}
	if w := request(server.cancelQueuedOperation, "schedule-deployer", "operationID", "schedule-op", `{}`); w.Code != 200 {
		t.Fatalf("deployer could not cancel: %d %s", w.Code, w.Body)
	}
}
