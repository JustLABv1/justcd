//go:build integration

package store

import (
	"context"
	"database/sql"
	"io/fs"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// The runner supplies a disposable database. Keeping this opt-in prevents the
// ordinary unit suite from touching a developer's PostgreSQL instance.
func TestIntegrationMigrationUpgradeAndPoller(t *testing.T) {
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to a disposable PostgreSQL database")
	}
	for _, pending := range []int{1, 2, 3} {
		t.Run(strings.Repeat("prior-", pending)+"release", func(t *testing.T) {
			testIntegrationMigrationUpgradeAndPoller(t, dsn, pending)
		})
	}
}

func testIntegrationMigrationUpgradeAndPoller(t *testing.T, dsn string, pending int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "justcd_e2e_upgrade_" + NewID()[:8]
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	dsn = parsed.String()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	files, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil || len(files) < 2 {
		t.Fatalf("list migrations: %v (%d files)", err, len(files))
	}
	sort.Strings(files)
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		t.Fatal(err)
	}
	if len(files) <= pending {
		t.Fatalf("not enough migrations for %d prior versions", pending)
	}
	for _, filename := range files[:len(files)-pending] {
		script, err := migrationFiles.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(ctx, string(script)); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, strings.TrimSuffix(strings.TrimPrefix(filename, "migrations/"), ".sql"))
		}
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("apply prior migration %s: %v", filename, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	var workspacesRenamed bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema=current_schema() AND table_name='workspaces')`).Scan(&workspacesRenamed); err != nil {
		t.Fatal(err)
	}
	seed := func(query string) error {
		if workspacesRenamed {
			query = strings.ReplaceAll(query, "project_", "workspace_")
			query = strings.ReplaceAll(query, "projects", "workspaces")
		}
		_, err := db.ExecContext(ctx, query)
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,email,display_name) VALUES('integration-owner','owner@example.invalid','Integration Owner')`); err != nil {
		t.Fatal(err)
	}
	if err := seed(`INSERT INTO projects(id,name) VALUES('integration-workspace','Migration survivor')`); err != nil {
		t.Fatal(err)
	}
	if err := seed(`INSERT INTO project_memberships(project_id,user_id,role) VALUES('integration-workspace','integration-owner','owner')`); err != nil {
		t.Fatal(err)
	}
	if err := seed(`INSERT INTO credentials(id,project_id,name,kind,secret_cipher) VALUES
		('integration-workspace-credential','integration-workspace','Workspace kube','kubernetes-token',decode('00','hex')),
		('integration-git-credential','integration-workspace','Workspace Git','git-https',decode('01','hex'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO clusters(id,name,api_server) VALUES('integration-cluster','Test cluster','https://example.invalid')`); err != nil {
		t.Fatal(err)
	}
	if err := seed(`INSERT INTO project_cluster_credentials(project_id,cluster_id,credential_id) VALUES('integration-workspace','integration-cluster','integration-workspace-credential')`); err != nil {
		t.Fatal(err)
	}
	if err := seed(`INSERT INTO namespace_bindings(id,project_id,cluster_id,namespace,credential_id) VALUES('integration-binding','integration-workspace','integration-cluster','default','integration-workspace-credential')`); err != nil {
		t.Fatal(err)
	}
	if err := seed(`INSERT INTO git_sources(id,project_id,name,repository_url,credential_id) VALUES('integration-git','integration-workspace','Test source','https://example.invalid/repo.git','integration-git-credential')`); err != nil {
		t.Fatal(err)
	}
	if err := seed(`INSERT INTO applications(id,project_id,name,source_id,revision,manifest_path,renderer,cluster_id) VALUES('integration-app','integration-workspace','Due application','integration-git','main','.','yaml','integration-cluster')`); err != nil {
		t.Fatal(err)
	}
	s := &Store{DB: db}
	if pending == 1 {
		// Reproduce an installation whose ledger contains 032 but whose
		// schema came from the earlier version without the plan column.
		if _, err := db.ExecContext(ctx, `ALTER TABLE plans DROP COLUMN namespace_creations`); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("upgrade migration: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	if _, err := s.ListPlans(ctx, "integration-app", 20); err != nil {
		t.Fatalf("list plans after schema repair: %v", err)
	}

	clusterApps, err := s.ListApplications(ctx, "integration-workspace")
	if err != nil || len(clusterApps) != 1 {
		t.Fatalf("application cluster lookup: %+v %v", clusterApps, err)
	}
	cluster, err := s.ClusterByID(ctx, clusterApps[0].ClusterID)
	if err != nil || clusterApps[0].ClusterName != cluster.Name {
		t.Fatalf("application target name: %+v %v", clusterApps[0], err)
	}
	single, err := s.ApplicationByID(ctx, clusterApps[0].ID)
	if err != nil || single.ClusterName != cluster.Name {
		t.Fatalf("application detail target name: %+v %v", single, err)
	}
	var name string
	if err := db.QueryRowContext(ctx, `SELECT name FROM workspaces WHERE id='integration-workspace'`).Scan(&name); err != nil || name != "Migration survivor" {
		t.Fatalf("existing workspace lost during upgrade: %q, %v", name, err)
	}
	var role string
	if err := db.QueryRowContext(ctx, `SELECT role FROM workspace_memberships WHERE workspace_id='integration-workspace' AND user_id='integration-owner'`).Scan(&role); err != nil || role != "owner" {
		t.Fatalf("workspace membership lost during upgrade: %q, %v", role, err)
	}
	var workspaceID, sourceCredentialID, clusterCredentialID, namespace string
	if err := db.QueryRowContext(ctx, `SELECT workspace_id,credential_id FROM git_sources WHERE id='integration-git'`).Scan(&workspaceID, &sourceCredentialID); err != nil || workspaceID != "integration-workspace" || sourceCredentialID != "integration-git-credential" {
		t.Fatalf("Git source references changed during upgrade: %q, %q, %v", workspaceID, sourceCredentialID, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT workspace_id,credential_id FROM workspace_cluster_credentials WHERE cluster_id='integration-cluster'`).Scan(&workspaceID, &clusterCredentialID); err != nil || workspaceID != "integration-workspace" || clusterCredentialID != "integration-workspace-credential" {
		t.Fatalf("cluster credential references changed during upgrade: %q, %q, %v", workspaceID, clusterCredentialID, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT workspace_id,namespace FROM namespace_bindings WHERE id='integration-binding'`).Scan(&workspaceID, &namespace); err != nil || workspaceID != "integration-workspace" || namespace != "default" {
		t.Fatalf("namespace binding reference changed during upgrade: %q, %q, %v", workspaceID, namespace, err)
	}
	var appSourceID, appClusterID string
	if err := db.QueryRowContext(ctx, `SELECT workspace_id,source_id,cluster_id FROM applications WHERE id='integration-app'`).Scan(&workspaceID, &appSourceID, &appClusterID); err != nil || workspaceID != "integration-workspace" || appSourceID != "integration-git" || appClusterID != "integration-cluster" {
		t.Fatalf("application references changed during upgrade: %q, %q, %q, %v", workspaceID, appSourceID, appClusterID, err)
	}
	var legacyClusterWorkspaceID sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT workspace_id FROM clusters WHERE id='integration-cluster'`).Scan(&legacyClusterWorkspaceID); err != nil || legacyClusterWorkspaceID.Valid {
		t.Fatalf("legacy instance-owned cluster unexpectedly acquired a workspace: %+v, %v", legacyClusterWorkspaceID, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workspaces(id,name) VALUES('integration-consumer','Receiving workspace')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('integration-consumer','integration-owner','owner')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO credentials(id,workspace_id,name,kind,secret_cipher) VALUES('integration-consumer-git-credential','integration-consumer','Consumer Git','git-https',decode('02','hex'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO clusters(id,workspace_id,name,api_server) VALUES('integration-owned-cluster','integration-workspace','Workspace cluster','https://owned.example.invalid')`); err != nil {
		t.Fatal(err)
	}
	clusterShare := WorkspaceConnectionShare{ID: "integration-cluster-share", ResourceID: "integration-owned-cluster", OwnerWorkspaceID: "integration-workspace", TargetWorkspaceID: "integration-consumer"}
	if err := s.CreateWorkspaceClusterShare(ctx, clusterShare); err != nil {
		t.Fatalf("offer cluster share: %v", err)
	}
	gitShare := WorkspaceConnectionShare{ID: "integration-git-share", ResourceID: "integration-git", OwnerWorkspaceID: "integration-workspace", TargetWorkspaceID: "integration-consumer"}
	if err := s.CreateWorkspaceGitSourceShare(ctx, gitShare); err != nil {
		t.Fatalf("offer Git share: %v", err)
	}
	if canUse, err := s.WorkspaceCanUseCluster(ctx, "integration-consumer", "integration-owned-cluster"); err != nil || canUse {
		t.Fatalf("pending cluster offer should remain private: allowed=%v err=%v", canUse, err)
	}
	if canUse, err := s.WorkspaceCanUseCluster(ctx, "integration-consumer", "integration-cluster"); err != nil || !canUse {
		t.Fatalf("legacy instance-owned cluster should remain available: allowed=%v err=%v", canUse, err)
	}
	if canUse, err := s.WorkspaceCanUseGitSource(ctx, "integration-consumer", "integration-git"); err != nil || canUse {
		t.Fatalf("pending Git offer should remain private: allowed=%v err=%v", canUse, err)
	}
	if err := s.DecideWorkspaceConnectionShare(ctx, "integration-cluster-share", "integration-consumer", true); err != nil {
		t.Fatalf("accept cluster share: %v", err)
	}
	if err := s.DecideWorkspaceConnectionShare(ctx, "integration-git-share", "integration-consumer", true); err != nil {
		t.Fatalf("accept Git share: %v", err)
	}
	if canUse, err := s.WorkspaceCanUseCluster(ctx, "integration-consumer", "integration-owned-cluster"); err != nil || !canUse {
		t.Fatalf("accepted cluster share should grant access: allowed=%v err=%v", canUse, err)
	}
	sharedSource, err := s.GitSourceForWorkspace(ctx, "integration-git", "integration-consumer")
	if err != nil || sharedSource.CredentialID != nil {
		t.Fatalf("accepted share must not disclose owner credential: credential=%v err=%v", sharedSource.CredentialID, err)
	}
	consumerCredentialID := "integration-consumer-git-credential"
	if err := s.SetWorkspaceGitSourceShareCredential(ctx, "integration-git-share", "integration-consumer", &consumerCredentialID); err != nil {
		t.Fatalf("assign recipient-owned Git credential: %v", err)
	}
	sharedSource, err = s.GitSourceForWorkspace(ctx, "integration-git", "integration-consumer")
	if err != nil || sharedSource.CredentialID == nil || *sharedSource.CredentialID != consumerCredentialID {
		t.Fatalf("recipient Git source did not use its own credential: credential=%v err=%v", sharedSource.CredentialID, err)
	}
	if err := s.RevokeWorkspaceConnectionShare(ctx, "integration-git-share", "integration-workspace"); err != nil {
		t.Fatalf("revoke Git share: %v", err)
	}
	if canUse, err := s.WorkspaceCanUseGitSource(ctx, "integration-consumer", "integration-git"); err != nil || canUse {
		t.Fatalf("revoked Git share should remove access: allowed=%v err=%v", canUse, err)
	}
	if err := s.CreateWorkspaceGitSourceShare(ctx, WorkspaceConnectionShare{ID: "integration-git-share-again", ResourceID: "integration-git", OwnerWorkspaceID: "integration-workspace", TargetWorkspaceID: "integration-consumer"}); err != nil {
		t.Fatalf("re-offer after revocation: %v", err)
	}
	var applied int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil || applied != len(files) {
		t.Fatalf("migration ledger: got %d, want %d: %v", applied, len(files), err)
	}
	apps, err := s.DueApplications(ctx, 25)
	if err != nil {
		t.Fatalf("poller query failed after upgrade: %v", err)
	}
	if len(apps) != 1 || apps[0].ID != "integration-app" {
		t.Fatalf("unexpected due applications: %+v", apps)
	}
	if _, err := db.ExecContext(ctx, `UPDATE applications SET last_checked_at=NOW() WHERE id='integration-app'`); err != nil {
		t.Fatal(err)
	}
	apps, err = s.DueApplications(ctx, 25)
	if err != nil || len(apps) != 0 {
		t.Fatalf("recently checked application should not be due: %+v, %v", apps, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO git_push_triggers(application_id,provider,source_key,delivery_id,ref,reported_sha) VALUES('integration-app','github','source','delivery','refs/heads/main','abcdef')`); err != nil {
		t.Fatal(err)
	}
	apps, err = s.DueApplications(ctx, 25)
	if err != nil || len(apps) != 1 {
		t.Fatalf("Git push should make the application due: %+v, %v", apps, err)
	}
	connection := SourceControlConnection{ID: "integration-pr-connection", WorkspaceID: "integration-workspace", ApplicationID: "integration-app", Provider: "github", APIURL: "https://api.github.com", Repository: "example/app", StatusTokenCipher: []byte{2}}
	if err := s.SaveSourceControlConnection(ctx, connection); err != nil {
		t.Fatal(err)
	}
	storedConnection, err := s.SourceControlConnectionByApplication(ctx, "integration-app")
	if err != nil || !storedConnection.Enabled {
		t.Fatalf("migrated connection should default to enabled: %+v, %v", storedConnection, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO pull_request_reviews(id,connection_id,number,head_sha,source_url,event_at) VALUES('integration-review','integration-pr-connection',1,$1,'https://example.invalid/review/1',NOW())`, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE source_control_connections SET webhook_secret_cipher=decode('01','hex') WHERE id='integration-pr-connection'`); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveSourceControlWebhookSecret(ctx, "integration-app"); err != nil {
		t.Fatal(err)
	}
	storedConnection, err = s.SourceControlConnectionByApplication(ctx, "integration-app")
	if err != nil || len(storedConnection.WebhookSecretCipher) != 0 || !storedConnection.Enabled {
		t.Fatalf("removing webhook should preserve enabled PR reporting: %+v, %v", storedConnection, err)
	}
	if reviews, err := s.ListReviews(ctx, storedConnection.ID); err != nil || len(reviews) != 1 {
		t.Fatalf("removing webhook should preserve reviews: %+v, %v", reviews, err)
	}
	if err := s.SetSourceControlConnectionEnabled(ctx, "integration-app", false); err != nil {
		t.Fatal(err)
	}
	if reviews, err := s.DueReviews(ctx, 10); err != nil || len(reviews) != 0 {
		t.Fatalf("disabled connection should not schedule reviews: %+v, %v", reviews, err)
	}
	if err := s.SetSourceControlConnectionEnabled(ctx, "integration-app", true); err != nil {
		t.Fatal(err)
	}
	if reviews, err := s.DueReviews(ctx, 10); err != nil || len(reviews) != 1 {
		t.Fatalf("enabled connection should resume reviews: %+v, %v", reviews, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO preview_slots(connection_id,number) VALUES('integration-pr-connection',1)`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSourceControlConnectionEnabled(ctx, "integration-app", false); err != ErrActivePreviews {
		t.Fatalf("active preview should prevent disabling: %v", err)
	}
	if err := s.DeleteSourceControlConnection(ctx, "integration-app"); err != ErrActivePreviews {
		t.Fatalf("active preview should prevent deletion: %v", err)
	}
	if err := s.ReleasePreviewSlot(ctx, "integration-pr-connection", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSourceControlConnection(ctx, "integration-app"); err != nil {
		t.Fatal(err)
	}
	var reviewCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pull_request_reviews WHERE id='integration-review'`).Scan(&reviewCount); err != nil || reviewCount != 0 {
		t.Fatalf("deleted connection should remove review history: count=%d err=%v", reviewCount, err)
	}
	testSyncPauseSafety(t, ctx, s)
	testRepositoryConfigurationSafety(t, ctx, s)
	testConnectionDeletionSafety(t, ctx, s)
}
