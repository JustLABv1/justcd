package syncer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

type StalePlanError struct{ Fresh store.PlanRecord }

func (e *StalePlanError) Error() string { return "plan changed and requires a new review" }

func (s *Service) Apply(ctx context.Context, planID, actorID, approvalID string) (store.Operation, error) {
	record, err := s.Store.PlanByID(ctx, planID)
	if err != nil {
		return store.Operation{}, err
	}
	app, err := s.Store.ApplicationByID(ctx, record.Plan.ApplicationID)
	if err != nil {
		return store.Operation{}, err
	}
	fresh, _, err := s.CalculatePlan(ctx, app)
	if err != nil {
		return store.Operation{}, err
	}
	if record.Status != "current" || time.Now().After(record.ExpiresAt) || fresh.Digest != record.Plan.Digest {
		newRecord, err := s.BuildPlan(ctx, app.ID, actorID)
		if err != nil {
			return store.Operation{}, err
		}
		return store.Operation{}, &StalePlanError{Fresh: newRecord}
	}
	var approval *core.DeletionApproval
	if record.Plan.RequiresApproval {
		if approvalID == "" {
			return store.Operation{}, errors.New("plan requires an owner approval")
		}
		stored, err := s.Store.ApprovalByID(ctx, approvalID)
		if err != nil {
			return store.Operation{}, errors.New("approval is expired, already used, or unavailable")
		}
		if stored.PlanID != record.ID {
			return store.Operation{}, errors.New("approval is for a different plan")
		}
		approval = &stored.Approval
		if err := core.AuthorizeApply(record.Plan, approval, time.Now()); err != nil {
			return store.Operation{}, err
		}
	} else if approvalID != "" {
		return store.Operation{}, errors.New("this plan does not require an approval")
	}
	operationID, err := s.Store.AcquireOperation(ctx, app.ID, record.ID, actorID, 10*time.Minute)
	if err != nil {
		return store.Operation{}, err
	}
	operation := store.Operation{ID: operationID, ApplicationID: app.ID, PlanID: &record.ID, ActorID: &actorID, Status: "running", StartedAt: time.Now().UTC()}
	finishFailure := func(cause error) (store.Operation, error) {
		message := safeApplyFailure(cause)
		_ = s.Store.FinishOperation(context.Background(), operationID, "failed", message)
		_ = s.Store.SetPlanStatus(context.Background(), record.ID, "failed")
		_, _ = s.Store.DB.ExecContext(context.Background(), `UPDATE applications SET health='degraded',last_checked_at=NOW(),updated_at=NOW() WHERE id=$1`, app.ID)
		_ = s.Store.Audit(context.Background(), actorID, "sync.failed", "application", app.ID, map[string]any{"operationId": operationID, "planId": record.ID, "message": message})
		operation.Status = "failed"
		operation.Message = message
		now := time.Now().UTC()
		operation.FinishedAt = &now
		return operation, cause
	}
	if record.Plan.RequiresApproval {
		used, err := s.Store.ConsumeApproval(ctx, approvalID, record.ID, record.Plan.Digest)
		if err != nil {
			return finishFailure(err)
		}
		if !used {
			return finishFailure(errors.New("approval has already been used or expired"))
		}
	}
	input, err := s.loadPlanInput(ctx, app)
	if err != nil {
		return finishFailure(err)
	}
	resources := append([]core.Resource(nil), record.Desired...)
	sort.Slice(resources, func(i, j int) bool { return resources[i].Identity.Key() < resources[j].Identity.Key() })
	for _, resource := range resources {
		change, exists := changeFor(record.Plan.Changes, resource.Identity)
		if !exists || change.Kind == core.Delete {
			continue
		}
		if err := s.Store.RenewOperationLease(ctx, operationID, 10*time.Minute); err != nil {
			return finishFailure(err)
		}
		if err := s.applyResource(ctx, input, app.ID, resource, change); err != nil {
			return finishFailure(err)
		}
	}
	for _, change := range record.Plan.Changes {
		if change.Kind != core.Delete {
			continue
		}
		if err := s.Store.RenewOperationLease(ctx, operationID, 10*time.Minute); err != nil {
			return finishFailure(err)
		}
		if err := s.deleteResource(ctx, input, app.ID, change); err != nil {
			return finishFailure(err)
		}
	}
	if err := s.Store.FinishOperation(ctx, operationID, "succeeded", "Sync completed successfully"); err != nil {
		return operation, err
	}
	if err := s.Store.SetPlanStatus(ctx, record.ID, "applied"); err != nil {
		return operation, err
	}
	if err := s.Store.MarkApplicationSynced(ctx, app.ID, record.Plan.Revision, "synced"); err != nil {
		return operation, err
	}
	_ = s.Store.Audit(ctx, actorID, "sync.succeeded", "application", app.ID, map[string]any{"operationId": operationID, "planId": record.ID, "revision": record.Plan.Revision, "digest": record.Plan.Digest})
	operation.Status = "succeeded"
	operation.Message = "Sync completed successfully"
	now := time.Now().UTC()
	operation.FinishedAt = &now
	return operation, nil
}

func safeApplyFailure(cause error) string {
	if cause != nil && (strings.Contains(cause.Error(), "changed after") || strings.Contains(cause.Error(), "recheck")) {
		return "Sync stopped because live state changed after review. Rebuild and review the plan."
	}
	return "Sync failed before completion. Review server logs for diagnostic details."
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
		if string(current.GetUID()) != change.LiveUID || current.GetResourceVersion() != change.LiveResourceVersion || current.GetLabels()["justcd.io/application-id"] != applicationID {
			return fmt.Errorf("update target %s %s/%s changed after plan review; create a new plan", resource.Identity.Kind, resource.Identity.Namespace, resource.Identity.Name)
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
	applied, err := resourceClient.Patch(ctx, resource.Identity.Name, types.ApplyPatchType, body, metav1.PatchOptions{FieldManager: fieldManager})
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
