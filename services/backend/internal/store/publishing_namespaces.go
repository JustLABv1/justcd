package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PublishingNamespaceBinding validates provisioning without leaving a binding on
// failed uploads or read-only repository previews. Registration commits it with
// the application using the same checks and inherited deployment credentials.
func (s *Store) PublishingNamespaceBinding(ctx context.Context, workspace, cluster, namespace string, create bool) (NamespaceBinding, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return NamespaceBinding{}, err
	}
	defer tx.Rollback()
	return publishingNamespaceBinding(ctx, tx, workspace, cluster, namespace, create)
}
func publishingNamespaceBinding(ctx context.Context, tx *sql.Tx, workspace, cluster, namespace string, create bool) (NamespaceBinding, error) {
	if !create {
		return ensureRepositoryNamespaceBinding(ctx, tx, workspace, cluster, namespace, false)
	}
	// Serialize onboarding of this destination across workspaces.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, cluster+"/"+namespace); err != nil {
		return NamespaceBinding{}, err
	}
	var other bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM namespace_bindings WHERE cluster_id=$1 AND namespace=$2 AND workspace_id<>$3) AND NOT EXISTS(SELECT 1 FROM namespace_bindings WHERE cluster_id=$1 AND namespace=$2 AND workspace_id=$3)`, cluster, namespace, workspace).Scan(&other)
	if err != nil {
		return NamespaceBinding{}, err
	}
	if other {
		return NamespaceBinding{}, errors.New("namespace is already bound to another workspace; use an approved existing binding")
	}
	return ensureRepositoryNamespaceBinding(ctx, tx, workspace, cluster, namespace, true)
}
func (s *Store) CreateRepositoryPublishedApplication(ctx context.Context, a Application, profile *PublishingProfile) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if profile != nil {
		var enabled bool
		var version int64
		if err := tx.QueryRowContext(ctx, `SELECT enabled,version FROM publishing_profiles WHERE id=$1 AND workspace_id=$2 FOR SHARE`, profile.ID, a.WorkspaceID).Scan(&enabled, &version); err != nil {
			return err
		}
		if !enabled || version != profile.Version {
			return errors.New("publishing profile changed; review a fresh proposal")
		}
	}
	if len(a.Namespaces) != 1 {
		return fmt.Errorf("publishing requires one namespace")
	}
	b, err := publishingNamespaceBinding(ctx, tx, a.WorkspaceID, a.ClusterID, a.Namespaces[0].Namespace, a.CreateNamespaces)
	if err != nil {
		return err
	}
	a.Namespaces = []NamespaceBinding{b}
	if err = createApplication(ctx, tx, a); err != nil {
		return err
	}
	return tx.Commit()
}
