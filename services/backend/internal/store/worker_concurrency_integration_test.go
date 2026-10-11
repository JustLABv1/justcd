//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
)

func testWorkerClaimConcurrency(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	app, err := s.ApplicationByID(ctx, "integration-app")
	if err != nil {
		t.Fatal(err)
	}
	app.ID, app.Name = "integration-worker-app", "Worker claim fixture"
	app.SyncPolicy = "manual"
	app.Namespaces = nil
	if err := s.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	defer s.DB.ExecContext(ctx, `DELETE FROM applications WHERE id=$1`, app.ID)
	const planID = "integration-worker-plan"
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at) VALUES($1,$2,'fixture','fixture-digest','[]','[]','integration-owner',NOW()+INTERVAL '1 hour')`, planID, app.ID); err != nil {
		t.Fatal(err)
	}
	queued, err := s.QueueOperation(ctx, app.ID, planID, "integration-owner", nil, "fixture-digest", "", 90*time.Second, OperationProgress{Phase: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueOperation(ctx, app.ID, planID, "integration-owner", nil, "fixture-digest", "", 90*time.Second, OperationProgress{}); err == nil {
		t.Fatal("duplicate operation accepted for the same application")
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE cluster_operation_gates SET next_operation_at=NOW() WHERE cluster_id=$1`, app.ClusterID); err != nil {
		t.Fatal(err)
	}
	type claimResult struct {
		operation Operation
		claimed   bool
		err       error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	for range 2 {
		go func() {
			<-start
			op, claimed, err := s.ClaimQueuedOperation(ctx, 90*time.Second)
			results <- claimResult{op, claimed, err}
		}()
	}
	close(start)
	claims := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.claimed {
			claims++
			if result.operation.ID != queued.ID {
				t.Fatalf("unexpected claim: %s", result.operation.ID)
			}
		}
	}
	if claims != 1 {
		t.Fatalf("two workers claimed %d operations; want exactly one", claims)
	}
	if err := s.RecoverInterruptedOperations(ctx); err != nil {
		t.Fatal(err)
	}
	active, err := s.OperationByID(ctx, queued.ID)
	if err != nil || active.Status != "running" {
		t.Fatalf("live leased operation recovered as interrupted: %+v, %v", active, err)
	}
	identity := core.Identity{ClusterID: app.ClusterID, APIVersion: "v1", Kind: "ConfigMap", Namespace: "default", Name: "already-applied"}
	if err := s.SetOperationProgress(ctx, queued.ID, OperationProgress{Phase: "applying", Total: 2, Completed: []core.Identity{identity}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE operation_leases SET expires_at=NOW()-INTERVAL '1 second' WHERE operation_id=$1`, queued.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RenewOperationLease(ctx, queued.ID, 90*time.Second); err == nil {
		t.Fatal("expired lease was resurrected")
	}
	for range 2 {
		if err := s.RecoverInterruptedOperations(ctx); err != nil {
			t.Fatal(err)
		}
	}
	interrupted, err := s.OperationByID(ctx, queued.ID)
	if err != nil || interrupted.Status != "failed" || interrupted.TerminalReason != "interrupted_requires_review" || interrupted.NextRetryAt != nil || len(interrupted.Progress.Completed) != 1 {
		t.Fatalf("partial progress lost or unsafe retry scheduled: %+v, %v", interrupted, err)
	}
	var auditCount int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE resource_id=$1 AND action='sync.failed' AND details->>'operationId'=$2`, app.ID, queued.ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("recovery should emit exactly one audit event: %d, %v", auditCount, err)
	}
	if _, err := s.QueueOperation(ctx, app.ID, planID, "integration-owner", nil, "fixture-digest", "", 90*time.Second, OperationProgress{}); err == nil {
		t.Fatal("interrupted plan could be replayed without fresh review")
	}
	if err := s.SetOperationProgress(ctx, queued.ID, OperationProgress{Phase: "complete"}); err == nil {
		t.Fatal("stale executor overwrote recovered progress")
	}
	if err := s.FinishOperationWithRetry(ctx, queued.ID, app.ID, "stale worker", 1, "stale", nil, "stale"); err == nil {
		t.Fatal("stale executor overwrote recovered terminal state")
	}
	if err := s.CompleteDeployment(ctx, interrupted, "stale", "synced", "sync.succeeded", nil); err == nil {
		t.Fatal("stale executor completed an interrupted operation")
	}
	guard, err := s.AcquireExecutionGuard(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if competing, err := s.AcquireExecutionGuard(ctx, app.ID); err == nil {
		competing.Close()
		t.Fatal("competing session acquired application execution guard")
	}
	var pid int
	if err := guard.conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `SELECT pg_terminate_backend($1)`, pid); err != nil {
		t.Fatal(err)
	}
	if err := guard.Check(ctx); err == nil {
		t.Fatal("dead execution session reported healthy")
	}
	guard.Close()
	replacement, err := s.AcquireExecutionGuard(ctx, app.ID)
	if err != nil {
		t.Fatalf("dead session retained execution lock: %v", err)
	}
	replacement.Close()
	const freshPlanID = "integration-worker-fresh-plan"
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at) VALUES($1,$2,'fresh','fresh-digest','[]','[]','integration-owner',NOW()+INTERVAL '1 hour')`, freshPlanID, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueOperation(ctx, app.ID, freshPlanID, "integration-owner", nil, "fresh-digest", "", 90*time.Second, OperationProgress{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE cluster_operation_gates SET next_operation_at=NOW() WHERE cluster_id=$1`, app.ClusterID); err != nil {
		t.Fatal(err)
	}
	fresh, claimed, err := s.ClaimQueuedOperation(ctx, 90*time.Second)
	if err != nil || !claimed {
		t.Fatalf("fresh reviewed plan did not become executable: %v", err)
	}
	fresh.Progress.Phase = "complete"
	fresh.Message = "completed"
	for range 2 {
		if err := s.CompleteDeployment(ctx, fresh, "fresh", "synced", "sync.succeeded", map[string]any{"operationId": fresh.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE resource_id=$1 AND action='sync.succeeded' AND details->>'operationId'=$2`, app.ID, fresh.ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("completion replay duplicated audit: %d, %v", auditCount, err)
	}
	if _, err := s.QueueOperation(ctx, app.ID, freshPlanID, "integration-owner", nil, "fresh-digest", "", 90*time.Second, OperationProgress{}); err == nil {
		t.Fatal("completed plan could be executed twice")
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=FALSE WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverInterruptedOperations(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := s.ApplicationByID(ctx, app.ID)
	if err != nil || current.AutoSyncPaused || current.Health != "synced" || current.LastSyncedRevision != "fresh" {
		t.Fatalf("historical interruption degraded a completed retry: %+v, %v", current, err)
	}
	type planResult struct {
		record PlanRecord
		err    error
	}
	planned := make(chan planResult, 2)
	for range 2 {
		go func() {
			record, _, err := s.SavePlanOrReuse(ctx, PlanRecord{ID: NewID(), Plan: core.Plan{ApplicationID: app.ID, Revision: "fresh", Digest: "concurrent-plan-digest"}, CreatedBy: "integration-owner", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Status: "current"})
			planned <- planResult{record, err}
		}()
	}
	first, second := <-planned, <-planned
	if first.err != nil || second.err != nil || first.record.ID != second.record.ID {
		t.Fatalf("identical planners did not reuse one immutable plan: %+v, %+v", first, second)
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE details->>'planId'=$1 AND action='plan.created'`, first.record.ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("concurrent planning duplicated its audit event: %d, %v", auditCount, err)
	}
}
