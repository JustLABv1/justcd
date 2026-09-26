package store

import "context"

func (s *Store) OperationQueueDepth(ctx context.Context) (queued, running int64, err error) {
	err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE status='queued'),COUNT(*) FILTER (WHERE status='running') FROM operations WHERE status IN ('queued','running')`).Scan(&queued, &running)
	return queued, running, err
}
