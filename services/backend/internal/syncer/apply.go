package syncer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/observability"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

type StalePlanError struct{ Fresh store.PlanRecord }

func (e *StalePlanError) Error() string { return "plan changed and requires a new review" }

func (s *Service) Apply(ctx context.Context, planID, actorID, approvalID string) (store.Operation, error) {
	var approvalIDs []string
	if approvalID != "" {
		approvalIDs = []string{approvalID}
	}
	return s.ApplyWithApprovals(ctx, planID, actorID, approvalIDs)
}

func (s *Service) ApplyWithApprovals(ctx context.Context, planID, actorID string, approvalIDs []string) (store.Operation, error) {
	record, err := s.Store.PlanByID(ctx, planID)
	if err != nil {
		return store.Operation{}, err
	}
	app, err := s.Store.ApplicationByID(ctx, record.Plan.ApplicationID)
	if err != nil {
		return store.Operation{}, err
	}
	if record.Plan.Rollback != nil {
		actor, err := s.Store.UserByID(ctx, actorID)
		if err != nil {
			return store.Operation{}, errors.New("rollback actor is no longer available")
		}
		role, err := s.Store.WorkspaceRole(ctx, actor, app.WorkspaceID)
		if err != nil || role != "owner" {
			return store.Operation{}, errors.New("rollback can only be applied by a workspace owner")
		}
	}
	// Enqueue must not wait for Git rendering or Kubernetes discovery/dry runs.
	// The worker rechecks the exact reviewed digest before any resource mutation
	// and records validation failures in the operation's visible progress.
	if record.Status != "current" || !time.Now().Before(record.ExpiresAt) {
		return store.Operation{}, store.ErrPlanNotCurrent
	}
	if err := s.Store.CheckPlanGuardrails(ctx, record.ID); err != nil {
		return store.Operation{}, err
	}
	required := core.RequiredApprovalCount(record.Plan)
	if required > 0 {
		if len(approvalIDs) < required {
			return store.Operation{}, fmt.Errorf("plan requires %d eligible approval(s)", required)
		}
		approvals := make([]core.DeletionApproval, 0, len(approvalIDs))
		seenIDs := make(map[string]bool, len(approvalIDs))
		seenActors := make(map[string]bool, len(approvalIDs))
		for _, approvalID := range approvalIDs {
			if approvalID == "" || seenIDs[approvalID] {
				return store.Operation{}, errors.New("approval IDs must be unique and non-empty")
			}
			seenIDs[approvalID] = true
			stored, err := s.Store.ApprovalByID(ctx, approvalID)
			if err != nil || stored.UsedAt != nil || stored.PlanID != record.ID {
				return store.Operation{}, errors.New("approval is expired, already used, or unavailable")
			}
			if seenActors[stored.Approval.ActorID] {
				return store.Operation{}, errors.New("approvals must come from distinct workspace members")
			}
			seenActors[stored.Approval.ActorID] = true
			approver, err := s.Store.UserByID(ctx, stored.Approval.ActorID)
			role := ""
			if err == nil {
				role, err = s.Store.WorkspaceRole(ctx, approver, app.WorkspaceID)
			}
			if err != nil || role == "" || !core.ApprovalRoleAllows(record.Plan, role, stored.Approval.ActorID) {
				return store.Operation{}, errors.New("approver is no longer eligible for this plan")
			}
			approvals = append(approvals, stored.Approval)
		}
		if err := core.AuthorizeApplyMany(record.Plan, approvals, time.Now()); err != nil {
			return store.Operation{}, err
		}
	} else if len(approvalIDs) > 0 {
		return store.Operation{}, errors.New("this plan does not require an approval")
	}
	progress, err := store.NewOperationProgress(record.Plan)
	if err != nil {
		return store.Operation{}, err
	}
	return s.Store.QueueOperation(ctx, app.ID, record.ID, actorID, approvalIDs, record.Plan.Digest, observability.TraceParent(ctx), 90*time.Second, progress)
}

