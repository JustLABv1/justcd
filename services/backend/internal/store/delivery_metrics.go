package store

import (
	"context"
	"database/sql"
	"time"
)

// DeliveryPeriod summarises the syncs and rollbacks of a workspace that started in one period.
type DeliveryPeriod struct {
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	// Deployments counts succeeded syncs and rollbacks.
	Deployments int `json:"deployments"`
	// Failed counts failed syncs and rollbacks. Cancelled runs are neither.
	Failed int `json:"failed"`
	// RestoreSeconds is the median time from the first failure of a run of
	// failures to the next successful deployment of the same application.
	// Nil when no failure in the period has been followed by a success yet.
	RestoreSeconds *float64 `json:"restoreSeconds,omitempty"`
	// Restored counts the failures that RestoreSeconds is taken over.
	Restored int `json:"restored"`
}

type ApplicationFailures struct {
	ApplicationID string `json:"applicationId"`
	Name          string `json:"name"`
	Failed        int    `json:"failed"`
	Total         int    `json:"total"`
}

type DeliveryMetrics struct {
	Current               DeliveryPeriod        `json:"current"`
	Previous              DeliveryPeriod        `json:"previous"`
	FailuresByApplication []ApplicationFailures `json:"failuresByApplication"`
}

const workspaceOperations = `application_id IN (SELECT id FROM applications WHERE workspace_id = $1) AND operation_type IN ('sync','rollback')`

// WorkspaceDeliveryMetrics compares [end-period, end) with the period before it.
func (s *Store) WorkspaceDeliveryMetrics(ctx context.Context, workspaceID string, end time.Time, period time.Duration) (DeliveryMetrics, error) {
	result := DeliveryMetrics{FailuresByApplication: []ApplicationFailures{}}
	var err error
	if result.Current, err = s.deliveryPeriod(ctx, workspaceID, end.Add(-period), end); err != nil {
		return result, err
	}
	if result.Previous, err = s.deliveryPeriod(ctx, workspaceID, end.Add(-2*period), end.Add(-period)); err != nil {
		return result, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id, a.name, count(*) FILTER (WHERE o.status = 'failed'), count(*)
		FROM operations o JOIN applications a ON a.id = o.application_id
		WHERE a.workspace_id = $1 AND o.operation_type IN ('sync','rollback') AND o.started_at >= $2 AND o.started_at < $3
		GROUP BY a.id, a.name HAVING count(*) FILTER (WHERE o.status = 'failed') > 0
		ORDER BY 3 DESC, a.name, a.id LIMIT 5`, workspaceID, end.Add(-period), end)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ApplicationFailures
		if err := rows.Scan(&item.ApplicationID, &item.Name, &item.Failed, &item.Total); err != nil {
			return result, err
		}
		result.FailuresByApplication = append(result.FailuresByApplication, item)
	}
	return result, rows.Err()
}

func (s *Store) deliveryPeriod(ctx context.Context, workspaceID string, start, end time.Time) (DeliveryPeriod, error) {
	period := DeliveryPeriod{StartedAt: start, EndedAt: end}
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE status = 'succeeded'), count(*) FILTER (WHERE status = 'failed')
		FROM operations WHERE `+workspaceOperations+` AND started_at >= $2 AND started_at < $3`, workspaceID, start, end).
		Scan(&period.Deployments, &period.Failed); err != nil {
		return period, err
	}
	// Only the first failure of a streak starts the clock: three failed retries
	// followed by a fix are one outage, not three.
	var median sql.NullFloat64
	err := s.DB.QueryRowContext(ctx, `WITH ops AS (
			SELECT id, application_id, status, started_at, COALESCE(finished_at, started_at) AS done,
				LAG(status) OVER (PARTITION BY application_id ORDER BY started_at, id) AS previous
			FROM operations WHERE `+workspaceOperations+` AND status IN ('succeeded','failed')
		), failures AS (
			SELECT application_id, started_at, done FROM ops
			WHERE status = 'failed' AND previous IS DISTINCT FROM 'failed' AND started_at >= $2 AND started_at < $3
		)
		SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM fix.done - f.done)), count(*)
		FROM failures f CROSS JOIN LATERAL (
			SELECT done FROM ops o WHERE o.application_id = f.application_id AND o.status = 'succeeded' AND o.started_at > f.started_at
			ORDER BY o.started_at, o.id LIMIT 1
		) fix`, workspaceID, start, end).Scan(&median, &period.Restored)
	if err != nil {
		return period, err
	}
	if median.Valid {
		value := median.Float64
		if value < 0 {
			value = 0
		}
		period.RestoreSeconds = &value
	}
	return period, nil
}
