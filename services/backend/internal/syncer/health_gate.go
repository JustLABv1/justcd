package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/justlab/justcd/services/backend/internal/apphealth"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// A rollout that has only just been applied can still look Healthy from the
// previous pods. Wait this long before accepting the first passing observation.
const healthGateSettle = 15 * time.Second

// startHealthGate opens a post-deploy check for a successful sync. It returns
// false when the application declares no gate, in which case the caller reports
// the deployment as successful immediately.
func (s *Service) startHealthGate(app store.Application, operationID string, plan store.PlanRecord, revision string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	gate, err := s.Store.ApplicationHealthGate(ctx, app.ID)
	if err != nil {
		slog.Warn("could not load health gate", "applicationId", app.ID, "error", err)
		return false
	}
	if gate == nil {
		return false
	}
	// GitLab shows the deployment as running while the gate verifies it.
	deploymentID := s.reportEnvironmentDeployment(app, operationID, plan, revision, "running")
	check := store.HealthGateCheck{
		OperationID: operationID, ApplicationID: app.ID, Revision: revision,
		Deadline: time.Now().Add(time.Duration(gate.TimeoutSeconds) * time.Second), OnFailure: gate.OnFailure, GitLabDeploymentID: deploymentID,
	}
	superseded, err := s.Store.StartHealthGateCheck(ctx, check)
	if err != nil {
		slog.Error("could not start health gate", "applicationId", app.ID, "operationId", operationID, "error", err)
		if deploymentID != 0 {
			s.finishEnvironmentDeployment(app, operationID, deploymentID, revision, "success")
		}
		return false
	}
	s.closeSuperseded(app, superseded)
	return true
}

func (s *Service) closeSuperseded(app store.Application, superseded []store.HealthGateCheck) {
	for _, old := range superseded {
		if old.GitLabDeploymentID != 0 {
			s.finishEnvironmentDeployment(app, old.OperationID, old.GitLabDeploymentID, old.Revision, "canceled")
		}
	}
}

// supersedeHealthGates cancels pending checks when a rollback or other newer
// operation makes their verdict irrelevant.
func (s *Service) supersedeHealthGates(app store.Application) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	superseded, err := s.Store.SupersedePendingHealthGateChecks(ctx, app.ID)
	if err != nil {
		slog.Warn("could not supersede health gates", "applicationId", app.ID, "error", err)
		return
	}
	s.closeSuperseded(app, superseded)
}

// RunHealthGateWorker resolves pending checks. Every verdict is written through
// a conditional update, so concurrent replicas cannot resolve a check twice.
func (s *Service) RunHealthGateWorker(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		checks, err := s.Store.PendingHealthGateChecks(ctx, 50)
		if err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "could not list health gate checks", "error", err)
		}
		workers := make(chan struct{}, 4)
		var group sync.WaitGroup
	evaluate:
		for _, check := range checks {
			select {
			case workers <- struct{}{}:
			case <-ctx.Done():
				break evaluate
			}
			group.Add(1)
			go func() {
				defer group.Done()
				defer func() { <-workers }()
				evalCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
				defer cancel()
				s.evaluateHealthGate(evalCtx, check, logger)
			}()
		}
		group.Wait()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) evaluateHealthGate(ctx context.Context, check store.HealthGateCheck, logger *slog.Logger) {
	app, err := s.Store.ApplicationByID(ctx, check.ApplicationID)
	if err != nil {
		if _, resolveErr := s.Store.ResolveHealthGateCheck(ctx, check.OperationID, "superseded", "The application no longer exists"); resolveErr != nil {
			logger.WarnContext(ctx, "could not close health gate", "operationId", check.OperationID, "error", resolveErr)
		}
		return
	}
	if err := s.RefreshApplicationHealth(ctx, app); err != nil && ctx.Err() == nil {
		logger.WarnContext(ctx, "health gate observation incomplete", "applicationId", app.ID, "error", err)
	}
	if app, err = s.Store.ApplicationByID(ctx, check.ApplicationID); err != nil {
		return
	}
	now := time.Now()
	condition := app.HealthCondition
	fresh := condition.ObservedAt != nil && condition.ObservedAt.After(check.StartedAt.Add(healthGateSettle))
	switch {
	case fresh && condition.Status == apphealth.Healthy:
		s.resolveHealthGate(ctx, app, check, "passed", "Application reported Healthy after the deployment", logger)
	case now.After(check.Deadline):
		message := fmt.Sprintf("Application was %s %d seconds after the deployment", condition.Status, int(now.Sub(check.StartedAt).Seconds()))
		if condition.Message != "" {
			message += ": " + condition.Message
		}
		s.resolveHealthGate(ctx, app, check, "failed", message, logger)
	}
}

func (s *Service) resolveHealthGate(ctx context.Context, app store.Application, check store.HealthGateCheck, verdict, message string, logger *slog.Logger) {
	won, err := s.Store.ResolveHealthGateCheck(ctx, check.OperationID, verdict, message)
	if err != nil {
		logger.ErrorContext(ctx, "could not record health gate verdict", "operationId", check.OperationID, "error", err)
		return
	}
	if !won {
		return
	}
	details := map[string]any{"operationId": check.OperationID, "revision": check.Revision, "message": message}
	if verdict == "passed" {
		_ = s.Store.Audit(ctx, systemActorID, "health_gate.passed", "application", app.ID, details)
		s.finishEnvironmentDeployment(app, check.OperationID, check.GitLabDeploymentID, check.Revision, "success")
		return
	}
	_ = s.Store.Audit(ctx, systemActorID, "health_gate.failed", "application", app.ID, details)
	s.finishEnvironmentDeployment(app, check.OperationID, check.GitLabDeploymentID, check.Revision, "failed")
	if err := s.Store.NotifyHealthGateFailure(ctx, app.ID, check.OperationID); err != nil {
		logger.WarnContext(ctx, "could not raise health gate notification", "applicationId", app.ID, "error", err)
	}
	if check.OnFailure == store.HealthGatePrepareRollback {
		s.prepareHealthGateRollback(ctx, app, check, logger)
	}
}

// prepareHealthGateRollback builds the usual reviewed rollback plan to the last
// good deployment. It deliberately stops there: rollbacks require owner
// approval, and a failing health check is not allowed to bypass that.
func (s *Service) prepareHealthGateRollback(ctx context.Context, app store.Application, check store.HealthGateCheck, logger *slog.Logger) {
	targets, err := s.Store.ListRollbackTargets(ctx, app.ID)
	if err != nil {
		logger.WarnContext(ctx, "could not list rollback targets", "applicationId", app.ID, "error", err)
		return
	}
	for _, target := range targets {
		if target.Kind != "successful_sync" || target.OperationID == check.OperationID {
			continue
		}
		record, err := s.BuildRollbackPlan(ctx, app, systemActorID, target.Kind, target.ID, "")
		if err != nil {
			_ = s.Store.SetHealthGateRollbackPlan(ctx, check.OperationID, "", "Could not prepare a rollback plan: "+err.Error())
			logger.WarnContext(ctx, "could not prepare health gate rollback", "applicationId", app.ID, "error", err)
			return
		}
		_ = s.Store.SetHealthGateRollbackPlan(ctx, check.OperationID, record.ID, "")
		_ = s.Store.Audit(ctx, systemActorID, "health_gate.rollback_prepared", "application", app.ID, map[string]any{"operationId": check.OperationID, "planId": record.ID, "revision": target.Revision})
		return
	}
	_ = s.Store.SetHealthGateRollbackPlan(ctx, check.OperationID, "", "No earlier successful deployment is available to roll back to")
}
