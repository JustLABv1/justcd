//go:build integration

package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationAuditCompliance(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email,display_name) VALUES('audit-owner','owner@example.invalid','Owner'),('audit-viewer','viewer@example.invalid','Viewer'),('audit-outsider','outsider@example.invalid','Outsider'),('audit-deleted','delete@example.invalid','Former Name');
 INSERT INTO workspaces(id,name) VALUES('audit-a','Workspace A'),('audit-b','Workspace B');
 INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('audit-a','audit-owner','owner'),('audit-a','audit-viewer','viewer');
 INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('audit-source','audit-a','Source','https://example.invalid/repo.git');
 INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('audit-cluster','audit-a','Cluster','https://example.invalid');
 INSERT INTO applications(id,workspace_id,name,source_id,revision,manifest_path,renderer,cluster_id) VALUES('audit-app','audit-a','Payments','audit-source','main','deploy','yaml','audit-cluster');
 INSERT INTO plans(id,application_id,revision,digest,bindings,changes,desired,created_by,expires_at,policy_evaluation,required_approvals) VALUES('audit-plan','audit-app','commit-sha','digest-123','[{"clusterId":"audit-cluster","namespace":"production","credentialRef":"never-export"}]','[]','[{"kind":"Secret","data":{"password":"never-export"}}]','audit-owner',NOW()+INTERVAL '1 day','{"mode":"block","configDigest":"policy-digest","results":[{"rule":"public-services","resource":{"kind":"Service","name":"payments"},"evidence":"never-export"}]}',1);
 INSERT INTO deletion_approvals(id,plan_id,actor_id,plan_digest,deletes,expires_at,used_at) VALUES('audit-approval','audit-plan','audit-viewer','digest-123','[]',NOW()+INTERVAL '1 day',NOW());
 INSERT INTO operations(id,application_id,plan_id,actor_id,status,approval_ids,finished_at) VALUES('audit-operation','audit-app','audit-plan','audit-owner','succeeded','["audit-approval"]',NOW());
 INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES('audit-owner','sync.succeeded','application','audit-app','{"planId":"audit-plan","operationId":"audit-operation","message":"never-export","settings":{"password":"never-export"}}');
 INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) SELECT 'audit-owner','workspace.updated','workspace','audit-a',jsonb_build_object('index',n) FROM generate_series(1,510) n;
 INSERT INTO audit_events(actor_id,action,resource_type,resource_id) VALUES('audit-owner','workspace.updated','workspace','audit-b');
 INSERT INTO audit_events(actor_id,action,resource_type,resource_id,created_at) VALUES('audit-deleted','workspace.updated','workspace','audit-a',NOW()-INTERVAL '100 days')`)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Store: db}
	request := func(handler http.HandlerFunc, user, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.SetPathValue("workspaceID", "audit-a")
		r = r.WithContext(context.WithValue(ctx, userContextKey, store.User{ID: user}))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	for _, user := range []string{"audit-owner", "audit-viewer"} {
		w := request(server.exportAudit, user, "/?workspaceId=audit-a&format=json", "")
		if w.Code != 200 {
			t.Fatalf("%s denied: %d %s", user, w.Code, w.Body)
		}
		var result struct {
			SchemaVersion int                 `json:"schemaVersion"`
			Items         []auditExportRecord `json:"items"`
			Complete      bool                `json:"complete"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Items) != 512 || !result.Complete || result.SchemaVersion != 1 {
			t.Fatalf("export lost pages: %d, complete %t", len(result.Items), result.Complete)
		}
		for i, e := range result.Items {
			if e.WorkspaceID != "audit-a" {
				t.Fatalf("scope leaked: %+v", e)
			}
			if i > 0 && result.Items[i-1].ID <= e.ID {
				t.Fatal("duplicate or out-of-order export")
			}
		}
		if strings.Contains(w.Body.String(), "never-export") {
			t.Fatal("sensitive evidence exported")
		}
	}
	for _, handler := range []http.HandlerFunc{server.listAudit, server.exportAudit} {
		for _, path := range []string{"/?workspaceId=audit-b", "/"} {
			if w := request(handler, "audit-viewer", path, ""); w.Code != 403 {
				t.Fatalf("scope authorization failed: %d", w.Code)
			}
		}
	}
	if w := request(server.exportAudit, "audit-outsider", "/?workspaceId=audit-a", ""); w.Code != 403 {
		t.Fatalf("outsider allowed: %d", w.Code)
	}
	// Both list pagination and exports honor the same group, query and resource.
	w := request(server.listAudit, "audit-viewer", "/?workspaceId=audit-a&group=changes&q=workspace", "")
	var page struct {
		Items   []store.AuditEvent `json:"items"`
		HasMore bool               `json:"hasMore"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 50 || !page.HasMore {
		t.Fatalf("bad page: %s", w.Body)
	}
	before := page.Items[49].ID
	w = request(server.listAudit, "audit-viewer", fmt.Sprintf("/?workspaceId=audit-a&before=%d", before), "")
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Items[0].ID >= before {
		t.Fatalf("bad cursor: %s %v", w.Body, err)
	}
	w = request(server.exportAudit, "audit-viewer", "/?workspaceId=audit-a&group=deployments&resourceId=audit-app&format=csv", "")
	rows, err := csv.NewReader(w.Body).ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("filtered CSV %d %v %s", len(rows), err, w.Body)
	}
	if !strings.Contains(rows[1][11], "audit-approval") || !strings.Contains(rows[1][11], "commit-sha") {
		t.Fatalf("deployment evidence incomplete: %v", rows)
	}
	var eventID int64
	if err := db.DB.QueryRowContext(ctx, `SELECT id FROM audit_events WHERE action='sync.succeeded'`).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	w = request(server.exportAudit, "audit-viewer", "/?workspaceId=audit-a&eventId="+strconv.FormatInt(eventID, 10), "")
	var single struct {
		Items []store.AuditEvent `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &single); err != nil || len(single.Items) != 1 {
		t.Fatalf("single export: %s %v", w.Body, err)
	}
	// Snapshot remains after application/plan/operation cascade deletion.
	if _, err := db.DB.ExecContext(ctx, `DELETE FROM applications WHERE id='audit-app'`); err != nil {
		t.Fatal(err)
	}
	e, err := db.AuditEventByID(ctx, eventID)
	if err != nil || !strings.Contains(string(e.DeploymentRecord), "digest-123") || e.WorkspaceID != "audit-a" {
		t.Fatalf("lost deleted application evidence: %+v %v", e, err)
	}
	// Account deletion retains the stable actor ID and anonymizes display data.
	if err := db.DeleteAdminUser(ctx, "audit-deleted"); err != nil {
		t.Fatal(err)
	}
	w = request(server.exportAudit, "audit-viewer", "/?workspaceId=audit-a&actorId=audit-deleted", "")
	if strings.Contains(w.Body.String(), "Former Name") || !strings.Contains(w.Body.String(), "Deleted user") || !strings.Contains(w.Body.String(), "audit-deleted") {
		t.Fatalf("actor history: %s", w.Body)
	}
	if w := request(server.setAuditRetention, "audit-viewer", "/", `{"days":30}`); w.Code != 403 {
		t.Fatalf("viewer changed retention: %d", w.Code)
	}
	for _, body := range []string{`{}`, `{"days":-1}`, `{"days":36501}`, `{"days":null}`} {
		if w := request(server.setAuditRetention, "audit-owner", "/", body); w.Code != 400 {
			t.Fatalf("bad retention accepted: %s %d", body, w.Code)
		}
	}
	if err := db.PruneAuditEvents(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	_ = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE actor_id='audit-deleted'`).Scan(&count)
	if count != 1 {
		t.Fatal("default retention expired evidence")
	}
	if w := request(server.setAuditRetention, "audit-owner", "/", `{"days":30}`); w.Code != 200 {
		t.Fatalf("retention update: %s", w.Body)
	}
	if err := db.PruneAuditEvents(ctx); err != nil {
		t.Fatal(err)
	}
	_ = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE actor_id='audit-deleted'`).Scan(&count)
	if count != 0 {
		t.Fatal("expired evidence retained")
	}
	_ = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE workspace_id='audit-b'`).Scan(&count)
	if count != 1 {
		t.Fatal("retention crossed workspace boundary")
	}
}
