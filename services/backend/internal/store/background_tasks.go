package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrTaskRequestMismatch = errors.New("request ID was already used for a different task")

type BackgroundTask struct {
	ID            string          `json:"id"`
	ApplicationID string          `json:"applicationId"`
	ActorID       string          `json:"-"`
	Kind          string          `json:"kind"`
	Target        string          `json:"-"`
	RequestID     string          `json:"-"`
	Input         json.RawMessage `json:"-"`
	Status        string          `json:"status"`
	Result        json.RawMessage `json:"result,omitempty"`
	HTTPStatus    *int            `json:"httpStatus,omitempty"`
	LeaseToken    sql.NullString  `json:"-"`
	LeaseUntil    *time.Time      `json:"-"`
	CreatedAt     time.Time       `json:"createdAt"`
	StartedAt     *time.Time      `json:"startedAt,omitempty"`
	FinishedAt    *time.Time      `json:"finishedAt,omitempty"`
}

const backgroundTaskColumns = `id,application_id,actor_id,kind,target,request_id,input,status,result,http_status,lease_token,lease_until,created_at,started_at,finished_at`

// Listing progress must not repeatedly transfer historical manifests or inputs.
const backgroundTaskSummaryColumns = `id,application_id,actor_id,kind,target,request_id,'{}'::jsonb,status,NULL::jsonb,http_status,NULL::text,NULL::timestamptz,created_at,started_at,finished_at`

func scanBackgroundTask(row interface{ Scan(...any) error }) (BackgroundTask, error) {
	var task BackgroundTask
	var input, result []byte
	err := row.Scan(&task.ID, &task.ApplicationID, &task.ActorID, &task.Kind, &task.Target, &task.RequestID, &input, &task.Status, &result, &task.HTTPStatus, &task.LeaseToken, &task.LeaseUntil, &task.CreatedAt, &task.StartedAt, &task.FinishedAt)
	task.Input, task.Result = input, result
	return task, err
}

// Serialize submission for this actor. Retries of a request return
// its original task; concurrent submissions of the same action share active work.
func (s *Store) EnqueueBackgroundTask(ctx context.Context, task BackgroundTask) (BackgroundTask, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return BackgroundTask{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7416,hashtext($1))`, task.ActorID); err != nil {
		return BackgroundTask{}, err
	}
	existing, err := scanBackgroundTask(tx.QueryRowContext(ctx, `SELECT `+backgroundTaskColumns+` FROM background_tasks WHERE id=(SELECT task_id FROM background_task_requests WHERE actor_id=$1 AND request_id=$2)`, task.ActorID, task.RequestID))
	if err == nil {
		var equal bool
		if err := tx.QueryRowContext(ctx, `SELECT input=$3::jsonb FROM background_task_requests WHERE actor_id=$1 AND request_id=$2`, task.ActorID, task.RequestID, string(task.Input)).Scan(&equal); err != nil {
			return BackgroundTask{}, err
		}
		if existing.ApplicationID != task.ApplicationID || existing.Kind != task.Kind || existing.Target != task.Target || !equal {
			return BackgroundTask{}, ErrTaskRequestMismatch
		}
		return existing, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return BackgroundTask{}, err
	}
	existing, err = scanBackgroundTask(tx.QueryRowContext(ctx, `SELECT `+backgroundTaskColumns+` FROM background_tasks WHERE application_id=$1 AND actor_id=$2 AND kind=$3 AND target=$4 AND status IN ('queued','running')`, task.ApplicationID, task.ActorID, task.Kind, task.Target))
	if err == nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO background_task_requests(actor_id,request_id,task_id,input) VALUES($1,$2,$3,$4)`, task.ActorID, task.RequestID, existing.ID, string(task.Input)); err != nil {
			return BackgroundTask{}, err
		}
		return existing, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return BackgroundTask{}, err
	}
	task, err = scanBackgroundTask(tx.QueryRowContext(ctx, `INSERT INTO background_tasks(id,application_id,actor_id,kind,target,request_id,input) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+backgroundTaskColumns, NewID(), task.ApplicationID, task.ActorID, task.Kind, task.Target, task.RequestID, string(task.Input)))
	if err != nil {
		return BackgroundTask{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO background_task_requests(actor_id,request_id,task_id,input) VALUES($1,$2,$3,$4)`, task.ActorID, task.RequestID, task.ID, string(task.Input)); err != nil {
		return BackgroundTask{}, err
	}
	return task, tx.Commit()
}

func (s *Store) BackgroundTaskByID(ctx context.Context, id string) (BackgroundTask, error) {
	return scanBackgroundTask(s.DB.QueryRowContext(ctx, `SELECT `+backgroundTaskColumns+` FROM background_tasks WHERE id=$1`, id))
}

func (s *Store) ListBackgroundTasks(ctx context.Context, actorID, applicationID, workspaceID string) ([]BackgroundTask, error) {
	// Active work always precedes history, so a busy workspace cannot hide it.
	rows, err := s.DB.QueryContext(ctx, `SELECT `+backgroundTaskSummaryColumns+` FROM background_tasks WHERE actor_id=$1 AND ($2='' OR application_id=$2) AND application_id IN (SELECT id FROM applications WHERE $3='' OR workspace_id=$3) ORDER BY (status IN ('queued','running')) DESC,created_at DESC LIMIT 100`, actorID, applicationID, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []BackgroundTask{}
	for rows.Next() {
		task, err := scanBackgroundTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) ClaimBackgroundTask(ctx context.Context, lease time.Duration) (BackgroundTask, bool, error) {
	task, err := scanBackgroundTask(s.DB.QueryRowContext(ctx, `UPDATE background_tasks SET status='running',started_at=NOW(),lease_token=$1,lease_until=NOW()+($2 * INTERVAL '1 second') WHERE id=(SELECT id FROM background_tasks WHERE status='queued' ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING `+backgroundTaskColumns, NewID(), lease.Seconds()))
	if errors.Is(err, sql.ErrNoRows) {
		return BackgroundTask{}, false, nil
	}
	return task, err == nil, err
}

func (s *Store) RenewBackgroundTask(ctx context.Context, task BackgroundTask, lease time.Duration) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE background_tasks SET lease_until=NOW()+($3 * INTERVAL '1 second') WHERE id=$1 AND lease_token=$2 AND status='running' AND lease_until>NOW()`, task.ID, task.LeaseToken.String, lease.Seconds())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return errors.New("background task lease lost")
	}
	return err
}

func (s *Store) FinishBackgroundTask(ctx context.Context, task BackgroundTask, status int, result json.RawMessage) error {
	state := "succeeded"
	if status >= 400 {
		state = "failed"
	}
	updated, err := s.DB.ExecContext(ctx, `UPDATE background_tasks SET status=$3,http_status=$4,result=$5,finished_at=NOW(),lease_until=NULL WHERE id=$1 AND lease_token=$2 AND status='running' AND lease_until>NOW()`, task.ID, task.LeaseToken.String, state, status, string(result))
	if err != nil {
		return err
	}
	count, err := updated.RowsAffected()
	if err == nil && count != 1 {
		return errors.New("background task lease lost")
	}
	return err
}

// Never replay interrupted approval work: it may already have committed.
func (s *Store) RecoverBackgroundTasks(ctx context.Context, result json.RawMessage) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE background_tasks SET status='failed',http_status=409,result=$1,finished_at=NOW(),lease_until=NULL WHERE status='running' AND lease_until<NOW()`, string(result))
	return err
}
