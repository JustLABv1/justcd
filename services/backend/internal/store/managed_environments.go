package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
)

type ManagedEnvironment struct {
	ID, ClusterID, WorkspaceID, Namespace, OwnerApplicationID, NamespaceUID, Phase string
	Shared                                                                         bool
}

func scanManagedEnvironment(row interface{ Scan(...any) error }) (ManagedEnvironment, error) {
	var e ManagedEnvironment
	err := row.Scan(&e.ID, &e.ClusterID, &e.WorkspaceID, &e.Namespace, &e.OwnerApplicationID, &e.NamespaceUID, &e.Phase, &e.Shared)
	return e, err
}

const managedEnvironmentColumns = "id,cluster_id,workspace_id,namespace,owner_application_id,namespace_uid,phase,shared"

func (s *Store) RegisterManagedEnvironment(ctx context.Context, app Application, namespace string) (ManagedEnvironment, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ManagedEnvironment{}, err
	}
	defer tx.Rollback()
	// Lock the app as queueing and deletion do. Trigger destination inserts/updates
	// also serialize against the environment row once it exists.
	var locked string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 AND workspace_id=$2 AND cluster_id=$3 AND namespaces @> jsonb_build_array(jsonb_build_object('namespace',$4::text)) AND (NOT $5::boolean OR (create_namespaces AND NOT decommissioning)) FOR UPDATE`, app.ID, app.WorkspaceID, app.ClusterID, namespace, app.CreateNamespaces && !app.Decommissioning).Scan(&locked); err != nil {
		return ManagedEnvironment{}, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(jsonb_build_array($1::text,$2::text)::text,0))`, app.ClusterID, namespace); err != nil {
		return ManagedEnvironment{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO managed_namespace_environments(id,cluster_id,workspace_id,namespace,owner_application_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT(cluster_id,namespace) DO NOTHING`, NewID(), app.ClusterID, app.WorkspaceID, namespace, app.ID)
	if err != nil {
		return ManagedEnvironment{}, err
	}
	e, err := scanManagedEnvironment(tx.QueryRowContext(ctx, `SELECT `+managedEnvironmentColumns+` FROM managed_namespace_environments WHERE cluster_id=$1 AND namespace=$2 FOR UPDATE`, app.ClusterID, namespace))
	if err != nil {
		return e, err
	}
	if e.WorkspaceID != app.WorkspaceID {
		return e, errors.New("namespace environment belongs to another workspace")
	}
	if e.Phase == "deleting" {
		return e, errors.New("namespace environment is undergoing deletion; finish cleanup before deploying")
	}
	var shared bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM applications WHERE cluster_id=$1 AND id<>$2 AND namespaces @> jsonb_build_array(jsonb_build_object('namespace',$3::text)))`, app.ClusterID, app.ID, namespace).Scan(&shared); err != nil {
		return e, err
	}
	if e.Phase == "deleted" && e.OwnerApplicationID == app.ID {
		return e, errors.New("namespace was deleted; complete application deletion before creating a new environment")
	}
	if e.Phase == "deleted" {
		e.ID, e.OwnerApplicationID, e.NamespaceUID, e.Phase, e.Shared = NewID(), app.ID, "", "pending", shared
		_, err = tx.ExecContext(ctx, `UPDATE managed_namespace_environments SET id=$2,owner_application_id=$3,namespace_uid='',phase='pending',shared=$4,status='{}' WHERE cluster_id=$1 AND namespace=$5`, app.ClusterID, e.ID, app.ID, shared, namespace)
	} else {
		e.Shared = e.Shared || shared || e.OwnerApplicationID != app.ID
		_, err = tx.ExecContext(ctx, `UPDATE managed_namespace_environments SET shared=$2 WHERE id=$1`, e.ID, e.Shared)
	}
	if err != nil {
		return e, err
	}
	return e, tx.Commit()
}
func (s *Store) ManagedEnvironmentForNamespace(ctx context.Context, cluster, namespace string) (ManagedEnvironment, error) {
	return scanManagedEnvironment(s.DB.QueryRowContext(ctx, `SELECT `+managedEnvironmentColumns+` FROM managed_namespace_environments WHERE cluster_id=$1 AND namespace=$2`, cluster, namespace))
}
func (s *Store) SaveManagedEnvironmentStatus(ctx context.Context, id string, status agentprotocol.EnvironmentStatus) error {
	raw, err := json.Marshal(status)
	if err != nil {
		return err
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE managed_namespace_environments SET namespace_uid=CASE WHEN namespace_uid='' THEN $2 ELSE namespace_uid END,phase=CASE WHEN phase='deleting' AND $3 NOT IN ('deleted','retained') THEN 'deleting' ELSE $3 END,shared=shared OR $4,status=$5,updated_at=NOW() WHERE id=$1 AND (namespace_uid='' OR namespace_uid=$2 OR $2='')`, id, status.NamespaceUID, status.Phase, status.Shared, raw)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("namespace ownership UID changed; refusing lifecycle update")
	}
	return err
}

// BeginManagedEnvironmentDeletion freezes new workload operations before any
// remote mutation, checks current references, and is retryable after disconnects.
func (s *Store) BeginManagedEnvironmentDeletion(ctx context.Context, app Application, namespace string, preserve bool) (ManagedEnvironment, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ManagedEnvironment{}, false, err
	}
	defer tx.Rollback()
	var id string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, app.ID).Scan(&id); err != nil {
		return ManagedEnvironment{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(jsonb_build_array($1::text,$2::text)::text,0))`, app.ClusterID, namespace); err != nil {
		return ManagedEnvironment{}, false, err
	}
	e, err := scanManagedEnvironment(tx.QueryRowContext(ctx, `SELECT `+managedEnvironmentColumns+` FROM managed_namespace_environments WHERE cluster_id=$1 AND namespace=$2 FOR UPDATE`, app.ClusterID, namespace))
	if errors.Is(err, sql.ErrNoRows) {
		return e, true, nil
	}
	if err != nil {
		return e, false, err
	}
	if e.WorkspaceID != app.WorkspaceID {
		return e, false, errors.New("namespace belongs to another workspace")
	}
	var busy, shared bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE application_id=$1 AND status IN ('queued','running')) OR EXISTS(SELECT 1 FROM managed_resources WHERE application_id=$1) OR EXISTS(SELECT 1 FROM preview_slots p JOIN source_control_connections c ON c.id=p.connection_id WHERE c.application_id=$1),EXISTS(SELECT 1 FROM applications WHERE cluster_id=$2 AND id<>$1 AND namespaces @> jsonb_build_array(jsonb_build_object('namespace',$3::text)))`, app.ID, app.ClusterID, namespace).Scan(&busy, &shared); err != nil {
		return e, false, err
	}
	if busy {
		return e, false, errors.New("namespace cleanup requires completed application resource cleanup and no active operation")
	}
	preserve = preserve || e.Shared || shared || e.OwnerApplicationID != app.ID
	// A started deletion cannot be canceled or changed to retention.
	if e.Phase == "deleting" {
		preserve = false
	}
	if !preserve && e.Phase != "deleted" {
		if _, err = tx.ExecContext(ctx, `UPDATE applications SET decommissioning=TRUE,auto_sync_paused=TRUE WHERE id=$1`, app.ID); err != nil {
			return e, false, err
		}
		e.Phase = "deleting"
		_, err = tx.ExecContext(ctx, `UPDATE managed_namespace_environments SET phase='deleting',updated_at=NOW() WHERE id=$1`, e.ID)
	} else if shared {
		_, err = tx.ExecContext(ctx, `UPDATE managed_namespace_environments SET shared=TRUE WHERE id=$1`, e.ID)
	}
	if err != nil {
		return e, false, err
	}
	return e, preserve, tx.Commit()
}

type ManagedEnvironmentView struct {
	Namespace     string                          `json:"namespace"`
	ApplicationID string                          `json:"applicationId,omitempty"`
	Status        agentprotocol.EnvironmentStatus `json:"status"`
}

func (s *Store) ListManagedEnvironments(ctx context.Context, cluster, workspace string) ([]ManagedEnvironmentView, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT e.namespace,COALESCE(a.id,''),e.phase,e.namespace_uid,e.shared,e.status FROM managed_namespace_environments e LEFT JOIN applications a ON a.id=e.owner_application_id WHERE e.cluster_id=$1 AND e.workspace_id=$2 ORDER BY e.namespace`, cluster, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ManagedEnvironmentView{}
	for rows.Next() {
		var v ManagedEnvironmentView
		var raw []byte
		var phase, uid string
		var shared bool
		if err = rows.Scan(&v.Namespace, &v.ApplicationID, &phase, &uid, &shared, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &v.Status); err != nil {
			return nil, err
		}
		v.Status.Phase = phase
		v.Status.NamespaceUID = uid
		v.Status.Shared = v.Status.Shared || shared
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ManagedEnvironmentsOwnedByApplication(ctx context.Context, application, workspace string) ([]ManagedEnvironment, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+managedEnvironmentColumns+` FROM managed_namespace_environments WHERE owner_application_id=$1 AND workspace_id=$2 ORDER BY cluster_id,namespace`, application, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ManagedEnvironment{}
	for rows.Next() {
		e, err := scanManagedEnvironment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
