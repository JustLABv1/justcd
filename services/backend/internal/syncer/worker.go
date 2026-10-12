package syncer

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/justlab/justcd/services/backend/internal/observability"
	"github.com/justlab/justcd/services/backend/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const operationWorkerCount = 20

// RunOperationWorker drains the durable queue with a bounded worker pool. The
// database applies the per-cluster concurrency and rate limits when claiming.
func (s *Service) RunOperationWorker(ctx context.Context, logger *slog.Logger) {
	s.RunOperationWorkerWithDrain(ctx, ctx, logger)
}

// RunOperationWorkerWithDrain stops acquisition when acquireCtx is cancelled,
// but keeps executing claimed operations and renewing their leases until they
// finish or executionCtx is cancelled. It waits for all operation goroutines.
func (s *Service) RunOperationWorkerWithDrain(acquireCtx, executionCtx context.Context, logger *slog.Logger) {
	ctx := acquireCtx
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
				s.executeOperationJob(executionCtx, job, logger)
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
			if err := s.Store.PruneAgentTasks(ctx); err != nil && ctx.Err() == nil {
				logger.Error("could not prune expired cluster agent tasks", "error", err)
			}
			if err := s.Store.RecoverInterruptedOperations(ctx); err != nil && ctx.Err() == nil {
				logger.Error("could not mark interrupted JustCD operations", "error", err)
			}
		case <-queueTicker.C:
		dispatch:
			for {
				if ctx.Err() != nil {
					return
				}
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
				// A claim committed just before shutdown belongs to this worker.
				// Hand it to an available executor during drain rather than abandon it.
				select {
				case jobs <- operationJob{operation: operation}:
				case <-executionCtx.Done():
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
	guard, err := s.Store.AcquireExecutionGuard(ctx, operation.ApplicationID)
	if err != nil {
		logger.Warn("could not acquire application execution guard", "operationId", operation.ID, "error", err)
		s.markClaimedOperationInterrupted(operation, logger)
		return
	}
	defer guard.Close()
	parent := observability.ContextFromTraceParent(ctx, operation.TraceParent)
	spanOptions := []trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(
		attribute.String("operation.id", operation.ID),
		attribute.String("application.id", operation.ApplicationID),
		attribute.String("operation.type", operation.Type),
	)}
	if operation.PlanID != nil {
		spanOptions = append(spanOptions, trace.WithAttributes(attribute.String("plan.id", *operation.PlanID)))
	}
	parent, span := otel.Tracer("justcd/worker").Start(parent, "operation.execute", spanOptions...)
	defer span.End()
	started := time.Now()
	opCtx, cancel := context.WithTimeout(parent, 30*time.Minute)
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
				if err := guard.Check(opCtx); err != nil {
					cancel()
					return
				}
				if err := s.Store.RenewOperationLease(opCtx, operation.ID, 90*time.Second); err != nil {
					logger.Error("could not renew JustCD sync lease; stopping the operation", "operationId", operation.ID, "error", err)
					cancel()
					return
				}
			}
		}
	}()
	s.checkpoint("operation.claimed")
	result, executeErr := s.executeQueuedOperation(opCtx, operation)
	cancel()
	<-heartbeatDone
	if executeErr == nil {
		s.Metrics.RecordOperation(operation.Type, result.Status, time.Since(started))
		logger.InfoContext(parent, "JustCD operation completed", "operationId", operation.ID, "applicationId", operation.ApplicationID, "planId", operation.PlanID, "operationType", operation.Type, "status", result.Status)
		return
	}
	if result.Status == "" || result.Status == "running" || result.Status == "queued" {
		s.markClaimedOperationInterrupted(operation, logger)
		result.Status = "failed"
	}
	observability.MarkError(span, executeErr)
	s.Metrics.RecordOperation(operation.Type, result.Status, time.Since(started))
	logger.WarnContext(parent, "JustCD sync operation failed", "operationId", operation.ID, "applicationId", operation.ApplicationID, "planId", operation.PlanID, "operationType", operation.Type, "status", result.Status, "error", executeErr)
}

func (s *Service) markClaimedOperationInterrupted(operation store.Operation, logger *slog.Logger) {
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	message := "Sync interrupted before a result was recorded. Review cluster state and build a fresh plan before retrying."
	if operation.Type == "rollback" {
		message = "Rollback interrupted before a result was recorded. It will not resume automatically; review cluster state and any available checkpoint."
	}
	attempt := operation.AttemptCount
	if attempt < 1 {
		attempt = 1
	}
	if err := s.Store.FinishOperationWithRetry(finishCtx, operation.ID, operation.ApplicationID, message, attempt, "operation.interrupted", nil, "interrupted_requires_review"); err != nil {
		logger.Error("could not persist interrupted JustCD operation", "operationId", operation.ID, "error", err)
		return
	}
}
