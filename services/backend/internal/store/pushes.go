package store

import (
	"context"
	"encoding/json"
	"strings"
)

type GitPushTrigger struct {
	ApplicationID string `json:"-"`
	SourceKey     string `json:"sourceKey"`
	Provider      string `json:"provider"`
	DeliveryID    string `json:"deliveryId"`
	Ref           string `json:"ref"`
	ReportedSHA   string `json:"reportedCommit"`
}

// RecordPushEvent atomically deduplicates a delivery and wakes every matching
// application, including children of application groups using the same source.
func (s *Store) RecordPushEvent(ctx context.Context, projectID, sourceID, sourceKey, provider, deliveryID, ref, sha string) (bool, int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM git_push_deliveries WHERE received_at < NOW()-INTERVAL '30 days'`); err != nil {
		return false, 0, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO git_push_deliveries(source_key,delivery_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, sourceKey, deliveryID)
	if err != nil {
		return false, 0, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, 0, err
	}
	if inserted == 0 {
		return false, 0, tx.Commit()
	}
	branch := strings.TrimPrefix(ref, "refs/heads/")
	result, err = tx.ExecContext(ctx, `INSERT INTO git_push_triggers(application_id,source_key,provider,delivery_id,ref,reported_sha)
		SELECT a.id,$1,$2,$3,$4,$5 FROM applications a
		JOIN git_sources candidate ON candidate.id=a.source_id
		JOIN git_sources origin ON origin.id=$6
		WHERE a.project_id=$7 AND candidate.project_id=$7 AND origin.project_id=$7
		AND candidate.repository_url=origin.repository_url AND NOT a.decommissioning
		AND (a.revision=$4 OR a.revision=$8)
		ON CONFLICT(application_id) DO UPDATE SET source_key=EXCLUDED.source_key,provider=EXCLUDED.provider,delivery_id=EXCLUDED.delivery_id,
		ref=EXCLUDED.ref,reported_sha=EXCLUDED.reported_sha,received_at=NOW()`, sourceKey, provider, deliveryID, ref, sha, sourceID, projectID, branch)
	if err != nil {
		return false, 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, 0, err
	}
	return true, int(count), tx.Commit()
}

func (s *Store) GitPushTrigger(ctx context.Context, applicationID string) (GitPushTrigger, error) {
	var trigger GitPushTrigger
	err := s.DB.QueryRowContext(ctx, `SELECT application_id,source_key,provider,delivery_id,ref,reported_sha FROM git_push_triggers WHERE application_id=$1`, applicationID).
		Scan(&trigger.ApplicationID, &trigger.SourceKey, &trigger.Provider, &trigger.DeliveryID, &trigger.Ref, &trigger.ReportedSHA)
	return trigger, err
}

func (s *Store) CompleteGitPushTrigger(ctx context.Context, trigger GitPushTrigger) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM git_push_triggers WHERE application_id=$1 AND source_key=$2 AND delivery_id=$3`, trigger.ApplicationID, trigger.SourceKey, trigger.DeliveryID)
	return err
}

func (s *Store) AttachGitPushTrigger(ctx context.Context, planID string, trigger GitPushTrigger) error {
	value, err := json.Marshal(trigger)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE plans SET trigger_info=$2::jsonb WHERE id=$1`, planID, string(value))
	return err
}

func (s *Store) SaveGitPushWebhook(ctx context.Context, sourceID string, cipher []byte) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO git_push_webhooks(source_id,secret_cipher) VALUES($1,$2) ON CONFLICT(source_id) DO UPDATE SET secret_cipher=EXCLUDED.secret_cipher,updated_at=NOW()`, sourceID, cipher)
	return err
}

func (s *Store) GitPushWebhookSecret(ctx context.Context, sourceID string) ([]byte, error) {
	var cipher []byte
	err := s.DB.QueryRowContext(ctx, `SELECT secret_cipher FROM git_push_webhooks WHERE source_id=$1`, sourceID).Scan(&cipher)
	return cipher, err
}
