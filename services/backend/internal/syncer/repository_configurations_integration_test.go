//go:build integration

package syncer

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/store"
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
	if err := db.CreateNamespaceBinding(ctx, workspaceID, clusterID, "shop", nil); err != nil {
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
	if err := svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
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
	repo.write(t, "yaml/justcd.yaml", "")
	repo.commit(t, "remove definitions")
	if err := svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	current, err = db.ApplicationByID(ctx, original.ID)
	if err != nil || !current.ConfigurationMissing || current.Decommissioning {
		t.Fatalf("removed definition behavior: %+v %v", current, err)
	}
	repo.write(t, "yaml/justcd.yaml", definition)
	repo.commit(t, "restore definition")
	if err := svc.ReconcileRepository(ctx, repository.ID); err != nil {
		t.Fatal(err)
	}
	current, err = db.ApplicationByID(ctx, original.ID)
	if err != nil || current.ConfigurationMissing {
		t.Fatalf("restored definition: %+v %v", current, err)
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
}
