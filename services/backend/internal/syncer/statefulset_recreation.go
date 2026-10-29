package syncer

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/render"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
)

var recreationResources = map[string]schema.GroupVersionResource{
	"Pod":                   {Version: "v1", Resource: "pods"},
	"PersistentVolumeClaim": {Version: "v1", Resource: "persistentvolumeclaims"},
	"ControllerRevision":    {Group: "apps", Version: "v1", Resource: "controllerrevisions"},
}

func immutableStatefulSetUpdate(id core.Identity, err error) bool {
	if id.APIVersion != "apps/v1" || id.Kind != "StatefulSet" || !apierrors.IsInvalid(err) {
		return false
	}
	var status apierrors.APIStatus
	if !errors.As(err, &status) || status.Status().Details == nil {
		return false
	}
	// Match Kubernetes' specific immutable StatefulSet validation, not unrelated
	// admission errors or schema failures, which must continue failing closed.
	for _, cause := range status.Status().Details.Causes {
		if cause.Field == "spec" && cause.Type == metav1.CauseType("FieldValueForbidden") && strings.Contains(cause.Message, "updates to statefulset spec for fields other than") {
			return true
		}
	}
	return false
}

func validRecreationPolicy(policy string) bool {
	return policy == "Orphan" || policy == "Foreground" || policy == "Background"
}

func recreationSafety(before, after *unstructured.Unstructured, policy string) string {
	for _, owner := range before.GetOwnerReferences() {
		if owner.Controller != nil && *owner.Controller {
			return "StatefulSet is managed by another controller. Update the parent resource or operator instead of recreating its StatefulSet."
		}
	}
	for _, obj := range []*unstructured.Unstructured{before, after} {
		if len(obj.GetFinalizers()) > 0 {
			return "StatefulSet recreation requires no custom finalizers. Resolve their cleanup requirements separately before recreation."
		}
	}
	for _, obj := range []*unstructured.Unstructured{before, after} {
		for _, field := range []string{"whenDeleted", "whenScaled"} {
			value, _, _ := unstructured.NestedString(obj.Object, "spec", "persistentVolumeClaimRetentionPolicy", field)
			if value != "" && value != "Retain" {
				return "StatefulSet recreation requires Retain for both PVC retention policies in the live and desired StatefulSet. Apply and reconcile that retention change separately first."
			}
		}
	}
	if policy == "Orphan" {
		for _, field := range []string{"selector", "serviceName"} {
			a, _, _ := unstructured.NestedFieldNoCopy(before.Object, "spec", field)
			b, _, _ := unstructured.NestedFieldNoCopy(after.Object, "spec", field)
			if !reflect.DeepEqual(a, b) {
				return "Orphan recreation requires unchanged selector and governing Service so existing Pods can be adopted. Choose a cascading policy or migrate separately."
			}
		}
	}
	return ""
}