func (s *Service) executeQueuedOperation(ctx context.Context, operation store.Operation) (store.Operation, error) {
	ctx, span := otel.Tracer("justcd/syncer").Start(ctx, "operation.apply")
	defer span.End()
	attributes := []attribute.KeyValue{
		attribute.String("operation.id", operation.ID),
		attribute.String("application.id", operation.ApplicationID),
		attribute.String("operation.type", operation.Type),
	}
	if operation.PlanID != nil {
		attributes = append(attributes, attribute.String("plan.id", *operation.PlanID))
	}
	span.SetAttributes(attributes...)
	if operation.PlanID == nil {
		observability.MarkError(span, errors.New("queued operation has no plan"))
		return operation, errors.New("queued operation has no plan")
	}
	operationID, planID := operation.ID, *operation.PlanID
	record, err := s.Store.PlanByID(ctx, planID)
	if err != nil {
		return operation, err
	}
	ctx = kube.WithExecution(ctx, operationID, planID, record.Plan.Digest)
	app, err := s.Store.ApplicationByID(ctx, record.Plan.ApplicationID)
	if err != nil {
		return operation, err
	}
	actorID := ""
	if operation.ActorID != nil {
		actorID = *operation.ActorID
	}
	finishFailure := func(cause error, planStatus string) (store.Operation, error) {
		finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if errors.Is(cause, store.ErrOperationCancelled) {
			if err := s.Store.FinishCancelledOperation(finishCtx, operation); err != nil {
				return operation, err
			}
			return s.Store.OperationByID(finishCtx, operationID)
		}
		message := safeApplyFailure(cause, operation.Progress, operation.Type)
		operation.Progress.Stop(message)
		if err := s.Store.SetOperationProgress(finishCtx, operationID, operation.Progress); err != nil {
			return operation, errors.Join(cause, err)
		}
		persisted, err := s.Store.OperationByID(finishCtx, operationID)
		if err != nil {
			return operation, errors.Join(cause, err)
		}
		automatic := !app.AutoSyncPaused && !persisted.Progress.CancelRequested && operation.Type == "sync" && !record.Plan.Decommission && record.Plan.Rollback == nil
		decision := DecideRetry(ctx, app.RetryPolicy, operation.AttemptCount, cause, time.Now().UTC(), retryJitterSample(), automatic)
		if persisted.Progress.CancelRequested {
			decision.NextRetryAt = nil
			decision.TerminalReason = "cancelled_after_failure_requires_review"
		}
		if planStatus == "stale" {
			decision.NextRetryAt = nil
			decision.TerminalReason = "plan_changed_requires_review"
		}
		if operation.Type == "rollback" {
			decision.NextRetryAt = nil
			decision.TerminalReason = "rollback_requires_review"
		}
		if record.Plan.Decommission {
			decision.NextRetryAt = nil
			decision.TerminalReason = "decommission_requires_review"
		}
		if decision.NextRetryAt != nil {
			message += " A fresh plan will be built automatically after the retry backoff."
		}
		if err := s.Store.FinishOperationWithRetry(finishCtx, operationID, app.ID, message, decision.AttemptCount, decision.ErrorCode, decision.NextRetryAt, decision.TerminalReason, planStatus); err != nil {
			return operation, errors.Join(cause, err)
		}
		operation.Status, operation.Message = "failed", message
		operation.AttemptCount = decision.AttemptCount
		operation.ErrorCode = decision.ErrorCode
		operation.NextRetryAt = decision.NextRetryAt
		operation.TerminalReason = decision.TerminalReason
		now := time.Now().UTC()
		operation.FinishedAt = &now
		return operation, cause
	}
	if actorID != systemActorID {
		actor, err := s.Store.UserByID(ctx, actorID)
		if err != nil {
			return finishFailure(errors.New("sync actor is no longer available"), "failed")
		}
		role, err := s.Store.WorkspaceRole(ctx, actor, app.WorkspaceID)
		if err != nil || (role != "owner" && role != "deployer") {
			return finishFailure(errors.New("sync actor no longer has deploy permission"), "failed")
		}
		if record.Plan.Decommission && role != "owner" {
			return finishFailure(errors.New("decommission requires a workspace owner"), "failed")
		}
		if record.Plan.Rollback != nil && role != "owner" {
			return finishFailure(errors.New("rollback requires a workspace owner"), "failed")
		}
	}
	if err := s.Store.CheckPlanGuardrails(ctx, record.ID); err != nil {
		return finishFailure(err, "stale")
	}
	authorizationTime := time.Now()
	// The schedule may delay a request already authorized and durably consumed.
	// Expiry still applies at enqueue; role, digest, and exact change checks remain live.
	if operation.ScheduleDeferred {
		authorizationTime = operation.StartedAt
	}
	if required := core.RequiredApprovalCount(record.Plan); required > 0 {
		approvalIDs := operation.ApprovalIDs
		if len(approvalIDs) == 0 && operation.ApprovalID != "" {
			approvalIDs = []string{operation.ApprovalID}
		}
		if len(approvalIDs) < required {
			return finishFailure(fmt.Errorf("queued operation has %d of %d required approvals", len(approvalIDs), required), "failed")
		}
		approvals := make([]core.DeletionApproval, 0, len(approvalIDs))
		seenActors := make(map[string]bool, len(approvalIDs))
		for _, approvalID := range approvalIDs {
			stored, err := s.Store.ApprovalByID(ctx, approvalID)
			if err != nil || stored.PlanID != record.ID || stored.UsedAt == nil {
				return finishFailure(errors.New("queued approval is unavailable"), "failed")
			}
			if seenActors[stored.Approval.ActorID] {
				return finishFailure(errors.New("queued approvals are not from distinct workspace members"), "failed")
			}
			seenActors[stored.Approval.ActorID] = true
			approver, err := s.Store.UserByID(ctx, stored.Approval.ActorID)
			role := ""
			if err == nil {
				role, err = s.Store.WorkspaceRole(ctx, approver, app.WorkspaceID)
			}
			if err != nil || role == "" || !core.ApprovalRoleAllows(record.Plan, role, stored.Approval.ActorID) {
				return finishFailure(errors.New("approver is no longer eligible for this plan"), "failed")
			}
			approvals = append(approvals, stored.Approval)
		}
		if err := core.AuthorizeApplyMany(record.Plan, approvals, authorizationTime); err != nil {
			return finishFailure(err, "failed")
		}
	}
	operation.Progress.Phase = "validating"
	if err := s.Store.SetOperationProgress(ctx, operationID, operation.Progress); err != nil {
		return finishFailure(err, "failed")
	}
	fresh, err := s.RecheckPlan(ctx, app, record)
	if err != nil {
		return finishFailure(err, "failed")
	}
	if record.Status != "current" || !authorizationTime.Before(record.ExpiresAt) || fresh.Digest != record.Plan.Digest {
		return finishFailure(errors.New("plan changed after approval while sync was queued"), "stale")
	}
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return finishFailure(err, "failed")
	}
	input.Selection = record.Plan.Selection
	input.IgnoreRules, err = s.Store.IgnoreRules(ctx, app.ID)
	if err != nil {
		return finishFailure(err, "failed")
	}
	orderedChanges, err := core.OrderChanges(record.Plan.Changes)
	if err != nil {
		return finishFailure(err, "failed")
	}
	// Queued operations created by older executors may have a different row
	// order. Preserve their results while aligning the timeline with execution.
	orderedProgress, err := store.NewOperationProgress(record.Plan)
	if err != nil {
		return finishFailure(err, "failed")
	}
	rank := make(map[string]int, len(orderedProgress.Resources))
	for i, resource := range orderedProgress.Resources {
		rank[resource.Identity.Key()] = i
	}
	sort.SliceStable(operation.Progress.Resources, func(i, j int) bool {
		return rank[operation.Progress.Resources[i].Identity.Key()] < rank[operation.Progress.Resources[j].Identity.Key()]
	})
	desired := make(map[string]core.Resource, len(record.Desired))
	for _, resource := range record.Desired {
		desired[resource.Identity.Key()] = resource
	}
	for _, change := range orderedChanges {
		if change.Kind != core.Delete {
			if _, exists := desired[change.Identity.Key()]; !exists {
				return finishFailure(fmt.Errorf("desired resource missing for %s %s/%s", change.Identity.Kind, change.Identity.Namespace, change.Identity.Name), "failed")
			}
		}
	}
	if len(record.Plan.Changes) > 0 && !record.Plan.Decommission {
		checkpoint, err := s.SavePreOperationCheckpoint(ctx, app, operationID, input)
		if err != nil {
			return finishFailure(fmt.Errorf("could not durably save the pre-operation checkpoint: %w", err), "failed")
		}
		operation.RollbackCheckpointID = checkpoint.ID
	}
	// Admission is checked again after potentially slow live-state validation.
	// Once admitted, the complete operation may finish beyond the window end.
	if operation.ScheduleOverrideReason != "" {
		if err := s.Store.CheckScheduleOverrideOwner(ctx, app.WorkspaceID, operation.ScheduleOverrideActorID); err != nil {
			return finishFailure(err, "failed")
		}
	} else {
		decision, err := s.Store.ApplicationSchedule(ctx, app.ID)
		if err != nil {
			return finishFailure(err, "failed")
		}
		if !decision.Allowed {
			if err := s.Store.DeferScheduledOperation(ctx, operationID, decision); err != nil {
				return finishFailure(err, "failed")
			}
			operation.Status, operation.Message = "queued", "Waiting for deployment schedule"
			operation.WaitingReasons, operation.NextAllowedAt = decision.Reasons, decision.NextAllowedAt
			return operation, nil
		}
	}
	operation.Progress.Phase = "applying"
	if err := s.Store.SetOperationProgress(ctx, operationID, operation.Progress); err != nil {
		return finishFailure(err, "failed")
	}
	beginResource := func(identity core.Identity) error {
		next := operation.Progress
		next.Resources = append([]store.ResourceProgress(nil), next.Resources...)
		next.BeginResource(identity, operation.AttemptCount)
		if err := s.Store.BeginOperationResource(ctx, operationID, next); err != nil {
			return err
		}
		operation.Progress = next
		return nil
	}
	completeResource := func(identity core.Identity) error {
		operation.Progress.CompleteResource(identity)
		if err := s.Store.SetOperationProgress(ctx, operationID, operation.Progress); err != nil {
			return err
		}
		s.checkpoint("resource.recorded")
		return nil
	}
	for _, identity := range record.Plan.NamespaceCreations {
		if !app.CreateNamespaces || record.Plan.Decommission || record.Plan.Rollback != nil || identity.ClusterID != app.ClusterID || identity.Kind != "Namespace" || identity.APIVersion != "v1" || !identity.ClusterScoped || identity.Namespace != "" {
			return finishFailure(errors.New("invalid namespace prerequisite"), "failed")
		}
		client, err := namespaceProvisioningClient(input, identity.Name)
		if err != nil {
			return finishFailure(err, "failed")
		}
		if err := s.Store.RenewOperationLease(ctx, operationID, 90*time.Second); err != nil {
			return finishFailure(err, "failed")
		}
		if err := beginResource(identity); err != nil {
			return finishFailure(err, "failed")
		}
		if err := createNamespacePrerequisite(ctx, client.Dynamic.Resource(namespaceResource), identity.Name); err != nil {
			return finishFailure(fmt.Errorf("create namespace %q: %w", identity.Name, err), "failed")
		}
		if err := completeResource(identity); err != nil {
			return finishFailure(err, "failed")
		}
	}
	for _, change := range orderedChanges {
		if change.Kind == core.Delete {
			continue
		}
		resource := desired[change.Identity.Key()]
		if err := s.Store.RenewOperationLease(ctx, operationID, 90*time.Second); err != nil {
			return finishFailure(err, "failed")
		}
		if record.Plan.Rollback != nil {
			input, err = s.ValidateRollbackResourceStep(ctx, operation, record, app, actorID)
			if err != nil {
				return finishFailure(err, "failed")
			}
		}
		if err := beginResource(resource.Identity); err != nil {
			return finishFailure(err, "failed")
		}
		if err := s.applyResource(ctx, input, app.ID, resource, change); err != nil {
			return finishFailure(err, "failed")
		}
		if err := completeResource(resource.Identity); err != nil {
			return finishFailure(err, "failed")
		}
	}
	for _, change := range orderedChanges {
		if change.Kind != core.Delete {
			continue
		}
		operation.Progress.Phase = "deleting"
		if err := s.Store.SetOperationProgress(ctx, operationID, operation.Progress); err != nil {
			return finishFailure(err, "failed")
		}
		if err := s.Store.RenewOperationLease(ctx, operationID, 90*time.Second); err != nil {
			return finishFailure(err, "failed")
		}
		if record.Plan.Rollback != nil {
			input, err = s.ValidateRollbackResourceStep(ctx, operation, record, app, actorID)
			if err != nil {
				return finishFailure(err, "failed")
			}
		}
		if err := beginResource(change.Identity); err != nil {
			return finishFailure(err, "failed")
		}
		if err := s.deleteResource(ctx, input, app.ID, change); err != nil {
			return finishFailure(err, "failed")
		}
		if err := completeResource(change.Identity); err != nil {
			return finishFailure(err, "failed")
		}
	}
	operation.Progress.Phase = "complete"
	if err := s.Store.BeginOperationResource(ctx, operationID, operation.Progress); err != nil {
		return finishFailure(err, "failed")
	}
	health := "synced"
	if record.Plan.Decommission {
		health = "decommissioned"
	}
	if record.Plan.Rollback != nil {
		rollbackRevision := record.Plan.Rollback.Revision
		requireRevision := rollbackRevision == ""
		if err := s.Store.PinApplicationForRollback(ctx, app.ID, record.ID, record.Plan.Rollback.Settings, rollbackRevision, requireRevision); err != nil {
			return finishFailure(fmt.Errorf("cluster changes completed but JustCD could not pin the rollback target: %w", err), "failed")
		}
		app.SourceID = record.Plan.Rollback.Settings.SourceID
		app.ManifestPath = record.Plan.Rollback.Settings.ManifestPath
		app.TargetManifestPath = record.Plan.Rollback.Settings.TargetManifestPath
		app.NamespaceManifestPaths = record.Plan.Rollback.Settings.NamespaceManifestPaths
		app.Renderer = record.Plan.Rollback.Settings.Renderer
		app.KustomizeHelmEnabled = record.Plan.Rollback.Settings.KustomizeHelmEnabled
		app.KustomizeNamespaceOverride = record.Plan.Rollback.Settings.KustomizeNamespaceOverride
		app.HelmValuesFiles = record.Plan.Rollback.Settings.HelmValuesFiles
		app.HelmValuesYAML = record.Plan.Rollback.Settings.HelmValuesYAML
		app.TargetHelmValuesFiles = record.Plan.Rollback.Settings.TargetHelmValuesFiles
		app.TargetHelmValuesYAML = record.Plan.Rollback.Settings.TargetHelmValuesYAML
		app.NamespaceHelmValues = record.Plan.Rollback.Settings.NamespaceHelmValues
		if rollbackRevision != "" {
			app.Revision = rollbackRevision
		}
	}
	if !record.Plan.Decommission {
		snapshotRevision, snapshotSettings := record.Plan.Revision, rollbackSettings(app)
		if record.Plan.Rollback != nil {
			snapshotRevision = record.Plan.Rollback.Revision
			snapshotSettings = record.Plan.Rollback.Settings
		}
		if _, err := s.SaveSuccessfulDeploymentSnapshot(ctx, app, operationID, snapshotRevision, snapshotSettings, input); err != nil {
			return finishFailure(fmt.Errorf("cluster changes completed but the deployment snapshot could not be saved: %w", err), "failed")
		}
	}
	lastSyncedRevision := record.Plan.Revision
	if record.Plan.Rollback != nil {
		lastSyncedRevision = record.Plan.Rollback.Revision
	}
	message := "Sync completed successfully"
	action := "sync.succeeded"
	if record.Plan.Rollback != nil {
		message = "Rollback completed successfully; automatic reconciliation is paused"
		action = "rollback.succeeded"
	}
	operation.Progress.Phase = "complete"
	details := map[string]any{"operationId": operationID, "planId": record.ID, "revision": lastSyncedRevision, "digest": record.Plan.Digest, "changes": len(record.Plan.Changes)}
	if target := record.Plan.Rollback; target != nil {
		details["rollbackTarget"] = map[string]any{"kind": target.Kind, "id": target.ID, "revision": target.Revision}
	}
	operation.Message = message
	s.checkpoint("operation.before_complete")
	if err := s.Store.CompleteDeployment(ctx, operation, lastSyncedRevision, health, action, details); err != nil {
		return operation, err
	}
	s.checkpoint("operation.completed")
	operation.Status = "succeeded"
	operation.Message = message
	now := time.Now().UTC()
	operation.FinishedAt = &now
	return operation, nil
}

