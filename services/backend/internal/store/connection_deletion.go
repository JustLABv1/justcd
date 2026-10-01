package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrConnectionInUse = errors.New("connection is still in use")

// DeleteConnection removes JustCD configuration only. Foreign keys protect
// application references; active shares must be revoked before deletion.
func (s *Store) DeleteConnection(ctx context.Context, kind, id string) error {
	var query, usage, lock string
	switch kind {
	case "cluster":
		query = `DELETE FROM clusters WHERE id=$1`
		lock = `SELECT id FROM clusters WHERE id=$1 FOR UPDATE`
		usage = `SELECT EXISTS(SELECT 1 FROM workspace_cluster_shares WHERE cluster_id=$1 AND status IN ('pending','accepted')) OR EXISTS(SELECT 1 FROM managed_resources WHERE cluster_id=$1)`
	case "git_source":
		query = `DELETE FROM git_sources WHERE id=$1`
		lock = `SELECT id FROM git_sources WHERE id=$1 FOR UPDATE`
		usage = `SELECT EXISTS(SELECT 1 FROM workspace_git_source_shares WHERE git_source_id=$1 AND status IN ('pending','accepted'))`
	case "repository_configuration":
		query = `DELETE FROM repository_configurations WHERE id=$1`
		usage = `SELECT EXISTS(SELECT 1 FROM repository_pr_applications WHERE repository_id=$1 AND application_id IS NOT NULL)`
		lock = `SELECT id FROM repository_configurations WHERE id=$1 FOR UPDATE`
	case "credential":
		query = `DELETE FROM credentials WHERE id=$1`
		lock = `SELECT id FROM credentials WHERE id=$1 FOR UPDATE`
	default:
		return errors.New("unsupported connection kind")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedID string
	if err := tx.QueryRowContext(ctx, lock, id).Scan(&lockedID); err != nil {
		return err
	}
	if usage != "" {
		var used bool
		if err := tx.QueryRowContext(ctx, usage, id).Scan(&used); err != nil {
			return err
		}
		if used {
			return ErrConnectionInUse
		}
	}
	result, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23503" {
			return ErrConnectionInUse
		}
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) DeleteNamespaceBinding(ctx context.Context, workspaceID, clusterID, namespace string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM namespace_bindings WHERE workspace_id=$1 AND cluster_id=$2 AND namespace=$3 FOR UPDATE`, workspaceID, clusterID, namespace).Scan(&id); err != nil {
		return err
	}
	var used bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM applications WHERE workspace_id=$1 AND cluster_id=$2 AND EXISTS(SELECT 1 FROM jsonb_array_elements(namespaces) binding WHERE binding->>'namespace'=$3)) OR EXISTS(SELECT 1 FROM managed_resources r JOIN applications a ON a.id=r.application_id WHERE a.workspace_id=$1 AND r.cluster_id=$2 AND r.namespace=$3)`, workspaceID, clusterID, namespace).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrConnectionInUse
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM namespace_bindings WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kubernetes_permission_tests WHERE workspace_id=$1 AND cluster_id=$2 AND namespace=$3`, workspaceID, clusterID, namespace); err != nil {
		return err
	}
	return tx.Commit()
}
