package main

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// backendLifecycle owns every background loop and active HTTP handler so the
// database remains open until they have stopped using it.
type backendLifecycle struct {
	acquireCtx      context.Context
	executionCtx    context.Context
	stopAcquiring   context.CancelFunc
	cancelExecution context.CancelFunc
	workers         sync.WaitGroup
	mu              sync.Mutex
	draining        bool
}

func newBackendLifecycle() *backendLifecycle {
	acquireCtx, stopAcquiring := context.WithCancel(context.Background())
	executionCtx, cancelExecution := context.WithCancel(context.Background())
	return &backendLifecycle{acquireCtx: acquireCtx, executionCtx: executionCtx, stopAcquiring: stopAcquiring, cancelExecution: cancelExecution}
}

// Start is called during startup, before Shutdown.
func (l *backendLifecycle) Start(run func(context.Context, context.Context)) {
	l.workers.Add(1)
	go func() {
		defer l.workers.Done()
		run(l.acquireCtx, l.executionCtx)
	}()
}

func (l *backendLifecycle) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		if l.draining {
			l.mu.Unlock()
			w.Header().Set("Connection", "close")
			http.Error(w, "backend is shutting down", http.StatusServiceUnavailable)
			return
		}
		l.workers.Add(1)
		l.mu.Unlock()
		defer l.workers.Done()
		next.ServeHTTP(w, r)
	})
}

func (l *backendLifecycle) Shutdown(server *http.Server, grace time.Duration) error {
	// Prevent new handler registrations before Wait, including connections that
	// were accepted before the listener is closed by http.Server.Shutdown.
	l.mu.Lock()
	l.draining = true
	l.mu.Unlock()
	l.stopAcquiring()
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	stopCancellation := context.AfterFunc(ctx, l.cancelExecution)
	defer stopCancellation()
	defer l.cancelExecution()

	err := server.Shutdown(ctx)
	if err != nil {
		// Close connections and cancel request contexts after the drain deadline.
		err = errors.Join(err, server.Close())
	}
	l.workers.Wait()
	return errors.Join(err, ctx.Err())
}
