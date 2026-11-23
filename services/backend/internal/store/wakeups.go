package store

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// wakeupChannel is the PostgreSQL NOTIFY channel that carries agent task
// wakeups between backend replicas.
const wakeupChannel = "justcd_agent_tasks"

// Wakeup keys. A queued task wakes the cluster's agent poll; a completed task
// wakes the request waiting for its result.
func agentQueueWakeup(clusterID string) string { return "queue:" + clusterID }
func agentResultWakeup(taskID string) string   { return "result:" + taskID }

// wakeups fans signals out to local waiters. A signal only means "look again":
// waiters still read the database, and keep polling in case a signal is lost
// (for example while the NOTIFY listener reconnects).
type wakeups struct {
	mu      sync.Mutex
	waiters map[string]map[chan struct{}]struct{}
}

func (w *wakeups) subscribe(key string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	w.mu.Lock()
	if w.waiters == nil {
		w.waiters = map[string]map[chan struct{}]struct{}{}
	}
	if w.waiters[key] == nil {
		w.waiters[key] = map[chan struct{}]struct{}{}
	}
	w.waiters[key][ch] = struct{}{}
	w.mu.Unlock()
	return ch, func() {
		w.mu.Lock()
		delete(w.waiters[key], ch)
		if len(w.waiters[key]) == 0 {
			delete(w.waiters, key)
		}
		w.mu.Unlock()
	}
}

func (w *wakeups) signal(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for ch := range w.waiters[key] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// WaitForAgentTask returns a channel that receives a value when a task may
// have been queued for the cluster's agent. Call the returned function to stop.
func (s *Store) WaitForAgentTask(clusterID string) (<-chan struct{}, func()) {
	return s.wakeups.subscribe(agentQueueWakeup(clusterID))
}

// WaitForAgentResult returns a channel that receives a value when the task may
// have a result. Call the returned function to stop.
func (s *Store) WaitForAgentResult(taskID string) (<-chan struct{}, func()) {
	return s.wakeups.subscribe(agentResultWakeup(taskID))
}

// ListenForWakeups relays wakeups sent by other backend replicas until ctx
// ends, reconnecting after failures. Without it, a single replica still wakes
// its own waiters directly and other replicas fall back to polling.
func (s *Store) ListenForWakeups(ctx context.Context, connectionString string, logger *slog.Logger) {
	delay := time.Second
	for ctx.Err() == nil {
		err := s.listenOnce(ctx, connectionString)
		if ctx.Err() != nil {
			return
		}
		logger.Warn("agent task wakeup listener stopped; agent requests fall back to polling until it reconnects", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(2*delay, 30*time.Second)
	}
}

func (s *Store) listenOnce(ctx context.Context, connectionString string) error {
	config, err := pgx.ParseConfig(connectionString)
	if err != nil {
		return err
	}
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "LISTEN "+wakeupChannel); err != nil {
		return err
	}
	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		s.wakeups.signal(notification.Payload)
	}
}
