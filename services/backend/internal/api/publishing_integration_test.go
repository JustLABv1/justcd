//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationPublishingAuthorizationAndAtomicRegistration(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	for _, query := range []string{
		`INSERT INTO users(id,email) VALUES('publishing-owner','owner@publishing.invalid'),('publishing-viewer','viewer@publishing.invalid')`,
		`INSERT INTO workspaces(id,name) VALUES('publishing-test','Publishing test')`,
		`INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('publishing-test','publishing-owner','owner'),('publishing-test','publishing-viewer','viewer')`,
		`INSERT INTO clusters(id,name,api_server) VALUES('publishing-cluster','Test','https://cluster.invalid')`,
	} {
		if _, e := db.DB.ExecContext(ctx, query); e != nil {
			t.Fatal(e)
		}
	}
	s := &Server{Store: db}
	// Authorization must reject this before any GitLab network call or credential use.
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	form.WriteField("metadata", `{"workspaceId":"publishing-test"}`)
	form.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/publishing", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, store.User{ID: "publishing-viewer"}))
	response := httptest.NewRecorder()
	s.publishApplication(response, request)
	if got := response.Code; got != 403 {
		t.Fatalf("viewer publishing: %d", got)
	}
	// Authorized malformed/oversized archives must release all temporary files,
	// without creating a project or contacting the supplied GitLab origin.
	if err := db.CreateNamespaceBinding(ctx, "publishing-test", "publishing-cluster", "analytics", nil); err != nil {
		t.Fatal(err)
	}
	tempRoot := t.TempDir()
	s.Config.PublishingTempDir = tempRoot
	metadata, _ := json.Marshal(publishInput{WorkspaceID: "publishing-test", ClusterID: "publishing-cluster", GitLabURL: "https://gitlab.invalid", Group: "analytics", GitLabToken: "test-api-token", GitReadToken: "test-read-token", RegistryUser: "registry-user", RegistryPassword: "test-push-token", Config: publishing.Config{Name: "app", Runtime: "shiny", EntryPoint: ".", BaseImage: "registry.invalid/base:1", BuilderImage: "registry.invalid/builder:1", RegistryImage: "registry.invalid/apps/app", Namespace: "analytics", SecretMode: "variables"}})
	for _, size := range []int{32, 65} {
		s.Config.PublishingLimits = publishing.Limits{ArchiveBytes: 64, ExpandedBytes: 128}
		request := uploadRequest(t, string(metadata), bytes.Repeat([]byte{'x'}, size), false)
		request = request.WithContext(context.WithValue(request.Context(), userContextKey, store.User{ID: "publishing-owner"}))
		response := httptest.NewRecorder()
		s.publishApplication(response, request)
		want := 400
		if size > 64 {
			want = 413
		}
		if response.Code != want {
			t.Fatalf("upload status %d, want %d: %s", response.Code, want, response.Body.String())
		}
		entries, err := os.ReadDir(tempRoot)
		if err != nil || len(entries) != 0 {
			t.Fatal("temporary source files retained after upload failure", err)
		}
	}
	workspace := "publishing-test"
	credential := store.Credential{ID: "publishing-credential", WorkspaceID: &workspace, Name: "read", Kind: "git-https", Cipher: []byte("encrypted")}
	source := store.GitSource{ID: "publishing-source", WorkspaceID: workspace, Name: "app", RepositoryURL: "https://gitlab.invalid/group/app.git", CredentialID: &credential.ID}
	app := store.Application{ID: "publishing-app", WorkspaceID: workspace, Name: "app", SourceID: source.ID, Revision: "main", ManifestPath: "deploy", Renderer: "yaml", ClusterID: "publishing-cluster", Namespaces: []store.NamespaceBinding{{Namespace: "analytics"}}, SyncPolicy: "manual", PollSeconds: 30, RetryPolicy: store.DefaultRetryPolicy()}
	publication := store.PublishedApplication{ApplicationID: app.ID, Fingerprint: "source-hash", RepositoryURL: source.RepositoryURL, ProjectURL: "https://gitlab.invalid/group/app", ProjectID: 1, Config: json.RawMessage(`{"runtime":"shiny"}`), GitLabURL: "https://gitlab.invalid", Group: "group"}
	if e := db.RegisterPublishedApplication(ctx, credential, source, app, publication); e != nil {
		t.Fatal(e)
	}
	read, e := db.PublishedApplication(ctx, workspace, source.RepositoryURL)
	if e != nil || read.ApplicationID != app.ID || read.Fingerprint != "source-hash" {
		t.Fatalf("publication round trip: %+v %v", read, e)
	}
	// An application uniqueness conflict must roll back the new credential and source.
	credential.ID = "publishing-rollback-credential"
	source.ID = "publishing-rollback-source"
	source.Name = "other"
	source.CredentialID = &credential.ID
	app.ID = "publishing-other"
	app.SourceID = source.ID
	if e = db.RegisterPublishedApplication(ctx, credential, source, app, publication); e == nil {
		t.Fatal("expected application name conflict")
	}
	if _, e = db.CredentialByID(ctx, credential.ID); e == nil {
		t.Fatal("orphan credential after failed registration")
	}
	if _, e = db.GitSourceByID(ctx, source.ID); e == nil {
		t.Fatal("orphan source after failed registration")
	}
}
