//go:build integration

package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

type publishingTransport func(*http.Request) (*http.Response, error)

func (f publishingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func profileRequest(t *testing.T, handler http.HandlerFunc, method, path, user string, body any, workspace, id string) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("workspaceID", workspace)
	r.SetPathValue("profileID", id)
	r = r.WithContext(context.WithValue(r.Context(), userContextKey, store.User{ID: user}))
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}
func profileSource(t *testing.T, version string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, value := range map[string]string{"app.R": "# " + version, "packages.txt": "shiny\n", "build_html.py": "# static build", "build.py": "# custom static build"} {
		f, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		f.Write([]byte(value))
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestIntegrationPublishingProfilesLifecycleAndDelegation(t *testing.T) {
	for _, runtime := range []string{"shiny", "python-static"} {
		t.Run(runtime, func(t *testing.T) { testPublishingProfileLifecycle(t, runtime) })
	}
}
func testPublishingProfileLifecycle(t *testing.T, runtime string) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO users(id,email) VALUES('profile-owner','owner@profiles.invalid'),('profile-analyst','analyst@profiles.invalid'),('profile-viewer','viewer@profiles.invalid'),('profile-other','other@profiles.invalid')`,
		`INSERT INTO workspaces(id,name) VALUES('profiles','Profiles'),('other-profiles','Other')`,
		`INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('profiles','profile-owner','owner'),('profiles','profile-analyst','deployer'),('profiles','profile-viewer','viewer'),('other-profiles','profile-other','owner')`,
		`INSERT INTO clusters(id,name,api_server) VALUES('profile-cluster','Analytics cluster','https://cluster.invalid')`,
	} {
		if _, err := db.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateNamespaceBinding(ctx, "profiles", "profile-cluster", "analytics", nil); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, EncryptionKey: []byte("0123456789abcdef0123456789abcdef")}
	settings := publishing.ProfileSettings{TemplateID: "shiny", GitLabURL: "https://gitlab.invalid", Group: "analytics", ClusterID: "profile-cluster", Namespace: "analytics", BaseImage: "registry.invalid/base/shiny:1", BuilderImage: "registry.invalid/base/builder:1", RegistryPrefix: "registry.invalid/apps", HostSuffix: "apps.example.invalid", SecretMode: "variables", SyncPolicy: "auto-safe"}
	if runtime == "python-static" {
		settings.TemplateID = runtime
		settings.WebServerImage = "registry.invalid/base/nginx:1"
	}
	body := map[string]any{"name": "Analytics development", "description": "Approved internal analytics apps", "enabled": true, "settings": settings, "secrets": publishingSecrets{GitLabToken: "profile-api-secret", GitReadToken: "profile-read-secret", RegistryUser: "registry-user", RegistryPassword: "profile-registry-secret"}}
	save := func(method, user, id string, body any) *httptest.ResponseRecorder {
		return profileRequest(t, s.savePublishingProfile, method, "/api/v1/test", user, body, "profiles", id)
	}
	for _, user := range []string{"profile-analyst", "profile-viewer", "profile-other"} {
		if got := save("POST", user, "", body).Code; got != 403 {
			t.Fatalf("%s created a profile: %d", user, got)
		}
	}
	created := save("POST", "profile-owner", "", body)
	if created.Code != 201 {
		t.Fatalf("create profile: %d %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), "profile-api-secret") || strings.Contains(created.Body.String(), "credentialId") {
		t.Fatal("profile response exposed credentials")
	}
	var profile store.PublishingProfile
	if err := json.Unmarshal(created.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	profile, err := db.PublishingProfile(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{profile.CredentialID, profile.GitReadCredentialID} {
		c, e := db.CredentialByID(ctx, id)
		if e != nil || bytes.Contains(c.Cipher, []byte("profile-")) {
			t.Fatal("profile secret not encrypted", e)
		}
	}
	// Name collisions roll back both newly allocated credentials.
	if got := save("POST", "profile-owner", "", body).Code; got != 409 {
		t.Fatal("duplicate profile name accepted", got)
	}
	countCredentials := func() int {
		var n int
		if err := db.DB.QueryRowContext(ctx, `SELECT count(*) FROM credentials WHERE workspace_id='profiles'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if countCredentials() != 2 {
		t.Fatal("duplicate profile left orphan credentials")
	}
	approved := profileRequest(t, s.listApprovedPublishingProfiles, "GET", "/api/v1/publishing/profiles?workspaceId=profiles", "profile-analyst", nil, "", "")
	if approved.Code != 200 || !strings.Contains(approved.Body.String(), profile.ID) {
		t.Fatal("analyst cannot discover approved profile", approved.Body.String())
	}
	for _, private := range []string{"registryPrefix", "gitlabUrl", "baseImage", "credentialId", "profile-api-secret"} {
		if strings.Contains(approved.Body.String(), private) {
			t.Fatal("analyst profile list exposed infrastructure", private)
		}
	}
	if got := profileRequest(t, s.listPublishingProfiles, "GET", "/", "profile-analyst", nil, "profiles", "").Code; got != 403 {
		t.Fatal("analyst accessed profile administration", got)
	}
	if got := profileRequest(t, s.listApprovedPublishingProfiles, "GET", "/?workspaceId=profiles", "profile-viewer", nil, "", "").Code; got != 403 {
		t.Fatal("viewer accessed publishing destinations", got)
	}
	var calls, pipelines int
	var project publishing.Project
	var lastRegistryToken string
	s.PublishingHTTP = &http.Client{Transport: publishingTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var result any = map[string]any{}
		status := 200
		p := r.URL.Path
		switch {
		case r.Method == "GET" && strings.Contains(p, "/repository/tree"):
			result = []map[string]string{{"path": "source/app.R", "type": "blob"}, {"path": "source/packages.txt", "type": "blob"}, {"path": "Dockerfile", "type": "blob"}, {"path": ".gitlab-ci.yml", "type": "blob"}, {"path": ".dockerignore", "type": "blob"}, {"path": ".justcd/deployment.yaml", "type": "blob"}, {"path": ".justcd/README.md", "type": "blob"}, {"path": "deploy/application.yaml", "type": "blob"}}
		case r.Method == "GET" && strings.Contains(p, "/repository/branches/main"):
			if pipelines == 0 {
				status = 404
			} else {
				result = map[string]any{"commit": map[string]string{"id": "source-sha"}}
			}
		case r.Method == "POST" && strings.HasSuffix(p, "/repository/commits"):
			var commit struct {
				Actions []struct {
					Path string `json:"file_path"`
				}
			}
			if e := json.NewDecoder(r.Body).Decode(&commit); e != nil {
				t.Fatal(e)
			}
			for _, a := range commit.Actions {
				if pipelines > 0 && a.Path == "deploy/application.yaml" {
					t.Fatal("source upload overwrote running manifest")
				}
			}
		case r.Method == "GET" && strings.Contains(p, "/protected_branches/main"):
			status = 404
		case strings.Contains(p, "/variables"):
			var v publishing.Variable
			if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
				t.Fatal(e)
			}
			if v.Key == "REGISTRY_PASSWORD" {
				lastRegistryToken = v.Value
				if !v.Masked || !v.Protected {
					t.Fatal("registry token was not protected and masked")
				}
			}
		case r.Method == "POST" && strings.HasSuffix(p, "/pipeline"):
			pipelines++
			result = map[string]string{"web_url": "https://gitlab.invalid/analytics/report/-/pipelines/1"}
		case r.Method == "GET" && strings.HasPrefix(p, "/api/v4/groups/"):
			result = map[string]int{"id": 10}
		case r.Method == "POST" && p == "/api/v4/projects":
			var input struct {
				Description string `json:"description"`
			}
			json.NewDecoder(r.Body).Decode(&input)
			project = publishing.Project{ID: 101, Description: input.Description, RepositoryURL: "https://gitlab.invalid/analytics/report.git", WebURL: "https://gitlab.invalid/analytics/report"}
			result = project
		case r.Method == "PUT" && p == "/api/v4/projects/101":
			result = map[string]bool{"ci_push_repository_for_job_token_allowed": true}
		case r.Method == "GET" && strings.HasPrefix(p, "/api/v4/projects/"):
			if project.ID == 0 {
				status = 404
			} else {
				result = project
			}
		}
		b, _ := json.Marshal(result)
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(b)), Header: make(http.Header)}, nil
	})}
	publish := func(user string, metadata map[string]any, version string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(metadata)
		r := uploadRequest(t, string(data), profileSource(t, version), false)
		r = r.WithContext(context.WithValue(r.Context(), userContextKey, store.User{ID: user}))
		w := httptest.NewRecorder()
		s.publishApplication(w, r)
		return w
	}
	metadata := map[string]any{"workspaceId": "profiles", "profileId": profile.ID, "name": "report", "entryPoint": "."}
	if runtime == "python-static" {
		metadata["buildCommand"] = "python build.py"
		metadata["outputDir"] = "public"
	}
	bad := map[string]any{"workspaceId": "profiles", "profileId": profile.ID, "name": "report", "config": map[string]string{"baseImage": "attacker/image"}}
	if got := publish("profile-analyst", bad, "v1").Code; got != 400 {
		t.Fatal("profile infrastructure override accepted", got)
	}
	if got := publish("profile-viewer", metadata, "v1").Code; got != 403 {
		t.Fatal("viewer published", got)
	}
	if got := publish("profile-other", map[string]any{"workspaceId": "other-profiles", "profileId": profile.ID, "name": "report"}, "v1").Code; got != 404 {
		t.Fatal("cross-workspace profile use accepted", got)
	}
	if calls != 0 {
		t.Fatal("unauthorized requests contacted GitLab")
	}
	published := publish("profile-analyst", metadata, "v1")
	if published.Code != 201 {
		t.Fatalf("analyst publish: %d %s", published.Code, published.Body.String())
	}
	var response struct {
		ApplicationID  string `json:"applicationId"`
		ApplicationURL string `json:"applicationUrl"`
	}
	json.Unmarshal(published.Body.Bytes(), &response)
	if response.ApplicationURL != "https://report.apps.example.invalid" || lastRegistryToken != "profile-registry-secret" || pipelines != 1 {
		t.Fatal("profile settings did not reach release")
	}
	if strings.Contains(published.Body.String(), "baseImage") || strings.Contains(published.Body.String(), "profile-api-secret") {
		t.Fatal("publication response exposed protected configuration")
	}
	app, e := db.ApplicationByID(ctx, response.ApplicationID)
	if e != nil || app.SyncPolicy != "auto-safe" || app.Namespaces[0].Namespace != "analytics" {
		t.Fatal("profile destination/policy not enforced", e)
	}
	source, e := db.GitSourceByID(ctx, app.SourceID)
	if e != nil || source.CredentialID == nil || *source.CredentialID != profile.GitReadCredentialID || countCredentials() != 2 {
		t.Fatal("Git read credential not shared by reference")
	}
	if runtime == "python-static" {
		saved, err := db.PublishedApplication(ctx, "profiles", source.RepositoryURL)
		if err != nil {
			t.Fatal(err)
		}
		var cfg publishing.Config
		if err := json.Unmarshal(saved.Config, &cfg); err != nil || cfg.BuildCommand != "python build.py" || cfg.OutputDir != "public" || cfg.WebServerImage != settings.WebServerImage {
			t.Fatal("static build configuration not retained", cfg, err)
		}
	}
	// An in-use profile cannot change destinations/images or be deleted.
	changed := settings
	changed.BaseImage = "registry.invalid/changed:1"
	body["settings"], body["version"], body["secrets"] = changed, profile.Version, publishingSecrets{GitReadToken: "must-not-be-saved"}
	if got := save("PUT", "profile-owner", profile.ID, body).Code; got != 409 {
		t.Fatal("changed in-use infrastructure", got)
	}
	if got := profileRequest(t, s.deletePublishingProfile, "DELETE", "/", "profile-owner", nil, "profiles", profile.ID).Code; got != 409 {
		t.Fatal("deleted in-use profile", got)
	}
	// Rotation retains settings and propagates read access by credential reference.
	body["settings"], body["secrets"] = settings, publishingSecrets{GitLabToken: "rotated-api-secret", GitReadToken: "rotated-read-secret", RegistryPassword: "rotated-registry-secret"}
	rotated := save("PUT", "profile-owner", profile.ID, body)
	if rotated.Code != 200 {
		t.Fatal("rotation failed", rotated.Body.String())
	}
	c, _ := db.CredentialByID(ctx, profile.GitReadCredentialID)
	plain, e := security.Decrypt(s.EncryptionKey, c.Cipher, "credential:"+c.ID)
	if e != nil || !bytes.Contains(plain, []byte("rotated-read-secret")) {
		t.Fatal("read credential rotation failed", e)
	}
	if got := save("PUT", "profile-owner", profile.ID, body).Code; got != 409 {
		t.Fatal("stale profile edit accepted", got)
	}
	update := map[string]any{"workspaceId": "profiles", "profileId": profile.ID, "updateApplicationId": app.ID}
	if runtime == "python-static" {
		update["buildCommand"] = "python changed.py"
		if got := publish("profile-analyst", update, "v1").Code; got != 400 {
			t.Fatal("source update changed build command", got)
		}
		delete(update, "buildCommand")
	}
	retried := publish("profile-analyst", update, "v1")
	if retried.Code != 201 || pipelines != 2 || lastRegistryToken != "rotated-registry-secret" {
		t.Fatal("credential rotation did not rebuild unchanged source", retried.Body.String())
	}
	updated := publish("profile-analyst", update, "v2")
	if updated.Code != 201 || pipelines != 3 || lastRegistryToken != "rotated-registry-secret" {
		t.Fatalf("credential-free source update failed: %d %s", updated.Code, updated.Body.String())
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.SetPathValue("applicationID", app.ID)
	r = r.WithContext(context.WithValue(r.Context(), userContextKey, store.User{ID: "profile-analyst"}))
	w := httptest.NewRecorder()
	s.getPublication(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "profileId") || strings.Contains(w.Body.String(), "gitlabUrl") {
		t.Fatal("analyst update settings are not sanitized", w.Body.String())
	}
	profile, _ = db.PublishingProfile(ctx, profile.ID)
	body["version"], body["enabled"], body["secrets"] = profile.Version, false, publishingSecrets{}
	if got := save("PUT", "profile-owner", profile.ID, body).Code; got != 200 {
		t.Fatal("disable profile failed", got)
	}
	before := calls
	if got := publish("profile-analyst", update, "v3").Code; got != 403 || calls != before {
		t.Fatal("disabled profile still authorized builds", got)
	}
	// Unused profiles can be deleted together with their private credentials.
	body["name"], body["enabled"], body["secrets"] = "Unused profile", true, publishingSecrets{GitLabToken: "unused-api-secret", GitReadToken: "unused-read-secret", RegistryUser: "registry-user", RegistryPassword: "unused-registry-secret"}
	unused := save("POST", "profile-owner", "", body)
	if unused.Code != 201 {
		t.Fatal(unused.Body.String())
	}
	var extra store.PublishingProfile
	json.Unmarshal(unused.Body.Bytes(), &extra)
	if got := profileRequest(t, s.deletePublishingProfile, "DELETE", "/", "profile-owner", nil, "profiles", extra.ID).Code; got != 204 || countCredentials() != 2 {
		t.Fatal("unused profile credentials not cleaned", got)
	}
	if _, _, err := db.DeleteWorkspaceKeepingResources(ctx, "profiles", true); err != nil {
		t.Fatal("profile foreign keys blocked workspace cleanup", err)
	}

}
