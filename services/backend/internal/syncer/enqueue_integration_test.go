//go:build integration

package syncer

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// An isolated schema keeps this regression independent of workers and clusters.
// No credential exists: synchronous plan recalculation would reject this fixture,
// whereas enqueue must accept it and let the worker report validation failure.
func isolatedPlannerStore(t *testing.T) (context.Context, *store.Store) {
	t.Helper()
	dsn := os.Getenv("JUSTCD_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("set JUSTCD_E2E_DATABASE_URL to PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	schema := "enqueue_test_" + store.NewID()
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`) })
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
	t.Cleanup(func() { db.DB.Close() })
	for _, statement := range []string{
		`INSERT INTO users(id,email,display_name) VALUES('owner','enqueue@example.invalid','Owner')`,
		`INSERT INTO workspaces(id,name) VALUES('workspace','Enqueue regression')`,
		`INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('workspace','owner','owner')`,
		`INSERT INTO clusters(id,name,api_server) VALUES('cluster','Unavailable cluster','https://example.invalid')`,
		`INSERT INTO git_sources(id,workspace_id,name,repository_url) VALUES('source','workspace','Unavailable Git','https://example.invalid/repo')`,
	} {
		if _, err := db.DB.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	return ctx, db
}

func TestIntegrationEnqueueWithoutExternalValidation(t *testing.T) {
	ctx, db := isolatedPlannerStore(t)
	svc := &Service{Store: db}
	for _, required := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "approved"}[required], func(t *testing.T) {
			app := store.Application{ID: store.NewID(), WorkspaceID: "workspace", Name: store.NewID(), SourceID: "source", Revision: "main", ManifestPath: ".", Renderer: "yaml", ClusterID: "cluster", SyncPolicy: "manual", PollSeconds: 300}
			if err := db.CreateApplication(ctx, app); err != nil {
				t.Fatal(err)
			}
			plan := core.Plan{ApplicationID: app.ID, Revision: "reviewed", RequiresApproval: required, ApprovalKind: "sync", Changes: []core.Change{}, Bindings: []core.Binding{}}
			if required {
				plan.RequiredApprovals, plan.ApproverRoles = 1, []string{"owner"}
			}
			if err := core.RefreshDigest(&plan); err != nil {
				t.Fatal(err)
			}
			record := store.PlanRecord{ID: store.NewID(), Plan: plan, Desired: []core.Resource{}, CreatedBy: "owner", ExpiresAt: time.Now().Add(time.Hour), Status: "current"}
			if err := db.SavePlan(ctx, record); err != nil {
				t.Fatal(err)
			}
			var ids []string
			if required {
				if _, err := svc.ApplyWithApprovals(ctx, record.ID, "owner", nil); err == nil {
					t.Fatal("missing approval accepted")
				}
				id := store.NewID()
				approval := core.DeletionApproval{ActorID: "owner", PlanDigest: plan.Digest, ExpiresAt: record.ExpiresAt}
				if err := db.CreateApproval(ctx, id, record.ID, approval, record.ExpiresAt, "Reviewed"); err != nil {
					t.Fatal(err)
				}
				ids = []string{id}
			}
			// Expiry must reject promptly without trying the unavailable dependencies.
			if _, err := db.DB.ExecContext(ctx, `UPDATE plans SET expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, record.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ApplyWithApprovals(ctx, record.ID, "owner", ids); !errors.Is(err, store.ErrPlanNotCurrent) {
				t.Fatalf("expired plan accepted or revalidated externally: %v", err)
			}
			if _, err := db.DB.ExecContext(ctx, `UPDATE plans SET expires_at=$2 WHERE id=$1`, record.ID, record.ExpiresAt); err != nil {
				t.Fatal(err)
			}
			op, err := svc.ApplyWithApprovals(ctx, record.ID, "owner", ids)
			if err != nil || op.Status != "queued" {
				t.Fatalf("enqueue waited for external validation: %+v %v", op, err)
			}
			if _, err := svc.ApplyWithApprovals(ctx, record.ID, "owner", ids); err == nil {
				t.Fatal("duplicate sync accepted")
			}
			if _, err := db.DB.ExecContext(ctx, `UPDATE cluster_operation_gates SET next_operation_at=NOW() WHERE cluster_id='cluster'`); err != nil {
				t.Fatal(err)
			}
			claimed, ok, err := db.ClaimQueuedOperation(ctx, time.Minute)
			if err != nil || !ok || claimed.ID != op.ID {
				t.Fatalf("worker could not claim queued sync: %+v %v", claimed, err)
			}
			result, err := svc.executeQueuedOperation(ctx, claimed)
			if err == nil || result.Status != "failed" {
				t.Fatalf("worker did not stop unsafe validation: %+v %v", result, err)
			}
			persisted, err := db.OperationByID(ctx, op.ID)
			if err != nil || persisted.Status != "failed" || persisted.Message == "" {
				t.Fatalf("validation failure is not visible: %+v %v", persisted, err)
			}
		})
	}
}
