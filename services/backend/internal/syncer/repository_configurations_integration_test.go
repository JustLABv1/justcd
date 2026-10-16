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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestIntegrationRepositoryConfiguration(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "justcd_e2e_repository_" + store.NewID()[:8]
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`) })
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
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
	ownerID, workspaceID, clusterID, sourceID := store.NewID(), store.NewID(), store.NewID(), store.NewID()
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES($1,$2)`, ownerID, ownerID+"@example.invalid"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateWorkspace(ctx, store.Workspace{ID: workspaceID, Name: "Repository test"}, ownerID); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateCluster(ctx, store.Cluster{ID: clusterID, Name: "production", CAData: []byte{}, APIServer: "https://example.invalid"}); err != nil {
		t.Fatal(err)
	}
	repo := newIntegrationGitRepo(t, "shop")
	definition := `apiVersion: justcd.io/v1alpha1
kind: Application
metadata:
  name: shop
spec:
  source:
    renderer: yaml
    path: .
  destination:
    cluster: production
    namespace: shop
    createNamespaces: true
  syncPolicy: manual
  ignoreResources:
    - apiVersion: secrets.hashicorp.com/v1beta1
      kind: VaultStaticSecret
      reason: Vault managed
    - apiVersion: v1
      kind: Secret
      name: vault-secret
      reason: Vault managed
`
	repo.write(t, "yaml/justcd.yaml", definition)
	repo.commit(t, "add application definition")
	if err := db.CreateGitSource(ctx, store.GitSource{ID: sourceID, WorkspaceID: workspaceID, Name: "test", RepositoryURL: repo.url}); err != nil {
		t.Fatal(err)
	}
	repository := store.RepositoryConfiguration{ID: store.NewID(), WorkspaceID: workspaceID, SourceID: sourceID, Revision: "main", Enabled: true}
	if err := db.CreateRepositoryConfiguration(ctx, repository); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: db}
	if err := svc.ReconcileRepository(ctx, repository.ID); err == nil {
		t.Fatal("automatic namespace binding accepted without default credentials")
	}
	if _, err := db.NamespaceBinding(ctx, workspaceID, clusterID, "shop"); err == nil {
		t.Fatal("failed discovery left a namespace binding behind")
	}
	credentialID := store.NewID()
	if err := db.CreateCredential(ctx, store.Credential{ID: credentialID, WorkspaceID: &workspaceID, Name: "Workspace Kubernetes", Kind: "kubernetes-token", Cipher: []byte{0}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetWorkspaceClusterCredential(ctx, workspaceID, clusterID, &credentialID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	binding, err := db.NamespaceBinding(ctx, workspaceID, clusterID, "shop")
	if err != nil || binding.CredentialID != nil {
		t.Fatalf("automatic binding should inherit the workspace default: %+v %v", binding, err)
	}
	// Existing namespace overrides must survive subsequent discovery.
	if err := db.UpdateNamespaceBinding(ctx, workspaceID, clusterID, "shop", &credentialID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	binding, err = db.NamespaceBinding(ctx, workspaceID, clusterID, "shop")
	if err != nil || binding.CredentialID == nil || *binding.CredentialID != credentialID {
		t.Fatalf("existing namespace credential override changed: %+v %v", binding, err)
	}
	var auditCount int
	if err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE action='repository.configuration.reconciled' AND resource_id=$1`, repository.ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("discovery audit missing: %d %v", auditCount, err)
	}
	apps, err := db.ListApplications(ctx, workspaceID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("discovery: %+v %v", apps, err)
	}
	original := apps[0]
	rules, err := db.IgnoreRules(ctx, original.ID)
	if err != nil || len(rules) != 1 || !rules[0].ManagedByGit || rules[0].Identity.Namespace != "shop" || rules[0].Identity.ClusterID != original.ClusterID {
		t.Fatalf("Git resource exclusion mapping: %+v %v", rules, err)
	}
	selectors, err := db.IgnoreSelectors(ctx, original.ID)
	if err != nil || len(selectors) != 1 || !selectors[0].ManagedByGit || selectors[0].Kind != "VaultStaticSecret" {
		t.Fatalf("Git kind exclusion mapping: %+v %v", selectors, err)
	}

	if original.ManifestPath != "yaml" || original.Renderer != "yaml" || original.ConfigurationCommit == "" {
		t.Fatalf("unexpected application: %+v", original)
	}
	// An invalid committed definition must leave application configuration intact.
	repo.write(t, "yaml/justcd.yaml", definition+"  unknownSetting: true\n")
	repo.commit(t, "invalid definition")
	if err := svc.ReconcileRepository(ctx, repository.ID); err == nil {
		t.Fatal("invalid configuration accepted")
	}
	current, err := db.ApplicationByID(ctx, original.ID)
	if err != nil || current.ConfigurationCommit != original.ConfigurationCommit || current.ConfigurationMissing {
		t.Fatalf("last valid configuration changed: %+v %v", current, err)
	}
	state, err := db.RepositoryConfigurationByID(ctx, repository.ID)
	if err != nil || state.LastError == "" {
		t.Fatalf("discovery diagnostics missing: %+v %v", state, err)
	}
	// Workspace authorization remains enforced even for an otherwise valid file.
	repo.write(t, "yaml/justcd.yaml", definition)
	repo.write(t, "kustomize/justcd.yaml", `apiVersion: justcd.io/v1alpha1
kind: Application
metadata:
  name: forbidden
spec:
  source:
    renderer: kustomize
  destination:
    cluster: production
    namespace: forbidden
`)
	repo.commit(t, "unbound namespace")
	if err := svc.ReconcileRepository(ctx, repository.ID); err == nil {
		t.Fatal("unbound namespace accepted")
	}
	apps, err = db.ListApplications(ctx, workspaceID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("unauthorized snapshot partially applied: %+v %v", apps, err)
	}
	repo.write(t, "kustomize/justcd.yaml", "")
	if err := os.RemoveAll(filepath.Join(repo.work, "yaml")); err != nil {
		t.Fatal(err)
	}
	repo.commit(t, "remove definitions")
	// A planning failure still stops normal sync and records actionable diagnostics.
	if err := svc.ReconcileRepository(ctx, repository.ID); err == nil {
		t.Fatal("invalid Kubernetes credentials unexpectedly produced a deletion plan")
	}
	current, err = db.ApplicationByID(ctx, original.ID)
	if err != nil || !current.ConfigurationMissing || len(current.StatusIssues) == 0 {
		t.Fatalf("removed definition diagnostics: %+v %v", current, err)
	}
	object := &unstructured.Unstructured{}
	if err := object.UnmarshalJSON([]byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"shop","namespace":"shop","uid":"shop-uid","resourceVersion":"1"},"data":{"value":"kept until approval"}}`)); err != nil {
		t.Fatal(err)
	}
	object.SetLabels(map[string]string{"justcd.io/application-id": original.ID})
	resource, err := render.CanonicalLive(object, clusterID, original.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertManagedResource(ctx, original.ID, resource); err != nil {
		t.Fatal(err)
	}
	namespaceObject := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": "shop", "uid": "shop-ns-uid", "resourceVersion": "1", "labels": map[string]any{"justcd.io/application-id": original.ID}}}}
	namespaceResource, err := render.CanonicalLive(namespaceObject, clusterID, original.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertManagedResource(ctx, original.ID, namespaceResource); err != nil {
		t.Fatal(err)
	}
	clusterAPI := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("discovery mutated Kubernetes: %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch r.URL.Path {
		case "/version":
			response = map[string]string{"major": "1", "minor": "32", "gitVersion": "v1.32.0"}
		case "/api":
			response = map[string]any{"kind": "APIVersions", "apiVersion": "v1", "versions": []string{"v1"}}
		case "/apis":
			response = map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": []any{}}
		case "/api/v1":
			response = map[string]any{"kind": "APIResourceList", "groupVersion": "v1", "resources": []any{map[string]any{"name": "namespaces", "kind": "Namespace", "namespaced": false, "verbs": []string{"get", "list", "delete"}}, map[string]any{"name": "configmaps", "kind": "ConfigMap", "namespaced": true, "verbs": []string{"get", "list", "delete"}}}}
		case "/api/v1/namespaces/shop/configmaps/shop":
			response = object.Object
		case "/api/v1/namespaces/shop/configmaps":
			response = map[string]any{"apiVersion": "v1", "kind": "ConfigMapList", "items": []any{object.Object}}
		case "/api/v1/namespaces/shop":
			response = namespaceObject.Object
		case "/api/v1/namespaces":
			response = map[string]any{"apiVersion": "v1", "kind": "NamespaceList", "items": []any{namespaceObject.Object}}
		default:
			t.Errorf("unexpected Kubernetes request: %s", r.URL)
			w.WriteHeader(404)
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer clusterAPI.Close()
	key := []byte(strings.Repeat("k", 32))
	cipher, err := security.Encrypt(key, []byte(`{"token":"test"}`), "credential:"+credentialID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE credentials SET secret_cipher=$2 WHERE id=$1`, credentialID, cipher); err != nil {
		t.Fatal(err)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clusterAPI.Certificate().Raw})
	if _, err := db.DB.ExecContext(ctx, `UPDATE clusters SET api_server=$2,ca_data=$3,cluster_scope_credential_id=$4 WHERE id=$1`, clusterID, clusterAPI.URL, ca, credentialID); err != nil {
		t.Fatal(err)
	}
	svc.EncryptionKey = key
	if err := svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	current, err = db.ApplicationByID(ctx, original.ID)
	if err != nil || !current.ConfigurationMissing || !current.Decommissioning || current.Health != "deletion_pending" || len(current.StatusIssues) != 0 {
		t.Fatalf("removed definition behavior: %+v %v", current, err)
	}
	plans, err := db.ListPlans(ctx, original.ID, 10)
	if err != nil || len(plans) != 1 {
		t.Fatalf("deletion plans: %+v %v", plans, err)
	}
	deletion := plans[0]
	if !deletion.Plan.Decommission || !deletion.Plan.RequiresApproval || len(deletion.Plan.Changes) != 2 || deletion.Plan.Changes[0].Kind != core.Delete {
		t.Fatalf("unsafe removal plan: %+v", deletion.Plan)
	}
	if err := core.AuthorizeApply(deletion.Plan, nil, time.Now()); err == nil {
		t.Fatal("deletion authorized without approval")
	}
	if err := svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	plans, err = db.ListPlans(ctx, original.ID, 10)
	if err != nil || len(plans) != 1 || plans[0].ID != deletion.ID {
		t.Fatalf("repeated discovery replaced plan: %+v %v", plans, err)
	}
	retained, err := svc.SelectDecommissionNamespaces(ctx, current, deletion, ownerID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !retained.Plan.Selection.KeepNamespaces || len(retained.Plan.Changes) != 1 || len(retained.Plan.Ignored) != 1 || retained.Plan.Ignored[0].Identity.Kind != "Namespace" || retained.Plan.Digest == deletion.Plan.Digest {
		t.Fatalf("invalid retained namespace plan: %+v", retained.Plan)
	}
	if _, err := svc.SelectDecommissionNamespaces(ctx, current, deletion, ownerID, false); err == nil {
		t.Fatal("superseded plan was selectable")
	}
	rechecked, err := svc.RecheckPlan(ctx, current, retained)
	if err != nil || rechecked.Digest != retained.Plan.Digest {
		t.Fatalf("namespace retention lost at live recheck: %+v %v", rechecked, err)
	}
	if err := svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	plans, err = db.ListPlans(ctx, original.ID, 10)
	if err != nil || plans[0].ID != retained.ID || !plans[0].Plan.Selection.KeepNamespaces {
		t.Fatalf("discovery discarded namespace retention: %+v %v", plans, err)
	}
	deletion = retained
	testRetainedNamespaceCompletion(t, ctx, db, original, ownerID, namespaceResource)
	var queued int
	if err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM operations WHERE application_id=$1`, original.ID).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("discovery queued deletion: %d %v", queued, err)
	}

	repo.write(t, "yaml/justcd.yaml", definition)
	repo.commit(t, "restore definition")
	if err := svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	current, err = db.ApplicationByID(ctx, original.ID)
	if err != nil || current.ConfigurationMissing || current.Decommissioning {
		t.Fatalf("restored definition: %+v %v", current, err)
	}
	restoredPlan, err := db.PlanByID(ctx, deletion.ID)
	if err != nil || restoredPlan.Status != "stale" {
		t.Fatalf("restored definition retained deletion plan: %+v %v", restoredPlan, err)
	}
	if _, err := svc.RecheckPlan(ctx, current, deletion); err == nil {
		t.Fatal("cancelled deletion remained applicable")
	}
	// A system plan computed before restoration must not be saved afterwards.
	if _, _, err := db.SavePlanOrReuse(ctx, store.PlanRecord{ID: store.NewID(), Plan: deletion.Plan, Desired: []core.Resource{}, CreatedBy: systemActorID, ExpiresAt: time.Now().Add(time.Minute), Status: "current"}); err == nil {
		t.Fatal("stale discovery reintroduced deletion after restore")
	}
	// Two callers must never apply snapshots fetched concurrently.
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(7412,hashtext($1))`, repository.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileRepository(ctx, repository.ID); err == nil {
		t.Fatal("concurrent repository discovery accepted")
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock(7412,hashtext($1))`, repository.ID); err != nil {
		t.Fatal(err)
	}
	testScheduledApprovalExpiry(t, ctx, db, original, ownerID)
	testOperationWorkerDrain(t, ctx, db, original, ownerID)
}

func testRetainedNamespaceCompletion(t *testing.T, ctx context.Context, db *store.Store, original store.Application, ownerID string, namespace core.Resource) {
	t.Helper()
	app := original
	app.ID = store.NewID()
	app.Name = "Retained namespace completion"
	app.RepositoryConfigurationID = ""
	if err := db.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertManagedResource(ctx, app.ID, namespace); err != nil {
		t.Fatal(err)
	}
	plan := core.Plan{ApplicationID: app.ID, Decommission: true, Selection: core.PlanSelection{KeepNamespaces: true}, Changes: []core.Change{}, Ignored: []core.Change{{Kind: core.Delete, Identity: namespace.Identity, LiveUID: namespace.UID}}}
	if err := core.RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	record := store.PlanRecord{ID: store.NewID(), Plan: plan, Desired: []core.Resource{}, CreatedBy: ownerID, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Status: "current"}
	if err := db.SavePlan(ctx, record); err != nil {
		t.Fatal(err)
	}
	op := store.Operation{ID: store.NewID(), ApplicationID: app.ID, PlanID: &record.ID, ActorID: &ownerID, Status: "running", Message: "Decommissioned; namespace retained"}
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO operations(id,application_id,plan_id,actor_id,status) VALUES($1,$2,$3,$4,'running')`, op.ID, app.ID, record.ID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO operation_leases(application_id,operation_id,expires_at) VALUES($1,$2,NOW()+INTERVAL '1 minute')`, app.ID, op.ID); err != nil {
		t.Fatal(err)
	}
	before, err := db.ManagedResources(ctx, app.ID)
	if err != nil || len(before) != 1 {
		t.Fatal("namespace released before successful completion")
	}
	for range 2 {
		if err := db.CompleteDeployment(ctx, op, "", "decommissioned", "sync.succeeded", nil); err != nil {
			t.Fatal(err)
		}
	}
	remaining, err := db.ManagedResources(ctx, app.ID)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("retained namespace blocks app removal: %+v %v", remaining, err)
	}
	if _, err := db.DeleteApplicationKeepingResources(ctx, app.ID, true); err != nil {
		t.Fatalf("cannot finish removing decommissioned app: %v", err)
	}
}
