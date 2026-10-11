//go:build integration

package store

import (
	"context"
	"database/sql"
	"io/fs"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestIntegrationConcurrentMigrations(t *testing.T) {
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
	schema := "justcd_e2e_concurrent_migrations_" + NewID()[:8]
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	type opened struct {
		store *Store
		err   error
	}
	start := make(chan struct{})
	results := make(chan opened, 2)
	for range 2 {
		go func() {
			<-start
			s, err := Open(ctx, parsed.String())
			results <- opened{s, err}
		}()
	}
	close(start)
	files, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent backend startup failed: %v", result.err)
		}
		defer result.store.DB.Close()
		var count int
		if err := result.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count != len(files) {
			t.Fatalf("incomplete or duplicated migration ledger: %d, %v", count, err)
		}
	}
}
