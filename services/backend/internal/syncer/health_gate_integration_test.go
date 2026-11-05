//go:build integration

package syncer

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/apphealth"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func gateFixture(t *testing.T, ctx context.Context, db *store.Store, started, deadline time.Duration, status apphealth.Status) (store.Application, string) {
	t.Helper()
	app := store.Application{ID: store.NewID(), WorkspaceID: "workspace", Name: "gate-" + store.NewID()[:6], SourceID: "source", Revision: "main", ManifestPath: ".", Renderer: "yaml", ClusterID: "cluster", SyncPolicy: "manual", PollSeconds: 86400}
	if err := db.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	operationID := store.NewID()
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO health_gate_checks(operation_id,application_id,revision,status,started_at,deadline,on_failure) VALUES($1,$2,'rev','pending',$3,$4,'mark-failed')`,
		operationID, app.ID, time.Now().Add(-started), time.Now().Add(deadline)); err != nil {
		t.Fatal(err)
	}
	observed := time.Now()
	if err := db.RecordApplicationHealthCondition(ctx, app.ID, apphealth.ApplicationCondition{Status: status, Reason: "Test", Message: "pods are crash looping", ObservedAt: &observed, LastTransitionTime: observed}); err != nil {
		t.Fatal(err)
	}
	return app, operationID
}

func gateState(t *testing.T, ctx context.Context, db *store.Store, applicationID string) *store.HealthGateCheck {
	t.Helper()
	check, err := db.LatestHealthGateCheck(ctx, applicationID)
	if err != nil || check == nil {
		t.Fatalf("no health gate check: %v", err)
	}
	return check
}

func TestIntegrationHealthGateVerdicts(t *testing.T) {
	ctx, db := isolatedPlannerStore(t)
	svc := &Service{Store: db}
	logger := slog.Default()
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO users(id,email,display_name,is_admin,disabled) VALUES('justcd-system','system@justcd.invalid','JustCD automatic reconciler',FALSE,TRUE) ON CONFLICT(id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}

	// Healthy after the settle period passes the gate.
	app, op := gateFixture(t, ctx, db, time.Minute, 4*time.Minute, apphealth.Healthy)
	checks, err := db.PendingHealthGateChecks(ctx, 10)
	if err != nil || len(checks) != 1 {
		t.Fatalf("pending checks: %v %v", checks, err)
	}
	svc.evaluateHealthGate(ctx, checks[0], logger)
	if got := gateState(t, ctx, db, app.ID); got.Status != "passed" || got.OperationID != op {
		t.Fatalf("expected passed, got %+v", got)
	}

	// Unhealthy before the deadline keeps waiting.
	app, _ = gateFixture(t, ctx, db, time.Minute, 4*time.Minute, apphealth.Degraded)
	pending, _ := db.PendingHealthGateChecks(ctx, 10)
	for _, c := range pending {
		if c.ApplicationID == app.ID {
			svc.evaluateHealthGate(ctx, c, logger)
		}
	}
	if got := gateState(t, ctx, db, app.ID); got.Status != "pending" {
		t.Fatalf("expected pending, got %+v", got)
	}

	// A passing observation inside the settle window is not trusted yet.
	app, _ = gateFixture(t, ctx, db, 2*time.Second, 4*time.Minute, apphealth.Healthy)
	pending, _ = db.PendingHealthGateChecks(ctx, 10)
	for _, c := range pending {
		if c.ApplicationID == app.ID {
			svc.evaluateHealthGate(ctx, c, logger)
		}
	}
	if got := gateState(t, ctx, db, app.ID); got.Status != "pending" {
		t.Fatalf("settle window ignored: %+v", got)
	}

	// Unhealthy past the deadline fails the gate and raises a notification once.
	app, op = gateFixture(t, ctx, db, 5*time.Minute, -time.Second, apphealth.Degraded)
	pending, _ = db.PendingHealthGateChecks(ctx, 10)
	for _, c := range pending {
		if c.ApplicationID == app.ID {
			svc.evaluateHealthGate(ctx, c, logger)
			svc.evaluateHealthGate(ctx, c, logger) // a second replica must not repeat the verdict
		}
	}
	got := gateState(t, ctx, db, app.ID)
	if got.Status != "failed" || got.Message == "" {
		t.Fatalf("expected failed with message, got %+v", got)
	}
	var events int
	if err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_events WHERE id=$1 AND event_type='unhealthy'`, "health_gate:"+op).Scan(&events); err != nil || events != 1 {
		t.Fatalf("notification events=%d err=%v", events, err)
	}
	var audits int
	if err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE action='health_gate.failed' AND resource_id=$1`, app.ID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}
}

func TestIntegrationHealthGateSupersedesOlderChecks(t *testing.T) {
	ctx, db := isolatedPlannerStore(t)
	app, first := gateFixture(t, ctx, db, time.Minute, time.Minute, apphealth.Progressing)
	superseded, err := db.StartHealthGateCheck(ctx, store.HealthGateCheck{OperationID: store.NewID(), ApplicationID: app.ID, Revision: "new", Deadline: time.Now().Add(time.Minute), OnFailure: "mark-failed"})
	if err != nil || len(superseded) != 1 || superseded[0].OperationID != first {
		t.Fatalf("superseded=%+v err=%v", superseded, err)
	}
	pending, err := db.PendingHealthGateChecks(ctx, 10)
	if err != nil || len(pending) != 1 || pending[0].Revision != "new" {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	won, err := db.ResolveHealthGateCheck(ctx, first, "passed", "late")
	if err != nil || won {
		t.Fatalf("a superseded check must not resolve: won=%v err=%v", won, err)
	}
}
