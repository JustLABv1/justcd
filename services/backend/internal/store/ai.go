package store

import (
	"context"
	"database/sql"
	"github.com/justlab/justcd/services/backend/internal/ai"
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