// Snapshot only dependency identity and owner references, never Pod/PVC content.
// Include mounted claims and claim-template names even when Retain leaves no owner.
func recreationDependencies(ctx context.Context, client dynamic.Interface, sts *unstructured.Unstructured) ([]core.RecreationDependency, error) {
	namespace := sts.GetNamespace()
	uidSet := map[types.UID]bool{sts.GetUID(): true}
	claims := map[string]bool{}
	lists := map[string]*unstructured.UnstructuredList{}
	// Read Pods first: claims and controller revisions may refer to those Pods.
	for _, kind := range []string{"Pod", "PersistentVolumeClaim", "ControllerRevision"} {
		list, err := client.Resource(recreationResources[kind]).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list StatefulSet recreation dependencies (%s): %w", kind, err)
		}
		lists[kind] = list
		if kind == "Pod" {
			for _, pod := range list.Items {
				for _, owner := range pod.GetOwnerReferences() {
					if owner.UID == sts.GetUID() {
						uidSet[pod.GetUID()] = true
						volumes, _, _ := unstructured.NestedSlice(pod.Object, "spec", "volumes")
						for _, v := range volumes {
							if m, ok := v.(map[string]interface{}); ok {
								name, _, _ := unstructured.NestedString(m, "persistentVolumeClaim", "claimName")
								if name != "" {
									claims[name] = true
								}
							}
						}
					}
				}
			}
		}
	}
	templates, _, _ := unstructured.NestedSlice(sts.Object, "spec", "volumeClaimTemplates")
	var dependencies []core.RecreationDependency
	for _, kind := range []string{"Pod", "PersistentVolumeClaim", "ControllerRevision"} {
		for _, obj := range lists[kind].Items {
			include := kind == "PersistentVolumeClaim" && claims[obj.GetName()]
			for _, owner := range obj.GetOwnerReferences() {
				if uidSet[owner.UID] {
					include = true
				}
			}
			if kind == "PersistentVolumeClaim" {
				for _, template := range templates {
					if m, ok := template.(map[string]interface{}); ok {
						name, _, _ := unstructured.NestedString(m, "metadata", "name")
						prefix := name + "-" + sts.GetName() + "-"
						if strings.HasPrefix(obj.GetName(), prefix) && decimalOrdinal(strings.TrimPrefix(obj.GetName(), prefix)) {
							include = true
						}
					}
				}
			}
			if include {
				if obj.GetDeletionTimestamp() != nil {
					return nil, &recreationDependencyError{reason: "StatefulSet dependent is already deleting; wait for a stable cluster state and refresh the plan"}
				}
				owners := obj.GetOwnerReferences()
				sort.Slice(owners, func(i, j int) bool { return string(owners[i].UID) < string(owners[j].UID) })
				dependencies = append(dependencies, core.RecreationDependency{Kind: kind, Name: obj.GetName(), UID: string(obj.GetUID()), Owners: owners})
			}
		}
	}
	sort.Slice(dependencies, func(i, j int) bool {
		return dependencies[i].Kind+"/"+dependencies[i].Name < dependencies[j].Kind+"/"+dependencies[j].Name
	})
	// Claim owner references can lag a retention-policy change. Fail closed until
	// the controller has removed deletion-capable references as well.
	for _, dep := range dependencies {
		if dep.Kind != "PersistentVolumeClaim" {
			continue
		}
		for _, owner := range dep.Owners {
			if uidSet[owner.UID] {
				return nil, &recreationDependencyError{reason: "StatefulSet PVC still has a deletion-capable workload owner reference. Reconcile Retain policies and refresh before recreation"}
			}
		}
	}
	return dependencies, nil
}

func decimalOrdinal(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (s *Service) planStatefulSetRecreation(ctx context.Context, input planInput, plan *core.Plan, change *core.Change, desired *unstructured.Unstructured) error {
	var selected *core.RecreationSelection
	for i := range plan.Selection.Recreations {
		if plan.Selection.Recreations[i].Identity.Key() == change.Identity.Key() {
			if selected != nil {
				return errors.New("duplicate StatefulSet recreation selection")
			}
			selected = &plan.Selection.Recreations[i]
		}
	}
	current := &unstructured.Unstructured{}
	if err := current.UnmarshalJSON(change.Before); err != nil {
		return err
	}
	policy := ""
	if selected != nil {
		policy = selected.PropagationPolicy
		if !validRecreationPolicy(policy) {
			return errors.New("invalid StatefulSet deletion propagation policy")
		}
	}
	change.RecreationBlocked = recreationSafety(current, desired, policy)
	if selected == nil {
		return nil
	}
	if change.RecreationBlocked != "" {
		return recreationPlanBlocked(change.Identity, change.RecreationBlocked)
	}
	if len(change.IgnoredPaths) > 0 || len(ignoredPathsFor(input, change.Identity)) > 0 {
		return recreationPlanBlocked(change.Identity, "StatefulSet recreation cannot omit fields: remove field exclusions and persistent field ignores before recreation")
	}
	client, err := clientFor(input, change.Identity)
	if err != nil {
		return err
	}
	rc := client.Dynamic.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}).Namespace(change.Identity.Namespace)
	live, err := rc.Get(ctx, change.Identity.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := validateUpdateTarget(live, *change, plan.ApplicationID, ignoredPathsFor(input, change.Identity)); err != nil {
		return err
	}
	if live.GetDeletionTimestamp() != nil {
		return errors.New("StatefulSet is already deleting; refresh after deletion finishes")
	}
	if reason := recreationSafety(live, desired, policy); reason != "" {
		return recreationPlanBlocked(change.Identity, reason)
	}
	deps, err := recreationDependencies(ctx, client.Dynamic, live)
	if err != nil {
		var unsafe *recreationDependencyError
		if errors.As(err, &unsafe) {
			return recreationPlanBlocked(change.Identity, unsafe.reason)
		}
		return &resourceAccessError{identity: change.Identity, action: "list recreation dependencies", err: err}
	}
	// A dry-run create at the real name would return AlreadyExists. A generated
	// preview name validates create schema/admission without any cluster mutation.
	preview := desired.DeepCopy()
	preview.SetName("")
	preview.SetGenerateName("justcd-recreation-")
	preview.SetResourceVersion("")
	preview.SetUID("")
	if _, err := rc.Create(ctx, preview, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}, FieldManager: "justcd/" + plan.ApplicationID}); err != nil {
		return &resourceAccessError{identity: change.Identity, action: "create replacement (dry-run)", err: err}
	}
	uid, rv := live.GetUID(), live.GetResourceVersion()
	propagation := metav1.DeletionPropagation(policy)
	if err := rc.Delete(ctx, live.GetName(), metav1.DeleteOptions{DryRun: []string{metav1.DryRunAll}, PropagationPolicy: &propagation, Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &rv}}); err != nil {
		return &resourceAccessError{identity: change.Identity, action: "delete for recreation (dry-run)", err: err}
	}
	change.Recreation = &core.StatefulSetRecreation{PropagationPolicy: policy, Dependencies: deps}
	return nil
}

