package syncer

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/justlab/justcd/services/backend/internal/observability"
	"github.com/justlab/justcd/services/backend/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
		logger.ErrorContext(ctx, "could not list due JustCD applications", "error", err)
		return
	}
	for _, app := range applications {
		if ctx.Err() != nil {
			return
		}
		s.reconcileApplication(ctx, logger, app)
	}
}

func (s *Service) reconcileApplication(parent context.Context, logger *slog.Logger, app store.Application) {
	ctx, span := otel.Tracer("justcd/poller").Start(parent, "reconciliation", trace.WithAttributes(attribute.String("application.id", app.ID)))
	defer span.End()
	trigger, triggerErr := s.Store.GitPushTrigger(ctx, app.ID)
	if triggerErr != nil && !errors.Is(triggerErr, sql.ErrNoRows) {
		logger.ErrorContext(ctx, "could not read Git push trigger", "applicationId", app.ID, "error", triggerErr)
		return
	}
	checkStarted := time.Now()
	record, err := s.BuildPlan(ctx, app.ID, systemActorID)
	if err != nil {
		s.Metrics.RecordReconciliation("failed", time.Since(checkStarted))
		observability.MarkError(span, err)
		logger.WarnContext(ctx, "automatic JustCD plan failed", "applicationId", app.ID, "error", err)
		if healthErr := s.RefreshApplicationHealth(ctx, app); healthErr != nil && ctx.Err() == nil {
			logger.WarnContext(ctx, "could not fully refresh Kubernetes application health", "applicationId", app.ID, "error", healthErr)
		}
		if ctx.Err() != nil {
			return
		}
		attempt := app.RetryAttemptCount + 1
		decision := DecideRetry(ctx, app.RetryPolicy, attempt, err, time.Now().UTC(), retryJitterSample(), true)
		if app.RetryNextAt != nil && decision.NextRetryAt == nil && decision.TerminalReason == "" {
			decision.TerminalReason = "retry_requires_review"
		}
		if err := s.Store.RecordApplicationRetry(ctx, app.ID, decision.AttemptCount, decision.ErrorCode, decision.NextRetryAt, decision.TerminalReason); err != nil {
			logger.ErrorContext(ctx, "could not persist JustCD reconciliation retry state", "applicationId", app.ID, "error", err)
		}
		if decision.NextRetryAt == nil && app.SyncPolicy == "auto-safe" {
			_ = s.Store.PauseAutoSync(ctx, app.ID)
		}
		_ = s.Store.Audit(ctx, systemActorID, "auto_sync.plan_failed", "application", app.ID, map[string]any{"message": "automatic drift check failed; see server logs", "attempt": decision.AttemptCount, "errorCode": decision.ErrorCode, "nextRetryAt": decision.NextRetryAt, "terminalReason": decision.TerminalReason})
		return
	}
	span.SetAttributes(attribute.String("plan.id", record.ID), attribute.Int("plan.change_count", len(record.Plan.Changes)))
	result := "drift"
	if len(record.Plan.Changes) == 0 {
		result = "in_sync"
	}
	s.Metrics.RecordReconciliation(result, time.Since(checkStarted))
	s.Metrics.RecordDriftChanges(record.Plan.Changes)
	if triggerErr == nil {
		if err := s.Store.AttachGitPushTrigger(ctx, record.ID, trigger); err != nil {
			logger.ErrorContext(ctx, "could not attach Git push trigger to plan", "applicationId", app.ID, "planId", record.ID, "error", err)
			return
		}
		_ = s.Store.Audit(ctx, systemActorID, "plan.webhook_triggered", "application", app.ID, map[string]any{"planId": record.ID, "provider": trigger.Provider, "sourceKey": trigger.SourceKey, "deliveryId": trigger.DeliveryID, "ref": trigger.Ref, "reportedCommit": trigger.ReportedSHA, "resolvedCommit": record.Plan.Revision})
		if err := s.Store.CompleteGitPushTrigger(ctx, trigger); err != nil {
			logger.ErrorContext(ctx, "could not complete Git push trigger", "applicationId", app.ID, "error", err)
		}
	}
	if healthErr := s.RefreshApplicationHealth(ctx, app); healthErr != nil && ctx.Err() == nil {
		logger.WarnContext(ctx, "could not fully refresh Kubernetes application health", "applicationId", app.ID, "error", healthErr)
	}
	if len(record.Plan.Changes) == 0 {
		_ = s.Store.SetPlanStatus(ctx, record.ID, "applied")
		if err := s.Store.MarkApplicationSynced(ctx, app.ID, record.Plan.Revision, "synced"); err != nil {
			logger.WarnContext(ctx, "could not mark no-op JustCD plan synced", "applicationId", app.ID, "error", err)
		} else if app.Health != "synced" || app.LastSyncedRevision != record.Plan.Revision {
			_ = s.Store.Audit(ctx, systemActorID, "sync.succeeded", "application", app.ID, map[string]any{"planId": record.ID, "revision": record.Plan.Revision, "digest": record.Plan.Digest, "changes": 0})
		}
		return
	}
	if app.SyncPolicy != "auto-safe" {
		if app.RetryNextAt != nil || app.RetryTerminalReason != "" {
			_ = s.Store.ResetApplicationRetry(ctx, app.ID)
		}
		return
	}
	if record.Plan.RequiresApproval {
		if app.RetryNextAt != nil {
			_ = s.Store.RecordApplicationRetry(ctx, app.ID, app.RetryAttemptCount, "plan.approval_required", nil, "approval_required")
			_ = s.Store.PauseAutoSync(ctx, app.ID)
		}
		return
	}
	if _, err := s.Apply(ctx, record.ID, systemActorID, ""); err != nil {
		observability.MarkError(span, err)
		logger.WarnContext(ctx, "automatic JustCD sync failed", "applicationId", app.ID, "planId", record.ID, "error", err)
	} else if healthErr := s.RefreshApplicationHealth(ctx, app); healthErr != nil && ctx.Err() == nil {
		logger.WarnContext(ctx, "could not fully refresh Kubernetes application health", "applicationId", app.ID, "error", healthErr)
	}
}