func safeApplyFailure(cause error, progress store.OperationProgress, operationType ...string) string {
	name := "Sync"
	if len(operationType) > 0 && operationType[0] == "rollback" {
		name = "Rollback"
	}
	if len(progress.Completed) > 0 {
		reason := ""
		if cause != nil && strings.Contains(cause.Error(), "changed after") {
			reason = " Live state or ownership changed after review."
		}
		return fmt.Sprintf("%s stopped after %d of %d resources; completed resources remain applied and were not rolled back.%s Review cluster state and rebuild the plan before retrying.", name, len(progress.Completed), progress.Total, reason)
	}
	if apierrors.IsConflict(cause) && strings.Contains(cause.Error(), "conflict with") {
		return fmt.Sprintf("%s stopped because Kubernetes field ownership conflicts with another manager. Review the affected fields and create a fresh plan before retrying. No automatic rollback was attempted.", name)
	}
	if cause != nil && (strings.Contains(cause.Error(), "changed after") || strings.Contains(cause.Error(), "recheck")) {
		return fmt.Sprintf("%s stopped because live state changed after review. No automatic rollback was attempted. Rebuild and review the plan.", name)
	}
	return fmt.Sprintf("%s did not complete. No automatic rollback was attempted; review cluster state and server logs before retrying.", name)
}

