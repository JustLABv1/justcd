//go:build integration

package api

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// fakeGitLab keeps one repository in memory: enough of the GitLab API for
// publishing a project, reviewing generated files and committing them.
type fakeGitLab struct {
	t         *testing.T
	project   publishing.Project
	files     map[string]string
	commits   map[string]string
	sha       string
	count     int
	pipelines int
	variables map[string]publishing.Variable
}

func (g *fakeGitLab) client() *http.Client {
	return &http.Client{Transport: publishingTransport(func(r *http.Request) (*http.Response, error) {
		var result any = map[string]any{}
		status := 200
		p := r.URL.Path
		switch {
		case r.Method == "GET" && strings.Contains(p, "/repository/tree"):
			entries := []map[string]string{}
			for name, content := range g.files {
				hash := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(content), content)))
				entries = append(entries, map[string]string{"path": name, "type": "blob", "mode": "100644", "id": fmt.Sprintf("%x", hash)})
			}
			result = entries
		case r.Method == "GET" && strings.Contains(p, "/repository/files/"):
			name := strings.SplitN(p, "/repository/files/", 2)[1]
			content, exists := g.files[name]
			if !exists {
				status = 404
			} else {
				result = map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content)), "last_commit_id": g.commits[name]}
			}
		case r.Method == "GET" && strings.Contains(p, "/repository/branches/main"):
			if g.sha == "" {
				status = 404
			} else {
				result = map[string]any{"commit": map[string]string{"id": g.sha}}
			}
		case r.Method == "POST" && strings.HasSuffix(p, "/repository/commits"):
			var commit struct {
				Actions []struct {
					Path     string `json:"file_path"`
					Content  string `json:"content"`
					Encoding string `json:"encoding"`
					Action   string `json:"action"`
				}
			}
			if e := json.NewDecoder(r.Body).Decode(&commit); e != nil {
				g.t.Fatal(e)
			}
			previous := g.sha
			g.count++
			g.sha = fmt.Sprintf("%040x", g.count)
			for _, a := range commit.Actions {
				content := a.Content
				if a.Encoding == "base64" {
					decoded, err := base64.StdEncoding.DecodeString(content)
					if err != nil {
						g.t.Fatal(err)
					}
					content = string(decoded)
				}
				if a.Action == "delete" {
					delete(g.files, a.Path)
				} else {
					g.files[a.Path] = content
					g.commits[a.Path] = g.sha
				}
			}
			result = map[string]any{"parent_ids": []string{previous}}
		case r.Method == "GET" && strings.Contains(p, "/protected_branches/main"):
			status = 404
		case strings.Contains(p, "/variables"):
			var v publishing.Variable
			if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
				g.t.Fatal(e)
			}
			g.variables[v.Key] = v
		case r.Method == "POST" && strings.HasSuffix(p, "/pipeline"):
			g.pipelines++
			result = map[string]string{"web_url": "https://gitlab.invalid/analytics/report/-/pipelines/1"}
		case r.Method == "GET" && strings.HasPrefix(p, "/api/v4/groups/"):
			result = map[string]int{"id": 10}
		case r.Method == "POST" && p == "/api/v4/projects":
			var input struct {
				Description string `json:"description"`
			}
			json.NewDecoder(r.Body).Decode(&input)
			g.project = publishing.Project{DefaultBranch: "main", ID: 101, Description: input.Description, RepositoryURL: "https://gitlab.invalid/analytics/report.git", WebURL: "https://gitlab.invalid/analytics/report"}
			result = g.project
		case r.Method == "PUT" && p == "/api/v4/projects/101":
			result = map[string]bool{"ci_push_repository_for_job_token_allowed": true}
		case r.Method == "GET" && strings.HasPrefix(p, "/api/v4/projects/"):
			if g.project.ID == 0 {
				status = 404
			} else {
				result = g.project
			}
		}
		b, _ := json.Marshal(result)
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(b)), Header: make(http.Header)}, nil
	})}
}

