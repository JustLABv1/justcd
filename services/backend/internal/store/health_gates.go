package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	DefaultHealthGateTimeoutSeconds = 300
	HealthGateMarkFailed            = "mark-failed"
	HealthGatePrepareRollback       = "prepare-rollback"
)

// HealthGate is the Git-declared post-deploy verification policy.
type HealthGate struct {
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
	OnFailure      string `json:"onFailure,omitempty"`
}

// Normalize fills defaults and validates declared values.
func (g HealthGate) Normalize() (HealthGate, error) {
	if g.TimeoutSeconds == 0 {
		g.TimeoutSeconds = DefaultHealthGateTimeoutSeconds
	}
	if g.TimeoutSeconds < 30 || g.TimeoutSeconds > 3600 {
		return g, errors.New("healthGate.timeoutSeconds must be between 30 and 3600")
	}
	if g.OnFailure == "" {
		g.OnFailure = HealthGateMarkFailed
	}
	if g.OnFailure != HealthGateMarkFailed && g.OnFailure != HealthGatePrepareRollback {
		return g, errors.New("healthGate.onFailure must be mark-failed or prepare-rollback")
	}
	return g, nil
}

func writeApplicationHealthGate(ctx context.Context, tx *sql.Tx, applicationID string, gate *HealthGate) error {
	if gate == nil {
		_, err := tx.ExecContext(ctx, `DELETE FROM application_health_gates WHERE application_id=$1`, applicationID)
		return err
	}
	normalized, err := gate.Normalize()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO application_health_gates(application_id,timeout_seconds,on_failure) VALUES($1,$2,$3)
		ON CONFLICT(application_id) DO UPDATE SET timeout_seconds=EXCLUDED.timeout_seconds,on_failure=EXCLUDED.on_failure`, applicationID, normalized.TimeoutSeconds, normalized.OnFailure)
	return err
}

// ApplicationHealthGate returns nil when the application declares no gate.
func (s *Store) ApplicationHealthGate(ctx context.Context, applicationID string) (*HealthGate, error) {
	var g HealthGate
	err := s.DB.QueryRowContext(ctx, `SELECT timeout_seconds,on_failure FROM application_health_gates WHERE application_id=$1`, applicationID).Scan(&g.TimeoutSeconds, &g.OnFailure)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &g, err
}

type HealthGateCheck struct {
	OperationID        string     `json:"operationId"`
	ApplicationID      string     `json:"applicationId"`
	Revision           string     `json:"revision,omitempty"`
	Status             string     `json:"status"`
	StartedAt          time.Time  `json:"startedAt"`
	Deadline           time.Time  `json:"deadline"`
	FinishedAt         *time.Time `json:"finishedAt,omitempty"`
	Message            string     `json:"message,omitempty"`
	OnFailure          string     `json:"onFailure"`
	GitLabDeploymentID int64      `json:"gitlabDeploymentId,omitempty"`
	RollbackPlanID     string     `json:"rollbackPlanId,omitempty"`
}

const healthGateColumns = `operation_id,application_id,revision,status,started_at,deadline,finished_at,message,on_failure,gitlab_deployment_id,rollback_plan_id`

func scanHealthGateCheck(row interface{ Scan(...any) error }) (HealthGateCheck, error) {
	var c HealthGateCheck
	err := row.Scan(&c.OperationID, &c.ApplicationID, &c.Revision, &c.Status, &c.StartedAt, &c.Deadline, &c.FinishedAt, &c.Message, &c.OnFailure, &c.GitLabDeploymentID, &c.RollbackPlanID)
	return c, err
}

// StartHealthGateCheck opens a pending check for a finished sync and supersedes
// any earlier pending check of the same application, returning the superseded
// ones so the caller can close their provider-side records.
func (s *Store) StartHealthGateCheck(ctx context.Context, c HealthGateCheck) ([]HealthGateCheck, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `UPDATE health_gate_checks SET status='superseded',finished_at=NOW(),message='A newer deployment replaced this check' WHERE application_id=$1 AND status='pending' RETURNING `+healthGateColumns, c.ApplicationID)
	if err != nil {
		return nil, err
	}
	var superseded []HealthGateCheck
	for rows.Next() {
		old, err := scanHealthGateCheck(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		superseded = append(superseded, old)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO health_gate_checks(operation_id,application_id,revision,status,deadline,on_failure,gitlab_deployment_id) VALUES($1,$2,$3,'pending',$4,$5,$6)
		ON CONFLICT(operation_id) DO NOTHING`, c.OperationID, c.ApplicationID, c.Revision, c.Deadline, c.OnFailure, c.GitLabDeploymentID); err != nil {
		return nil, err
	}
	return superseded, tx.Commit()
}

func (s *Store) PendingHealthGateChecks(ctx context.Context, limit int) ([]HealthGateCheck, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+healthGateColumns+` FROM health_gate_checks WHERE status='pending' ORDER BY started_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []HealthGateCheck
	for rows.Next() {
		c, err := scanHealthGateCheck(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

// ResolveHealthGateCheck records the verdict exactly once. It reports false when
// another worker or a newer deployment already resolved the check.
func (s *Store) ResolveHealthGateCheck(ctx context.Context, operationID, status, message string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE health_gate_checks SET status=$2,message=$3,finished_at=NOW() WHERE operation_id=$1 AND status='pending'`, operationID, status, message)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) SetHealthGateRollbackPlan(ctx context.Context, operationID, planID, message string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE health_gate_checks SET rollback_plan_id=$2,message=CASE WHEN $3='' THEN message ELSE $3 END WHERE operation_id=$1`, operationID, planID, message)
	return err
}

func (s *Store) LatestHealthGateCheck(ctx context.Context, applicationID string) (*HealthGateCheck, error) {
	c, err := scanHealthGateCheck(s.DB.QueryRowContext(ctx, `SELECT `+healthGateColumns+` FROM health_gate_checks WHERE application_id=$1 AND status<>'superseded' ORDER BY started_at DESC LIMIT 1`, applicationID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}

// NotifyHealthGateFailure raises the standard "unhealthy" notification. The
// event ID is derived from the operation so a retry cannot notify twice.
func (s *Store) NotifyHealthGateFailure(ctx context.Context, applicationID, operationID string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO notification_events(id,workspace_id,application_id,resource_id,event_type,severity)
		SELECT 'health_gate:'||$2,workspace_id,id,$2,'unhealthy','error' FROM applications WHERE id=$1 ON CONFLICT(id) DO NOTHING`, applicationID, operationID)
	return err
}

// SupersedePendingHealthGateChecks closes every pending check of an application.
func (s *Store) SupersedePendingHealthGateChecks(ctx context.Context, applicationID string) ([]HealthGateCheck, error) {
	rows, err := s.DB.QueryContext(ctx, `UPDATE health_gate_checks SET status='superseded',finished_at=NOW(),message='A newer operation replaced this check' WHERE application_id=$1 AND status='pending' RETURNING `+healthGateColumns, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []HealthGateCheck
	for rows.Next() {
		c, err := scanHealthGateCheck(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