func (s *Service) applyResource(ctx context.Context, input planInput, applicationID string, resource core.Resource, change core.Change) error {
	mapping, err := input.Mapper.Mapper.RESTMapping(groupKind(resource.Identity), groupVersion(resource.Identity))
	if err != nil {
		return err
	}
	client, err := clientFor(input, resource.Identity)
	if err != nil {
		return err
	}
	var resourceClient dynamic.ResourceInterface
	if mapping.Scope.Name() == "namespace" {
		resourceClient = client.Dynamic.Resource(mapping.Resource).Namespace(resource.Identity.Namespace)
	} else {
		resourceClient = client.Dynamic.Resource(mapping.Resource)
	}
	var existingUID types.UID
	resourceVersion := change.LiveResourceVersion
	if change.Kind == core.Create {
		if _, err := resourceClient.Get(ctx, resource.Identity.Name, metav1.GetOptions{}); err == nil {
			return fmt.Errorf("create target %s %s/%s appeared after plan review; create a new plan", resource.Identity.Kind, resource.Identity.Namespace, resource.Identity.Name)
		} else if !apierrors.IsNotFound(err) {
			return fmt.Errorf("recheck create target %s %s/%s: %w", resource.Identity.Kind, resource.Identity.Namespace, resource.Identity.Name, err)
		}
	} else if change.Kind == core.Update {
		current, err := resourceClient.Get(ctx, resource.Identity.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("recheck update target %s %s/%s: %w", resource.Identity.Kind, resource.Identity.Namespace, resource.Identity.Name, err)
		}
		if err := validateUpdateTarget(current, change, applicationID, ignoredPathsFor(input, resource.Identity)); err != nil {
			return err
		}
		existingUID = current.GetUID()
		resourceVersion = current.GetResourceVersion()
	}
	object, err := render.NormalizedObject(resource)
	if err != nil {
		return err
	}
	object.SetLabels(mergeOwnerLabel(object.GetLabels(), applicationID))
	body, err := object.MarshalJSON()
	if err != nil {
		return err
	}
	if change.Kind == core.Create {
		applied, err := resourceClient.Create(ctx, object, metav1.CreateOptions{FieldManager: "justcd/" + applicationID})
		if err != nil {
			return fmt.Errorf("create failed for %s %s/%s: %w", resource.Identity.Kind, resource.Identity.Namespace, resource.Identity.Name, err)
		}
		s.checkpoint("resource.applied")
		current, err := render.CanonicalLive(applied, input.Cluster.ID, applicationID, resource.Identity.ClusterScoped)
		if err != nil {
			return err
		}
		current.Manifest = resource.Manifest
		return s.Store.UpsertManagedResource(ctx, applicationID, current)
	}
	object.SetResourceVersion(resourceVersion)
	body, err = object.MarshalJSON()
	if err != nil {
		return err
	}
	fieldManager := "justcd/" + applicationID
	patchOptions := applyPatchOptions(change, fieldManager)
	applied, err := applyWithOwnershipMigration(ctx, resourceClient, resource.Identity.Name, body, patchOptions)
	if err != nil {
		return fmt.Errorf("server-side apply failed for %s %s/%s: %w", resource.Identity.Kind, resource.Identity.Namespace, resource.Identity.Name, err)
	}
	s.checkpoint("resource.applied")
	if applied.GetUID() != existingUID {
		return fmt.Errorf("update target %s %s/%s was replaced during apply; create a new plan", resource.Identity.Kind, resource.Identity.Namespace, resource.Identity.Name)
	}
	current, err := render.CanonicalLive(applied, input.Cluster.ID, applicationID, resource.Identity.ClusterScoped)
	if err != nil {
		return err
	}
	current.Manifest = resource.Manifest
	if err := s.Store.UpsertManagedResource(ctx, applicationID, current); err != nil {
		return err
	}
	return nil
}

