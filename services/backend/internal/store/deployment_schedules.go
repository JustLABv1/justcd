package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/schedule"
)

type policyQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func deploymentPolicy(ctx context.Context, q policyQuery, scope, id string) (schedule.Policy, error) {
	query := `SELECT policy FROM deployment_policies WHERE workspace_id=$1`
	if scope == "application" {
		query = `SELECT policy FROM application_deployment_policies WHERE application_id=$1`
	}
	p := schedule.Policy{Windows: []schedule.Window{}, Freezes: []schedule.Freeze{}}
	var raw []byte
	err := q.QueryRowContext(ctx, query, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(raw, &p)
	if err == nil {
		err = p.Validate()
	}
	return p, err
}
func (s *Store) DeploymentPolicy(ctx context.Context, scope, id string) (schedule.Policy, error) {
	return deploymentPolicy(ctx, s.DB, scope, id)
}
func applicationSchedule(ctx context.Context, q policyQuery, id string, now time.Time) (schedule.Decision, error) {
	var workspaceID string
	if err := q.QueryRowContext(ctx, `SELECT workspace_id FROM applications WHERE id=$1`, id).Scan(&workspaceID); err != nil {
		return schedule.Decision{}, err
	}
	workspace, err := deploymentPolicy(ctx, q, "workspace", workspaceID)
	if err != nil {
		return schedule.Decision{}, err
	}
	app, err := deploymentPolicy(ctx, q, "application", id)
	if err != nil {
		return schedule.Decision{}, err
	}
	return schedule.Evaluate([]schedule.ScopedPolicy{{Scope: "Workspace", Policy: workspace}, {Scope: "Application", Policy: app}}, now), nil
}
func (s *Store) ApplicationSchedule(ctx context.Context, id string) (schedule.Decision, error) {
	return applicationSchedule(ctx, s.DB, id, time.Now())
}
func writeApplicationPolicy(ctx context.Context, tx *sql.Tx, id string, p schedule.Policy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO application_deployment_policies(application_id,policy) VALUES($1,$2) ON CONFLICT(application_id) DO UPDATE SET policy=EXCLUDED.policy`, id, raw)
	return err
}
func (s *Store) SetDeploymentPolicy(ctx context.Context, scope, id, actor string, p schedule.Policy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if scope == "application" {
		var managed bool
		if err := tx.QueryRowContext(ctx, `SELECT repository_configuration_id IS NOT NULL OR configuration_path<>'' FROM applications WHERE id=$1 FOR UPDATE`, id).Scan(&managed); err != nil {
			return err
		}
		if managed {
			return errors.New("deployment schedule is managed by justcd.yaml; edit spec.deploymentSchedule in Git")
		}
		err = writeApplicationPolicy(ctx, tx, id, p)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO deployment_policies(workspace_id,policy) VALUES($1,$2) ON CONFLICT(workspace_id) DO UPDATE SET policy=EXCLUDED.policy`, id, raw)
	}
	if err != nil {
		return err
	}
	// Wake queued entries so removing a freeze does not wait for a cached date.
	if scope == "application" {
		_, err = tx.ExecContext(ctx, `UPDATE operations SET schedule_check_at=NULL WHERE application_id=$1 AND status='queued'`, id)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE operations SET schedule_check_at=NULL WHERE status='queued' AND application_id IN (SELECT id FROM applications WHERE workspace_id=$1)`, id)
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'deployment.schedule.updated',$2,$3,$4)`, actor, scope, id, raw); err != nil {
		return err
	}
	return tx.Commit()
}

