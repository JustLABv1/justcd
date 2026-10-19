package syncer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/render"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

func fieldOwnershipConflict(err error) bool {
	var status apierrors.APIStatus
	if !errors.As(err, &status) || !apierrors.IsConflict(err) {
		return false
	}
	details := status.Status().Details
	if details == nil || len(details.Causes) == 0 {
		return false
	}
	for _, cause := range details.Causes {
		if cause.Type != metav1.CauseTypeFieldManagerConflict {
			return false
		}
	}
	return true
}

// applyWithOwnershipMigration transitions JustCD's own Create/Update field
// ownership to Apply ownership. Kubernetes distinguishes these operations even
// under the same manager name. Only conflicts exclusively with this application's
// manager, or the legacy client-side manager of an explicitly adopted object,
// can be retried automatically. Other managers require a sealed takeover.
// Preserve the caller's dry-run and resource-version preconditions on the retry.
func applyWithOwnershipMigration(ctx context.Context, rc dynamic.ResourceInterface, name string, body []byte, options metav1.PatchOptions) (*unstructured.Unstructured, error) {
	// Inspect the pre-apply snapshot, including for removal-only updates that
	// produce no SSA conflict. Their omitted legacy fields still need cleanup.
	var legacyManagers []string
	request := &unstructured.Unstructured{}
	if request.UnmarshalJSON(body) == nil && request.GetResourceVersion() != "" {
		if live, err := rc.Get(ctx, name, metav1.GetOptions{}); err == nil && live.GetResourceVersion() == request.GetResourceVersion() && pendingClientSideAdoption(live, options.FieldManager) {
			legacyManagers = []string{"kubectl-client-side-apply"}
		}
	}
	object, err := rc.Patch(ctx, name, types.ApplyPatchType, body, options)
	if options.Force != nil && *options.Force || !fieldOwnershipConflict(err) {
		if err != nil {
			return object, err
		}
		return finishOwnershipMigration(ctx, rc, name, object, options, legacyManagers...)
	}
	var status apierrors.APIStatus
	if !errors.As(err, &status) {
		return object, err
	}
	legacy := false
	for _, cause := range status.Status().Details.Causes {
		if managerConflict(cause.Message, options.FieldManager) {
			continue
		}
		if managerConflict(cause.Message, "kubectl-client-side-apply") {
			legacy = true
			continue
		}
		return object, err
	}
	if legacy && len(legacyManagers) == 0 {
		return object, err
	}
	force := true
	options.Force = &force
	object, err = rc.Patch(ctx, name, types.ApplyPatchType, body, options)
	if err != nil {
		return object, err
	}
	return finishOwnershipMigration(ctx, rc, name, object, options, legacyManagers...)
}

func managerConflict(message, manager string) bool {
	prefix := "conflict with " + strconv.Quote(manager)
	return message == prefix || strings.HasPrefix(message, prefix+" using ")
}

// Adoption is the explicit authorization to replace the old client-side manager.
// Only finish that initial handoff: once this app has Apply ownership, subsequent
// external kubectl edits are real conflicts and must still require review.
func pendingClientSideAdoption(object *unstructured.Unstructured, manager string) bool {
	applicationID, ok := strings.CutPrefix(manager, "justcd/")
	if !ok || applicationID == "" || object.GetLabels()["justcd.io/application-id"] != applicationID {
		return false
	}
	adopted, clientSide := false, false
	for _, entry := range object.GetManagedFields() {
		if entry.Subresource != "" {
			continue
		}
		if entry.Manager == manager && entry.Operation == metav1.ManagedFieldsOperationApply {
			return false
		}
		if entry.Operation == metav1.ManagedFieldsOperationUpdate {
			adopted = adopted || entry.Manager == "justcd-adoption/"+applicationID
			clientSide = clientSide || entry.Manager == "kubectl-client-side-apply"
		}
	}
	return adopted && clientSide
}

