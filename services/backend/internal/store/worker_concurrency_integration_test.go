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
}
