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

func TestIntegrationCustomPublishingTemplates(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO users(id,email) VALUES('ct-owner','owner@ct.invalid'),('ct-analyst','analyst@ct.invalid'),('ct-foreign','foreign@ct.invalid')`,
		`INSERT INTO workspaces(id,name) VALUES('ct-workspace','Custom templates'),('ct-other','Other')`,
		`INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('ct-workspace','ct-owner','owner'),('ct-workspace','ct-analyst','deployer'),('ct-other','ct-foreign','owner')`,
		`INSERT INTO clusters(id,name,api_server) VALUES('ct-cluster','Cluster','https://cluster.invalid')`,
	} {
		if _, err := db.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateNamespaceBinding(ctx, "ct-workspace", "ct-cluster", "analytics", nil); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, EncryptionKey: []byte("0123456789abcdef0123456789abcdef")}
	do := func(handler http.HandlerFunc, method, user, templateID, query string, body any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/?"+query, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.SetPathValue("workspaceID", "ct-workspace")
		r.SetPathValue("templateID", templateID)
		r = r.WithContext(context.WithValue(r.Context(), userContextKey, store.User{ID: user}))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	dash := map[string]any{
		"id": "dash-app", "name": "Dash", "kind": "server", "port": 8050, "entryPoint": "app.py",
		"layout": []map[string]any{{"message": "Dash needs app.py", "anyOf": [][]string{{"{entry}"}}}},
	}

	if w := do(s.createPublishingTemplate, "POST", "ct-analyst", "", "", dash); w.Code != 403 {
		t.Fatal("deployer created a template", w.Code)
	}
	if w := do(s.createPublishingTemplate, "POST", "ct-owner", "", "", map[string]any{"id": "shiny", "name": "x", "kind": "server"}); w.Code != 400 {
		t.Fatal("built-in id accepted", w.Code, w.Body.String())
	}
	if w := do(s.createPublishingTemplate, "POST", "ct-owner", "", "", map[string]any{"id": "bad-kind", "name": "x", "kind": "ruby"}); w.Code != 400 {
		t.Fatal("unknown kind accepted", w.Code)
	}
	if w := do(s.createPublishingTemplate, "POST", "ct-owner", "", "", dash); w.Code != 201 {
		t.Fatal("create template", w.Code, w.Body.String())
	}
	if w := do(s.createPublishingTemplate, "POST", "ct-owner", "", "", dash); w.Code != 409 {
		t.Fatal("duplicate id accepted", w.Code)
	}

	// Copy a built-in template; the files are those of the built-in one.
	if w := do(s.createPublishingTemplate, "POST", "ct-owner", "", "", map[string]any{"id": "shiny-team", "name": "Shiny team", "baseTemplate": "shiny"}); w.Code != 201 {
		t.Fatal("copy built-in", w.Code, w.Body.String())
	}
	copied, err := db.PublishingTemplateVersion(ctx, "ct-workspace", "shiny-team", 1)
	builtin, _ := publishing.BuiltinTemplateFiles("shiny")
	if err != nil || copied.Files[publishing.FileDockerfile] != builtin[publishing.FileDockerfile] || !strings.HasPrefix(copied.Label, "Copied from") {
		t.Fatal("copied files", err)
	}
	if tpl, _ := db.CustomPublishingTemplate(ctx, "ct-workspace", "shiny-team"); tpl.Runtime != "shiny" || tpl.BaseTemplate != "shiny" || tpl.EntryPoint != "." {
		t.Fatalf("copied definition %+v", tpl)
	}

	list := do(s.listPublishingTemplates, "GET", "ct-owner", "", "", nil)
	var listed struct {
		Items []struct{ Template publishing.Template }
	}
	if list.Code != 200 || json.Unmarshal(list.Body.Bytes(), &listed) != nil || len(listed.Items) != len(publishing.Templates())+2 {
		t.Fatal("list with custom templates", list.Code, list.Body.String())
	}
	if w := do(s.getPublishingTemplates, "GET", "ct-foreign", "", "workspaceId=ct-workspace", nil); w.Code != 403 {
		t.Fatal("foreign user listed workspace templates", w.Code)
	}
	if w := do(s.getPublishingTemplates, "GET", "ct-analyst", "", "workspaceId=ct-workspace", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "dash-app") {
		t.Fatal("deployer cannot see workspace templates", w.Code)
	}

	// Export and import round trip, with the ID overridden.
	exported := do(s.exportPublishingTemplate, "GET", "ct-owner", "dash-app", "", nil)
	if exported.Code != 200 || !strings.Contains(exported.Body.String(), "justcd.template/v1") {
		t.Fatal("export", exported.Code, exported.Body.String())
	}
	var doc map[string]any
	_ = json.Unmarshal(exported.Body.Bytes(), &doc)
	if w := do(s.importPublishingTemplate, "POST", "ct-owner", "", "id=dash-copy&name=Dash+copy", doc); w.Code != 201 {
		t.Fatal("import", w.Code, w.Body.String())
	}
	if w := do(s.importPublishingTemplate, "POST", "ct-owner", "", "id=dash-copy", doc); w.Code != 409 {
		t.Fatal("import over an existing id", w.Code)
	}
	if w := do(s.importPublishingTemplate, "POST", "ct-owner", "", "", map[string]any{"format": "other"}); w.Code != 400 {
		t.Fatal("import of a foreign format", w.Code)
	}
	tampered := map[string]any{"format": "justcd.template/v1", "template": doc["template"], "files": map[string]string{"Dockerfile": "FROM {{ .Nope }}"}}
	if w := do(s.importPublishingTemplate, "POST", "ct-owner", "", "id=dash-bad", tampered); w.Code != 400 {
		t.Fatal("import with broken files", w.Code, w.Body.String())
	}

	// Editing the definition.
	changed := map[string]any{"name": "Dash apps", "entryPoint": "app.py", "port": 9000}
	if w := do(s.updatePublishingTemplate, "PUT", "ct-owner", "shiny", "", changed); w.Code != 404 {
		t.Fatal("built-in template edited", w.Code)
	}
	if w := do(s.updatePublishingTemplate, "PUT", "ct-owner", "dash-app", "", changed); w.Code != 200 {
		t.Fatal("update", w.Code, w.Body.String())
	}
	if tpl, _ := db.CustomPublishingTemplate(ctx, "ct-workspace", "dash-app"); tpl.Port != 9000 || tpl.Name != "Dash apps" || tpl.Runtime != publishing.RuntimeServer || len(tpl.Layout) != 0 {
		t.Fatalf("updated definition %+v", tpl)
	}

	// A profile can use a custom template only with a pinned version, and
	// blocks deleting it.
	settings := publishing.ProfileSettings{TemplateID: "dash-app", GitLabURL: "https://gitlab.invalid", Group: "analytics", ClusterID: "ct-cluster", Namespace: "analytics", BaseImage: "registry.invalid/python:1", BuilderImage: "registry.invalid/builder:1", RegistryPrefix: "registry.invalid/apps", SecretMode: "variables", SyncPolicy: "auto-safe"}
	secrets := publishingSecrets{GitLabToken: "ct-api-token", GitReadToken: "ct-read-token", RegistryUser: "user", RegistryPassword: "ct-registry-token"}
	save := func(version int) *httptest.ResponseRecorder {
		withVersion := settings
		withVersion.TemplateVersion = version
		return profileRequest(t, s.savePublishingProfile, "POST", "/", "ct-owner", map[string]any{"name": "Dash profile", "enabled": true, "settings": withVersion, "secrets": secrets}, "ct-workspace", "")
	}
	if w := save(0); w.Code != 400 {
		t.Fatal("custom template without a version", w.Code, w.Body.String())
	}
	if w := save(1); w.Code != 201 {
		t.Fatal("profile with custom template", w.Code, w.Body.String())
	}
	if w := do(s.deletePublishingTemplate, "DELETE", "ct-owner", "dash-app", "", nil); w.Code != 409 {
		t.Fatal("template in use deleted", w.Code, w.Body.String())
	}
	if w := do(s.deletePublishingTemplate, "DELETE", "ct-owner", "shiny", "", nil); w.Code != 400 {
		t.Fatal("built-in deleted", w.Code)
	}
	if w := do(s.deletePublishingTemplate, "DELETE", "ct-owner", "dash-copy", "", nil); w.Code != 204 {
		t.Fatal("unused template not deleted", w.Code, w.Body.String())
	}
	if w := do(s.deletePublishingTemplate, "DELETE", "ct-owner", "dash-copy", "", nil); w.Code != 404 {
		t.Fatal("deleted twice", w.Code)
	}

	// Resolution applies the template's port and rules, and rejects a runtime mismatch.
	cfg := publishing.Config{TemplateID: "dash-app", Runtime: publishing.RuntimeServer, TemplateVersion: 1}
	if err := s.attachTemplate(ctx, "ct-workspace", &cfg); err != nil || cfg.Port != 9000 || cfg.TemplateFiles == nil {
		t.Fatal("attach custom template", err, cfg.Port)
	}
	cfg = publishing.Config{TemplateID: "dash-app", Runtime: "shiny", TemplateVersion: 1}
	if err := s.attachTemplate(ctx, "ct-workspace", &cfg); err == nil {
		t.Fatal("runtime mismatch accepted")
	}
	cfg = publishing.Config{TemplateID: "dash-app", Runtime: publishing.RuntimeServer, TemplateVersion: 1}
	if err := s.attachTemplate(ctx, "ct-other", &cfg); err == nil {
		t.Fatal("template resolved in another workspace")
	}
}
