package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/justlab/justcd/services/backend/internal/ai"
	"time"
)

func (s *Store) AISettings(ctx context.Context) (ai.Settings, []byte, error) {
	var settings ai.Settings
	var cipher []byte
	err := s.DB.QueryRowContext(ctx, `SELECT enabled,base_url,model,api_key_cipher,max_tokens FROM ai_settings WHERE singleton=TRUE`).Scan(&settings.Enabled, &settings.BaseURL, &settings.Model, &cipher, &settings.MaxTokens)
	return settings, cipher, err
}
func (s *Store) SaveAISettings(ctx context.Context, settings ai.Settings, cipher []byte, actor string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE ai_settings SET enabled=$1,base_url=$2,model=$3,api_key_cipher=$4,max_tokens=$5 WHERE singleton=TRUE`, settings.Enabled, settings.BaseURL, settings.Model, cipher, settings.MaxTokens)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'ai.settings_updated','instance','instance',jsonb_build_object('enabled',$2::boolean,'model',$3::text))`, actor, settings.Enabled, settings.Model)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) WorkspaceAIEnabled(ctx context.Context, id string) (bool, error) {
	var enabled bool
	err := s.DB.QueryRowContext(ctx, `SELECT enabled FROM workspace_ai_settings WHERE workspace_id=$1`, id).Scan(&enabled)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return enabled, err
}
func (s *Store) SetWorkspaceAI(ctx context.Context, id, actor string, enabled bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO workspace_ai_settings(workspace_id,enabled) VALUES($1,$2) ON CONFLICT(workspace_id) DO UPDATE SET enabled=EXCLUDED.enabled`, id, enabled)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,workspace_id,details) VALUES($1,'ai.workspace_updated','workspace',$2,$2,jsonb_build_object('enabled',$3::boolean))`, actor, id, enabled)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// WorkspaceAIAutoTriage reports whether background triage is enabled for the workspace.
func (s *Store) WorkspaceAIAutoTriage(ctx context.Context, id string) (bool, error) {
	var enabled bool
	err := s.DB.QueryRowContext(ctx, `SELECT enabled AND auto_triage FROM workspace_ai_settings WHERE workspace_id=$1`, id).Scan(&enabled)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return enabled, err
}

func (s *Store) SetWorkspaceAIAutoTriage(ctx context.Context, id, actor string, enabled bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO workspace_ai_settings(workspace_id,auto_triage) VALUES($1,$2) ON CONFLICT(workspace_id) DO UPDATE SET auto_triage=EXCLUDED.auto_triage`, id, enabled)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,workspace_id,details) VALUES($1,'ai.triage_policy_updated','workspace',$2,$2,jsonb_build_object('autoTriage',$3::boolean))`, actor, id, enabled)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// AutoTriageWorkspaces lists workspaces where AI and background triage are both enabled.
func (s *Store) AutoTriageWorkspaces(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT workspace_id FROM workspace_ai_settings WHERE enabled AND auto_triage ORDER BY workspace_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type AITriageRecord struct {
	InputsHash  string
	Trigger     string
	Model       string
	Result      json.RawMessage
	GeneratedAt time.Time
}

func (s *Store) SaveAITriage(ctx context.Context, workspaceID string, record AITriageRecord) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO ai_triage_results(workspace_id,inputs_hash,trigger,model,result,generated_at) VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(workspace_id) DO UPDATE SET inputs_hash=EXCLUDED.inputs_hash,trigger=EXCLUDED.trigger,model=EXCLUDED.model,result=EXCLUDED.result,generated_at=EXCLUDED.generated_at`,
		workspaceID, record.InputsHash, record.Trigger, record.Model, []byte(record.Result), record.GeneratedAt)
	return err
}

func (s *Store) AITriage(ctx context.Context, workspaceID string) (AITriageRecord, bool, error) {
	var record AITriageRecord
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT inputs_hash,trigger,model,result,generated_at FROM ai_triage_results WHERE workspace_id=$1`, workspaceID).
		Scan(&record.InputsHash, &record.Trigger, &record.Model, &raw, &record.GeneratedAt)
	if err == sql.ErrNoRows {
		return record, false, nil
	}
	record.Result = raw
	return record, err == nil, err
}

type AIPlanReviewRecord struct {
	PlanDigest  string
	Model       string
	Result      json.RawMessage
	GeneratedAt time.Time
}

func (s *Store) SaveAIPlanReview(ctx context.Context, planID string, record AIPlanReviewRecord) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO ai_plan_reviews(plan_id,plan_digest,model,result,generated_at) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(plan_id) DO UPDATE SET plan_digest=EXCLUDED.plan_digest,model=EXCLUDED.model,result=EXCLUDED.result,generated_at=EXCLUDED.generated_at`,
		planID, record.PlanDigest, record.Model, []byte(record.Result), record.GeneratedAt)
	return err
}

func (s *Store) AIPlanReview(ctx context.Context, planID string) (AIPlanReviewRecord, bool, error) {
	var record AIPlanReviewRecord
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT plan_digest,model,result,generated_at FROM ai_plan_reviews WHERE plan_id=$1`, planID).Scan(&record.PlanDigest, &record.Model, &raw, &record.GeneratedAt)
	if err == sql.ErrNoRows {
		return record, false, nil
	}
	record.Result = raw
	return record, err == nil, err
}

type AIDigestRecord struct {
	Model       string
	Result      json.RawMessage
	GeneratedAt time.Time
}

func (s *Store) SaveAIDigest(ctx context.Context, workspaceID, period string, record AIDigestRecord) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO ai_digests(workspace_id,period,model,result,generated_at) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(workspace_id,period) DO UPDATE SET model=EXCLUDED.model,result=EXCLUDED.result,generated_at=EXCLUDED.generated_at`,
		workspaceID, period, record.Model, []byte(record.Result), record.GeneratedAt)
	return err
}

func (s *Store) AIDigest(ctx context.Context, workspaceID, period string) (AIDigestRecord, bool, error) {
	var record AIDigestRecord
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT model,result,generated_at FROM ai_digests WHERE workspace_id=$1 AND period=$2`, workspaceID, period).Scan(&record.Model, &raw, &record.GeneratedAt)
	if err == sql.ErrNoRows {
		return record, false, nil
	}
	record.Result = raw
	return record, err == nil, err
}
