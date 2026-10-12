package store

import (
	"context"
	"encoding/json"
	"errors"
)

// CompleteDeployment commits the terminal result, plan, application status,
// progress, audit event, and lease release together. Repeating a committed
// completion is a no-op; a recovered or superseded executor cannot complete it.
func (s *Store) CompleteDeployment(ctx context.Context, operation Operation, revision, health, action string, details any) error {
	for _, resource := range operation.Progress.Resources {
		if resource.Status != "succeeded" {
			return errors.New("operation cannot succeed with unfinished resource results")
		}
	}
	progress, err := json.Marshal(operation.Progress)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var applicationID, status string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, operation.ApplicationID).Scan(&applicationID); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT status FROM operations WHERE id=$1 AND application_id=$2 AND plan_id=$3 FOR UPDATE`, operation.ID, applicationID, operation.PlanID).Scan(&status); err != nil {
		return err
	}
	if status == "succeeded" {
		return nil
	}
	var valid bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operation_leases WHERE operation_id=$1 AND expires_at>NOW())`, operation.ID).Scan(&valid); err != nil {
		return err
	}
	if status != "running" || !valid {
		return errors.New("operation cannot complete after losing its lease")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET last_synced_revision=CASE WHEN $2='' THEN last_synced_revision ELSE $2 END,health=$3,status_issues='[]',retry_attempt_count=0,retry_next_at=NULL,retry_terminal_reason='',retry_last_error_code='',last_checked_at=NOW(),updated_at=NOW() WHERE id=$1`, applicationID, revision, health); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='applied' WHERE id=$1`, operation.PlanID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE operations SET status='succeeded',message=$2,progress=$3,finished_at=NOW() WHERE id=$1`, operation.ID, operation.Message, progress); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,$2,'application',$3,$4)`, operation.ActorID, action, applicationID, payload); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM operation_leases WHERE operation_id=$1`, operation.ID); err != nil {
		return err
	}
	return tx.Commit()
}
