//go:build integration

package syncer

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationRepositoryPRBootstrapsOpenShiftProject(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable database required")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "justcd_bootstrap_" + store.NewID()[:8]
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
	parsed, _ := url.Parse(dsn)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := store.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var created atomic.Bool
	var requests atomic.Int32
	var clusterCalls atomic.Int32
	project := map[string]any{"apiVersion": "project.openshift.io/v1", "kind": "Project", "metadata": map[string]string{"name": "mimir-dev"}}
	clusterAPI := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clusterCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer workload-test-token" {
			t.Error("bootstrap did not use the workload account")
		}
		var response any
		switch r.URL.Path {
		case "/version":
			response = map[string]string{"major": "1", "minor": "32", "gitVersion": "v1.32.0"}
		case "/api":
			response = map[string]any{"kind": "APIVersions", "apiVersion": "v1", "versions": []string{"v1"}}
		case "/apis":
			version := map[string]string{"groupVersion": "project.openshift.io/v1", "version": "v1"}
			response = map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": []any{map[string]any{"name": "project.openshift.io", "versions": []any{version}, "preferredVersion": version}}}
		case "/api/v1":
			response = map[string]any{"kind": "APIResourceList", "groupVersion": "v1", "resources": []any{map[string]any{"name": "namespaces", "kind": "Namespace", "namespaced": false, "verbs": []string{"get", "create"}}}}
		case "/apis/project.openshift.io/v1":
			response = map[string]any{"kind": "APIResourceList", "groupVersion": "project.openshift.io/v1", "resources": []any{map[string]any{"name": "projects", "kind": "Project", "namespaced": false, "verbs": []string{"get"}}, map[string]any{"name": "projectrequests", "kind": "ProjectRequest", "namespaced": false, "verbs": []string{"create"}}}}
		case "/apis/project.openshift.io/v1/projects/mimir-dev":
			if !created.Load() {
				w.WriteHeader(http.StatusForbidden)
				response = map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "Forbidden", "message": "project not accessible yet", "code": 403}
			} else {
				response = project
			}
		case "/apis/project.openshift.io/v1/projectrequests":
			if r.Method != http.MethodPost {
				t.Error("project request was not a creation")
			}
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request["kind"] != "ProjectRequest" {
				t.Error("invalid project request", err)
			}
			created.Store(true)
			requests.Add(1)
			w.WriteHeader(http.StatusCreated)
			response = project
		default:
			t.Error("unexpected cluster request before workload planning", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			response = map[string]string{"error": "unexpected request"}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer clusterAPI.Close()
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO workspaces(id,name) VALUES('bootstrap-workspace','Bootstrap'); INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('bootstrap-source','bootstrap-workspace','Manifests','https://gitlab.com/org/manifests.git')`); err != nil {
		t.Fatal(err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	cipher, err := security.Encrypt(key, []byte(`{"token":"workload-test-token"}`), "credential:bootstrap-kube")
	if err != nil {
		t.Fatal(err)
	}
	workspace := "bootstrap-workspace"
	for _, credential := range []store.Credential{{ID: "bootstrap-kube", WorkspaceID: &workspace, Name: "Tenant", Kind: "kubernetes-token", Cipher: cipher}, {ID: "bootstrap-git", WorkspaceID: &workspace, Name: "Git", Kind: "git-https", Cipher: []byte{0}}} {
		if err := db.CreateCredential(ctx, credential); err != nil {
			t.Fatal(err)
		}
	}
	credentialID := "bootstrap-kube"
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clusterAPI.Certificate().Raw})
	if err := db.CreateCluster(ctx, store.Cluster{ID: "bootstrap-cluster", Name: "OpenShift", APIServer: clusterAPI.URL, CAData: ca, DefaultCredentialID: &credentialID}); err != nil {
		t.Fatal(err)
	}
	repo := store.RepositoryConfiguration{ID: "bootstrap-repo", WorkspaceID: workspace, SourceID: "bootstrap-source", Revision: "main", Enabled: true}
	if err := db.CreateRepositoryConfiguration(ctx, repo); err != nil {
		t.Fatal(err)
	}
	policy := store.RepositoryPRSettings{Enabled: true, Mode: "existing", CredentialID: "bootstrap-git", AllowNamespaceCreation: true, Profile: store.PreviewProfile{ConfirmShared: true}, Destinations: []store.PRDestination{{ClusterID: "bootstrap-cluster", NamespacePattern: ".*-dev"}}}
	if err := db.SaveRepositoryPRSettings(ctx, repo.ID, policy); err != nil {
		t.Fatal(err)
	}
	repo, err = db.RepositoryConfigurationByID(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	app := store.Application{WorkspaceID: workspace, SourceID: repo.SourceID, Name: "mimir-dev", Revision: strings.Repeat("a", 40), Renderer: "yaml", ManifestPath: "deploy", ClusterID: "bootstrap-cluster", Namespaces: []store.NamespaceBinding{{Namespace: "mimir-dev"}}, CreateNamespaces: true, SyncPolicy: "manual", PollSeconds: 86400, RetryPolicy: store.DefaultRetryPolicy()}
	connection := store.SourceControlConnection{ID: store.NewID(), Provider: "gitlab", APIURL: "https://gitlab.com/api/v4", Repository: "org/manifests", StatusCredentialID: &policy.CredentialID, PreviewProfile: store.PreviewProfile{Enabled: true, DeploymentMode: "existing", ConfirmShared: true}}
	entry, err := db.UpsertRepositoryPRApplication(ctx, repo, store.RepositoryPRApplication{RepositoryID: repo.ID, Number: 147, DefinitionName: app.Name, Mode: "existing"}, app, connection)
	if err != nil {
		t.Fatal(err)
	}
	service := Service{Store: db, EncryptionKey: key}
	for i := 0; i < 2; i++ {
		if err := service.BootstrapRepositoryPRNamespaces(ctx, *entry.ApplicationID); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 || !created.Load() {
		t.Fatalf("missing or duplicate project creation: %d", requests.Load())
	}
	// Manual and tracked-branch apps are not repository PR applications, even
	// when they use the same namespace and have createNamespaces enabled.
	for _, repositoryID := range []string{"", repo.ID} {
		ordinary := app
		ordinary.ID, ordinary.Name = store.NewID(), "ordinary-"+store.NewID()[:8]
		ordinary.RepositoryConfigurationID = repositoryID
		if err := db.CreateApplication(ctx, ordinary); err != nil {
			t.Fatal(err)
		}
		if repositoryID != "" {
			if _, err := db.DB.ExecContext(ctx, `UPDATE applications SET repository_configuration_id=$2 WHERE id=$1`, ordinary.ID, repositoryID); err != nil {
				t.Fatal(err)
			}
		}
		before := clusterCalls.Load()
		if err := service.BootstrapRepositoryPRNamespaces(ctx, ordinary.ID); err == nil || clusterCalls.Load() != before {
			t.Fatal("ordinary application invoked PR environment setup", err)
		}
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE repository_configurations SET enabled=FALSE WHERE id=$1`, repo.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.BootstrapRepositoryPRNamespaces(ctx, *entry.ApplicationID); err == nil || requests.Load() != 1 {
		t.Fatal("disabled discovery authorized bootstrap", err)
	}
}
