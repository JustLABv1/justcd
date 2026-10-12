//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/schedule"
)

func testDeploymentSchedules(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	if err := s.EnsureSystemActor(ctx); err != nil {
		t.Fatal(err)
	}
	app, err := s.ApplicationByID(ctx, "integration-app")
	if err != nil {
		t.Fatal(err)
	}
	app.ID, app.Name = "integration-scheduled-app", "Scheduled queue fixture"
	app.SyncPolicy = "auto-safe"
	app.AutoSyncPaused = false
	if err := s.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	defer s.DB.ExecContext(ctx, `DELETE FROM applications WHERE id=$1`, app.ID)
	freeze := schedule.Policy{Freezes: []schedule.Freeze{{Reason: "Incident"}}}
	if err := s.SetDeploymentPolicy(ctx, "workspace", app.WorkspaceID, "integration-owner", freeze); err != nil {
		t.Fatal(err)
	}
	defer s.DB.ExecContext(ctx, `DELETE FROM deployment_policies WHERE workspace_id=$1`, app.WorkspaceID)
	const planID = "integration-scheduled-plan"
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at) VALUES($1,$2,'fixture','scheduled-digest','[]','[]','integration-owner',NOW()+INTERVAL '1 hour')`, planID, app.ID); err != nil {
		t.Fatal(err)
	}
	queued, err := s.QueueOperation(ctx, app.ID, planID, "justcd-system", nil, "scheduled-digest", "", time.Minute, OperationProgress{Phase: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != "queued" || !queued.ScheduleDeferred || len(queued.WaitingReasons) != 1 || queued.NextAllowedAt != nil {
		t.Fatalf("missing wait state %+v", queued)
	}
	resetGate := func() {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, `UPDATE cluster_operation_gates SET next_operation_at=NOW() WHERE cluster_id=$1`, app.ClusterID); err != nil {
			t.Fatal(err)
		}
	}
	resetGate()
	if op, claimed, err := s.ClaimQueuedOperation(ctx, time.Minute); err != nil || claimed {
		t.Fatalf("auto-safe bypassed freeze: %+v %v %v", op, claimed, err)
	}
	// Fair scheduling: an older frozen entry ready for recheck must not
	// starve a new eligible entry behind it.
	next := app
	next.ID = "integration-schedule-next"
	next.Name = "Eligible queue fixture"
	if err := s.CreateApplication(ctx, next); err != nil {
		t.Fatal(err)
	}
	defer s.DB.ExecContext(ctx, `DELETE FROM applications WHERE id=$1`, next.ID)
	const nextPlan = "integration-schedule-next-plan"
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at) VALUES($1,$2,'fixture','next-digest','[]','[]','integration-owner',NOW()+INTERVAL '1 hour')`, nextPlan, next.ID); err != nil {
		t.Fatal(err)
	}
	nextOp, err := s.QueueOperation(ctx, next.ID, nextPlan, "integration-owner", nil, "next-digest", "", time.Minute, OperationProgress{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.OverrideOperationSchedule(ctx, nextOp.ID, "integration-owner", "Queue fairness fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE operations SET schedule_check_at=NOW()-INTERVAL '1 second' WHERE id=$1`, queued.ID); err != nil {
		t.Fatal(err)
	}
	eligible, claimed, err := s.ClaimQueuedOperation(ctx, time.Minute)
	if err != nil || !claimed || eligible.ID != nextOp.ID {
		t.Fatalf("frozen entry starved eligible work %+v %v %v", eligible, claimed, err)
	}
	if err := s.FinishOperation(ctx, nextOp.ID, "succeeded", "fairness fixture"); err != nil {
		t.Fatal(err)
	}
	resetGate()
	// Simulate restart by reopening the store facade and expiring the queue lease.
	if _, err := s.DB.ExecContext(ctx, `UPDATE operation_leases SET expires_at=NOW()-INTERVAL '1 minute' WHERE operation_id=$1`, queued.ID); err != nil {
		t.Fatal(err)
	}
	restarted := &Store{DB: s.DB}
	if err := restarted.RecoverInterruptedOperations(ctx); err != nil {
		t.Fatal(err)
	}
	persisted, err := restarted.OperationByID(ctx, queued.ID)
	if err != nil || persisted.Status != "queued" || len(persisted.WaitingReasons) != 1 {
		t.Fatalf("restart lost scheduled queue %+v %v", persisted, err)
	}
	if err := s.OverrideOperationSchedule(ctx, queued.ID, "integration-owner", " "); err == nil {
		t.Fatal("empty override accepted")
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO users(id,email) VALUES('integration-schedule-deployer','schedule@example.invalid'); INSERT INTO workspace_memberships(workspace_id,user_id,role) VALUES('integration-workspace','integration-schedule-deployer','deployer')`); err != nil {
		t.Fatal(err)
	}
	defer s.DB.ExecContext(ctx, `DELETE FROM users WHERE id='integration-schedule-deployer'`)
	if err := s.OverrideOperationSchedule(ctx, queued.ID, "integration-schedule-deployer", "Incident response"); !errors.Is(err, ErrScheduleOverrideForbidden) {
		t.Fatalf("deployer bypassed owner check %v", err)
	}
	if err := s.OverrideOperationSchedule(ctx, queued.ID, "integration-owner", "Incident response"); err != nil {
		t.Fatal(err)
	}
	var audited int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE action='deployment.schedule.overridden' AND details->>'operationId'=$1 AND details->>'reason'='Incident response' AND actor_id='integration-owner' AND jsonb_array_length(details->'restrictions')=1`, queued.ID).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("override not audited %d %v", audited, err)
	}
	overridden, claimed, err := s.ClaimQueuedOperation(ctx, time.Minute)
	if err != nil || !claimed || overridden.ID != queued.ID || overridden.ScheduleOverrideActorID != "integration-owner" {
		t.Fatalf("override did not admit %+v %v %v", overridden, claimed, err)
	}
	if cancelled, err := s.CancelQueuedOperation(ctx, queued.ID, "integration-owner", "Too late"); err != nil || cancelled {
		t.Fatalf("cancelled running operation %v %v", cancelled, err)
	}
	if err := s.DeferScheduledOperation(ctx, queued.ID, schedule.Decision{Reasons: []string{"Window closed"}}); err != nil {
		t.Fatal(err)
	}
	// Clearing the policy wakes the queue immediately, without touching plans.
	if err := s.SetDeploymentPolicy(ctx, "workspace", app.WorkspaceID, "integration-owner", schedule.Policy{}); err != nil {
		t.Fatal(err)
	}
	// Clear the fixture override so the newly opened policy is actually evaluated.
	if _, err := s.DB.ExecContext(ctx, `UPDATE operations SET schedule_override_reason='',schedule_override_actor_id=NULL WHERE id=$1`, queued.ID); err != nil {
		t.Fatal(err)
	}
	resetGate()
	admitted, claimed, err := s.ClaimQueuedOperation(ctx, time.Minute)
	if err != nil || !claimed || admitted.ID != queued.ID || len(admitted.WaitingReasons) != 0 {
		t.Fatalf("removing freeze did not wake queue %+v %v %v", admitted, claimed, err)
	}
	if err := s.FinishOperation(ctx, queued.ID, "succeeded", "fixture complete"); err != nil {
		t.Fatal(err)
	}
	// Revoking or disabling the owner invalidates an override at execution time.
	if _, err := s.DB.ExecContext(ctx, `UPDATE users SET disabled=TRUE WHERE id='integration-schedule-deployer'`); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckScheduleOverrideOwner(ctx, app.WorkspaceID, "integration-schedule-deployer"); !errors.Is(err, ErrScheduleOverrideForbidden) {
		t.Fatalf("disabled user allowed: %v", err)
	}
}
