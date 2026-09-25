package syncer

import (
	"context"
	"log/slog"
	"time"
)

// RunOperationWorker drains the database-backed sync queue. Jobs transition to
// running only after a worker claims them; abandoned running jobs are marked
// interrupted and are never re-applied automatically.
func (s *Service) RunOperationWorker(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := s.Store.RecoverInterruptedOperations(ctx); err != nil {
		logger.Error("could not recover interrupted JustCD operations", "error", err)
	}
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := s.Store.RecoverInterruptedOperations(ctx); err != nil {
			logger.Error("could not mark interrupted JustCD operations", "error", err)
		}
		for ctx.Err() == nil {
			operation, claimed, err := s.Store.ClaimQueuedOperation(ctx, 90*time.Second)
			if err != nil {
				logger.Error("could not claim queued JustCD operation", "error", err)
				break
			}
			if !claimed {
				break
			}
			logger.Info("starting queued JustCD operation", "operationId", operation.ID, "applicationId", operation.ApplicationID, "operationType", operation.Type)
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
			if executeErr != nil {
				if result.Status == "" || result.Status == "running" || result.Status == "queued" {
					message := "Sync interrupted before a result was recorded. Review cluster state and rebuild the plan before retrying."
					if operation.Type == "rollback" {
						message = "Rollback interrupted before a result was recorded. It will not be resumed automatically; review cluster state and any available checkpoint."
					}
					if err := s.Store.FinishOperation(context.Background(), operation.ID, "failed", message); err != nil {
						logger.Error("could not persist failed JustCD sync result", "operationId", operation.ID, "error", err)
					}
					if operation.PlanID != nil {
						_ = s.Store.SetPlanStatus(context.Background(), *operation.PlanID, "failed")
					}
					result.Status = "failed"
					_ = s.Store.PauseAutoSync(context.Background(), operation.ApplicationID)
					actorID := ""
					if operation.ActorID != nil {
						actorID = *operation.ActorID
					}
					_ = s.Store.Audit(context.Background(), actorID, operation.Type+".failed", "application", operation.ApplicationID, map[string]any{"operationId": operation.ID, "planId": operation.PlanID, "message": message, "interrupted": true})
				}
				logger.Warn("JustCD sync operation failed", "operationId", operation.ID, "applicationId", operation.ApplicationID, "status", result.Status, "error", executeErr)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