// A mutation may have succeeded even if a subsequent request failed. Never
// automatically replay this operation or claim that the original controller exists.
type recreationFailure struct{ cause error }

func (e *recreationFailure) Error() string {
	return "StatefulSet recreation stopped after deletion was requested; controller may be absent and dependents may be partially deleted or orphaned. Review cluster state and the pre-operation recovery checkpoint before building a fresh plan."
}
func (e *recreationFailure) Unwrap() error     { return e.cause }
func (e *recreationFailure) Retryable() bool   { return false }
func (e *recreationFailure) ErrorCode() string { return "kubernetes.statefulset_recreation_incomplete" }

func (s *Service) recreateStatefulSet(ctx context.Context, input planInput, appID string, resource core.Resource, change core.Change, rc dynamic.ResourceInterface) (result error) {
	if resource.Identity.Kind != "StatefulSet" || resource.Identity.APIVersion != "apps/v1" || change.Kind != core.Update || !change.RecreationRequired || !validRecreationPolicy(change.Recreation.PropagationPolicy) {
		return errors.New("invalid StatefulSet recreation")
	}
	live, err := rc.Get(ctx, resource.Identity.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := validateUpdateTarget(live, change, appID, ignoredPathsFor(input, resource.Identity)); err != nil {
		return err
	}
	if live.GetDeletionTimestamp() != nil {
		return errors.New("StatefulSet is already deleting")
	}
	object, err := render.NormalizedObject(resource)
	if err != nil {
		return err
	}
	object.SetLabels(mergeOwnerLabel(object.GetLabels(), appID))
	if reason := recreationSafety(live, object, change.Recreation.PropagationPolicy); reason != "" {
		return recreationPlanBlocked(change.Identity, reason)
	}
	if len(ignoredPathsFor(input, resource.Identity)) > 0 {
		return errors.New("StatefulSet recreation cannot omit fields")
	}
	client, err := clientFor(input, resource.Identity)
	if err != nil {
		return err
	}
	deps, err := recreationDependencies(ctx, client.Dynamic, live)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(deps, change.Recreation.Dependencies) {
		return errors.New("StatefulSet dependents changed after review; refresh the plan")
	}
	if err := reportResourceStage(ctx, "deleting_statefulset", false); err != nil {
		return err
	}
	// From this point a lost API response is also an uncertain mutation.
	defer func() {
		if result != nil {
			result = &recreationFailure{cause: result}
		}
	}()
	applied, err := replaceStatefulSet(ctx, client.Dynamic, rc, live, object, change, appID, deps)
	if err != nil {
		return err
	}
	if err := reportResourceStage(ctx, "recording", true); err != nil {
		return err
	}
	current, err := render.CanonicalLive(applied, input.Cluster.ID, appID, resource.Identity.ClusterScoped)
	if err != nil {
		return err
	}
	current.Manifest = resource.Manifest
	return s.Store.UpsertManagedResource(ctx, appID, current)
}

func replaceStatefulSet(ctx context.Context, client dynamic.Interface, rc dynamic.ResourceInterface, live, object *unstructured.Unstructured, change core.Change, appID string, deps []core.RecreationDependency) (*unstructured.Unstructured, error) {
	uid, rv := live.GetUID(), live.GetResourceVersion()
	policy := metav1.DeletionPropagation(change.Recreation.PropagationPolicy)
	if err := rc.Delete(ctx, live.GetName(), metav1.DeleteOptions{PropagationPolicy: &policy, Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &rv}}); err != nil {
		return nil, err
	}
	if err := reportResourceStage(ctx, "waiting_statefulset_dependents", true); err != nil {
		return nil, err
	}
	if err := wait.PollUntilContextTimeout(ctx, time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		current, err := rc.Get(ctx, live.GetName(), metav1.GetOptions{})
		if err == nil {
			if current.GetUID() != uid {
				return false, errors.New("another StatefulSet appeared during recreation")
			}
			return false, nil
		}
		if !apierrors.IsNotFound(err) {
			return false, err
		}
		for _, dep := range deps {
			current, err := client.Resource(recreationResources[dep.Kind]).Namespace(live.GetNamespace()).Get(ctx, dep.Name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				if dep.Kind == "PersistentVolumeClaim" {
					return false, errors.New("reviewed StatefulSet PVC disappeared during recreation")
				}
				continue
			}
			if err != nil {
				return false, err
			}
			if string(current.GetUID()) != dep.UID {
				return false, errors.New("StatefulSet dependent was replaced during recreation")
			}
			if dep.Kind == "PersistentVolumeClaim" {
				owners := current.GetOwnerReferences()
				sort.Slice(owners, func(i, j int) bool { return string(owners[i].UID) < string(owners[j].UID) })
				if !reflect.DeepEqual(owners, dep.Owners) {
					return false, errors.New("StatefulSet PVC ownership changed during recreation")
				}
				if current.GetDeletionTimestamp() != nil {
					return false, errors.New("StatefulSet PVC is deleting")
				}
				continue
			}
			if policy != metav1.DeletePropagationOrphan {
				return false, nil
			}
			if current.GetDeletionTimestamp() != nil {
				return false, errors.New("orphaned StatefulSet dependent is deleting")
			}
			for _, owner := range current.GetOwnerReferences() {
				if owner.UID == uid {
					return false, nil
				}
				if owner.Controller != nil && *owner.Controller {
					return false, errors.New("orphaned dependent was claimed by another controller")
				}
			}
		}
		// Controllers can create dependents between the last review read and
		// the delete request. Never overlap a replacement with old-UID garbage collection.
		for _, kind := range []string{"Pod", "ControllerRevision"} {
			list, err := client.Resource(recreationResources[kind]).Namespace(live.GetNamespace()).List(ctx, metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			for _, obj := range list.Items {
				for _, owner := range obj.GetOwnerReferences() {
					if owner.UID == uid {
						return false, nil
					}
				}
			}
		}
		return true, nil
	}); err != nil {
		return nil, err
	}
	if err := reportResourceStage(ctx, "recreating_statefulset", true); err != nil {
		return nil, err
	}
	object.SetResourceVersion("")
	object.SetUID("")
	applied, err := rc.Create(ctx, object, metav1.CreateOptions{FieldManager: "justcd/" + appID})
	if err != nil {
		return nil, err
	}
	if policy == metav1.DeletePropagationOrphan {
		if err := reportResourceStage(ctx, "waiting_statefulset_adoption", true); err != nil {
			return nil, err
		}
		if err := wait.PollUntilContextTimeout(ctx, time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			controller, err := rc.Get(ctx, live.GetName(), metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			if controller.GetUID() != applied.GetUID() || controller.GetDeletionTimestamp() != nil {
				return false, errors.New("replacement StatefulSet changed while waiting for adoption")
			}
			for _, dep := range deps {
				if dep.Kind != "Pod" && dep.Kind != "ControllerRevision" {
					continue
				}
				current, err := client.Resource(recreationResources[dep.Kind]).Namespace(live.GetNamespace()).Get(ctx, dep.Name, metav1.GetOptions{})
				if apierrors.IsNotFound(err) {
					continue
				}
				if err != nil {
					return false, err
				}
				if string(current.GetUID()) != dep.UID {
					continue
				}
				adopted := false
				for _, owner := range current.GetOwnerReferences() {
					if owner.Controller != nil && *owner.Controller {
						if owner.UID != applied.GetUID() {
							return false, errors.New("orphaned dependent was adopted by another controller")
						}
						adopted = true
					}
				}
				if !adopted {
					return false, nil
				}
			}
			return true, nil
		}); err != nil {
			return nil, err
		}
	}
	return applied, nil
}

func recreationPlanBlocked(id core.Identity, reason string) error {
	return &PlanConfigurationError{Source: "cluster", Code: "kubernetes.statefulset_recreation_blocked", Summary: reason, Remediation: "Resolve the recreation safety check separately, then refresh the plan. Or exclude this update and migrate the StatefulSet manually.", Resource: &id}
}

type recreationDependencyError struct{ reason string }

func (e *recreationDependencyError) Error() string { return e.reason }
