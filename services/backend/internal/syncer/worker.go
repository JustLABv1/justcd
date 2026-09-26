package syncer

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/justlab/justcd/services/backend/internal/store"
)

const operationWorkerCount = 20

// RunOperationWorker drains the durable queue with a bounded worker pool. The
// database applies the per-cluster concurrency and rate limits when claiming.
func (s *Service) RunOperationWorker(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := s.Store.RecoverInterruptedOperations(ctx); err != nil {
		logger.Error("could not recover interrupted JustCD operations", "error", err)
	}

	jobs := make(chan operationJob)
	available := make(chan struct{}, operationWorkerCount)
	for i := 0; i < operationWorkerCount; i++ {
		available <- struct{}{}
	}
	var workers sync.WaitGroup
	for i := 0; i < operationWorkerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				s.executeOperationJob(ctx, job, logger)
				available <- struct{}{}
			}
		}()
	}
	defer func() {
		close(jobs)
		workers.Wait()
	}()

	queueTicker := time.NewTicker(750 * time.Millisecond)
	defer queueTicker.Stop()
	recoveryTicker := time.NewTicker(30 * time.Second)
	defer recoveryTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-recoveryTicker.C:
			if err := s.Store.RecoverInterruptedOperations(ctx); err != nil && ctx.Err() == nil {
				logger.Error("could not mark interrupted JustCD operations", "error", err)
			}
		case <-queueTicker.C:
		dispatch:
			for {
				select {
				case <-available:
				default:
					break dispatch
				}
				operation, claimed, err := s.Store.ClaimQueuedOperation(ctx, 90*time.Second)
				if err != nil {
					available <- struct{}{}
					if ctx.Err() == nil {
						logger.Error("could not claim queued JustCD operation", "error", err)
					}
					break dispatch
				}
				if !claimed {
					available <- struct{}{}
					break dispatch
				}
				logger.Info("starting queued JustCD operation", "operationId", operation.ID, "applicationId", operation.ApplicationID, "operationType", operation.Type)
				select {
				case jobs <- operationJob{operation: operation}:
				case <-ctx.Done():
					available <- struct{}{}
					s.markClaimedOperationInterrupted(operation, logger)
					return
				}
			}
		}
	}
}

type operationJob struct {
	operation store.Operation
}

func (s *Service) executeOperationJob(ctx context.Context, job operationJob, logger *slog.Logger) {
	operation := job.operation
	opCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-opCtx.Done():
				return
			case <-ticker.C:
				if err := s.Store.RenewOperationLease(opCtx, operation.ID, 90*time.Second); err != nil {
					logger.Error("could not renew JustCD sync lease; stopping the operation", "operationId", operation.ID, "error", err)
					cancel()
					return
				}
			}
		}
	}()
	result, executeErr := s.executeQueuedOperation(opCtx, operation)
	cancel()
	<-heartbeatDone
	if executeErr == nil {
		return
	}
	if result.Status == "" || result.Status == "running" || result.Status == "queued" {
		s.markClaimedOperationInterrupted(operation, logger)
		result.Status = "failed"
	}
	logger.Warn("JustCD sync operation failed", "operationId", operation.ID, "applicationId", operation.ApplicationID, "status", result.Status, "error", executeErr)
}

func (s *Service) markClaimedOperationInterrupted(operation store.Operation, logger *slog.Logger) {
	message := "Sync interrupted before a result was recorded. Review cluster state and build a fresh plan before retrying."
	if operation.Type == "rollback" {
		message = "Rollback interrupted before a result was recorded. It will not resume automatically; review cluster state and any available checkpoint."
	}
	attempt := operation.AttemptCount
	if attempt < 1 {
		attempt = 1
	}
	if err := s.Store.FinishOperationWithRetry(context.Background(), operation.ID, operation.ApplicationID, message, attempt, "operation.interrupted", nil, "interrupted_requires_review"); err != nil {
		logger.Error("could not persist interrupted JustCD operation", "operationId", operation.ID, "error", err)
	}
	if operation.PlanID != nil {
		_ = s.Store.SetPlanStatus(context.Background(), *operation.PlanID, "failed")
	}
	_ = s.Store.PauseAutoSync(context.Background(), operation.ApplicationID)
	actorID := ""
	if operation.ActorID != nil {
		actorID = *operation.ActorID
	}
	_ = s.Store.Audit(context.Background(), actorID, operation.Type+".failed", "application", operation.ApplicationID, map[string]any{"operationId": operation.ID, "planId": operation.PlanID, "message": message, "attemptCount": attempt, "errorCode": "operation.interrupted", "terminalReason": "interrupted_requires_review", "interrupted": true})
}
