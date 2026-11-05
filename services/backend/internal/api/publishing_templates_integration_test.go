//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/publishing"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func templateRequest(t *testing.T, handler http.HandlerFunc, method, user string, body any, workspace, templateID, version string) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "/", bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("workspaceID", workspace)
	r.SetPathValue("templateID", templateID)
	r.SetPathValue("version", version)
	r = r.WithContext(context.WithValue(r.Context(), userContextKey, store.User{ID: user}))
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func TestIntegrationPublishingTemplateVersions(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO users(id,email) VALUES('tpl-owner','owner@tpl.invalid'),('tpl-analyst','analyst@tpl.invalid'),('tpl-foreign','foreign@tpl.invalid')`,
		`INSERT INTO workspaces(id,name) VALUES('tpl-workspace','Templates'),('tpl-other','Other')`,
		`INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('tpl-workspace','tpl-owner','owner'),('tpl-workspace','tpl-analyst','deployer'),('tpl-other','tpl-foreign','owner')`,
		`INSERT INTO clusters(id,name,api_server) VALUES('tpl-cluster','Cluster','https://cluster.invalid')`,
	} {
		if _, err := db.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateNamespaceBinding(ctx, "tpl-workspace", "tpl-cluster", "analytics", nil); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, EncryptionKey: []byte("0123456789abcdef0123456789abcdef")}

	if w := templateRequest(t, s.listPublishingTemplates, "GET", "tpl-analyst", nil, "tpl-workspace", "", ""); w.Code != 403 {
		t.Fatal("deployer listed templates", w.Code)
	}
	list := templateRequest(t, s.listPublishingTemplates, "GET", "tpl-owner", nil, "tpl-workspace", "", "")
	var listed struct{ Items []map[string]any }
	if list.Code != 200 || json.Unmarshal(list.Body.Bytes(), &listed) != nil || len(listed.Items) != len(publishing.Templates()) {
		t.Fatal("template list", list.Code, list.Body.String())
	}
	detail := templateRequest(t, s.getPublishingTemplate, "GET", "tpl-owner", nil, "tpl-workspace", "shiny", "")
	var shiny struct {
		Builtin      map[string]string
		Placeholders []publishing.Placeholder
	}
	if detail.Code != 200 || json.Unmarshal(detail.Body.Bytes(), &shiny) != nil || len(shiny.Builtin) != len(publishing.TemplateFilePaths) || len(shiny.Placeholders) == 0 {
		t.Fatal("template detail", detail.Code, detail.Body.String())
	}
	if w := templateRequest(t, s.getPublishingTemplate, "GET", "tpl-owner", nil, "tpl-workspace", "unknown", ""); w.Code != 404 {
		t.Fatal("unknown template", w.Code)
	}

	edited := map[string]string{}
	for k, v := range shiny.Builtin {
		edited[k] = v
	}
	edited[publishing.FileDeployment] = strings.Replace(edited[publishing.FileDeployment], "  replicas: 1\n", "  replicas: 3\n", 1)
	create := func(user, workspace string, files map[string]string, label string) *httptest.ResponseRecorder {
		return templateRequest(t, s.createPublishingTemplateVersion, "POST", user, map[string]any{"label": label, "files": files}, workspace, "shiny", "")
	}
	if w := create("tpl-analyst", "tpl-workspace", edited, "x"); w.Code != 403 {
		t.Fatal("deployer created a template version", w.Code)
	}
	if w := create("tpl-foreign", "tpl-workspace", edited, "x"); w.Code != 403 {
		t.Fatal("foreign owner created a template version", w.Code)
	}
	unsafe := map[string]string{}
	for k, v := range edited {
		unsafe[k] = v
	}
	unsafe[publishing.FileDeployment] = strings.Replace(unsafe[publishing.FileDeployment], "allowPrivilegeEscalation: false", "privileged: true", 1)
	if w := create("tpl-owner", "tpl-workspace", unsafe, "unsafe"); w.Code != 400 {
		t.Fatal("unsafe template accepted", w.Code, w.Body.String())
	}
	for i, label := range []string{"three replicas", "second"} {
		w := create("tpl-owner", "tpl-workspace", edited, label)
		var v store.PublishingTemplateVersion
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Version != i+1 || v.CreatedBy != "tpl-owner" {
			t.Fatal("create version", i+1, w.Code, w.Body.String())
		}
	}
	got := templateRequest(t, s.getPublishingTemplateVersion, "GET", "tpl-owner", nil, "tpl-workspace", "shiny", "1")
	var v1 store.PublishingTemplateVersion
	if got.Code != 200 || json.Unmarshal(got.Body.Bytes(), &v1) != nil || !strings.Contains(v1.Files[publishing.FileDeployment], "replicas: 3") {
		t.Fatal("read version", got.Code, got.Body.String())
	}
	if w := templateRequest(t, s.getPublishingTemplateVersion, "GET", "tpl-owner", nil, "tpl-other", "shiny", "1"); w.Code != 403 {
		t.Fatal("version readable through another workspace", w.Code)
	}
	if w := templateRequest(t, s.getPublishingTemplateVersion, "GET", "tpl-foreign", nil, "tpl-other", "shiny", "1"); w.Code != 404 {
		t.Fatal("versions leak across workspaces", w.Code)
	}
	builtin := templateRequest(t, s.getPublishingTemplateVersion, "GET", "tpl-owner", nil, "tpl-workspace", "shiny", "0")
	if builtin.Code != 200 || !strings.Contains(builtin.Body.String(), "replicas: 1") {
		t.Fatal("built-in version", builtin.Code)
	}

	preview := templateRequest(t, s.previewPublishingTemplate, "POST", "tpl-owner", map[string]any{"files": edited}, "tpl-workspace", "shiny", "")
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), "replicas: 3") {
		t.Fatal("preview", preview.Code, preview.Body.String())
	}
	if w := templateRequest(t, s.previewPublishingTemplate, "POST", "tpl-owner", map[string]any{"files": unsafe}, "tpl-workspace", "shiny", ""); w.Code != 400 {
		t.Fatal("unsafe preview accepted", w.Code)
	}

	settings := publishing.ProfileSettings{TemplateID: "shiny", GitLabURL: "https://gitlab.invalid", Group: "analytics", ClusterID: "tpl-cluster", Namespace: "analytics", BaseImage: "registry.invalid/shiny:1", BuilderImage: "registry.invalid/builder:1", RegistryPrefix: "registry.invalid/apps", SecretMode: "variables", SyncPolicy: "auto-safe"}
	secrets := publishingSecrets{GitLabToken: "tpl-api-token", GitReadToken: "tpl-read-token", RegistryUser: "user", RegistryPassword: "tpl-registry-token"}
	save := func(version int) *httptest.ResponseRecorder {
		withVersion := settings
		withVersion.TemplateVersion = version
		return profileRequest(t, s.savePublishingProfile, "POST", "/", "tpl-owner", map[string]any{"name": "Profile " + string(rune('A'+version%26)), "enabled": true, "settings": withVersion, "secrets": secrets}, "tpl-workspace", "")
	}
	if w := save(99); w.Code != 400 {
		t.Fatal("profile pinned to a missing template version", w.Code, w.Body.String())
	}
	if w := save(1); w.Code != 201 {
		t.Fatal("profile with template version", w.Code, w.Body.String())
	}

	pinned := settings
	pinned.TemplateVersion = 1
	cfg, err := pinned.Resolve("report", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.attachTemplate(ctx, "tpl-workspace", &cfg); err != nil {
		t.Fatal(err)
	}
	files, err := publishing.Generate(cfg, []publishing.File{{Path: "source/app.R", Content: "x", Encoding: "text"}, {Path: "source/packages.txt", Content: "shiny", Encoding: "text"}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f.Path == publishing.FileDeployment {
			found = strings.Contains(f.Content, "replicas: 3")
		}
	}
	if !found {
		t.Fatal("generated deployment does not use the pinned custom template")
	}
	other := cfg
	other.TemplateVersion = 5
	if err := s.attachTemplate(ctx, "tpl-workspace", &other); err == nil {
		t.Fatal("missing template version attached")
	}
	if err := s.attachTemplate(ctx, "tpl-other", &cfg); err == nil {
		t.Fatal("template version attached across workspaces")
	}
}
