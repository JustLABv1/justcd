package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"
)

// ExecutionGuard prevents a paused worker with an expired lease and its
// replacement from executing the same application at once. The session lock
// is released by PostgreSQL if the worker process or connection dies.
type ExecutionGuard struct {
	conn          *sql.Conn
	applicationID string
}

func (s *Store) AcquireExecutionGuard(ctx context.Context, applicationID string) (*ExecutionGuard, error) {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var acquired bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(7413,hashtext($1))`, applicationID).Scan(&acquired); err != nil || !acquired {
		conn.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("another executor still owns this application")
	}
	return &ExecutionGuard{conn: conn, applicationID: applicationID}, nil
}

func (g *ExecutionGuard) Check(ctx context.Context) error { return g.conn.PingContext(ctx) }

func (g *ExecutionGuard) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := g.conn.ExecContext(ctx, `SELECT pg_advisory_unlock(7413,hashtext($1))`, g.applicationID); err != nil {
		_ = g.conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	g.conn.Close()
}
