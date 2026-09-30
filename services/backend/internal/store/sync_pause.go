package store

import (
	"context"
	"errors"
)

// PauseApplication serializes with operation creation, so a successful pause
// guarantees there is no queued or running sync that can overwrite a hotfix.
func (s *Store) PauseApplication(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, id).Scan(&locked); err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE application_id=$1 AND status IN ('queued','running'))`, id).Scan(&active); err != nil {
		return err
	}
	if active {
		return errors.New("application is currently syncing; wait for the operation to finish before pausing")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=TRUE,retry_next_at=NULL,updated_at=NOW() WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}
