//go:build integration

package api

import (
	"archive/zip"
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
	for name, value := range map[string]string{"app.R": "# " + version, "packages.txt": "shiny\n", "build_html.py": "# static build", "build.py": "# custom static build", "index.html": "<h1>Static report</h1>", "app.py": "# streamlit", "requirements.txt": "streamlit"} {
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
	for _, runtime := range []string{"shiny", "streamlit", "python-static", "static", "per-application", "services", "managed-services"} {
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
	if runtime == "streamlit" {
		settings.TemplateID = "streamlit"
	}
	if runtime == "static" {
		settings.TemplateID = runtime
		settings.BaseImage = ""
		settings.WebServerImage = "registry.invalid/base/nginx:1"
	}
	if runtime == "python-static" {
		settings.TemplateID = runtime
		settings.WebServerImage = "registry.invalid/base/nginx:1"
	}
	if runtime == "services" {
		settings.ServiceCatalog = `{"database":{"name":"PostgreSQL","postgresVersion":"17","instances":1,"storage":"10Gi","databaseName":"app","credentialsSecret":"{app}-database","usernameKey":"username","passwordKey":"password","bootstrapSecret":"{app}-database","bootstrapKey":"init.sql","requiredVaultSecret":"database"},"vaultSecrets":[{"id":"database","name":"Database credentials","authRef":"platform/apps","mount":"secret","path":"apps/{app}/database","type":"kv-v2","refreshAfter":"1m","destination":"{app}-database"}]}`
	}
	if runtime == "managed-services" {
		settings.ServiceCatalog = `{"allowEnvironment":true,"database":{"name":"PostgreSQL","credentialMode":"generated","username":"app","allowConfigure":true,"allowEnvMapping":true,"allowCustomPassword":true,"postgresVersion":"17","instances":1,"storage":"10Gi","databaseName":"app","credentialsSecret":"{app}-database","usernameKey":"username","passwordKey":"password","bootstrapSecret":"{app}-database","bootstrapKey":"init.sql"}}`
	}
	expectedNamespace := "analytics"
	if runtime == "per-application" {
		settings.NamespaceMode = "per-application"
		settings.NamespacePrefix = "analytics-"
		settings.Namespace = ""
		expectedNamespace = "analytics-report"
		if _, err := db.DB.ExecContext(ctx, `INSERT INTO credentials(id,name,kind,secret_cipher) VALUES('namespace-default','Provisioner','kubernetes-token','encrypted'); UPDATE clusters SET default_credential_id='namespace-default' WHERE id='profile-cluster'`); err != nil {
			t.Fatal(err)
		}
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
		if err := db.DB.QueryRowContext(ctx, `SELECT count(*) FROM credentials WHERE workspace_id='profiles' AND kind!='publishing-database'`).Scan(&n); err != nil {
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
	var generatedDocker, generatedCI string
	remoteFiles := map[string]string{}
	fileCommits := map[string]string{}
	remoteSHA := ""
	commitCount := 0
	failNextPipeline := false
	s.PublishingHTTP = &http.Client{Transport: publishingTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var result any = map[string]any{}
		status := 200
		p := r.URL.Path
		switch {
		case r.Method == "GET" && strings.Contains(p, "/repository/tree"):
			entries := []map[string]string{}
			for name, content := range remoteFiles {
				hash := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(content), content)))
				entries = append(entries, map[string]string{"path": name, "type": "blob", "mode": "100644", "id": fmt.Sprintf("%x", hash)})
			}
			result = entries
		case r.Method == "GET" && strings.Contains(p, "/repository/files/"):
			name := strings.SplitN(p, "/repository/files/", 2)[1]
			content, exists := remoteFiles[name]
			if !exists {
				status = 404
			} else {
				result = map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content)), "last_commit_id": fileCommits[name]}
			}
		case r.Method == "GET" && strings.Contains(p, "/repository/branches/main"):
			if remoteSHA == "" {
				status = 404
			} else {
				result = map[string]any{"commit": map[string]string{"id": remoteSHA}}
			}
		case r.Method == "POST" && strings.HasSuffix(p, "/repository/commits"):
			var commit struct {
				CommitMessage string `json:"commit_message"`
				Actions       []struct {
					Path         string `json:"file_path"`
					Content      string `json:"content"`
					Encoding     string `json:"encoding"`
					Action       string `json:"action"`
					LastCommitID string `json:"last_commit_id"`
				}
			}
			if e := json.NewDecoder(r.Body).Decode(&commit); e != nil {
				t.Fatal(e)
			}
			previousSHA := remoteSHA
			commitCount++
			remoteSHA = fmt.Sprintf("%040x", commitCount)
			for _, a := range commit.Actions {
				if strings.HasPrefix(commit.CommitMessage, "JustCD profile upgrade") {
					if strings.HasPrefix(a.Path, "source/") || strings.HasPrefix(a.Path, "deploy/") {
						t.Fatal("upgrade changed source or deployment", a.Path)
					}
					if a.Action == "update" && a.LastCommitID != fileCommits[a.Path] {
						t.Fatal("upgrade omitted recipe concurrency guard")
					}
				}
				content := a.Content
				if a.Encoding == "base64" {
					decoded, err := base64.StdEncoding.DecodeString(content)
					if err != nil {
						t.Fatal(err)
					}
					content = string(decoded)
				}
				if a.Action == "delete" {
					delete(remoteFiles, a.Path)
					delete(fileCommits, a.Path)
				} else {
					remoteFiles[a.Path] = content
					fileCommits[a.Path] = remoteSHA
				}
			}
			result = map[string]any{"parent_ids": []string{previousSHA}}
			for _, a := range commit.Actions {
				if a.Path == "Dockerfile" {
					generatedDocker = a.Content
				}
				if a.Path == ".gitlab-ci.yml" {
					generatedCI = a.Content
				}
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
			if failNextPipeline {
				failNextPipeline = false
				status = 503
				break
			}
			pipelines++
			result = map[string]string{"web_url": "https://gitlab.invalid/analytics/report/-/pipelines/1"}
		case r.Method == "GET" && strings.HasPrefix(p, "/api/v4/groups/"):
			result = map[string]int{"id": 10}
		case r.Method == "POST" && p == "/api/v4/projects":
			var input struct {
				Description string `json:"description"`
			}
			json.NewDecoder(r.Body).Decode(&input)
			project = publishing.Project{DefaultBranch: "main", ID: 101, Description: input.Description, RepositoryURL: "https://gitlab.invalid/analytics/report.git", WebURL: "https://gitlab.invalid/analytics/report"}
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
	if runtime == "streamlit" {
		metadata["entryPoint"] = "app.py"
	}
	if runtime == "services" {
		metadata["services"] = publishing.ServiceSelection{Database: true}
	}
	if runtime == "managed-services" {
		metadata["services"] = publishing.ServiceSelection{Database: true, DatabaseConfig: &publishing.DatabaseInput{Name: "reports", Username: "report_user", EnvNames: map[string]string{"DB_HOST": "PGHOST"}}, Environment: []publishing.LiteralEnv{{Name: "APP_THEME", Value: "dark"}}}
		metadata["databasePassword"] = "integration-db-password-canary"
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
	if e != nil || app.SyncPolicy != "auto-safe" || app.Namespaces[0].Namespace != expectedNamespace || app.CreateNamespaces != (runtime == "per-application") {
		t.Fatal("profile destination/policy not enforced", e)
	}
	source, e := db.GitSourceByID(ctx, app.SourceID)
	if e != nil || source.CredentialID == nil || *source.CredentialID != profile.GitReadCredentialID || countCredentials() != 2 {
		t.Fatal("Git read credential not shared by reference")
	}
	if runtime == "managed-services" {
		saved, err := db.PublishedApplication(ctx, "profiles", source.RepositoryURL)
		if err != nil {
			t.Fatal(err)
		}
		var cfg publishing.Config
		json.Unmarshal(saved.Config, &cfg)
		d, err := publishing.ManagedDatabase(cfg)
		if err != nil || d == nil || d.DatabaseName != "reports" || d.Username != "report_user" || d.CredentialID == "" {
			t.Fatal("app database parameters not persisted", err)
		}
		credential, err := db.CredentialByID(ctx, d.CredentialID)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(credential.Cipher, []byte("integration-db-password-canary")) {
			t.Fatal("database password stored unencrypted")
		}
		plain, err := security.Decrypt(s.EncryptionKey, credential.Cipher, "credential:"+credential.ID)
		if err != nil {
			t.Fatal(err)
		}
		var values publishing.DatabaseCredentials
		json.Unmarshal(plain, &values)
		if values.Username != "report_user" || values.Password != "integration-db-password-canary" {
			t.Fatal("wrong protected database values")
		}
		for _, check := range []struct {
			handler http.HandlerFunc
			method  string
			status  int
		}{{s.updateCredential, "PUT", 400}, {s.deleteCredential, "DELETE", 409}} {
			request := httptest.NewRequest(check.method, "/", strings.NewReader(`{"name":"changed"}`))
			request.SetPathValue("credentialID", credential.ID)
			request = request.WithContext(context.WithValue(request.Context(), userContextKey, store.User{ID: "profile-owner"}))
			result := httptest.NewRecorder()
			check.handler(result, request)
			if result.Code != check.status {
				t.Fatal("generic credential settings bypass database lifecycle", result.Code)
			}
		}
		metadata["databasePassword"] = "different-password-canary"
		beforePipelines := pipelines
		if retry := publish("profile-analyst", metadata, "v1"); retry.Code != 409 || pipelines != beforePipelines {
			t.Fatal("retry changed existing database password", retry.Code)
		}
		metadata["databasePassword"] = "integration-db-password-canary"
		for _, content := range remoteFiles {
			if strings.Contains(content, values.Password) {
				t.Fatal("database password entered Git")
			}
		}
		if strings.Contains(published.Body.String(), values.Password) || strings.Contains(string(saved.Config), values.Password) {
			t.Fatal("database password entered response/config")
		}
	}
	if runtime == "services" {
		saved, err := db.PublishedApplication(ctx, "profiles", source.RepositoryURL)
		if err != nil {
			t.Fatal(err)
		}
		var cfg publishing.Config
		if err := json.Unmarshal(saved.Config, &cfg); err != nil || cfg.Services == "" {
			t.Fatal("selected services not persisted", err)
		}
		for _, want := range []string{"kind: SGCluster", "kind: SGScript", "kind: VaultStaticSecret", "DB_PASSWORD"} {
			if !strings.Contains(remoteFiles[".justcd/deployment.yaml"], want) {
				t.Fatalf("missing generated service %s", want)
			}
		}
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
	if runtime == "per-application" {
		if _, err := db.NamespaceBinding(ctx, "profiles", "profile-cluster", expectedNamespace); err != nil {
			t.Fatal("namespace binding not registered with app", err)
		}
		if _, err := db.NamespaceBinding(ctx, "profiles", "profile-cluster", "analytics-profile-check"); err == nil {
			t.Fatal("profile preview left a namespace binding")
		}
	}
	// An in-use profile cannot change destinations or be deleted.
	changed := settings
	changed.RegistryPrefix = "registry.invalid/other-apps"
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

	// Image corrections are editable without silently rebuilding existing applications.
	currentProfile, err := db.PublishingProfile(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentProfile.CurrentRevision != 1 {
		t.Fatal("credential rotation created a release")
	}
	nextImages := settings
	if runtime != "static" {
		nextImages.BaseImage = "registry.invalid/base/runtime:2"
	}
	nextImages.BuilderImage = "registry.invalid/base/builder:2"
	nextImages.GitImage = "registry.invalid/tools/git:2"
	nextImages.BuildEngine = "podman"
	nextImages.CAVariable = "PLATFORM_ROOT_CA"
	nextImages.RunnerTag = "approved-runner-v2"
	nextImages.PullSecret = "registry-pull-v2"
	if runtime == "static" || runtime == "python-static" {
		nextImages.WebServerImage = "registry.invalid/base/nginx:2"
	}
	body["settings"], body["version"], body["secrets"] = nextImages, currentProfile.Version, publishingSecrets{}
	imageEdit := save("PUT", "profile-owner", profile.ID, body)
	if imageEdit.Code != 200 || pipelines != 3 {
		t.Fatal("image correction failed or silently rebuilt apps", imageEdit.Code, imageEdit.Body.String(), pipelines)
	}
	if got := save("PUT", "profile-owner", profile.ID, body).Code; got != 409 {
		t.Fatal("stale image edit accepted", got)
	}
	rebuilt := publish("profile-analyst", update, "v2")
	if rebuilt.Code != 201 || pipelines != 4 {
		t.Fatal("pinned source upload failed", rebuilt.Code, rebuilt.Body.String(), pipelines)
	}
	savedPublication, err := db.PublishedApplication(ctx, "profiles", source.RepositoryURL)
	if err != nil {
		t.Fatal(err)
	}
	var savedConfig publishing.Config
	if err := json.Unmarshal(savedPublication.Config, &savedConfig); err != nil || savedConfig.BaseImage != settings.BaseImage || savedConfig.BuilderImage != settings.BuilderImage || savedConfig.WebServerImage != settings.WebServerImage || savedConfig.RunnerTag != settings.RunnerTag || savedConfig.PullSecret != settings.PullSecret {
		t.Fatal("source upload silently changed pinned images", savedConfig, err)
	}
	runtimeImage := settings.BaseImage
	if runtime == "static" || runtime == "python-static" {
		runtimeImage = settings.WebServerImage
	}
	if !strings.Contains(generatedDocker, runtimeImage) || !strings.Contains(generatedCI, settings.BuilderImage) {
		t.Fatal("pinned build recipe changed without an explicit upgrade")
	}
	if savedPublication.ProfileRevision != 1 {
		t.Fatal("source upload changed the profile pin")
	}
	historical, err := db.PublishingProfileVersion(ctx, profile.ID, 1)
	if err != nil || historical.Settings != settings {
		t.Fatal("published snapshot was mutated", err)
	}
	latest, err := db.PublishingProfile(ctx, profile.ID)
	if err != nil || latest.CurrentRevision != 2 {
		t.Fatal("image change did not publish v2", err)
	}
	apps := []store.Application{app}
	if err := db.PopulatePublishingProfileStatus(ctx, apps); err != nil || apps[0].PublishingProfile == nil || !apps[0].PublishingProfile.UpdateAvailable || apps[0].PublishingProfile.Revision != 1 || apps[0].PublishingProfile.LatestRevision != 2 {
		t.Fatal("update not advertised", apps, err)
	}
	upgradeRequest := func(handler http.HandlerFunc, user string, body any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		request := httptest.NewRequest("POST", "/", bytes.NewReader(data))
		request.SetPathValue("applicationID", app.ID)
		request = request.WithContext(context.WithValue(request.Context(), userContextKey, store.User{ID: user}))
		response := httptest.NewRecorder()
		handler(response, request)
		return response
	}
	for _, user := range []string{"profile-viewer", "profile-other"} {
		before := calls
		if w := upgradeRequest(s.previewPublishingProfileUpgrade, user, nil); w.Code != 403 || calls != before {
			t.Fatal("unauthorized upgrade preview", user, w.Code)
		}
	}
	beforeCommits := commitCount
	preview := upgradeRequest(s.previewPublishingProfileUpgrade, "profile-analyst", nil)
	if preview.Code != 200 {
		t.Fatal("upgrade preview failed", preview.Code, preview.Body.String())
	}
	var upgrade profileUpgradePlan
	if err := json.Unmarshal(preview.Body.Bytes(), &upgrade); err != nil {
		t.Fatal(err)
	}
	if upgrade.FromRevision != 1 || upgrade.ToRevision != 2 || len(upgrade.Changes) == 0 || commitCount != beforeCommits {
		t.Fatal("preview did not show immutable upgrade", upgrade)
	}
	originalSHA := remoteSHA
	remoteSHA = strings.Repeat("f", 40)
	stale := upgradeRequest(s.applyPublishingProfileUpgrade, "profile-analyst", map[string]any{"revision": 2, "hash": upgrade.Hash})
	if stale.Code != 409 || commitCount != beforeCommits {
		t.Fatal("stale source upgrade accepted", stale.Code)
	}
	remoteSHA = originalSHA
	if _, err := db.DB.ExecContext(ctx, `UPDATE publishing_profiles SET enabled=false WHERE id=$1`, profile.ID); err != nil {
		t.Fatal(err)
	}
	if w := upgradeRequest(s.applyPublishingProfileUpgrade, "profile-analyst", map[string]any{"revision": 2, "hash": upgrade.Hash}); w.Code != 403 {
		t.Fatal("disabled profile upgrade allowed", w.Code)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE publishing_profiles SET enabled=true WHERE id=$1`, profile.ID); err != nil {
		t.Fatal(err)
	}
	failNextPipeline = true
	attempted := upgradeRequest(s.applyPublishingProfileUpgrade, "profile-analyst", map[string]any{"revision": 2, "hash": upgrade.Hash})
	if attempted.Code != 502 {
		t.Fatal("expected retryable build start failure", attempted.Code, attempted.Body.String())
	}
	pending, _ := db.PublishedApplication(ctx, "profiles", source.RepositoryURL)
	if pending.ProfileRevision != 2 || pending.PipelineURL != "" {
		t.Fatal("pending revision was not recorded")
	}
	pendingApps := []store.Application{app}
	if err := db.PopulatePublishingProfileStatus(ctx, pendingApps); err != nil || !pendingApps[0].PublishingProfile.NeedsBuild {
		t.Fatal("pending build was not exposed", err)
	}
	retryPreview := upgradeRequest(s.previewPublishingProfileUpgrade, "profile-analyst", nil)
	if retryPreview.Code != 200 {
		t.Fatal(retryPreview.Body.String())
	}
	var retryPlan profileUpgradePlan
	json.Unmarshal(retryPreview.Body.Bytes(), &retryPlan)
	if !retryPlan.Resume {
		t.Fatal("upgrade could not resume build")
	}
	retriedUpgrade := upgradeRequest(s.applyPublishingProfileUpgrade, "profile-analyst", map[string]any{"revision": 2, "hash": retryPlan.Hash})
	if retriedUpgrade.Code != 200 || pipelines != 5 {
		t.Fatal("upgrade build retry failed", retriedUpgrade.Code, retriedUpgrade.Body.String(), pipelines)
	}
	pinnedAfterUpgrade, _ := db.PublishedApplication(ctx, "profiles", source.RepositoryURL)
	var upgradedConfig publishing.Config
	json.Unmarshal(pinnedAfterUpgrade.Config, &upgradedConfig)
	if (runtime == "services" || runtime == "managed-services") && upgradedConfig.Services == "" {
		t.Fatal("profile update lost service selection")
	}
	if pinnedAfterUpgrade.ProfileRevision != 2 || upgradedConfig.GitImage != nextImages.GitImage || upgradedConfig.BuildEngine != nextImages.BuildEngine || upgradedConfig.CAVariable != nextImages.CAVariable || upgradedConfig.BuilderImage != nextImages.BuilderImage || upgradedConfig.WebServerImage != nextImages.WebServerImage || upgradedConfig.BaseImage != nextImages.BaseImage || upgradedConfig.RunnerTag != nextImages.RunnerTag || upgradedConfig.PullSecret != nextImages.PullSecret {
		t.Fatal("explicit upgrade did not adopt images", upgradedConfig)
	}
	completedApps := []store.Application{app}
	if err := db.PopulatePublishingProfileStatus(ctx, completedApps); err != nil || completedApps[0].PublishingProfile.UpdateAvailable || completedApps[0].PublishingProfile.NeedsBuild {
		t.Fatal("completed upgrade still advertised", err)
	}
	if !strings.Contains(generatedCI, nextImages.BuilderImage) || !strings.Contains(generatedCI, nextImages.RunnerTag) || !strings.Contains(remoteFiles[".justcd/deployment.yaml"], nextImages.PullSecret) {
		t.Fatal("CI image not upgraded")
	}
	duplicate := upgradeRequest(s.applyPublishingProfileUpgrade, "profile-analyst", map[string]any{"revision": 2, "hash": retryPlan.Hash})
	if duplicate.Code != 200 || pipelines != 5 {
		t.Fatal("repeated upgrade started duplicate build", duplicate.Code, pipelines)
	}
	upgradedSource := publish("profile-analyst", update, "v3")
	if upgradedSource.Code != 201 || pipelines != 6 {
		t.Fatal("source update after upgrade failed", upgradedSource.Code, upgradedSource.Body.String())
	}
	// A recipe-only release is explicit; ordinary metadata edits do not release.
	recipeProfile, _ := db.PublishingProfile(ctx, profile.ID)
	body["version"], body["publishRevision"], body["settings"], body["secrets"] = recipeProfile.Version, true, nextImages, publishingSecrets{}
	recipeRelease := save("PUT", "profile-owner", profile.ID, body)
	if recipeRelease.Code != 200 {
		t.Fatal("recipe-only release failed", recipeRelease.Body.String())
	}
	released, _ := db.PublishingProfile(ctx, profile.ID)
	if released.CurrentRevision != 3 {
		t.Fatal("recipe release not published")
	}
	body["publishRevision"] = false
	history := profileRequest(t, s.listPublishingProfileVersions, "GET", "/", "profile-owner", nil, "profiles", profile.ID)
	if history.Code != 200 || strings.Contains(history.Body.String(), "rotated-api-secret") {
		t.Fatal("unsafe revision history", history.Code)
	}
	if w := profileRequest(t, s.listPublishingProfileVersions, "GET", "/", "profile-analyst", nil, "profiles", profile.ID); w.Code != 403 {
		t.Fatal("analyst browsed full profile history", w.Code)
	}
	beforeSelection := calls
	oldSelection := map[string]any{"workspaceId": "profiles", "profileId": profile.ID, "name": "another-report", "expectedProfileRevision": 2}
	if w := publish("profile-analyst", oldSelection, "v1"); w.Code != 409 || calls != beforeSelection {
		t.Fatal("stale initial profile selection accepted", w.Code)
	}
	settings = nextImages
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
	// Copying keeps credentials server-side and creates independent credential records.
	copyBody := map[string]any{"name": "Copied database profile", "settings": settings, "enabled": false, "copyFromProfileId": profile.ID}
	if got := profileRequest(t, s.savePublishingProfile, "POST", "/", "profile-other", copyBody, "other-profiles", "").Code; got != 404 {
		t.Fatal("cross-workspace copy accepted", got)
	}
	if got := save("POST", "profile-analyst", "", copyBody).Code; got != 403 {
		t.Fatal("deployer copied protected credentials", got)
	}
	copied := save("POST", "profile-owner", "", copyBody)
	if copied.Code != 201 {
		t.Fatal("copy failed", copied.Body.String())
	}
	var clone store.PublishingProfile
	json.Unmarshal(copied.Body.Bytes(), &clone)
	clone, err = db.PublishingProfile(ctx, clone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if clone.ID == profile.ID || clone.CredentialID == profile.CredentialID || clone.GitReadCredentialID == profile.GitReadCredentialID || clone.CurrentRevision != 1 || clone.Applications != 0 || clone.Enabled {
		t.Fatal("copy did not create an independent disabled profile")
	}
	sourceSecrets, err := s.loadPublishingSecrets(httptest.NewRequest("GET", "/", nil), profile)
	if err != nil {
		t.Fatal(err)
	}
	copySecrets, err := s.loadPublishingSecrets(httptest.NewRequest("GET", "/", nil), clone)
	if err != nil || copySecrets != sourceSecrets {
		t.Fatal("copied credentials differ", err)
	}
	if strings.Contains(copied.Body.String(), sourceSecrets.GitLabToken) {
		t.Fatal("copied credentials leaked")
	}
	if got := profileRequest(t, s.deletePublishingProfile, "DELETE", "/", "profile-owner", nil, "profiles", clone.ID).Code; got != 204 || countCredentials() != 2 {
		t.Fatal("copy cleanup affected original credentials", got)
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

func TestIntegrationGeneratedDatabaseRoleBootstrap(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	user := "app_" + store.NewID()[:12]
	plan := publishing.DatabasePlan{Name: "Test", PostgresVersion: "17", Instances: 1, Storage: "1Gi", DatabaseName: "reports", Username: user, CredentialMode: "generated", AllowCustomPassword: true, CredentialsSecret: "{app}-database", BootstrapSecret: "{app}-database", UsernameKey: "username", PasswordKey: "password", BootstrapKey: "init.sql"}
	values, err := publishing.NewDatabaseCredentials(plan, "quotes'and\\slashes-and-$delimiters$-canary")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 2; i++ {
		if _, err := tx.ExecContext(ctx, values.SQL); err != nil {
			t.Fatal("generated role bootstrap failed", err)
		}
	}
	var login, superuser, createDB, createRole, replication bool
	if err := tx.QueryRowContext(ctx, `SELECT rolcanlogin,rolsuper,rolcreatedb,rolcreaterole,rolreplication FROM pg_roles WHERE rolname=$1`, user).Scan(&login, &superuser, &createDB, &createRole, &replication); err != nil {
		t.Fatal(err)
	}
	if !login || superuser || createDB || createRole || replication {
		t.Fatal("generated role has wrong privileges")
	}
}
