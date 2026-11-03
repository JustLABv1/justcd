//go:build integration

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationPublishingExistingRepository(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO users(id,email) VALUES('repo-owner','owner@repo.invalid'),('repo-analyst','analyst@repo.invalid'),('repo-viewer','viewer@repo.invalid'),('repo-other','other@repo.invalid')`,
		`INSERT INTO workspaces(id,name) VALUES('repo-workspace','Repository'),('repo-other','Other')`,
		`INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('repo-workspace','repo-owner','owner'),('repo-workspace','repo-analyst','deployer'),('repo-workspace','repo-viewer','viewer'),('repo-other','repo-other','owner')`,
		`INSERT INTO clusters(id,name,api_server) VALUES('repo-cluster','Cluster','https://cluster.invalid')`,
		`INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('repo-source','repo-workspace','Report','https://gitlab.invalid/analytics/report.git'),('foreign-source','repo-other','Other','https://gitlab.invalid/analytics/other.git')`,
	} {
		if _, err := db.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateNamespaceBinding(ctx, "repo-workspace", "repo-cluster", "analytics", nil); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, EncryptionKey: []byte("0123456789abcdef0123456789abcdef")}
	sha := strings.Repeat("a", 40)
	writes := 0
	proposalCreated := false
	s.PublishingHTTP = &http.Client{Transport: publishingTransport(func(r *http.Request) (*http.Response, error) {
		body := "{}"
		status := 200
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/repository/tree"):
			body = `[{"path":".gitlab-ci.yml","type":"blob","mode":"100644"}]`
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/repository/branches/justcd/"):
			status = 404
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/repository/branches/justcd%2F"):
			status = 404
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/repository/branches/justcd"):
			status = 404
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/repository/branches/"):
			body = `{"commit":{"id":"` + sha + `"}}`
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/merge_requests"):
			body = `[]`
			if proposalCreated {
				body = `[{"iid":1,"web_url":"https://gitlab.invalid/analytics/report/-/merge_requests/1"}]`
			}
		case r.Method == "GET":
			body = `{"id":1,"http_url_to_repo":"https://gitlab.invalid/analytics/report.git","default_branch":"main"}`
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/repository/commits"):
			writes++
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/merge_requests"):
			writes++
			status = 201
			proposalCreated = true
			body = `{"iid":1,"web_url":"https://gitlab.invalid/analytics/report/-/merge_requests/1"}`
		default:
			t.Fatalf("changed existing settings or CI secrets: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	settings := publishing.ProfileSettings{TemplateID: "shiny", GitLabURL: "https://gitlab.invalid", Group: "analytics", ClusterID: "repo-cluster", Namespace: "analytics", BaseImage: "registry.invalid/shiny:1", BuilderImage: "registry.invalid/builder:1", RegistryPrefix: "registry.invalid/apps", SecretMode: "variables", SyncPolicy: "auto-safe"}
	created := profileRequest(t, s.savePublishingProfile, "POST", "/", "repo-owner", map[string]any{"name": "Approved", "enabled": true, "settings": settings, "secrets": publishingSecrets{GitLabToken: "repo-api-token", GitReadToken: "repo-read-token", RegistryUser: "user", RegistryPassword: "repo-registry-token"}}, "repo-workspace", "")
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var profile store.PublishingProfile
	if err := json.Unmarshal(created.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	input := repositoryPublishingInput{WorkspaceID: "repo-workspace", SourceID: "repo-source", ProfileID: profile.ID, Name: "report", Revision: "main", AppDir: ".", Image: "registry.invalid/apps/report@sha256:" + strings.Repeat("b", 64)}
	request := func(handler http.HandlerFunc, user string, in repositoryPublishingInput) *httptest.ResponseRecorder {
		return profileRequest(t, handler, "POST", "/", user, in, "", "")
	}
	for _, user := range []string{"repo-viewer", "repo-other"} {
		if w := request(s.inspectPublishingRepository, user, input); w.Code != 403 {
			t.Fatal("unauthorized inspection", user, w.Code)
		}
	}
	bad := input
	bad.SourceID = "foreign-source"
	if w := request(s.inspectPublishingRepository, "repo-analyst", bad); w.Code != 404 {
		t.Fatal("foreign source accepted", w.Code)
	}
	bad = input
	bad.Config.BaseImage = "attacker.invalid/image"
	if w := request(s.planPublishingRepository, "repo-analyst", bad); w.Code != 400 {
		t.Fatal("profile override accepted", w.Code)
	}
	bad = input
	bad.Image = "other.invalid/report@sha256:" + strings.Repeat("b", 64)
	if w := request(s.planPublishingRepository, "repo-analyst", bad); w.Code != 400 {
		t.Fatal("profile registry boundary bypassed", w.Code)
	}
	bad = input
	bad.ProfileID = ""
	bad.GitLabToken = "manual-api-token"
	if w := request(s.inspectPublishingRepository, "repo-analyst", bad); w.Code != 403 {
		t.Fatal("analyst used manual credentials", w.Code)
	}
	inspected := request(s.inspectPublishingRepository, "repo-analyst", input)
	if inspected.Code != 200 || !strings.Contains(inspected.Body.String(), `"hasPipeline":true`) {
		t.Fatal(inspected.Code, inspected.Body.String())
	}
	planned := request(s.planPublishingRepository, "repo-analyst", input)
	if planned.Code != 200 {
		t.Fatal(planned.Code, planned.Body.String())
	}
	var plan publishing.RepositoryPlan
	if err := json.Unmarshal(planned.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if writes != 0 {
		t.Fatal("preview mutated repository")
	}
	input.PlanHash = plan.Hash
	sha = strings.Repeat("c", 40)
	if w := request(s.publishRepository, "repo-analyst", input); w.Code != 409 || writes != 0 {
		t.Fatal("stale plan accepted", w.Code, writes)
	}
	sha = strings.Repeat("a", 40)
	published := request(s.publishRepository, "repo-analyst", input)
	if published.Code != 201 {
		t.Fatal(published.Code, published.Body.String())
	}
	var result struct {
		ApplicationID string `json:"applicationId"`
	}
	json.Unmarshal(published.Body.Bytes(), &result)
	app, err := db.ApplicationByID(ctx, result.ApplicationID)
	if err != nil || app.SourceID != "repo-source" || app.ManifestPath != "deploy/report" || app.SyncPolicy != "manual" || app.Revision != "main" {
		t.Fatal("incorrect imported app", app, err)
	}
	if _, err := db.PublishedApplication(ctx, "repo-workspace", "https://gitlab.invalid/analytics/report.git"); err == nil {
		t.Fatal("adopted repo entered ZIP replacement workflow")
	}
	retry := request(s.publishRepository, "repo-analyst", input)
	if retry.Code != 201 {
		t.Fatal("retry failed", retry.Code, retry.Body.String())
	}
	apps, err := db.ListApplications(ctx, "repo-workspace")
	if err != nil || len(apps) != 1 {
		t.Fatal("retry duplicated application", len(apps), err)
	}
	if writes != 2 {
		t.Fatal("unexpected writes", writes)
	}
}
