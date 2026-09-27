package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// PreviewProfile is deliberately explicit: application owners provide a preview
// overlay and its dependency policy before JustCD may deploy untrusted code.
type PreviewProfile struct {
	Enabled          bool     `json:"enabled"`
	ManifestPath     string   `json:"manifestPath,omitempty"`
	NamespacePrefix  string   `json:"namespacePrefix,omitempty"`
	HostSuffix       string   `json:"hostSuffix,omitempty"`
	HelmValuesYAML   string   `json:"helmValuesYaml,omitempty"`
	HelmValuesFiles  []string `json:"helmValuesFiles,omitempty"`
	AllowedSecrets   []string `json:"allowedSecrets,omitempty"`
	DatabaseStrategy string   `json:"databaseStrategy,omitempty"`
	MaxActive        int      `json:"maxActive,omitempty"`
	MaxLifetimeHours int      `json:"maxLifetimeHours,omitempty"`
	AllowForks       bool     `json:"allowForks,omitempty"`
	QuotaCPU         string   `json:"quotaCpu,omitempty"`
	QuotaMemory      string   `json:"quotaMemory,omitempty"`
}

type SourceControlConnection struct {
	ID                  string         `json:"id"`
	WorkspaceID         string         `json:"workspaceId"`
	ApplicationID       string         `json:"applicationId"`
	Provider            string         `json:"provider"`
	APIURL              string         `json:"apiUrl"`
	Repository          string         `json:"repository"`
	WebhookSecretCipher []byte         `json:"-"`
	StatusTokenCipher   []byte         `json:"-"`
	PreviewProfile      PreviewProfile `json:"previewProfile"`
	CreatedAt           time.Time      `json:"createdAt"`
}

const connectionColumns = `id,workspace_id,application_id,provider,api_url,repository,webhook_secret_cipher,status_token_cipher,preview_profile,created_at`

func scanSourceControlConnection(row interface{ Scan(...any) error }) (SourceControlConnection, error) {
	var c SourceControlConnection
	var profile []byte
	err := row.Scan(&c.ID, &c.WorkspaceID, &c.ApplicationID, &c.Provider, &c.APIURL, &c.Repository, &c.WebhookSecretCipher, &c.StatusTokenCipher, &profile, &c.CreatedAt)
	if err == nil {
		err = json.Unmarshal(profile, &c.PreviewProfile)
	}
	return c, err
}

func (s *Store) SaveSourceControlConnection(ctx context.Context, c SourceControlConnection) error {
	profile, err := json.Marshal(c.PreviewProfile)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO source_control_connections(id,workspace_id,application_id,provider,api_url,repository,webhook_secret_cipher,status_token_cipher,preview_profile)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)
		ON CONFLICT(application_id) DO UPDATE SET provider=EXCLUDED.provider,api_url=EXCLUDED.api_url,repository=EXCLUDED.repository,
		webhook_secret_cipher=EXCLUDED.webhook_secret_cipher,status_token_cipher=EXCLUDED.status_token_cipher,
		preview_profile=EXCLUDED.preview_profile,updated_at=NOW()`,
		c.ID, c.WorkspaceID, c.ApplicationID, c.Provider, c.APIURL, c.Repository, c.WebhookSecretCipher, c.StatusTokenCipher, string(profile))
	return err
}

func (s *Store) SourceControlConnectionByID(ctx context.Context, id string) (SourceControlConnection, error) {
	return scanSourceControlConnection(s.DB.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM source_control_connections WHERE id=$1`, id))
}

