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
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,email,display_name) VALUES('integration-owner','owner@example.invalid','Integration Owner')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO projects(id,name) VALUES('integration-project','Migration survivor')`); err != nil {
		t.Fatal(err)
	}
	s := &Store{DB: db}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("upgrade migration: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	var name string
	if err := db.QueryRowContext(ctx, `SELECT name FROM projects WHERE id='integration-project'`).Scan(&name); err != nil || name != "Migration survivor" {
		t.Fatalf("existing project lost during upgrade: %q, %v", name, err)
	}
	var applied int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil || applied != len(files) {
		t.Fatalf("migration ledger: got %d, want %d: %v", applied, len(files), err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO clusters(id,name,api_server) VALUES('integration-cluster','Test cluster','https://example.invalid')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO git_sources(id,project_id,name,repository_url) VALUES('integration-git','integration-project','Test source','https://example.invalid/repo.git')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO applications(id,project_id,name,source_id,revision,manifest_path,renderer,cluster_id) VALUES('integration-app','integration-project','Due application','integration-git','main','.','yaml','integration-cluster')`); err != nil {
		t.Fatal(err)
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
}
