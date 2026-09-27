package syncer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/observability"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	fresh, err := s.RecheckPlan(ctx, app, record)
	if err != nil {
		return store.Operation{}, err
	}
	if record.Status != "current" || !time.Now().Before(record.ExpiresAt) || fresh.Digest != record.Plan.Digest {
		newRecord, err := s.RefreshPlan(ctx, app, actorID, record)
		if err != nil {
			return store.Operation{}, err
		}
		return store.Operation{}, &StalePlanError{Fresh: newRecord}
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
	progress := store.OperationProgress{Phase: "queued", Total: len(record.Plan.Changes), Completed: []core.Identity{}}
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
	app, err := s.Store.ApplicationByID(ctx, record.Plan.ApplicationID)
	if err != nil {
		return operation, err
	}
	actorID := ""
	if operation.ActorID != nil {
		actorID = *operation.ActorID
	}
	finishFailure := func(cause error, planStatus string) (store.Operation, error) {
		message := safeApplyFailure(cause, operation.Progress, operation.Type)
		automatic := !app.AutoSyncPaused && operation.Type == "sync" && !record.Plan.Decommission && record.Plan.Rollback == nil
		decision := DecideRetry(ctx, app.RetryPolicy, operation.AttemptCount, cause, time.Now().UTC(), retryJitterSample(), automatic)
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
		if err := s.Store.FinishOperationWithRetry(context.Background(), operationID, app.ID, message, decision.AttemptCount, decision.ErrorCode, decision.NextRetryAt, decision.TerminalReason); err != nil {
			_ = s.Store.FinishOperation(context.Background(), operationID, "failed", message)
		}
		if planStatus != "" {
			_ = s.Store.SetPlanStatus(context.Background(), planID, planStatus)
		}
		if decision.NextRetryAt == nil && (app.SyncPolicy == "auto-safe" || operation.Type == "rollback") {
			_ = s.Store.PauseAutoSync(context.Background(), app.ID)
		}
		action := "sync.failed"
		if record.Plan.Rollback != nil {
			action = "rollback.failed"
		}
		_ = s.Store.Audit(context.Background(), actorID, action, "application", app.ID, map[string]any{"operationId": operationID, "planId": planID, "message": message, "attemptCount": decision.AttemptCount, "errorCode": decision.ErrorCode, "nextRetryAt": decision.NextRetryAt, "terminalReason": decision.TerminalReason, "completed": len(operation.Progress.Completed), "total": operation.Progress.Total, "rollbackCheckpointId": operation.RollbackCheckpointID})
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
		if err := core.AuthorizeApplyMany(record.Plan, approvals, time.Now()); err != nil {
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
	if record.Status != "current" || !time.Now().Before(record.ExpiresAt) || fresh.Digest != record.Plan.Digest {
		return finishFailure(errors.New("plan changed after approval while sync was queued"), "stale")
	}
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return finishFailure(err, "failed")
	}
	if len(record.Plan.Changes) > 0 && !record.Plan.Decommission {
		checkpoint, err := s.SavePreOperationCheckpoint(ctx, app, operationID, input)
		if err != nil {
			return finishFailure(fmt.Errorf("could not durably save the pre-operation checkpoint: %w", err), "failed")
		}
		operation.RollbackCheckpointID = checkpoint.ID
	}
	operation.Progress.Phase = "applying"
	if err := s.Store.SetOperationProgress(ctx, operationID, operation.Progress); err != nil {
		return finishFailure(err, "failed")
	}
	beginResource := func(identity core.Identity) error {
		operation.Progress.Current = &identity
		return s.Store.SetOperationProgress(ctx, operationID, operation.Progress)
	}
	completeResource := func(identity core.Identity) error {
		operation.Progress.Completed = append(operation.Progress.Completed, identity)
		operation.Progress.Current = nil
		return s.Store.SetOperationProgress(ctx, operationID, operation.Progress)
	}
	resources := append([]core.Resource(nil), record.Desired...)
	sort.Slice(resources, func(i, j int) bool { return resources[i].Identity.Key() < resources[j].Identity.Key() })
	for _, resource := range resources {
		change, exists := changeFor(record.Plan.Changes, resource.Identity)
		if !exists || change.Kind == core.Delete {
			continue
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
	for _, change := range record.Plan.Changes {
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
	if err := s.Store.MarkApplicationSynced(ctx, app.ID, lastSyncedRevision, health); err != nil {
		return finishFailure(fmt.Errorf("cluster changes completed but sync status could not be saved: %w", err), "failed")
	}
	if err := s.Store.SetPlanStatus(ctx, record.ID, "applied"); err != nil {
		return finishFailure(fmt.Errorf("cluster changes completed but plan status could not be saved: %w", err), "failed")
	}
	message := "Sync completed successfully"
	action := "sync.succeeded"
	if record.Plan.Rollback != nil {
		message = "Rollback completed successfully; automatic reconciliation is paused"
		action = "rollback.succeeded"
	}
	if err := s.Store.FinishOperation(ctx, operationID, "succeeded", message); err != nil {
		return operation, err
	}
	operation.Progress.Phase = "complete"
	_ = s.Store.SetOperationProgress(ctx, operationID, operation.Progress)
	details := map[string]any{"operationId": operationID, "planId": record.ID, "revision": lastSyncedRevision, "digest": record.Plan.Digest, "changes": len(record.Plan.Changes)}
	if target := record.Plan.Rollback; target != nil {
		details["rollbackTarget"] = map[string]any{"kind": target.Kind, "id": target.ID, "revision": target.Revision}
	}
	_ = s.Store.Audit(ctx, actorID, action, "application", app.ID, details)
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
		return fmt.Sprintf("%s stopped after %d of %d resources; completed resources remain applied and were not rolled back. Review cluster state and rebuild the plan before retrying.", name, len(progress.Completed), progress.Total)
	}
	if cause != nil && (strings.Contains(cause.Error(), "changed after") || strings.Contains(cause.Error(), "recheck")) {
		return fmt.Sprintf("%s stopped because live state changed after review. No automatic rollback was attempted. Rebuild and review the plan.", name)
	}
	return fmt.Sprintf("%s did not complete. No automatic rollback was attempted; review cluster state and server logs before retrying.", name)
}

func changeFor(changes []core.Change, identity core.Identity) (core.Change, bool) {
	for _, change := range changes {
		if change.Identity.Key() == identity.Key() {
			return change, true
		}
	}
	return core.Change{}, false
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
		if err := validateUpdatePreconditions(resource.Identity, change.LiveUID, change.LiveResourceVersion, applicationID, string(current.GetUID()), current.GetResourceVersion(), current.GetLabels()["justcd.io/application-id"]); err != nil {
			return err
		}
		existingUID = current.GetUID()
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
		current, err := render.CanonicalLive(applied, input.Cluster.ID, applicationID, resource.Identity.ClusterScoped)
		if err != nil {
			return err
		}
		current.Manifest = resource.Manifest
		return s.Store.UpsertManagedResource(ctx, applicationID, current)
	}
	object.SetResourceVersion(change.LiveResourceVersion)
	body, err = object.MarshalJSON()
	if err != nil {
		return err
	}
	fieldManager := "justcd/" + applicationID
	patchOptions := applyPatchOptions(change, fieldManager)
	applied, err := resourceClient.Patch(ctx, resource.Identity.Name, types.ApplyPatchType, body, patchOptions)
	if err != nil {
		return fmt.Errorf("server-side apply failed for %s %s/%s: %w", resource.Identity.Kind, resource.Identity.Namespace, resource.Identity.Name, err)
	}
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
	if canonical.Fingerprint != change.LiveFingerprint || canonical.ResourceVersion != change.LiveResourceVersion {
		return errors.New("delete target changed after approval; create a new plan")
	}
	uid := types.UID(change.LiveUID)
	resourceVersion := change.LiveResourceVersion
	err = resourceClient.Delete(ctx, change.Identity.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion}})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete failed for %s %s/%s: %w", change.Identity.Kind, change.Identity.Namespace, change.Identity.Name, err)
	}
	return s.Store.DeleteManagedResource(ctx, applicationID, change.Identity)
}

func mergeOwnerLabel(labels map[string]string, applicationID string) map[string]string {
	if labels == nil {
		labels = map[string]string{}
	}
	labels["justcd.io/application-id"] = applicationID
	return labels
}
