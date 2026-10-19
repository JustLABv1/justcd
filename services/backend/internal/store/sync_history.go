package store

import (
	"context"
	"time"
)

type SyncHistoryBucket struct {
	StartedAt time.Time `json:"startedAt"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Cancelled int       `json:"cancelled"`
	Active    int       `json:"active"`
}

type SyncHistory struct {
	StartedAt     time.Time           `json:"startedAt"`
	EndedAt       time.Time           `json:"endedAt"`
	BucketSeconds int                 `json:"bucketSeconds"`
	Buckets       []SyncHistoryBucket `json:"buckets"`
}

// SyncHistory aggregates every sync and rollback started in [start, end).
// Counts reflect the current outcome, including runs still queued or running.
// Database aggregation avoids downloading or truncating the operation history.
func (s *Store) SyncHistory(ctx context.Context, applicationID string, start, end time.Time, bucketWidth time.Duration) (SyncHistory, error) {
	return s.syncHistory(ctx, "application_id = $1", applicationID, start, end, bucketWidth)
}

type ApplicationSyncActivity struct {
	ApplicationID string     `json:"applicationId"`
	Name          string     `json:"name"`
	Status        string     `json:"status,omitempty"`
	Type          string     `json:"type,omitempty"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	FinishedAt    *time.Time `json:"finishedAt,omitempty"`
}

type WorkspaceSyncHistory struct {
	SyncHistory
	Applications []ApplicationSyncActivity `json:"applications"`
}

// WorkspaceSyncHistory includes the latest sync or rollback for every application,
// even when that run predates the chart range or the application has never synced.
func (s *Store) WorkspaceSyncHistory(ctx context.Context, workspaceID string, start, end time.Time, bucketWidth time.Duration) (WorkspaceSyncHistory, error) {
	history, err := s.syncHistory(ctx, "application_id IN (SELECT id FROM applications WHERE workspace_id = $1)", workspaceID, start, end, bucketWidth)
	result := WorkspaceSyncHistory{SyncHistory: history, Applications: []ApplicationSyncActivity{}}
	if err != nil {
		return result, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id, a.name, COALESCE(o.status,''), COALESCE(o.operation_type,''), o.started_at, o.finished_at
		FROM applications a LEFT JOIN LATERAL (
			SELECT status, operation_type, started_at, finished_at FROM operations
			WHERE application_id = a.id AND operation_type IN ('sync','rollback')
			ORDER BY started_at DESC, id DESC LIMIT 1
		) o ON true WHERE a.workspace_id = $1
		ORDER BY o.started_at DESC NULLS LAST, a.name, a.id`, workspaceID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var app ApplicationSyncActivity
		if err := rows.Scan(&app.ApplicationID, &app.Name, &app.Status, &app.Type, &app.StartedAt, &app.FinishedAt); err != nil {
			return result, err
		}
		result.Applications = append(result.Applications, app)
	}
	return result, rows.Err()
}

// filter is supplied only by the two scope-specific methods above.
func (s *Store) syncHistory(ctx context.Context, filter, scopeID string, start, end time.Time, bucketWidth time.Duration) (SyncHistory, error) {
	history := SyncHistory{StartedAt: start, EndedAt: end, BucketSeconds: int(bucketWidth.Seconds()), Buckets: []SyncHistoryBucket{}}
	for at := start; at.Before(end); at = at.Add(bucketWidth) {
		history.Buckets = append(history.Buckets, SyncHistoryBucket{StartedAt: at})
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT
		floor(extract(epoch FROM (started_at - $2::timestamptz)) / $4)::int,
		count(*) FILTER (WHERE status = 'succeeded'),
		count(*) FILTER (WHERE status = 'failed'),
		count(*) FILTER (WHERE status = 'cancelled'),
		count(*) FILTER (WHERE status IN ('queued','running'))
		FROM operations WHERE `+filter+` AND started_at >= $2 AND started_at < $3
		AND operation_type IN ('sync','rollback') GROUP BY 1`, scopeID, start, end, history.BucketSeconds)
	if err != nil {
		return history, err
	}
	defer rows.Close()
	for rows.Next() {
		var index int
		var bucket SyncHistoryBucket
		if err := rows.Scan(&index, &bucket.Succeeded, &bucket.Failed, &bucket.Cancelled, &bucket.Active); err != nil {
			return history, err
		}
		if index >= 0 && index < len(history.Buckets) {
			bucket.StartedAt = history.Buckets[index].StartedAt
			history.Buckets[index] = bucket
		}
	}
	return history, rows.Err()
}
