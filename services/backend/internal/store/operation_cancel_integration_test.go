//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
)

func testOperationCancellation(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	app, err := s.ApplicationByID(ctx, "integration-app")
	if err != nil {
		t.Fatal(err)
	}
	app.ID, app.Name, app.SyncPolicy = "integration-cancel-app", "Cancellation fixture", "manual"
	app.AutoSyncPaused = false
	if err := s.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	defer s.DB.ExecContext(ctx, `DELETE FROM applications WHERE id=$1`, app.ID)
	identities := []core.Identity{{Kind: "ConfigMap", Name: "first"}, {Kind: "ConfigMap", Name: "second"}, {Kind: "ConfigMap", Name: "third"}}
	newOperation := func() Operation {
		t.Helper()
		planID := NewID()
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at) VALUES($1,$2,'fixture','cancel-digest','[]','[]','integration-owner',NOW()+INTERVAL '1 hour')`, planID, app.ID); err != nil {
			t.Fatal(err)
		}
		progress := NewOperationProgress(core.Plan{Changes: []core.Change{{Kind: core.Create, Identity: identities[0]}, {Kind: core.Update, Identity: identities[1]}, {Kind: core.Delete, Identity: identities[2]}}})
		op, err := s.QueueOperation(ctx, app.ID, planID, "integration-owner", nil, "cancel-digest", "", time.Minute, progress)
		if err != nil {
			t.Fatal(err)
		}
		return op
	}
	queued := newOperation()
	if ok, err := s.RequestOperationCancellation(ctx, queued.ID, "integration-owner"); err != nil || !ok {
		t.Fatalf("queued cancellation: %v %v", ok, err)
	}
	cancelled, err := s.OperationByID(ctx, queued.ID)
	if err != nil || cancelled.Status != "cancelled" || cancelled.Progress.Resources[0].Status != "skipped" {
		t.Fatalf("queued progress: %+v %v", cancelled, err)
	}
	op := newOperation()
	if _, err := s.DB.ExecContext(ctx, `UPDATE cluster_operation_gates SET next_operation_at=NOW() WHERE cluster_id=$1`, app.ClusterID); err != nil {
		t.Fatal(err)
	}
	op, claimed, err := s.ClaimQueuedOperation(ctx, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim: %+v %v", op, err)
	}
	op.Progress.Phase = "applying"
	op.Progress.BeginResource(identities[0], 1)
	if err := s.BeginOperationResource(ctx, op.ID, op.Progress); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.RequestOperationCancellation(ctx, op.ID, "integration-owner"); err != nil || !ok {
		t.Fatalf("running cancellation: %v %v", ok, err)
	}
	// The in-flight mutation finishes and writes using the executor's old flag.
	op.Progress.CompleteResource(identities[0])
	if err := s.SetOperationProgress(ctx, op.ID, op.Progress); err != nil {
		t.Fatal(err)
	}
	restarted := &Store{DB: s.DB}
	resumed, err := restarted.OperationByID(ctx, op.ID)
	if err != nil || resumed.Status != "running" || !resumed.Progress.CancelRequested || resumed.Progress.Resources[0].Status != "succeeded" {
		t.Fatalf("restart lost cancellation/progress: %+v %v", resumed, err)
	}
	if _, err := s.QueueOperation(ctx, app.ID, *op.PlanID, "integration-owner", nil, "cancel-digest", "", time.Minute, OperationProgress{}); err == nil {
		t.Fatal("duplicate execution accepted during cancellation")
	}
	next := op.Progress
	next.Resources = append([]ResourceProgress(nil), op.Progress.Resources...)
	next.BeginResource(identities[1], 1)
	if err := restarted.BeginOperationResource(ctx, op.ID, next); !errors.Is(err, ErrOperationCancelled) {
		t.Fatalf("new step allowed after stop: %v", err)
	}
	if err := restarted.FinishCancelledOperation(ctx, op); err != nil {
		t.Fatal(err)
	}
	result, err := restarted.OperationByID(ctx, op.ID)
	if err != nil || result.Status != "cancelled" || result.Progress.Resources[0].Status != "succeeded" || result.Progress.Resources[1].Status != "skipped" || len(result.Progress.Completed) != 1 || result.NextRetryAt != nil {
		t.Fatalf("partial cancellation: %+v %v", result, err)
	}
	if ok, err := s.RequestOperationCancellation(ctx, op.ID, "integration-owner"); err != nil || !ok {
		t.Fatalf("duplicate cancellation not idempotent: %v %v", ok, err)
	}
	if err := s.SetOperationProgress(ctx, op.ID, next); err == nil {
		t.Fatal("stale executor changed terminal progress")
	}
	application, err := s.ApplicationByID(ctx, app.ID)
	if err != nil || !application.AutoSyncPaused {
		t.Fatalf("automatic reconciliation not paused: %+v %v", application, err)
	}
	// Cancellation is closed after all resource steps settle, during finalization.
	finalizing := newOperation()
	if _, err := s.DB.ExecContext(ctx, `UPDATE operations SET status='running',progress=jsonb_set(progress,'{phase}','"complete"') WHERE id=$1`, finalizing.ID); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.RequestOperationCancellation(ctx, finalizing.ID, "integration-owner"); err != nil || ok {
		t.Fatalf("cancellation accepted during finalization: %v %v", ok, err)
	}
}
