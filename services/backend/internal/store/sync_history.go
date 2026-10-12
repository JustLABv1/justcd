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
		FROM operations WHERE application_id = $1 AND started_at >= $2 AND started_at < $3
		AND operation_type IN ('sync','rollback') GROUP BY 1`, applicationID, start, end, history.BucketSeconds)
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
