package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/justlab/justcd/services/backend/internal/scm"
)

func reconcileRepositoryPR(ctx context.Context, tx *sql.Tx, app Application) error {
	old, err := scanSourceControlConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM source_control_connections WHERE application_id=$1 FOR UPDATE`, app.ID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	exists := err == nil
	config := app.RepositoryPullRequests
	if config == nil && (!exists || !old.ManagedByGit) {
		return nil
	}
	if config == nil {
		active, err := sourceControlHasActivePreviews(ctx, tx, old.ID)
		if err != nil {
			return err
		}
		if active {
			return ErrActivePreviews
		}
		_, err = tx.ExecContext(ctx, `UPDATE source_control_connections SET enabled=FALSE,managed_by_git=FALSE,updated_at=NOW() WHERE id=$1`, old.ID)
		return err
	}
	var sourceURL string
	if err := tx.QueryRowContext(ctx, `SELECT repository_url FROM git_sources WHERE id=$1`, app.SourceID).Scan(&sourceURL); err != nil {
		return err
	}
	details, err := scm.Details(sourceURL, config.Provider, config.APIURL)
	if err != nil {
		return err
	}
	credentialID := config.CredentialID
	if credentialID == "" && exists && old.StatusCredentialID != nil {
		credentialID = *old.StatusCredentialID
	}
	var kind string
	var expiry sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT kind,expires_at FROM credentials WHERE id=$1 AND (workspace_id IS NULL OR workspace_id=$2) FOR SHARE`, credentialID, app.WorkspaceID).Scan(&kind, &expiry); err != nil || kind != "git-https" || (expiry.Valid && !expiry.Time.After(time.Now())) {
		return errors.New("pullRequests.credentialId must reference an active HTTPS credential available to this workspace")
	}
	profile, err := json.Marshal(config.PreviewProfile)
	if err != nil {
		return err
	}
	if exists {
		active, err := sourceControlHasActivePreviews(ctx, tx, old.ID)
		if err != nil {
			return err
		}
		oldProfile, _ := json.Marshal(old.PreviewProfile)
		if active && (!config.Enabled || old.Provider != details.Provider || old.APIURL != details.APIURL || old.Repository != details.Repository || string(oldProfile) != string(profile)) {
			return ErrActivePreviews
		}
	}
	id := old.ID
	if !exists {
		id = NewID()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO source_control_connections(id,workspace_id,application_id,provider,api_url,repository,status_token_cipher,status_credential_id,preview_profile,enabled,managed_by_git,pipeline_status_reporting)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,TRUE,$11)
 ON CONFLICT(application_id) DO UPDATE SET provider=EXCLUDED.provider,api_url=EXCLUDED.api_url,repository=EXCLUDED.repository,status_credential_id=EXCLUDED.status_credential_id,status_token_cipher=EXCLUDED.status_token_cipher,preview_profile=EXCLUDED.preview_profile,enabled=EXCLUDED.enabled,managed_by_git=TRUE,pipeline_status_reporting=EXCLUDED.pipeline_status_reporting,updated_at=NOW()`, id, app.WorkspaceID, app.ID, details.Provider, details.APIURL, details.Repository, []byte{}, credentialID, string(profile), config.Enabled, config.PipelineStatusReporting)
	if err != nil {
		return fmt.Errorf("PR handling: %w", err)
	}
	if exists {
		if old.PipelineStatusReporting != config.PipelineStatusReporting {
			if _, err := tx.ExecContext(ctx, `UPDATE pull_request_reviews SET reported_phase='' WHERE connection_id=$1`, id); err != nil {
				return err
			}
		}
		oldProfile, _ := json.Marshal(old.PreviewProfile)
		if old.Enabled != config.Enabled || old.Provider != details.Provider || old.APIURL != details.APIURL || old.Repository != details.Repository || string(oldProfile) != string(profile) {
			if _, err := tx.ExecContext(ctx, `UPDATE pull_request_reviews SET phase='pending',processed_sha='',reported_phase='',updated_at=NOW() WHERE connection_id=$1 AND NOT closed AND preview_application_id IS NULL`, id); err != nil {
				return err
			}
		}
	}
	return nil
}
