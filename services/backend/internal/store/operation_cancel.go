package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrOperationCancelled = errors.New("operation cancellation requested")

// RequestOperationCancellation keeps a running operation and its lease active:
// the current resource step must settle before another execution can begin.
func (s *Store) RequestOperationCancellation(ctx context.Context, id, actor string) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var appID, locked, status, kind, phase string
	var requested bool
	if err = tx.QueryRowContext(ctx, `SELECT application_id FROM operations WHERE id=$1`, id).Scan(&appID); err != nil {
		return false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, appID).Scan(&locked); err != nil {
		return false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT status,operation_type,COALESCE(progress->>'phase',''),COALESCE((progress->>'cancelRequested')::boolean,false) FROM operations WHERE id=$1 FOR UPDATE`, id).Scan(&status, &kind, &phase, &requested); err != nil {
		return false, err
	}
	if status == "cancelled" || (status == "running" && requested) {
		return true, nil
	}
	if status != "queued" && status != "running" || kind == "resource_action" || phase == "complete" {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=TRUE,retry_next_at=NULL,updated_at=NOW() WHERE id=$1`, appID); err != nil {
		return false, err
	}
	if status == "queued" {
		if _, err = cancelQueuedOperationTx(ctx, tx, id, actor, "Cancelled before execution; no resources attempted. Automatic reconciliation is paused. Create a fresh plan to deploy.", appID); err != nil {
			return false, err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE operations SET progress=jsonb_set(progress,'{cancelRequested}','true'),message='Cancellation requested; the current resource step will finish before stopping. Applied changes will remain.' WHERE id=$1`, id); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'operation.cancel_requested','application',$2,jsonb_build_object('operationId',$3::text))`, actor, appID, id); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// BeginOperationResource atomically orders a stop request against starting the
// next step. Ordinary progress writes preserve the independently persisted flag.
func (s *Store) BeginOperationResource(ctx context.Context, id string, progress OperationProgress) error {
	raw, err := json.Marshal(progress)
	if err != nil {
		return err
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE operations o SET progress=$2::jsonb || jsonb_build_object('cancelRequested',COALESCE((o.progress->>'cancelRequested')::boolean,false)) WHERE o.id=$1 AND o.status='running' AND NOT COALESCE((o.progress->>'cancelRequested')::boolean,false) AND EXISTS(SELECT 1 FROM operation_leases l WHERE l.operation_id=o.id AND l.expires_at>NOW())`, id, raw)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	op, err := s.OperationByID(ctx, id)
	if err != nil {
		return err
	}
	if op.Progress.CancelRequested {
		return ErrOperationCancelled
	}
	return errors.New("operation no longer owns an active lease")
}

func (s *Store) FinishCancelledOperation(ctx context.Context, operation Operation) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, operation.ApplicationID).Scan(&locked); err != nil {
		return err
	}
	operation.Progress.CancelRequested = true
	operation.Progress.Stop("Resource outcome requires review; changes may remain applied.")
	message := fmt.Sprintf("Cancelled after %d of %d resources completed. Applied changes remain; skipped resources were not attempted. Automatic reconciliation is paused. Review cluster state and create a fresh plan.", len(operation.Progress.Completed), operation.Progress.Total)
	raw, err := json.Marshal(operation.Progress)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE operations o SET status='cancelled',message=$2,progress=$3,finished_at=NOW(),next_retry_at=NULL,terminal_reason='cancelled_requires_review' WHERE o.id=$1 AND o.status='running' AND EXISTS(SELECT 1 FROM operation_leases l WHERE l.operation_id=o.id AND l.expires_at>NOW())`, operation.ID, message, raw)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("operation cannot cancel after losing its lease")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE id=$1`, operation.PlanID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=TRUE,retry_next_at=NULL,retry_terminal_reason='cancelled_requires_review',health='out_of_sync',updated_at=NOW() WHERE id=$1`, operation.ApplicationID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) SELECT actor_id,'operation.cancelled','application',application_id,jsonb_build_object('operationId',id,'progress',progress) FROM operations WHERE id=$1`, operation.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM operation_leases WHERE operation_id=$1`, operation.ID); err != nil {
		return err
	}
	return tx.Commit()
}
