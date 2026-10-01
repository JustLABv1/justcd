//go:build integration

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationConnectionDeletionAuthorization(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	if _, err := db.DB.ExecContext(context.Background(), `INSERT INTO users(id,email) VALUES('delete-owner','owner@example.invalid'),('delete-viewer','viewer@example.invalid');
 INSERT INTO workspaces(id,name) VALUES('delete-workspace','Owner'),('delete-other','Other');
 INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('delete-workspace','delete-owner','owner'),('delete-workspace','delete-viewer','viewer');
 INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('delete-cluster','delete-workspace','Cluster','https://api.invalid');
 INSERT INTO credentials(id,workspace_id,name,kind,secret_cipher) VALUES('delete-credential','delete-workspace','Token','kubernetes-token',decode('00','hex'));
 INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('delete-source','delete-workspace','Git','https://git.invalid/repo.git');
 INSERT INTO repository_configurations(id,workspace_id,source_id,revision) VALUES('delete-repository','delete-workspace','delete-source','main');
 INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('delete-other-source','delete-other','Other Git','https://git.invalid/other.git');
 INSERT INTO namespace_bindings(id,workspace_id,cluster_id,namespace) VALUES('delete-binding','delete-workspace','delete-cluster','unused');
 UPDATE namespace_bindings SET credential_id='delete-credential' WHERE id='delete-binding';`); err != nil {
		t.Fatal(err)
	}
	server := &Server{Store: db}

	assigned := httptest.NewRequest(http.MethodDelete, "/api/v1/credentials/delete-credential", nil)
	assigned.SetPathValue("credentialID", "delete-credential")
	assigned = assigned.WithContext(context.WithValue(assigned.Context(), userContextKey, store.User{ID: "delete-owner"}))
	blocked := httptest.NewRecorder()
	server.deleteCredential(blocked, assigned)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("assigned credential deletion: %d %s", blocked.Code, blocked.Body.String())
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/git-sources/delete-other-source", nil)
	request.SetPathValue("sourceID", "delete-other-source")
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, store.User{ID: "delete-owner"}))
	response := httptest.NewRecorder()
	server.deleteGitSource(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-workspace deletion: %d %s", response.Code, response.Body.String())
	}
	for _, item := range []struct {
		handler           http.HandlerFunc
		pathKey, id, path string
	}{{server.deleteNamespaceBinding, "namespace", "unused", "/api/v1/clusters/delete-cluster/bindings/unused?workspaceId=delete-workspace"}, {server.deleteCluster, "clusterID", "delete-cluster", "/api/v1/clusters/delete-cluster"}, {server.deleteRepositoryConfiguration, "repositoryID", "delete-repository", "/api/v1/repository-configurations/delete-repository"}, {server.deleteGitSource, "sourceID", "delete-source", "/api/v1/git-sources/delete-source"}, {server.deleteCredential, "credentialID", "delete-credential", "/api/v1/credentials/delete-credential"}} {
		for _, user := range []struct {
			id     string
			status int
		}{{"delete-viewer", http.StatusForbidden}, {"delete-owner", http.StatusNoContent}} {
			request := httptest.NewRequest(http.MethodDelete, item.path, nil)
			request.SetPathValue(item.pathKey, item.id)
			request.SetPathValue("clusterID", "delete-cluster")
			request = request.WithContext(context.WithValue(request.Context(), userContextKey, store.User{ID: user.id}))
			response := httptest.NewRecorder()
			item.handler(response, request)
			if response.Code != user.status {
				t.Fatalf("%s %s: status %d body %s", user.id, item.path, response.Code, response.Body.String())
			}
		}
	}
}