func applyPatchOptions(change core.Change, fieldManager string) metav1.PatchOptions {
	options := metav1.PatchOptions{FieldManager: fieldManager}
	if change.Takeover {
		force := true
		options.Force = &force
	}
	return options
}

// Validate reviewed configuration rather than the review-time version: controllers
// may update status while earlier resources in a long rollout are applied. The
// subsequent PATCH still pins the freshly checked version to reject write races.
func validateUpdateTarget(current *unstructured.Unstructured, change core.Change, applicationID string, ignoredPaths []string) error {
	if len(change.Before) == 0 {
		return validateUpdatePreconditions(change.Identity, change.LiveUID, change.LiveResourceVersion, applicationID, string(current.GetUID()), current.GetResourceVersion(), current.GetLabels()["justcd.io/application-id"])
	}
	if err := validateUpdatePreconditions(change.Identity, change.LiveUID, current.GetResourceVersion(), applicationID, string(current.GetUID()), current.GetResourceVersion(), current.GetLabels()["justcd.io/application-id"]); err != nil {
		return err
	}
	canonical, err := render.CanonicalLive(current, change.Identity.ClusterID, applicationID, change.Identity.ClusterScoped)
	if err != nil {
		return err
	}
	actual, reviewed, err := render.ServerSideComparisonManifests(canonical.Manifest, change.Before, ignoredPaths)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, reviewed) {
		return fmt.Errorf("update target %s %s/%s changed after plan review; create a new plan", change.Identity.Kind, change.Identity.Namespace, change.Identity.Name)
	}
	return nil
}

