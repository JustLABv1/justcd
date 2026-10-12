//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationApplicationOperationsPagination(t *testing.T) {
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
 INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at) VALUES('history-plan','history-app','main','digest','[]','[]','history-viewer',NOW()+interval '1 hour');
 INSERT INTO operations(id,application_id,plan_id,status,started_at) SELECT 'operation-' || lpad(i::text,3,'0'),'history-app','history-plan','succeeded','2026-10-12T10:00:00Z' FROM generate_series(1,63) i`)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Store: db}
	request := func(query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/?"+query, nil)
		r.SetPathValue("applicationID", "history-app")
		r = r.WithContext(context.WithValue(ctx, userContextKey, store.User{ID: "history-viewer"}))
		w := httptest.NewRecorder()
		server.listApplicationOperations(w, r)
		return w
	}
	w := request("")
	var defaults struct {
		Items   []store.Operation `json:"items"`
		HasMore bool              `json:"hasMore"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &defaults); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || len(defaults.Items) != 50 || !defaults.HasMore {
		t.Fatalf("default page changed: %d %#v", w.Code, defaults)
	}
	seen := map[string]bool{}
	for page := 0; page < 7; page++ {
		w := request(fmt.Sprintf("limit=10&offset=%d", page*10))
		if w.Code != http.StatusOK {
			t.Fatalf("page %d: %d %s", page, w.Code, w.Body.String())
		}
		var response struct {
			Items   []store.Operation `json:"items"`
			HasMore bool              `json:"hasMore"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		wantCount := 10
		if page == 6 {
			wantCount = 3
		}
		if len(response.Items) != wantCount || response.HasMore != (page < 6) {
			t.Fatalf("page %d: %#v", page, response)
		}
		for _, item := range response.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate operation %s", item.ID)
			}
			seen[item.ID] = true
		}
		if response.Items[0].ID != fmt.Sprintf("operation-%03d", 63-page*10) {
			t.Fatalf("unstable timestamp tie order: %#v", response.Items)
		}
	}
	if len(seen) != 63 {
		t.Fatalf("history truncated: %d", len(seen))
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=no", "offset=-1", "offset=no", "offset=1000001"} {
		if w := request(query); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", query, w.Code)
		}
	}
	if w := request("offset=70&limit=10"); w.Code != http.StatusOK {
		t.Fatalf("empty page: %d", w.Code)
	} else {
		var response struct {
			Items   []store.Operation `json:"items"`
			HasMore bool              `json:"hasMore"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Items) != 0 || response.HasMore {
			t.Fatalf("invalid empty page: %#v", response)
		}
	}
}
