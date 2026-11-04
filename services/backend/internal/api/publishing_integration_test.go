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

func TestIntegrationPublishingNamespaceBinding(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO workspaces(id,name) VALUES('namespace-publish','Namespace'),('namespace-other','Other')`,
		`INSERT INTO clusters(id,name,api_server) VALUES('namespace-cluster','Cluster','https://cluster.invalid')`,
	} {
		if _, err := db.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.PublishingNamespaceBinding(ctx, "namespace-publish", "namespace-cluster", "new-app", true); err == nil {
		t.Fatal("creation without deployment credentials accepted")
	}
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO credentials(id,name,kind,secret_cipher) VALUES('namespace-credential','Provisioner','kubernetes-token','encrypted'); UPDATE clusters SET default_credential_id='namespace-credential' WHERE id='namespace-cluster'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PublishingNamespaceBinding(ctx, "namespace-publish", "namespace-cluster", "new-app", true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NamespaceBinding(ctx, "namespace-publish", "namespace-cluster", "new-app"); err == nil {
		t.Fatal("preview persisted binding")
	}
	if _, err := db.PublishingNamespaceBinding(ctx, "namespace-publish", "namespace-cluster", "new-app", false); err == nil {
		t.Fatal("implicit creation allowed")
	}
	if err := db.CreateNamespaceBinding(ctx, "namespace-other", "namespace-cluster", "taken", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PublishingNamespaceBinding(ctx, "namespace-publish", "namespace-cluster", "taken", true); err == nil {
		t.Fatal("another workspace binding adopted")
	}
	// A malformed manual upload must not leave the requested binding behind.
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('namespace-owner','owner@namespace.invalid'); INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('namespace-publish','namespace-owner','owner')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{Store: db}
	server.Config.PublishingTempDir = t.TempDir()
	metadata, _ := json.Marshal(publishInput{WorkspaceID: "namespace-publish", ClusterID: "namespace-cluster", GitLabURL: "https://gitlab.invalid", Group: "analytics", GitLabToken: "test-api-token", GitReadToken: "test-read-token", RegistryUser: "registry-user", RegistryPassword: "test-push-token", Config: publishing.Config{Name: "new-app", Runtime: "shiny", EntryPoint: ".", BaseImage: "registry.invalid/base:1", BuilderImage: "registry.invalid/builder:1", RegistryImage: "registry.invalid/apps/new-app", Namespace: "new-app", SecretMode: "variables", CreateNamespace: true}})
	request := uploadRequest(t, string(metadata), []byte("invalid zip"), false)
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, store.User{ID: "namespace-owner"}))
	response := httptest.NewRecorder()
	server.publishApplication(response, request)
	if response.Code != 400 {
		t.Fatal("invalid namespace upload accepted", response.Code, response.Body.String())
	}
	if _, err := db.NamespaceBinding(ctx, "namespace-publish", "namespace-cluster", "new-app"); err == nil {
		t.Fatal("invalid upload persisted binding")
	}
	// Application registration and namespace binding must roll back together.
	app := store.Application{ID: "missing-source-app", WorkspaceID: "namespace-publish", Name: "test", SourceID: "missing", ClusterID: "namespace-cluster", Revision: "main", ManifestPath: "deploy", Renderer: "yaml", Namespaces: []store.NamespaceBinding{{Namespace: "new-app"}}, CreateNamespaces: true, SyncPolicy: "manual", PollSeconds: 300, RetryPolicy: store.DefaultRetryPolicy()}
	if err := db.CreateRepositoryPublishedApplication(ctx, app, nil); err == nil {
		t.Fatal("invalid app created")
	}
	if _, err := db.NamespaceBinding(ctx, "namespace-publish", "namespace-cluster", "new-app"); err == nil {
		t.Fatal("failed app left binding")
	}
	if err := db.CreateGitSource(ctx, store.GitSource{ID: "namespace-source", WorkspaceID: "namespace-publish", Name: "Source", RepositoryURL: "https://gitlab.invalid/apps/report.git"}); err != nil {
		t.Fatal(err)
	}
	app.SourceID = "namespace-source"
	if err := db.CreateRepositoryPublishedApplication(ctx, app, nil); err != nil {
		t.Fatal(err)
	}
	saved, err := db.ApplicationByID(ctx, app.ID)
	if err != nil || !saved.CreateNamespaces {
		t.Fatal("provisioning choice lost", err)
	}
	if _, err := db.NamespaceBinding(ctx, "namespace-publish", "namespace-cluster", "new-app"); err != nil {
		t.Fatal("binding missing after registration", err)
	}

}