func TestIntegrationManualApplicationBuildSetupAndProfileAdoption(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO users(id,email) VALUES('bs-owner','owner@bs.invalid'),('bs-analyst','analyst@bs.invalid')`,
		`INSERT INTO workspaces(id,name) VALUES('bs-workspace','Build setup')`,
		`INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('bs-workspace','bs-owner','owner'),('bs-workspace','bs-analyst','deployer')`,
		`INSERT INTO clusters(id,name,api_server) VALUES('bs-cluster','Cluster','https://cluster.invalid')`,
	} {
		if _, err := db.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateNamespaceBinding(ctx, "bs-workspace", "bs-cluster", "analytics", nil); err != nil {
		t.Fatal(err)
	}
	gitlab := &fakeGitLab{t: t, files: map[string]string{}, commits: map[string]string{}, variables: map[string]publishing.Variable{}}
	s := &Server{Store: db, EncryptionKey: []byte("0123456789abcdef0123456789abcdef"), PublishingHTTP: gitlab.client()}
	s.Config.PublishingTempDir = t.TempDir()

	// A manually published Shiny app.
	metadata, _ := json.Marshal(map[string]any{
		"workspaceId": "bs-workspace", "clusterId": "bs-cluster", "gitlabUrl": "https://gitlab.invalid", "group": "analytics",
		"gitlabToken": "bs-api-token-value", "gitReadToken": "bs-read-token-value", "registryUser": "registry-user", "registryPassword": "bs-registry-secret",
		"config": map[string]any{"name": "report", "runtime": "shiny", "templateId": "shiny", "entryPoint": ".", "baseImage": "registry.invalid/shiny:1", "builderImage": "registry.invalid/builder:1", "registryImage": "registry.invalid/apps/report", "namespace": "analytics", "secretMode": "variables"},
	})
	request := uploadRequest(t, string(metadata), profileSource(t, "v1"), false)
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, store.User{ID: "bs-owner"}))
	published := httptest.NewRecorder()
	s.publishApplication(published, request)
	var app struct {
		ApplicationID string `json:"applicationId"`
	}
	if published.Code != 201 || json.Unmarshal(published.Body.Bytes(), &app) != nil {
		t.Fatalf("manual publish: %d %s", published.Code, published.Body.String())
	}
	call := func(handler http.HandlerFunc, user string, body any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/", bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.SetPathValue("applicationID", app.ApplicationID)
		r = r.WithContext(context.WithValue(r.Context(), userContextKey, store.User{ID: user}))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	token := "bs-owner-gitlab-token"

	// Without changes the recipe is up to date: regenerating must be idempotent.
	same := call(s.previewBuildSetup, "bs-owner", map[string]any{"gitlabToken": token})
	var plan buildSetupPlan
	if same.Code != 200 || json.Unmarshal(same.Body.Bytes(), &plan) != nil || !plan.UpToDate {
		t.Fatalf("an unchanged setup must be up to date: %d %s", same.Code, same.Body.String())
	}
	if w := call(s.previewBuildSetup, "bs-analyst", map[string]any{"gitlabToken": token}); w.Code != 403 {
		t.Fatalf("a deployer changed the build setup: %d", w.Code)
	}

	sources := `[{"ecosystem":"pip","url":"https://art.example.com/artifactory/api/pypi/pypi-remote/simple","authenticated":true},{"ecosystem":"cran","url":"https://art.example.com/artifactory/api/cran/cran-remote","authenticated":true}]`
	change := map[string]any{"gitlabToken": token, "packageSources": sources, "packagePolicy": "enforce"}
	preview := call(s.previewBuildSetup, "bs-owner", change)
	plan = buildSetupPlan{}
	if preview.Code != 200 || json.Unmarshal(preview.Body.Bytes(), &plan) != nil || plan.UpToDate || plan.Hash == "" {
		t.Fatalf("preview: %d %s", preview.Code, preview.Body.String())
	}
	paths := map[string]bool{}
	for _, c := range plan.Changes {
		paths[c.Path] = c.Exists
		if strings.HasPrefix(c.Path, "source/") || strings.HasPrefix(c.Path, "deploy/") {
			t.Fatal("the build setup touched source or deployment files", c.Path)
		}
	}
	if exists, ok := paths[".justcd/package-sources.sh"]; !ok || exists {
		t.Fatalf("the package script must be added: %v", paths)
	}
	if _, ok := paths["Dockerfile"]; !ok {
		t.Fatalf("the Dockerfile must change: %v", paths)
	}
	if gitlab.pipelines != 1 || gitlab.files["Dockerfile"] == "" || strings.Contains(gitlab.files["Dockerfile"], "justcd_pkg") {
		t.Fatal("a preview must not change the repository")
	}

	apply := func(body map[string]any) *httptest.ResponseRecorder { return call(s.applyBuildSetup, "bs-owner", body) }
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{"hash": plan.Hash}
		for k, v := range change {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	if w := apply(with(map[string]any{"hash": "stale", "packageUser": "reader", "packageToken": "bs-package-token"})); w.Code != 409 {
		t.Fatalf("a stale review was applied: %d", w.Code)
	}
	if w := apply(with(nil)); w.Code != 400 || !strings.Contains(w.Body.String(), "package credentials") {
		t.Fatalf("authenticated sources without credentials: %d %s", w.Code, w.Body.String())
	}
	if w := apply(with(map[string]any{"packageUser": "reader", "packageToken": "bs-package-token"})); w.Code != 200 {
		t.Fatalf("apply: %d %s", w.Code, w.Body.String())
	}
	if token := gitlab.variables["JUSTCD_PKG_TOKEN"]; token.Value != "bs-package-token" || !token.Masked || !token.Protected {
		t.Fatalf("the package token must be a masked, protected variable: %+v", token)
	}
	if _, touched := gitlab.variables["REGISTRY_PASSWORD"]; touched && gitlab.variables["REGISTRY_PASSWORD"].Value != "bs-registry-secret" {
		t.Fatal("the registry credentials were overwritten")
	}
	if !strings.Contains(gitlab.files["Dockerfile"], "--mount=type=secret,id=justcd_pkg") || gitlab.files[".justcd/package-sources.sh"] == "" || !strings.Contains(gitlab.files[".gitlab-ci.yml"], "id=justcd_pkg") {
		t.Fatal("the generated files were not committed")
	}
	if gitlab.pipelines != 2 {
		t.Fatalf("applying must start the build: %d pipelines", gitlab.pipelines)
	}
	if strings.Contains(gitlab.files[".justcd/package-sources.sh"], "bs-package-token") {
		t.Fatal("the committed script contains the token")
	}
	var stored publishing.Config
	read, _ := db.PublishedApplication(ctx, "bs-workspace", "https://gitlab.invalid/analytics/report.git")
	if json.Unmarshal(read.Config, &stored) != nil || stored.PackageSources != sources || stored.PackagePolicy != "enforce" || strings.Contains(string(read.Config), "bs-package-token") {
		t.Fatalf("stored configuration: %s", read.Config)
	}
	if again := call(s.previewBuildSetup, "bs-owner", change); again.Code != 200 || !strings.Contains(again.Body.String(), `"upToDate":true`) {
		t.Fatalf("after applying, the same setup must be up to date: %s", again.Body.String())
	}

	// Template parameters change through the same reviewed update.
	params := map[string]any{"gitlabToken": token, "packageSources": sources, "packagePolicy": "enforce", "templateParameters": "renvRestore=missing"}
	paramPreview := call(s.previewBuildSetup, "bs-owner", params)
	var paramPlan buildSetupPlan
	if paramPreview.Code != 200 || json.Unmarshal(paramPreview.Body.Bytes(), &paramPlan) != nil || paramPlan.UpToDate || len(paramPlan.Images) != 1 || paramPlan.Images[0].Name != "Template parameters" {
		t.Fatalf("parameter preview: %d %s", paramPreview.Code, paramPreview.Body.String())
	}
	params["hash"], params["packageUser"], params["packageToken"] = paramPlan.Hash, "reader", "bs-package-token"
	if w := call(s.applyBuildSetup, "bs-owner", params); w.Code != 200 {
		t.Fatalf("apply parameters: %d %s", w.Code, w.Body.String())
	}
	if _, restored := gitlab.files["Dockerfile"]; !restored || strings.Contains(gitlab.files["Dockerfile"], "renv::restore(prompt=FALSE)") {
		t.Fatalf("the Dockerfile must install only absent packages now: %s", gitlab.files["Dockerfile"])
	}
	change["templateParameters"] = "renvRestore=missing"

	// Converting to a profile: first one that does not match the destination.
	secrets := publishingSecrets{GitLabToken: "bs-profile-api-token", GitReadToken: "bs-profile-read-token", RegistryUser: "registry-user", RegistryPassword: "bs-profile-registry-secret"}
	profileSettings := func(prefix string) publishing.ProfileSettings {
		return publishing.ProfileSettings{TemplateID: "shiny", GitLabURL: "https://gitlab.invalid", Group: "analytics", ClusterID: "bs-cluster", Namespace: "analytics", BaseImage: "registry.invalid/shiny:2", BuilderImage: "registry.invalid/builder:2", RegistryPrefix: prefix, SecretMode: "variables", SyncPolicy: "manual"}
	}
	createProfile := func(name, prefix string) string {
		w := profileRequest(t, s.savePublishingProfile, "POST", "/", "bs-owner", map[string]any{"name": name, "enabled": true, "settings": profileSettings(prefix), "secrets": secrets}, "bs-workspace", "")
		var created struct{ ID string }
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &created) != nil {
			t.Fatal("create profile", w.Code, w.Body.String())
		}
		return created.ID
	}
	mismatch := createProfile("Other registry", "registry.invalid/elsewhere")
	w := call(s.adoptPublishingProfile, "bs-owner", map[string]any{"profileId": mismatch})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"compatible":false`) || !strings.Contains(w.Body.String(), "registryImage") || !strings.Contains(w.Body.String(), `"applied":false`) {
		t.Fatalf("a profile with another registry must be reported incompatible: %s", w.Body.String())
	}
	matching := createProfile("Matching", "registry.invalid/apps")
	if w := call(s.adoptPublishingProfile, "bs-analyst", map[string]any{"profileId": matching}); w.Code != 403 {
		t.Fatalf("a deployer converted an application: %d", w.Code)
	}
	if w := call(s.adoptPublishingProfile, "bs-owner", map[string]any{"profileId": matching, "dryRun": true}); w.Code != 200 || !strings.Contains(w.Body.String(), `"compatible":true`) || !strings.Contains(w.Body.String(), `"applied":false`) {
		t.Fatalf("dry run: %d %s", w.Code, w.Body.String())
	}
	if read, _ := db.PublishedApplication(ctx, "bs-workspace", "https://gitlab.invalid/analytics/report.git"); read.ProfileID != nil {
		t.Fatal("a dry run converted the application")
	}
	if w := call(s.adoptPublishingProfile, "bs-owner", map[string]any{"profileId": matching}); w.Code != 200 || !strings.Contains(w.Body.String(), `"applied":true`) {
		t.Fatalf("convert: %d %s", w.Code, w.Body.String())
	}
	read, _ = db.PublishedApplication(ctx, "bs-workspace", "https://gitlab.invalid/analytics/report.git")
	if read.ProfileID == nil || *read.ProfileID != matching || read.ProfileRevision != 1 || !strings.HasPrefix(read.Fingerprint, "adopt:") {
		t.Fatalf("publication after conversion: %+v", read)
	}
	if loaded, _ := db.ApplicationByID(ctx, app.ApplicationID); loaded.Labels["justcd.io/publishing-profile"] != matching {
		t.Fatalf("the application must carry the profile label: %v", loaded.Labels)
	}
	if w := call(s.adoptPublishingProfile, "bs-owner", map[string]any{"profileId": matching}); w.Code != 409 {
		t.Fatalf("converting twice: %d", w.Code)
	}
	if w := call(s.previewBuildSetup, "bs-owner", change); w.Code != 409 {
		t.Fatalf("a profile-managed app must update through its profile: %d", w.Code)
	}
	// The normal reviewed profile update now offers the profile's build settings.
	upgrade := httptest.NewRequest("GET", "/", nil)
	upgrade.SetPathValue("applicationID", app.ApplicationID)
	upgrade = upgrade.WithContext(context.WithValue(upgrade.Context(), userContextKey, store.User{ID: "bs-owner"}))
	preview2 := httptest.NewRecorder()
	s.previewPublishingProfileUpgrade(preview2, upgrade)
	var upgradePlan profileUpgradePlan
	if preview2.Code != 200 || json.Unmarshal(preview2.Body.Bytes(), &upgradePlan) != nil || upgradePlan.UpToDate || !upgradePlan.Adopting || len(upgradePlan.Changes) == 0 || upgradePlan.FromRevision != upgradePlan.ToRevision {
		t.Fatalf("profile update after conversion: %d %s", preview2.Code, preview2.Body.String())
	}
	// A ZIP upload must wait until the profile's build settings are adopted.
	blocked, _ := json.Marshal(map[string]any{"workspaceId": "bs-workspace", "profileId": matching, "name": "report", "entryPoint": ".", "updateApplicationId": app.ApplicationID})
	blockedRequest := uploadRequest(t, string(blocked), profileSource(t, "v2"), false)
	blockedRequest = blockedRequest.WithContext(context.WithValue(blockedRequest.Context(), userContextKey, store.User{ID: "bs-owner"}))
	blockedResponse := httptest.NewRecorder()
	s.publishApplication(blockedResponse, blockedRequest)
	if blockedResponse.Code != 409 || !strings.Contains(blockedResponse.Body.String(), "review the profile update first") {
		t.Fatalf("upload before adopting the profile: %d %s", blockedResponse.Code, blockedResponse.Body.String())
	}
	// Applying the reviewed update completes the conversion.
	applyBody, _ := json.Marshal(map[string]any{"revision": upgradePlan.ToRevision, "hash": upgradePlan.Hash})
	applyRequest := httptest.NewRequest("POST", "/", bytes.NewReader(applyBody))
	applyRequest.Header.Set("Content-Type", "application/json")
	applyRequest.SetPathValue("applicationID", app.ApplicationID)
	applyRequest = applyRequest.WithContext(context.WithValue(applyRequest.Context(), userContextKey, store.User{ID: "bs-owner"}))
	applied := httptest.NewRecorder()
	s.applyPublishingProfileUpgrade(applied, applyRequest)
	if applied.Code != 200 {
		t.Fatalf("applying the profile update: %d %s", applied.Code, applied.Body.String())
	}
	read, _ = db.PublishedApplication(ctx, "bs-workspace", "https://gitlab.invalid/analytics/report.git")
	if strings.HasPrefix(read.Fingerprint, "adopt:") || !strings.Contains(gitlab.files["Dockerfile"], "registry.invalid/shiny:2") {
		t.Fatalf("the profile's build settings were not adopted: %s", read.Fingerprint)
	}
	// The profile now has no package sources, so the mirrors are gone from the recipe.
	if strings.Contains(gitlab.files["Dockerfile"], "justcd_pkg") {
		t.Fatal("the Dockerfile still mounts the package secret after adopting a profile without sources")
	}
}
