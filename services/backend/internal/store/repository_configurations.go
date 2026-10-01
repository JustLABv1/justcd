package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type RepositoryConfiguration struct {
	PRSettings    RepositoryPRSettings `json:"prSettings"`
	PRError       string               `json:"prError"`
	ID            string               `json:"id"`
	WorkspaceID   string               `json:"workspaceId"`
	SourceID      string               `json:"sourceId"`
	Revision      string               `json:"revision"`
	Enabled       bool                 `json:"enabled"`
	LastCheckedAt *time.Time           `json:"lastCheckedAt,omitempty"`
	LastCommit    string               `json:"lastCommit"`
	LastError     string               `json:"lastError"`
	CreatedAt     time.Time            `json:"createdAt"`
}

const repositoryColumns = `id,workspace_id,source_id,revision,enabled,last_checked_at,last_commit,last_error,created_at,pr_settings,pr_error`

func scanRepository(row interface{ Scan(...any) error }) (RepositoryConfiguration, error) {
	var v RepositoryConfiguration
	var settings []byte
	err := row.Scan(&v.ID, &v.WorkspaceID, &v.SourceID, &v.Revision, &v.Enabled, &v.LastCheckedAt, &v.LastCommit, &v.LastError, &v.CreatedAt, &settings, &v.PRError)
	if err == nil {
		err = json.Unmarshal(settings, &v.PRSettings)
	}
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
	// Lock PR connections before application rows, matching PR worker lock order.
	rowsPR, err := tx.QueryContext(ctx, `SELECT c.id FROM source_control_connections c JOIN applications a ON a.id=c.application_id WHERE a.repository_configuration_id=$1 ORDER BY c.id FOR UPDATE OF c`, repository.ID)
	if err != nil {
		return err
	}
	for rowsPR.Next() {
		var id string
		if err := rowsPR.Scan(&id); err != nil {
			rowsPR.Close()
			return err
		}
	}
	err = rowsPR.Err()
	rowsPR.Close()
	if err != nil {
		return err
	}
	ids := []string{}
	for _, app := range apps {
		existing, err := scanApplication(tx.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE repository_configuration_id=$1 AND name=$2 FOR UPDATE`, repository.ID, app.Name))
		changed := errors.Is(err, sql.ErrNoRows) || (err == nil && existing.ConfigurationHash != app.ConfigurationHash && existing.RollbackResumeState == nil && existing.BranchTest == nil)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			var conflict bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM applications WHERE workspace_id=$1 AND name=$2)`, app.WorkspaceID, app.Name).Scan(&conflict); err != nil {
				return err
			}
			if conflict {
				promotedID, err := promoteRepositoryPR(ctx, tx, repository, app)
				if err != nil {
					return err
				}
				if promotedID == "" {
					return fmt.Errorf("application %q already exists outside this repository connection", app.Name)
				}
				app.ID = promotedID
			} else {
				app.ID = NewID()
				if err := createApplication(ctx, tx, app); err != nil {
					return err
				}
			}
		case err != nil:
			return err
		default:
			app.ID = existing.ID
			// A branch test freezes the complete tracked definition until resume.
			// Do not register destinations or update metadata from the base branch.
			if existing.BranchTest != nil {
				ids = append(ids, existing.ID)
				continue
			}
			// Legacy adoption changed the policy instead of pausing. Repair that
			// drift without allowing automatic sync before a fresh review.
			if existing.ConfigurationHash == app.ConfigurationHash && existing.SyncPolicy != app.SyncPolicy && existing.RollbackResumeState == nil && existing.BranchTest == nil {
				if _, err := tx.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=TRUE WHERE id=$1`, app.ID); err != nil {
					return err
				}
			}
			// Operational rollback pins remain effective until explicitly resumed.
			if (existing.ConfigurationHash != app.ConfigurationHash || existing.SyncPolicy != app.SyncPolicy) && existing.RollbackResumeState == nil && existing.BranchTest == nil {
				if err := updateApplication(ctx, tx, app); err != nil {
					return fmt.Errorf("application %q: %w", app.Name, err)
				}
			} else if existing.RollbackResumeState != nil || existing.BranchTest != nil {
				app.ConfigurationHash = existing.ConfigurationHash
				app.HelmReleaseName = existing.HelmReleaseName
			}
		}
		for i, binding := range app.Namespaces {
			resolved, err := ensureRepositoryNamespaceBinding(ctx, tx, repository.WorkspaceID, app.ClusterID, binding.Namespace, app.CreateNamespaces)
			if err != nil {
				return fmt.Errorf("%s: %w", app.ConfigurationPath, err)
			}
			app.Namespaces[i] = resolved
		}
		if changed {
			if err := replaceRepositoryIgnores(ctx, tx, app); err != nil {
				return fmt.Errorf("application %q: %w", app.Name, err)
			}
		}
		if existing.RollbackResumeState == nil {
			if err := reconcileRepositoryPR(ctx, tx, app); err != nil {
				return fmt.Errorf("%s: %w", app.ConfigurationPath, err)
			}
		}
		ids = append(ids, app.ID)
		if _, err := tx.ExecContext(ctx, `UPDATE applications SET repository_configuration_id=$2,configuration_path=$3,configuration_commit=$4,configuration_hash=$5,last_checked_at=CASE WHEN configuration_missing THEN NULL ELSE last_checked_at END,configuration_missing=FALSE,helm_release_name=$6 WHERE id=$1`, app.ID, repository.ID, app.ConfigurationPath, commit, app.ConfigurationHash, app.HelmReleaseName); err != nil {
			return err
		}
	}
	// Lock missing applications before invalidating plans, as the operation queue does.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM applications WHERE repository_configuration_id=$1 AND branch_test IS NULL AND NOT(id=ANY($2::text[])) ORDER BY id FOR UPDATE`, repository.ID, ids)
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
		missing, err := scanApplication(tx.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1`, id))
		if err != nil {
			return err
		}
		if err := reconcileRepositoryPR(ctx, tx, missing); err != nil {
			return err
		}
		var active bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE application_id=$1 AND status IN ('queued','running'))`, id).Scan(&active); err != nil {
			return err
		}
		if active {
			return errors.New("cannot remove a definition while its application is syncing; discovery will retry")
		}
	}
	// Invalidate outstanding plans when a definition disappears. Workloads stay intact.
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE status='current' AND application_id IN (SELECT id FROM applications WHERE repository_configuration_id=$1 AND branch_test IS NULL AND NOT configuration_missing AND NOT(id=ANY($2::text[])))`, repository.ID, ids); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE applications SET configuration_missing=TRUE WHERE repository_configuration_id=$1 AND branch_test IS NULL AND NOT(id=ANY($2::text[]))`, repository.ID, ids); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE repository_configurations SET last_checked_at=NOW(),last_commit=$2,last_error='' WHERE id=$1`, repository.ID, commit); err != nil {
		return err
	}
	return tx.Commit()
}

// Bindings inherit credentials rather than copying their IDs, so changing the
// workspace default later also updates access for discovered namespaces.
func ensureRepositoryNamespaceBinding(ctx context.Context, tx *sql.Tx, workspaceID, clusterID, namespace string, create bool) (NamespaceBinding, error) {
	var binding NamespaceBinding
	err := tx.QueryRowContext(ctx, `SELECT namespace,credential_id FROM namespace_bindings WHERE workspace_id=$1 AND cluster_id=$2 AND namespace=$3 FOR SHARE`, workspaceID, clusterID, namespace).Scan(&binding.Namespace, &binding.CredentialID)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return binding, err
	}
	if !create {
		return binding, fmt.Errorf("namespace %q requires an existing workspace binding or spec.destination.createNamespaces: true", namespace)
	}
	var kind sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT CASE WHEN EXISTS(SELECT 1 FROM cluster_agents a WHERE a.cluster_id=c.id) THEN 'agent' ELSE credential.kind END FROM clusters c
		LEFT JOIN workspace_cluster_credentials defaults ON defaults.cluster_id=c.id AND defaults.workspace_id=$1
		LEFT JOIN credentials credential ON credential.id=COALESCE(defaults.credential_id,c.default_credential_id)
		WHERE c.id=$2 AND (c.workspace_id IS NULL OR c.workspace_id=$1 OR EXISTS (
			SELECT 1 FROM workspace_cluster_shares share WHERE share.cluster_id=c.id AND share.target_workspace_id=$1 AND share.status='accepted'
		))`, workspaceID, clusterID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return binding, errors.New("target cluster is unavailable to this workspace")
	}
	if err != nil {
		return binding, err
	}
	if !kind.Valid || (kind.String != "kubernetes-token" && kind.String != "kubeconfig" && kind.String != "agent") {
		return binding, fmt.Errorf("namespace %q cannot be registered automatically: configure a default Kubernetes credential for this workspace and cluster", namespace)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO namespace_bindings(id,workspace_id,cluster_id,namespace,credential_id) VALUES($1,$2,$3,$4,NULL) ON CONFLICT(workspace_id,cluster_id,namespace) DO NOTHING`, NewID(), workspaceID, clusterID, namespace); err != nil {
		return binding, err
	}
	// Read back to preserve an override if another discovery registered it first.
	err = tx.QueryRowContext(ctx, `SELECT namespace,credential_id FROM namespace_bindings WHERE workspace_id=$1 AND cluster_id=$2 AND namespace=$3`, workspaceID, clusterID, namespace).Scan(&binding.Namespace, &binding.CredentialID)
	return binding, err
}