func validateUpdatePreconditions(identity core.Identity, expectedUID, expectedResourceVersion, applicationID, actualUID, actualResourceVersion, actualOwner string) error {
	if actualUID != expectedUID || actualResourceVersion != expectedResourceVersion || actualOwner != applicationID {
		return fmt.Errorf("update target %s %s/%s changed after plan review; create a new plan", identity.Kind, identity.Namespace, identity.Name)
	}
	return nil
}

func (s *Service) deleteResource(ctx context.Context, input planInput, applicationID string, change core.Change) error {
	mapping, err := input.Mapper.Mapper.RESTMapping(groupKind(change.Identity), groupVersion(change.Identity))
	if err != nil {
		return err
	}
	client, err := clientFor(input, change.Identity)
	if err != nil {
		return err
	}
	var resourceClient dynamic.ResourceInterface
	if mapping.Scope.Name() == "namespace" {
		resourceClient = client.Dynamic.Resource(mapping.Resource).Namespace(change.Identity.Namespace)
	} else {
		resourceClient = client.Dynamic.Resource(mapping.Resource)
	}
	current, err := resourceClient.Get(ctx, change.Identity.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("recheck delete target %s %s/%s: %w", change.Identity.Kind, change.Identity.Namespace, change.Identity.Name, err)
	}
	if string(current.GetUID()) != change.LiveUID || current.GetLabels()["justcd.io/application-id"] != applicationID {
		return errors.New("delete target UID or ownership changed after approval")
	}
	canonical, err := render.CanonicalLive(current, input.Cluster.ID, applicationID, change.Identity.ClusterScoped)
	if err != nil {
		return err
	}
	if canonical.Fingerprint != change.LiveFingerprint {
		return errors.New("delete target changed after approval; create a new plan")
	}
	uid := types.UID(change.LiveUID)
	// Controllers can update status while earlier resources are being deleted
	// (notably PDBs after workloads disappear). CanonicalLive excludes status and
	// server metadata, so unchanged reviewed content remains safe to delete. Use
	// the version just checked to reject any race between this GET and DELETE.
	resourceVersion := canonical.ResourceVersion
	err = resourceClient.Delete(ctx, change.Identity.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion}})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete failed for %s %s/%s: %w", change.Identity.Kind, change.Identity.Namespace, change.Identity.Name, err)
	}
	s.checkpoint("resource.deleted")
	return s.Store.DeleteManagedResource(ctx, applicationID, change.Identity)
}

func mergeOwnerLabel(labels map[string]string, applicationID string) map[string]string {
	if labels == nil {
		labels = map[string]string{}
	}
	labels["justcd.io/application-id"] = applicationID
	return labels
}