func (s *Store) SourceControlConnectionByApplication(ctx context.Context, id string) (SourceControlConnection, error) {
	return scanSourceControlConnection(s.DB.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM source_control_connections WHERE application_id=$1`, id))
}

type PullRequestReview struct {
	ID                   string          `json:"id"`
	ConnectionID         string          `json:"connectionId"`
	Number               int             `json:"number"`
	HeadSHA              string          `json:"headSha"`
	SourceURL            string          `json:"sourceUrl"`
	Fork                 bool            `json:"fork"`
	Closed               bool            `json:"closed"`
	Phase                string          `json:"phase"`
	Plan                 json.RawMessage `json:"plan,omitempty"`
	Error                string          `json:"error,omitempty"`
	ReportError          string          `json:"reportError,omitempty"`
	PreviewApplicationID *string         `json:"previewApplicationId,omitempty"`
	ExpiresAt            *time.Time      `json:"expiresAt,omitempty"`
	ProcessedSHA         string          `json:"processedSha,omitempty"`
	ReportedPhase        string          `json:"-"`
	EventAt              time.Time       `json:"-"`
	UpdatedAt            time.Time       `json:"updatedAt"`
}

const reviewColumns = `id,connection_id,number,head_sha,source_url,fork,closed,phase,plan,error,report_error,preview_application_id,expires_at,processed_sha,reported_phase,event_at,updated_at`

func scanReview(row interface{ Scan(...any) error }) (PullRequestReview, error) {
	var v PullRequestReview
	var plan []byte
	err := row.Scan(&v.ID, &v.ConnectionID, &v.Number, &v.HeadSHA, &v.SourceURL, &v.Fork, &v.Closed, &v.Phase, &plan, &v.Error, &v.ReportError, &v.PreviewApplicationID, &v.ExpiresAt, &v.ProcessedSHA, &v.ReportedPhase, &v.EventAt, &v.UpdatedAt)
	if err == nil {
		v.Plan = plan
	}
	return v, err
}

func (s *Store) ReviewByID(ctx context.Context, id string) (PullRequestReview, error) {
	return scanReview(s.DB.QueryRowContext(ctx, `SELECT `+reviewColumns+` FROM pull_request_reviews WHERE id=$1`, id))
}

func (s *Store) ReviewByPreviewApplication(ctx context.Context, applicationID string) (PullRequestReview, SourceControlConnection, error) {
	review, err := scanReview(s.DB.QueryRowContext(ctx, `SELECT `+reviewColumns+` FROM pull_request_reviews WHERE preview_application_id=$1`, applicationID))
	if err != nil {
		return PullRequestReview{}, SourceControlConnection{}, err
	}
	connection, err := s.SourceControlConnectionByID(ctx, review.ConnectionID)
	return review, connection, err
}

func (s *Store) ListReviews(ctx context.Context, connectionID string) ([]PullRequestReview, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+reviewColumns+` FROM pull_request_reviews WHERE connection_id=$1 ORDER BY updated_at DESC LIMIT 100`, connectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PullRequestReview{}
	for rows.Next() {
		item, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// RecordReviewEvent commits the delivery and the latest PR state together.
// Provider timestamps prevent old deliveries from reversing a newer event.
func (s *Store) RecordReviewEvent(ctx context.Context, connectionID, deliveryID string, v PullRequestReview) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO source_control_deliveries(connection_id,delivery_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, connectionID, deliveryID)
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if inserted == 0 {
		return false, tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO pull_request_reviews(id,connection_id,number,head_sha,source_url,fork,closed,phase,event_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,'pending',$8)
		ON CONFLICT(connection_id,number) DO UPDATE SET head_sha=EXCLUDED.head_sha,source_url=EXCLUDED.source_url,
		fork=EXCLUDED.fork,closed=EXCLUDED.closed,phase='pending',plan=NULL,error='',report_error='',reported_phase='',event_at=EXCLUDED.event_at,updated_at=NOW()
		WHERE pull_request_reviews.event_at <= EXCLUDED.event_at`,
		v.ID, connectionID, v.Number, v.HeadSHA, v.SourceURL, v.Fork, v.Closed, v.EventAt)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) DueReviews(ctx context.Context, limit int) ([]PullRequestReview, error) {
	rows, err := s.DB.QueryContext(ctx, `UPDATE pull_request_reviews SET lease_until=NOW()+INTERVAL '5 minutes'
		WHERE id IN (SELECT id FROM pull_request_reviews
		WHERE (lease_until IS NULL OR lease_until<NOW()) AND (
		phase='pending' OR (expires_at<=NOW() AND NOT closed AND phase NOT IN ('cleanup_pending','removed'))
		OR (phase IN ('approval_required','syncing','ready','degraded','cleanup_pending') AND updated_at < NOW()-INTERVAL '30 seconds')
		OR (phase='failed' AND updated_at < NOW()-INTERVAL '60 seconds')
		OR (reported_phase<>phase AND updated_at < NOW()-INTERVAL '30 seconds'))
		ORDER BY updated_at LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING `+reviewColumns, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PullRequestReview{}
	for rows.Next() {
		item, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ReleaseReviewLease(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE pull_request_reviews SET lease_until=NULL WHERE id=$1`, id)
	return err
}

// UpdateReviewResult is compare-and-swap: a late planner cannot publish a result
// for a SHA that has since been superseded or closed.
func (s *Store) UpdateReviewResult(ctx context.Context, v PullRequestReview, expectedSHA string, expectedClosed bool) (bool, error) {
	result, err := s.DB.ExecContext(ctx, `UPDATE pull_request_reviews SET phase=$2,plan=$3::jsonb,error=$4,preview_application_id=$5,
		expires_at=$6,processed_sha=$7,updated_at=NOW() WHERE id=$1 AND head_sha=$8 AND closed=$9`,
		v.ID, v.Phase, nullableJSON(v.Plan), v.Error, v.PreviewApplicationID, v.ExpiresAt, v.ProcessedSHA, expectedSHA, expectedClosed)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func (s *Store) MarkReviewReported(ctx context.Context, id, phase string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE pull_request_reviews SET reported_phase=$2,report_error='' WHERE id=$1`, id, phase)
	return err
}

func (s *Store) SetReviewReportError(ctx context.Context, id, message string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE pull_request_reviews SET report_error=$2 WHERE id=$1`, id, message)
	return err
}

func (s *Store) SetReviewWorkerError(ctx context.Context, id, message string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE pull_request_reviews SET phase='failed',error=$2,updated_at=NOW() WHERE id=$1`, id, message)
	return err
}

func (s *Store) ActivePreviewCount(ctx context.Context, connectionID string) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pull_request_reviews WHERE connection_id=$1 AND preview_application_id IS NOT NULL AND phase NOT IN ('removed','cleanup_pending')`, connectionID).Scan(&count)
	return count, err
}

func (s *Store) ReservePreviewSlot(ctx context.Context, connectionID string, number, maxActive int) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM source_control_connections WHERE id=$1 FOR UPDATE`, connectionID).Scan(&id); err != nil {
		return false, err
	}
	var existing bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM preview_slots WHERE connection_id=$1 AND number=$2)`, connectionID, number).Scan(&existing); err != nil {
		return false, err
	}
	if existing {
		return false, tx.Commit()
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM preview_slots WHERE connection_id=$1`, connectionID).Scan(&count); err != nil {
		return false, err
	}
	if count >= maxActive {
		return false, ErrPreviewQuotaReached
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO preview_slots(connection_id,number) VALUES($1,$2)`, connectionID, number); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

var ErrPreviewQuotaReached = errors.New("preview quota reached")

func (s *Store) ReleasePreviewSlot(ctx context.Context, connectionID string, number int) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM preview_slots WHERE connection_id=$1 AND number=$2`, connectionID, number)
	return err
}

func (s *Store) PreviewSlotCount(ctx context.Context, connectionID string) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM preview_slots WHERE connection_id=$1`, connectionID).Scan(&count)
	return count, err
}

func (s *Store) DeletePreviewNamespaceBinding(ctx context.Context, workspaceID, clusterID, namespace string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM namespace_bindings WHERE workspace_id=$1 AND cluster_id=$2 AND namespace=$3`, workspaceID, clusterID, namespace)
	return err
}

var _ = sql.ErrNoRows
