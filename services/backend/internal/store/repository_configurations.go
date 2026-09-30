package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type RepositoryConfiguration struct {
	ID            string     `json:"id"`
	WorkspaceID   string     `json:"workspaceId"`
	SourceID      string     `json:"sourceId"`
	Revision      string     `json:"revision"`
	Enabled       bool       `json:"enabled"`
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`
	LastCommit    string     `json:"lastCommit"`
	LastError     string     `json:"lastError"`
	CreatedAt     time.Time  `json:"createdAt"`
}

const repositoryColumns = `id,workspace_id,source_id,revision,enabled,last_checked_at,last_commit,last_error,created_at`

func scanRepository(row interface{ Scan(...any) error }) (RepositoryConfiguration, error) {
	var v RepositoryConfiguration
	err := row.Scan(&v.ID, &v.WorkspaceID, &v.SourceID, &v.Revision, &v.Enabled, &v.LastCheckedAt, &v.LastCommit, &v.LastError, &v.CreatedAt)
	return v, err
}
func (s *Store) RepositoryConfigurationByID(ctx context.Context, id string) (RepositoryConfiguration, error) {
	return scanRepository(s.DB.QueryRowContext(ctx, `SELECT `+repositoryColumns+` FROM repository_configurations WHERE id=$1`, id))
}
func (s *Store) ListRepositoryConfigurations(ctx context.Context, workspaceID string, due bool) ([]RepositoryConfiguration, error) {
	query := `SELECT ` + repositoryColumns + ` FROM repository_configurations WHERE workspace_id=$1 ORDER BY created_at`
	var args []any = []any{workspaceID}
	if due {
		query = `SELECT ` + repositoryColumns + ` FROM repository_configurations WHERE enabled AND (last_checked_at IS NULL OR last_checked_at<NOW()-INTERVAL '60 seconds') ORDER BY last_checked_at NULLS FIRST LIMIT 25`
		args = nil
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RepositoryConfiguration{}
	for rows.Next() {
		v, err := scanRepository(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) CreateRepositoryConfiguration(ctx context.Context, v RepositoryConfiguration) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO repository_configurations(id,workspace_id,source_id,revision) VALUES($1,$2,$3,$4)`, v.ID, v.WorkspaceID, v.SourceID, v.Revision)
	return err
}
func (s *Store) SetRepositoryConfigurationEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE repository_configurations SET enabled=$2,last_checked_at=NULL WHERE id=$1`, id, enabled)
	return err
}
func (s *Store) RecordRepositoryConfigurationError(ctx context.Context, id, message string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE repository_configurations SET last_checked_at=NOW(),last_error=$2 WHERE id=$1`, id, message)
	return err
}

// ApplyRepositoryApplications commits the complete validated snapshot atomically.
// Existing manual applications are never adopted implicitly.
func (s *Store) ApplyRepositoryApplications(ctx context.Context, repository RepositoryConfiguration, commit string, apps []Application) error {
	if err := s.EnsureSystemActor(ctx); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT enabled FROM repository_configurations WHERE id=$1 FOR UPDATE`, repository.ID).Scan(&enabled); err != nil {
		return err
	}
	if !enabled {
		return errors.New("repository discovery is paused")
	}
	ids := []string{}
	for _, app := range apps {
		existing, err := scanApplication(tx.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE repository_configuration_id=$1 AND name=$2 FOR UPDATE`, repository.ID, app.Name))
		changed := errors.Is(err, sql.ErrNoRows) || (err == nil && existing.ConfigurationHash != app.ConfigurationHash && existing.RollbackResumeState == nil)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			var conflict bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM applications WHERE workspace_id=$1 AND name=$2)`, app.WorkspaceID, app.Name).Scan(&conflict); err != nil {
				return err
			}
			if conflict {
				return fmt.Errorf("application %q already exists outside this repository connection", app.Name)
			}
			app.ID = NewID()
			if err := createApplication(ctx, tx, app); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			app.ID = existing.ID
			// Operational rollback pins remain effective until explicitly resumed.
			if existing.ConfigurationHash != app.ConfigurationHash && existing.RollbackResumeState == nil {
				if err := updateApplication(ctx, tx, app); err != nil {
					return fmt.Errorf("application %q: %w", app.Name, err)
				}
			} else if existing.RollbackResumeState != nil {
				app.ConfigurationHash = existing.ConfigurationHash
				app.HelmReleaseName = existing.HelmReleaseName
			}
		}
		if changed {
			if err := replaceRepositoryIgnores(ctx, tx, app); err != nil {
				return fmt.Errorf("application %q: %w", app.Name, err)
			}
		}
		ids = append(ids, app.ID)
		if _, err := tx.ExecContext(ctx, `UPDATE applications SET repository_configuration_id=$2,configuration_path=$3,configuration_commit=$4,configuration_hash=$5,last_checked_at=CASE WHEN configuration_missing THEN NULL ELSE last_checked_at END,configuration_missing=FALSE,helm_release_name=$6 WHERE id=$1`, app.ID, repository.ID, app.ConfigurationPath, commit, app.ConfigurationHash, app.HelmReleaseName); err != nil {
			return err
		}
	}
	// Lock missing applications before invalidating plans, as the operation queue does.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM applications WHERE repository_configuration_id=$1 AND NOT(id=ANY($2::text[])) ORDER BY id FOR UPDATE`, repository.ID, ids)
	if err != nil {
		return err
	}
	missingIDs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		missingIDs = append(missingIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range missingIDs {
		var active bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE application_id=$1 AND status IN ('queued','running'))`, id).Scan(&active); err != nil {
			return err
		}
		if active {
			return errors.New("cannot remove a definition while its application is syncing; discovery will retry")
		}
	}
	// Invalidate outstanding plans when a definition disappears. Workloads stay intact.
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE status='current' AND application_id IN (SELECT id FROM applications WHERE repository_configuration_id=$1 AND NOT configuration_missing AND NOT(id=ANY($2::text[])))`, repository.ID, ids); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET configuration_missing=TRUE WHERE repository_configuration_id=$1 AND NOT(id=ANY($2::text[]))`, repository.ID, ids); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE repository_configurations SET last_checked_at=NOW(),last_commit=$2,last_error='' WHERE id=$1`, repository.ID, commit); err != nil {
		return err
	}
	return tx.Commit()
}
