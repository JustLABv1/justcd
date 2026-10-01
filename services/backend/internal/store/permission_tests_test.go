package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// PostgreSQL report::text is returned as a string, rather than []byte.
type permissionReportDriver struct{}
type permissionReportConn struct{}
type permissionReportRows struct{ read bool }

func (permissionReportDriver) Open(string) (driver.Conn, error)  { return permissionReportConn{}, nil }
func (permissionReportConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (permissionReportConn) Close() error                        { return nil }
func (permissionReportConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (permissionReportConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &permissionReportRows{}, nil
}
func (*permissionReportRows) Columns() []string {
	return []string{"workspace_id", "cluster_id", "namespace", "report", "checked_at"}
}
func (*permissionReportRows) Close() error { return nil }
func (r *permissionReportRows) Next(values []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	copy(values, []driver.Value{"workspace", "cluster", "default", `{"status":"passed"}`, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
	return nil
}
func init() { sql.Register("permission-report-text", permissionReportDriver{}) }
func TestListKubernetesPermissionTestsScansPostgresText(t *testing.T) {
	db, err := sql.Open("permission-report-text", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	items, err := (&Store{DB: db}).ListKubernetesPermissionTests(context.Background(), "workspace", "cluster")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !json.Valid(items[0].Report) || string(items[0].Report) != `{"status":"passed"}` {
		t.Fatalf("unexpected reports: %#v", items)
	}
}
