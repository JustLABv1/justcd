package syncer

import (
	"context"
	"log/slog"
	"time"
)

const systemActorID = "justcd-system"

// RunPoller checks drift for every application and performs auto-safe
// reconciliation outside the Kubernetes cluster. Destructive or cluster-scoped
// plans are recorded for review but never applied.
func (s *Service) RunPoller(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileDue(ctx, logger)
		}
	}
}

func (s *Service) reconcileDue(ctx context.Context, logger *slog.Logger) {
	applications, err := s.Store.DueApplications(ctx, 25)
	if err != nil {
		logger.Error("could not list due JustCD applications", "error", err)
		return
	}
	for _, app := range applications {
		if ctx.Err() != nil {
			return
		}
		record, err := s.BuildPlan(ctx, app.ID, systemActorID)
		if err != nil {
			logger.Warn("automatic JustCD plan failed", "applicationId", app.ID, "error", err)
			if ctx.Err() != nil {
				return
			}
			attempt := app.RetryAttemptCount + 1
			decision := DecideRetry(ctx, app.RetryPolicy, attempt, err, time.Now().UTC(), retryJitterSample(), true)
			if app.RetryNextAt != nil && decision.NextRetryAt == nil && decision.TerminalReason == "" {
				decision.TerminalReason = "retry_requires_review"
			}
			if err := s.Store.RecordApplicationRetry(ctx, app.ID, decision.AttemptCount, decision.ErrorCode, decision.NextRetryAt, decision.TerminalReason); err != nil {
				logger.Error("could not persist JustCD reconciliation retry state", "applicationId", app.ID, "error", err)
			}
			if decision.NextRetryAt == nil && app.SyncPolicy == "auto-safe" {
				_ = s.Store.PauseAutoSync(ctx, app.ID)
			}
			_ = s.Store.Audit(ctx, systemActorID, "auto_sync.plan_failed", "application", app.ID, map[string]any{"message": "automatic drift check failed; see server logs", "attempt": decision.AttemptCount, "errorCode": decision.ErrorCode, "nextRetryAt": decision.NextRetryAt, "terminalReason": decision.TerminalReason})
			continue
		}
		if len(record.Plan.Changes) == 0 {
			_ = s.Store.SetPlanStatus(ctx, record.ID, "applied")
			if err := s.Store.MarkApplicationSynced(ctx, app.ID, record.Plan.Revision, "synced"); err != nil {
				logger.Warn("could not mark no-op JustCD plan synced", "applicationId", app.ID, "error", err)
			} else if app.Health != "synced" || app.LastSyncedRevision != record.Plan.Revision {
				_ = s.Store.Audit(ctx, systemActorID, "sync.succeeded", "application", app.ID, map[string]any{"planId": record.ID, "revision": record.Plan.Revision, "digest": record.Plan.Digest, "changes": 0})
			}
			continue
		}
		if app.SyncPolicy != "auto-safe" {
			if app.RetryNextAt != nil || app.RetryTerminalReason != "" {
				_ = s.Store.ResetApplicationRetry(ctx, app.ID)
			}
			continue
		}
		if record.Plan.RequiresApproval {
			if app.RetryNextAt != nil {
				_ = s.Store.RecordApplicationRetry(ctx, app.ID, app.RetryAttemptCount, "plan.approval_required", nil, "approval_required")
				_ = s.Store.PauseAutoSync(ctx, app.ID)
			}
			continue
		}
		if _, err := s.Apply(ctx, record.ID, systemActorID, ""); err != nil {
			logger.Warn("automatic JustCD sync failed", "applicationId", app.ID, "planId", record.ID, "error", err)
		}
	}
}
