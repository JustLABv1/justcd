//go:build integration

package syncer

import (
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestIntegrationRuntimeObservation(t *testing.T) {
	ctx, db := isolatedPlannerStore(t)
	app := store.Application{ID: store.NewID(), WorkspaceID: "workspace", Name: "runtime", SourceID: "source", Revision: "main", ManifestPath: ".", Renderer: "yaml", ClusterID: "cluster", SyncPolicy: "manual", PollSeconds: 86400}
	if err := db.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=TRUE,retry_terminal_reason='review_required',last_checked_at=NOW() WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AcquireResourceAction(ctx, app.ID, "owner", time.Minute); err != nil {
		t.Fatal(err)
	}
	due, err := db.DueRuntimeApplications(ctx, 25)
	if err != nil || len(due) != 1 || due[0].ID != app.ID {
		t.Fatalf("paused runtime application not due: %v %v", due, err)
	}
	svc := &Service{Store: db}
	// There are no credentials and the cluster is unreachable. An empty
	// inventory must still publish an observation without cluster access.
	if err := svc.RefreshApplicationHealth(ctx, app); err != nil {
		t.Fatal(err)
	}
	fresh, err := db.ApplicationByID(ctx, app.ID)
	if err != nil || fresh.HealthCondition.Reason != "ReconciliationSuspended" || fresh.HealthCondition.ObservedAt == nil {
		t.Fatalf("empty observation: %#v %v", fresh.HealthCondition, err)
	}
	observedAt := *fresh.HealthCondition.ObservedAt
	if err := svc.RefreshApplicationHealth(ctx, app); err != nil {
		t.Fatal(err)
	}
	fresh, err = db.ApplicationByID(ctx, app.ID)
	if err != nil || !fresh.HealthCondition.ObservedAt.Equal(observedAt) {
		t.Fatalf("duplicate observation was not coalesced: %#v %v", fresh.HealthCondition, err)
	}
	due, err = db.DueRuntimeApplications(ctx, 25)
	if err != nil || len(due) != 0 {
		t.Fatalf("fresh application should not be due: %v %v", due, err)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE applications SET health_condition_observed_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(7417,hashtext($1))`, app.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshApplicationHealth(ctx, app); err != nil {
		t.Fatal(err)
	}
	fresh, err = db.ApplicationByID(ctx, app.ID)
	if err != nil || time.Since(*fresh.HealthCondition.ObservedAt) < 30*time.Second {
		t.Fatalf("parallel observer bypassed lock: %#v %v", fresh.HealthCondition, err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock(7417,hashtext($1))`, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=FALSE WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshApplicationHealth(ctx, app); err != nil {
		t.Fatal(err)
	}
	fresh, err = db.ApplicationByID(ctx, app.ID)
	if err != nil || fresh.HealthCondition.Reason != "NoManagedWorkloads" {
		t.Fatalf("unmanaged application = %#v %v", fresh.HealthCondition, err)
	}
}
