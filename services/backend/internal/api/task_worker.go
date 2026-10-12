package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/justlab/justcd/services/backend/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const backgroundTaskLease = 90 * time.Second

func interruptedTaskResponse() *httptest.ResponseRecorder {
	r := httptest.NewRecorder()
	writeClassifiedError(r, 409, "Background work was interrupted. Refresh the plan, approvals and operations before retrying.", apiErrorMetadata{code: "task.interrupted", category: "request", remediation: "Refresh the current state before submitting another task; an approval may already have been recorded."}, nil)
	return r
}

// Each replica has a bounded pool. PostgreSQL claims each task exactly once;
// queued tasks survive restarts, while interrupted work is never blindly replayed.
func (s *Server) RunTaskWorker(acquireCtx, executionCtx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ticker := time.NewTicker(750 * time.Millisecond)
			defer ticker.Stop()
			var lastRecovery time.Time
			for {
				select {
				case <-acquireCtx.Done():
					return
				case <-ticker.C:
				}
				if acquireCtx.Err() != nil {
					return
				}
				if time.Since(lastRecovery) >= 30*time.Second {
					if err := s.Store.RecoverBackgroundTasks(acquireCtx, interruptedTaskResponse().Body.Bytes()); err != nil {
						if acquireCtx.Err() == nil {
							logger.Error("could not recover background tasks", "error", err)
						}
						continue
					}
					lastRecovery = time.Now()
				}
				task, claimed, err := s.Store.ClaimBackgroundTask(acquireCtx, backgroundTaskLease)
				if err != nil {
					if acquireCtx.Err() == nil {
						logger.Error("could not claim background task", "error", err)
					}
					continue
				}
				if claimed {
					s.runBackgroundTask(executionCtx, task, logger)
				}
			}
		}()
	}
	workers.Wait()
}

func (s *Server) runBackgroundTask(ctx context.Context, task store.BackgroundTask, logger *slog.Logger) {
	ctx, span := otel.Tracer("justcd/tasks").Start(ctx, "task.execute")
	defer span.End()
	span.SetAttributes(attribute.String("task.id", task.ID), attribute.String("task.kind", task.Kind), attribute.String("application.id", task.ApplicationID))
	workCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-workCtx.Done():
				return
			case <-ticker.C:
			}
			heartbeatCtx, heartbeatCancel := context.WithTimeout(workCtx, 5*time.Second)
			err := s.Store.RenewBackgroundTask(heartbeatCtx, task, backgroundTaskLease)
			heartbeatCancel()
			if err != nil {
				logger.Warn("background task lease lost", "taskId", task.ID, "error", err)
				cancel()
				return
			}
		}
	}()
	recorder := s.executeBackgroundTask(workCtx, task)
	if workCtx.Err() != nil {
		recorder = interruptedTaskResponse()
	}
	close(stop)
	<-done
	// Save the terminal outcome even when execution was cancelled during drain.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	if !json.Valid(recorder.Body.Bytes()) {
		recorder = interruptedTaskResponse()
	}
	if err := s.Store.FinishBackgroundTask(finishCtx, task, recorder.Code, recorder.Body.Bytes()); err != nil {
		logger.Error("could not save background task outcome", "taskId", task.ID, "error", err)
	}
}
