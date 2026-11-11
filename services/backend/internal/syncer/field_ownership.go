package syncer

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	// legacyManagers may always be cleaned up after an Apply: a leftover
	// client-side entry keeps co-owning fields removed from Git, so server-side
	// apply would never delete them. Only the initial adoption handoff may
	// force through conflicts with that manager (pendingAdoption).
	var legacyManagers []string
	pendingAdoption := false
	request := &unstructured.Unstructured{}
	if request.UnmarshalJSON(body) == nil && request.GetResourceVersion() != "" {
		if live, err := rc.Get(ctx, name, metav1.GetOptions{}); err == nil && live.GetResourceVersion() == request.GetResourceVersion() {
			pendingAdoption = pendingClientSideAdoption(live, options.FieldManager)
			legacyManagers = leftoverLegacyManagers(live, options.FieldManager)
			if pendingAdoption && len(legacyManagers) == 0 {
				legacyManagers = []string{"kubectl-client-side-apply"}
			}
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
	if legacy && !pendingAdoption {
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

// legacyFieldManagers are managers whose Update entries predate server-side
// apply (kubectl, Helm 3, or objects written before managed fields existed).
var legacyFieldManagers = map[string]bool{"kubectl-client-side-apply": true, "before-first-apply": true, "helm": true}

// leftoverLegacyManagers returns the legacy Update managers still present on an
// object owned by this application, e.g. after an adoption whose handoff never
// completed. Their stale fields are cleaned up after Apply.
func leftoverLegacyManagers(object *unstructured.Unstructured, manager string) []string {
	applicationID, ok := strings.CutPrefix(manager, "justcd/")
	if !ok || applicationID == "" || object.GetLabels()["justcd.io/application-id"] != applicationID {
		return nil
	}
	var found []string
	seen := map[string]bool{}
	for _, entry := range object.GetManagedFields() {
		if entry.Subresource == "" && entry.Operation == metav1.ManagedFieldsOperationUpdate && legacyFieldManagers[entry.Manager] && !seen[entry.Manager] {
			seen[entry.Manager] = true
			found = append(found, entry.Manager)
		}
	}
	sort.Strings(found)
	return found
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

// unhonoredGitRemovals returns paths the Git-owned comparison shows as removed
// (present in the live projection, absent from desired) that are still present
// in the live object the server-side dry-run was compared against.
func unhonoredGitRemovals(localBefore, localAfter, live []byte) ([]string, error) {
	if len(localBefore) == 0 || len(localAfter) == 0 {
		return nil, nil
	}
	changed, err := render.ChangedJSONPointers(localBefore, localAfter)
	if err != nil {
		return nil, err
	}
	removed := make([]string, 0, len(changed))
	for _, path := range changed {
		if render.JSONPointerExists(localBefore, path) && !render.JSONPointerExists(localAfter, path) && render.JSONPointerExists(live, path) {
			removed = append(removed, path)
		}
	}
	return removed, nil
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
		if immutableStatefulSetUpdate(change.Identity, err) {
			change.Takeover = false
			change.RecreationRequired = true
			if err := s.planStatefulSetRecreation(ctx, input, plan, change, object); err != nil {
				return err
			}
			changes = append(changes, *change)
			continue
		}
		if err != nil {
			return &resourceAccessError{identity: change.Identity, action: "patch (dry-run)", err: err}
		}
		for _, selected := range plan.Selection.Recreations {
			if selected.Identity.Key() == change.Identity.Key() {
				return errors.New("StatefulSet no longer requires recreation; refresh the plan")
			}
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
		if len(paths) == 0 {
			// Server-side apply keeps fields that another manager still owns,
			// so a field removed from Git can vanish from the dry-run diff while
			// it is still live. Keep that Git-driven removal visible.
			removed, err := unhonoredGitRemovals(change.Before, change.After, before)
			if err != nil {
				return err
			}
			if len(removed) > 0 {
				for _, path := range removed {
					if after, err = render.RemoveJSONPointer(after, path); err != nil {
						return err
					}
				}
				paths = removed
			}
		}
		// A no-op value change may still transfer ownership. Keep takeovers
		// visible so auto-safe never silently approves an ownership transfer.
		if len(paths) == 0 && !change.Takeover {
			continue
		}
		change.Before, change.After, change.ChangedPaths = before, after, paths
		changes = append(changes, *change)
	}
	for _, selected := range plan.Selection.Recreations {
		found := false
		for _, change := range changes {
			if change.Identity.Key() == selected.Identity.Key() && change.Recreation != nil {
				found = true
			}
		}
		if !found {
			return errors.New("recreation selection no longer matches an immutable StatefulSet update")
		}
	}
	plan.Changes = changes
	return nil
}
