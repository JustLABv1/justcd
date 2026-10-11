//go:build integration

package syncer

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/store"
)

// Block the executor's plan read in PostgreSQL after a real queue claim. This
// tests the worker's actual acquisition/execution contexts without cluster writes.
func testOperationWorkerDrain(t *testing.T, ctx context.Context, db *store.Store, original store.Application, ownerID string) {
	t.Helper()
	for _, forceCancel := range []bool{false, true} {
		name := "finish during drain"
		if forceCancel {
			name = "cancel during drain"
		}
		t.Run(name, func(t *testing.T) {
			app := original
			app.ID, app.Name = store.NewID(), "Drain "+store.NewID()
			app.SyncPolicy = "manual"
			if err := db.CreateApplication(ctx, app); err != nil {
				t.Fatal(err)
			}
			defer db.DB.ExecContext(ctx, `DELETE FROM applications WHERE id=$1`, app.ID)
			planID := store.NewID()
			if _, err := db.DB.ExecContext(ctx, `INSERT INTO plans(id,application_id,revision,digest,changes,desired,created_by,expires_at) VALUES($1,$2,'fixture','drain-digest','[]','[]',$3,NOW()+INTERVAL '1 hour')`, planID, app.ID, ownerID); err != nil {
				t.Fatal(err)
			}
			// A missing actor intentionally fails authorization after the blocked
			// read; reaching that failure proves drain did not cancel execution.
			operation, err := db.QueueOperation(ctx, app.ID, planID, "", nil, "drain-digest", "", 90*time.Second, store.OperationProgress{Phase: "queued"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.DB.ExecContext(ctx, `UPDATE cluster_operation_gates SET next_operation_at=NOW() WHERE cluster_id=$1`, app.ClusterID); err != nil {
				t.Fatal(err)
			}
			blocker, err := db.DB.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			if _, err := blocker.ExecContext(ctx, `LOCK TABLE plans IN ACCESS EXCLUSIVE MODE`); err != nil {
				t.Fatal(err)
			}
			acquireCtx, stopAcquiring := context.WithCancel(ctx)
			executionCtx, cancelExecution := context.WithCancel(ctx)
			done := make(chan struct{})
			t.Cleanup(func() {
				stopAcquiring()
				cancelExecution()
				_ = blocker.Rollback()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Error("operation worker did not stop")
				}
			})
			go func() {
				defer close(done)
				(&Service{Store: db}).RunOperationWorkerWithDrain(acquireCtx, executionCtx, slog.New(slog.NewTextHandler(io.Discard, nil)))
			}()
			waitCtx, cancelWait := context.WithTimeout(ctx, 10*time.Second)
			defer cancelWait()
			for {
				var blocked bool
				if err := db.DB.QueryRowContext(waitCtx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT id,application_id,revision,digest,bindings%')`).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				select {
				case <-waitCtx.Done():
					t.Fatal("executor did not reach blocked plan read")
				case <-time.After(10 * time.Millisecond):
				}
			}
			stopAcquiring()
			select {
			case <-done:
				t.Fatal("worker returned with a claimed operation still executing")
			default:
			}
			if forceCancel {
				cancelExecution()
			}
			if err := blocker.Rollback(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-waitCtx.Done():
				t.Fatal("worker did not finish drain")
			}
			result, err := db.OperationByID(ctx, operation.ID)
			if err != nil || result.Status != "failed" {
				t.Fatalf("operation result not persisted before worker exit: %+v, %v", result, err)
			}
			if forceCancel {
				if result.TerminalReason != "interrupted_requires_review" || result.NextRetryAt != nil {
					t.Fatalf("cancelled operation should require review: %+v", result)
				}
			} else if result.ErrorCode != "operation.validation_failed" || result.TerminalReason != "validation_failure" {
				t.Fatalf("execution cancelled instead of draining to authorization: %+v", result)
			}
		})
	}
}
