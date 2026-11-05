//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/publishing"
)

func TestIntegrationPackageSourcesProfile(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL")
	}
	db := openWorkspaceAuthorizationDB(t, dsn)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO users(id,email) VALUES('pk-owner','owner@pk.invalid'),('pk-analyst','analyst@pk.invalid')`,
		`INSERT INTO workspaces(id,name) VALUES('pk-workspace','Packages')`,
		`INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('pk-workspace','pk-owner','owner'),('pk-workspace','pk-analyst','deployer')`,
		`INSERT INTO clusters(id,name,api_server) VALUES('pk-cluster','Cluster','https://cluster.invalid')`,
	} {
		if _, err := db.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateNamespaceBinding(ctx, "pk-workspace", "pk-cluster", "analytics", nil); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, EncryptionKey: []byte("0123456789abcdef0123456789abcdef")}
	sources := `[{"ecosystem":"pip","url":"https://art.example.com/artifactory/api/pypi/pypi-remote/simple","authenticated":true},{"ecosystem":"cran","url":"https://art.example.com/artifactory/api/cran/cran-remote","authenticated":true}]`
	settings := publishing.ProfileSettings{TemplateID: "streamlit", GitLabURL: "https://gitlab.invalid", Group: "analytics", ClusterID: "pk-cluster", Namespace: "analytics", BaseImage: "registry.invalid/python:1", BuilderImage: "registry.invalid/builder:1", RegistryPrefix: "registry.invalid/apps", SecretMode: "variables", SyncPolicy: "auto-safe", PackageSources: sources, PackagePolicy: "enforce"}
	secrets := publishingSecrets{GitLabToken: "pk-api-token", GitReadToken: "pk-read-token", RegistryUser: "user", RegistryPassword: "pk-registry-token"}
	version := 1
	save := func(method, id string, mutate func(*publishing.ProfileSettings, *publishingSecrets)) *httptest.ResponseRecorder {
		st, sec := settings, secrets
		mutate(&st, &sec)
		return profileRequest(t, s.savePublishingProfile, method, "/", "pk-owner", map[string]any{"name": "Packages profile", "enabled": true, "settings": st, "secrets": sec, "version": version}, "pk-workspace", id)
	}
	none := func(*publishing.ProfileSettings, *publishingSecrets) {}

	if w := save("POST", "", none); w.Code != 400 || !strings.Contains(w.Body.String(), "package credentials") {
		t.Fatal("authenticated sources without credentials", w.Code, w.Body.String())
	}
	if w := save("POST", "", func(st *publishing.ProfileSettings, _ *publishingSecrets) {
		st.PackageSources = `[{"ecosystem":"pip","url":"http://art.example.com/simple"}]`
	}); w.Code != 400 {
		t.Fatal("plain http source accepted", w.Code)
	}
	if w := save("POST", "", func(st *publishing.ProfileSettings, _ *publishingSecrets) { st.PackagePolicy = "maybe" }); w.Code != 400 {
		t.Fatal("unknown policy accepted", w.Code)
	}
	withCredentials := func(_ *publishing.ProfileSettings, sec *publishingSecrets) {
		sec.PackageUser, sec.PackageToken = "reader", "pk-package-token"
	}
	created := save("POST", "", withCredentials)
	var profile struct{ ID string }
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &profile) != nil {
		t.Fatal("create with credentials", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), "pk-package-token") || strings.Contains(created.Body.String(), "pk-registry-token") {
		t.Fatal("a credential was returned to the browser")
	}
	// Updating without re-entering the credentials keeps them.
	if w := save("PUT", profile.ID, none); w.Code != 200 {
		t.Fatal("update without re-entering credentials", w.Code, w.Body.String())
	}
	version++
	// Vault mode reads package credentials from Vault instead.
	if w := save("PUT", profile.ID, func(st *publishing.ProfileSettings, _ *publishingSecrets) {
		st.SecretMode, st.VaultAddress, st.VaultRole, st.VaultPath = "vault", "https://vault.invalid", "publisher", "secret/data/team/registry"
	}); w.Code != 200 {
		t.Fatal("vault mode", w.Code, w.Body.String())
	}

	// Deployers learn which ecosystems are mirrored and the policy, never the URLs.
	approved := profileRequest(t, s.listApprovedPublishingProfiles, "GET", "/?workspaceId=pk-workspace", "pk-analyst", nil, "", "")
	if approved.Code != 200 || !strings.Contains(approved.Body.String(), `"packageEcosystems":["pip","cran"]`) || !strings.Contains(approved.Body.String(), `"packagePolicy":"enforce"`) {
		t.Fatal("approved profile summary", approved.Code, approved.Body.String())
	}
	if strings.Contains(approved.Body.String(), "art.example.com") || strings.Contains(approved.Body.String(), "pk-package-token") {
		t.Fatal("package URLs or credentials leaked to deployers")
	}
}