// DeferScheduledOperation retains the durable queue and its consumed approvals.
// It is used when the window closes between claiming and the first write.
func (s *Store) DeferScheduledOperation(ctx context.Context, id string, d schedule.Decision) error {
	raw, err := json.Marshal(d.Reasons)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE operations SET status='queued',schedule_deferred=TRUE,progress=jsonb_set(progress,'{phase}','"queued"'),message='Waiting for deployment schedule',waiting_reasons=$2,next_allowed_at=$3,schedule_check_at=NOW()+INTERVAL '30 seconds' WHERE id=$1 AND status='running'`, id, raw, d.NextAllowedAt)
	return err
}

var ErrScheduleOverrideForbidden = errors.New("emergency schedule override requires a current workspace owner")

func scheduleOwner(ctx context.Context, q policyQuery, workspaceID, actor string) error {
	var allowed bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u WHERE u.id=$2 AND NOT u.disabled AND u.deleted_at IS NULL AND (u.is_admin OR EXISTS(SELECT 1 FROM workspace_memberships m WHERE m.workspace_id=$1 AND m.user_id=u.id AND m.role='owner') OR EXISTS(SELECT 1 FROM oidc_membership_grants m WHERE m.workspace_id=$1 AND m.user_id=u.id AND m.role='owner')))`, workspaceID, actor).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrScheduleOverrideForbidden
	}
	return nil
}
func (s *Store) CheckScheduleOverrideOwner(ctx context.Context, workspaceID, actor string) error {
	return scheduleOwner(ctx, s.DB, workspaceID, actor)
}
func (s *Store) OverrideOperationSchedule(ctx context.Context, id, actor, reason string) error {
	if len(reason) > 1000 || strings.TrimSpace(reason) == "" {
		return errors.New("emergency override reason is required (at most 1000 characters)")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var appID, workspaceID, status, planID, digest string
	if err := tx.QueryRowContext(ctx, `SELECT o.application_id,a.workspace_id,o.status,COALESCE(o.plan_id,''),COALESCE(p.digest,'') FROM operations o JOIN applications a ON a.id=o.application_id LEFT JOIN plans p ON p.id=o.plan_id WHERE o.id=$1 FOR UPDATE OF o`, id).Scan(&appID, &workspaceID, &status, &planID, &digest); err != nil {
		return err
	}
	if err := scheduleOwner(ctx, tx, workspaceID, actor); err != nil {
		return err
	}
	if status != "queued" {
		return errors.New("only queued operations can receive a schedule override")
	}
	decision, err := applicationSchedule(ctx, tx, appID, time.Now())
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE operations SET schedule_override_reason=$2,schedule_override_actor_id=$3,schedule_check_at=NULL,waiting_reasons='[]',next_allowed_at=NULL,message='Emergency schedule override queued' WHERE id=$1`, id, strings.TrimSpace(reason), actor); err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{"operationId": id, "planId": planID, "digest": digest, "reason": strings.TrimSpace(reason), "restrictions": decision.Reasons, "workspaceId": workspaceID})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'deployment.schedule.overridden','application',$2,$3)`, actor, appID, details); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ScheduledQueuedOperation(ctx context.Context, applicationID string) (Operation, error) {
	var id string
	if err := s.DB.QueryRowContext(ctx, `SELECT id FROM operations WHERE application_id=$1 AND status='queued' AND schedule_deferred ORDER BY started_at LIMIT 1`, applicationID).Scan(&id); err != nil {
		return Operation{}, err
	}
	return s.OperationByID(ctx, id)
}

// CancelQueuedOperation races safely with workers: a claimed entry is never cancelled.
func (s *Store) CancelQueuedOperation(ctx context.Context, id, actor, reason string) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var appID string
	if err := tx.QueryRowContext(ctx, `SELECT application_id FROM operations WHERE id=$1`, id).Scan(&appID); err != nil {
		return false, err
	}
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id=$1 FOR UPDATE`, appID).Scan(&locked); err != nil {
		return false, err
	}
	cancelled, err := cancelQueuedOperationTx(ctx, tx, id, actor, reason, appID)
	if err != nil || !cancelled {
		return cancelled, err
	}
	return true, tx.Commit()
}
func cancelQueuedOperationTx(ctx context.Context, tx *sql.Tx, id, actor, reason, appID string) (bool, error) {
	result, err := tx.ExecContext(ctx, `UPDATE operations SET status='cancelled',message=$2,finished_at=NOW(),waiting_reasons='[]',next_allowed_at=NULL WHERE id=$1 AND status='queued'`, id, reason)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE id=(SELECT plan_id FROM operations WHERE id=$1)`, id); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM operation_leases WHERE operation_id=$1`, id); err != nil {
		return false, err
	}
	details, _ := json.Marshal(map[string]any{"operationId": id, "reason": reason})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'deployment.queue.cancelled','application',$2,$3)`, actor, appID, details); err != nil {
		return false, err
	}
	return true, nil
}

// Git changes must not be held hostage by an indefinitely frozen queue entry.
func cancelScheduledForConfiguration(ctx context.Context, tx *sql.Tx, appID string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM operations WHERE application_id=$1 AND status='queued' AND schedule_deferred LIMIT 1`, appID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = cancelQueuedOperationTx(ctx, tx, id, "justcd-system", "Git definition changed while waiting; a fresh plan is required", appID)
	return err
}