// reconcileUpdatePlan validates ownership and compares the live snapshot with
// the server-side apply dry-run result before sealing the approval policy.
// Admission defaults and CRD list merging are interpreted by Kubernetes, not
// by the local list projection. The original Git manifest remains the apply body.
func (s *Service) reconcileUpdatePlan(ctx context.Context, input planInput, plan *core.Plan, desired, live []core.Resource) error {
	byKey := make(map[string]core.Resource, len(desired))
	for _, resource := range desired {
		byKey[resource.Identity.Key()] = resource
	}
	liveByKey := make(map[string]core.Resource, len(live))
	for _, resource := range live {
		liveByKey[resource.Identity.Key()] = resource
	}
	changes := make([]core.Change, 0, len(plan.Changes))
	for index := range plan.Changes {
		change := &plan.Changes[index]
		if change.Kind != core.Update {
			changes = append(changes, *change)
			continue
		}
		resource, ok := byKey[change.Identity.Key()]
		if !ok {
			return errors.New("update plan desired resource is missing")
		}
		mapping, err := input.Mapper.Mapper.RESTMapping(groupKind(change.Identity), groupVersion(change.Identity))
		if err != nil {
			return err
		}
		client, err := clientFor(input, change.Identity)
		if err != nil {
			return err
		}
		var rc dynamic.ResourceInterface = client.Dynamic.Resource(mapping.Resource)
		if mapping.Scope.Name() == "namespace" {
			rc = client.Dynamic.Resource(mapping.Resource).Namespace(change.Identity.Namespace)
		}
		object, err := render.NormalizedObject(resource)
		if err != nil {
			return err
		}
		object.SetLabels(mergeOwnerLabel(object.GetLabels(), plan.ApplicationID))
		object.SetResourceVersion(change.LiveResourceVersion)
		body, err := object.MarshalJSON()
		if err != nil {
			return err
		}
		options := applyPatchOptions(*change, "justcd/"+plan.ApplicationID)
		options.DryRun = []string{metav1.DryRunAll}
		applied, err := applyWithOwnershipMigration(ctx, rc, change.Identity.Name, body, options)
		if !change.Takeover && fieldOwnershipConflict(err) {
			change.Takeover = true
			options = applyPatchOptions(*change, "justcd/"+plan.ApplicationID)
			options.DryRun = []string{metav1.DryRunAll}
			applied, err = applyWithOwnershipMigration(ctx, rc, change.Identity.Name, body, options)
		}
		if err != nil {
			return &resourceAccessError{identity: change.Identity, action: "patch (dry-run)", err: err}
		}
		current, ok := liveByKey[change.Identity.Key()]
		if !ok {
			return errors.New("update plan live resource is missing")
		}
		if applied == nil || applied.GetAPIVersion() != change.Identity.APIVersion || applied.GetKind() != change.Identity.Kind || applied.GetName() != change.Identity.Name || applied.GetNamespace() != change.Identity.Namespace {
			return fmt.Errorf("invalid dry-run response for %s %s/%s", change.Identity.Kind, change.Identity.Namespace, change.Identity.Name)
		}
		predicted, err := render.CanonicalLive(applied, change.Identity.ClusterID, plan.ApplicationID, change.Identity.ClusterScoped)
		if err != nil {
			return err
		}
		before, after, err := render.ServerSideComparisonManifests(current.Manifest, predicted.Manifest, ignoredPathsFor(input, change.Identity))
		if err != nil {
			return err
		}
		paths, err := render.ChangedJSONPointers(before, after)
		if err != nil {
			return err
		}
		// A no-op value change may still transfer ownership. Keep takeovers
		// visible so auto-safe never silently approves an ownership transfer.
		if len(paths) == 0 && !change.Takeover {
			continue
		}
		change.Before, change.After, change.ChangedPaths = before, after, paths
		changes = append(changes, *change)
	}
	plan.Changes = changes
	return nil
}
