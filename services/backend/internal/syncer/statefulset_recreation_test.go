package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

var stsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}

func recreationFixture(t *testing.T) (*unstructured.Unstructured, *unstructured.Unstructured, core.Change) {
	t.Helper()
	live := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1", "kind": "StatefulSet",
		"metadata": map[string]interface{}{"name": "db", "namespace": "team", "uid": "sts-old", "resourceVersion": "10", "labels": map[string]interface{}{"justcd.io/application-id": "app"}},
		"spec":     map[string]interface{}{"serviceName": "db", "selector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": "db"}}, "volumeClaimTemplates": []interface{}{map[string]interface{}{"metadata": map[string]interface{}{"name": "data"}}}},
	}}
	desired := live.DeepCopy()
	desired.SetUID("")
	desired.SetResourceVersion("")
	_ = unstructured.SetNestedField(desired.Object, "Parallel", "spec", "podManagementPolicy")
	canonical, err := render.CanonicalLive(live, "cluster", "app", false)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := desired.MarshalJSON()
	change := core.Change{Kind: core.Update, Identity: canonical.Identity, LiveUID: "sts-old", LiveResourceVersion: "10", Before: canonical.Manifest, After: after, RecreationRequired: true}
	return live, desired, change
}
func recreationClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		stsGVR: "StatefulSetList", recreationResources["Pod"]: "PodList", recreationResources["PersistentVolumeClaim"]: "PersistentVolumeClaimList", recreationResources["ControllerRevision"]: "ControllerRevisionList",
	}, objects...)
}
func dependent(kind, name, uid string, owners ...metav1.OwnerReference) *unstructured.Unstructured {
	apiVersion := "v1"
	if kind == "ControllerRevision" {
		apiVersion = "apps/v1"
	}
	o := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": apiVersion, "kind": kind, "metadata": map[string]interface{}{"name": name, "namespace": "team", "uid": uid}}}
	o.SetOwnerReferences(owners)
	return o
}
func stsOwner(uid string) metav1.OwnerReference {
	yes := true
	return metav1.OwnerReference{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "db", UID: types.UID(uid), Controller: &yes}
}

func TestImmutableStatefulSetDetectionFailsClosed(t *testing.T) {
	id := core.Identity{APIVersion: "apps/v1", Kind: "StatefulSet"}
	forbidden := func(field, message string) *apierrors.StatusError {
		return &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonInvalid, Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: metav1.CauseType("FieldValueForbidden"), Field: field, Message: message}}}}}
	}
	immutable := forbidden("spec", "updates to statefulset spec for fields other than 'replicas' are forbidden")
	if !immutableStatefulSetUpdate(id, immutable) {
		t.Fatal("expected immutable update")
	}
	for _, err := range []error{nil, forbidden("spec.template", "admission denied"), apierrors.NewForbidden(schema.GroupResource{Resource: "statefulsets"}, "db", errors.New("denied"))} {
		if immutableStatefulSetUpdate(id, err) {
			t.Fatalf("misclassified %v", err)
		}
	}
	id.Kind = "Deployment"
	if immutableStatefulSetUpdate(id, immutable) {
		t.Fatal("must be StatefulSet")
	}
}
func TestRecreationSafety(t *testing.T) {
	live, desired, _ := recreationFixture(t)
	if reason := recreationSafety(live, desired, "Orphan"); reason != "" {
		t.Fatal(reason)
	}
	_ = unstructured.SetNestedField(desired.Object, "new-service", "spec", "serviceName")
	if recreationSafety(live, desired, "Orphan") == "" {
		t.Fatal("unsafe orphan adoption accepted")
	}
	if reason := recreationSafety(live, desired, "Foreground"); reason != "" {
		t.Fatal(reason)
	}
	_ = unstructured.SetNestedField(live.Object, "Delete", "spec", "persistentVolumeClaimRetentionPolicy", "whenScaled")
	for _, policy := range []string{"Orphan", "Foreground", "Background"} {
		if !strings.Contains(recreationSafety(live, desired, policy), "Retain") {
			t.Fatal("PVC deletion policy accepted")
		}
	}
}
func TestRecreationDependenciesAndPVCOwners(t *testing.T) {
	live, _, _ := recreationFixture(t)
	pod := dependent("Pod", "db-0", "pod-old", stsOwner("sts-old"))
	_ = unstructured.SetNestedSlice(pod.Object, []interface{}{map[string]interface{}{"name": "shared", "persistentVolumeClaim": map[string]interface{}{"claimName": "mounted"}}}, "spec", "volumes")
	claim := dependent("PersistentVolumeClaim", "data-db-0", "pvc")
	mounted := dependent("PersistentVolumeClaim", "mounted", "mounted")
	rev := dependent("ControllerRevision", "db-hash", "rev", stsOwner("sts-old"))
	client := recreationClient(live, pod, claim, mounted, rev, dependent("Pod", "unrelated", "other"))
	deps, err := recreationDependencies(context.Background(), client, live)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 4 {
		t.Fatalf("unexpected dependencies: %+v", deps)
	}
	raw, _ := json.Marshal(deps)
	if strings.Contains(string(raw), "volumes") {
		t.Fatal("dependency snapshot exposed resource content")
	}
	claim.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "db-0", UID: "pod-old"}})
	if err := client.Tracker().Update(recreationResources["PersistentVolumeClaim"], claim, "team"); err != nil {
		t.Fatal(err)
	}
	if _, err := recreationDependencies(context.Background(), client, live); err == nil {
		t.Fatal("stale PVC deletion owner must block")
	}
}
func TestPlanRecreationDryRunsOnly(t *testing.T) {
	for _, policy := range []string{"Orphan", "Foreground", "Background"} {
		t.Run(policy, func(t *testing.T) {
			live, desired, change := recreationFixture(t)
			client := recreationClient(live)
			creates, deletes := 0, 0
			client.PrependReactor("create", "statefulsets", func(a ktesting.Action) (bool, runtime.Object, error) {
				creates++
				opts := a.(interface{ GetCreateOptions() metav1.CreateOptions }).GetCreateOptions()
				obj := a.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
				if !reflect.DeepEqual(opts.DryRun, []string{metav1.DryRunAll}) || obj.GetName() != "" || obj.GetGenerateName() == "" || obj.GetResourceVersion() != "" {
					t.Fatalf("unsafe create validation: %+v %+v", opts, obj)
				}
				return true, obj, nil
			})
			client.PrependReactor("delete", "statefulsets", func(a ktesting.Action) (bool, runtime.Object, error) {
				deletes++
				opts := a.(ktesting.DeleteAction).GetDeleteOptions()
				if !reflect.DeepEqual(opts.DryRun, []string{metav1.DryRunAll}) || string(*opts.PropagationPolicy) != policy || *opts.Preconditions.UID != "sts-old" {
					t.Fatalf("unsafe delete: %+v", opts)
				}
				return true, nil, nil
			})
			input := planInput{NamespaceClients: map[string]*kube.Clients{"team": {Dynamic: client}}}
			plan := core.Plan{ApplicationID: "app", Selection: core.PlanSelection{Recreations: []core.RecreationSelection{{Identity: change.Identity, PropagationPolicy: policy}}}}
			if err := (&Service{}).planStatefulSetRecreation(context.Background(), input, &plan, &change, desired); err != nil {
				t.Fatal(err)
			}
			if creates != 1 || deletes != 1 || change.Recreation == nil {
				t.Fatalf("missing validation: %+v", change)
			}
			if _, err := client.Resource(stsGVR).Namespace("team").Get(context.Background(), "db", metav1.GetOptions{}); err != nil {
				t.Fatal("planning deleted resource")
			}
		})
	}
}
func TestReconcileImmutableStatefulSetBecomesBlockedPlan(t *testing.T) {
	live, desired, change := recreationFixture(t)
	client := recreationClient(live)
	client.PrependReactor("patch", "statefulsets", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonInvalid, Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: metav1.CauseType("FieldValueForbidden"), Field: "spec", Message: "updates to statefulset spec for fields other than replicas are forbidden"}}}}}
	})
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "apps", Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}, meta.RESTScopeNamespace)
	clients := &kube.Clients{Dynamic: client, Mapper: mapper}
	input := planInput{Mapper: clients, NamespaceClients: map[string]*kube.Clients{"team": clients}}
	plan := core.Plan{ApplicationID: "app", Changes: []core.Change{change}}
	body, _ := desired.MarshalJSON()
	if err := (&Service{}).reconcileUpdatePlan(context.Background(), input, &plan, []core.Resource{{Identity: change.Identity, Manifest: body}}, []core.Resource{{Identity: change.Identity, Manifest: change.Before}}); err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || !plan.Changes[0].RecreationRequired || plan.Changes[0].Recreation != nil {
		t.Fatalf("not a blocked candidate: %+v", plan)
	}
	if err := core.AuthorizeApplyMany(plan, nil, time.Now()); err == nil {
		t.Fatal("unselected recreation must never apply")
	}
}
func TestRecreationApprovalAndDigest(t *testing.T) {
	_, _, change := recreationFixture(t)
	change.Recreation = &core.StatefulSetRecreation{PropagationPolicy: "Orphan"}
	for _, syncPolicy := range []string{"manual", "auto", "auto-safe"} {
		plan := core.Plan{Changes: []core.Change{change}}
		policy := store.ApprovalPolicy{Sync: store.ApprovalRule{RequiredApprovals: 3}, Deletion: store.ApprovalRule{RequiredApprovals: 2}}
		setSyncApprovalPolicy(&plan, store.Application{SyncPolicy: syncPolicy}, policy)
		if plan.RequiredApprovals != 3 || !reflect.DeepEqual(plan.ApproverRoles, []string{"owner"}) || plan.ApprovalKind != "recreation" {
			t.Fatalf("unsafe recreation approval: %+v", plan)
		}
	}
	plan := core.Plan{Changes: []core.Change{change}}
	if err := core.RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	old := plan.Digest
	plan.Changes[0].Recreation.PropagationPolicy = "Background"
	_ = core.RefreshDigest(&plan)
	if old == plan.Digest {
		t.Fatal("policy must change digest")
	}
	old = plan.Digest
	plan.Changes[0].Recreation.Dependencies = []core.RecreationDependency{{Kind: "Pod", Name: "db-0", UID: "pod-old"}}
	_ = core.RefreshDigest(&plan)
	if old == plan.Digest {
		t.Fatal("dependency must change digest")
	}
}
func TestReplaceStatefulSetWaitsForDependentsAndAdoption(t *testing.T) {
	for _, policy := range []string{"Orphan", "Foreground", "Background"} {
		t.Run(policy, func(t *testing.T) {
			live, desired, change := recreationFixture(t)
			change.Recreation = &core.StatefulSetRecreation{PropagationPolicy: policy}
			pod := dependent("Pod", "db-0", "pod-old", stsOwner("sts-old"))
			client := recreationClient(live, pod)
			reads := 0
			created := false
			deleted := false
			client.PrependReactor("delete", "statefulsets", func(a ktesting.Action) (bool, runtime.Object, error) {
				opts := a.(ktesting.DeleteAction).GetDeleteOptions()
				if string(*opts.PropagationPolicy) != policy || *opts.Preconditions.UID != "sts-old" || *opts.Preconditions.ResourceVersion != "10" {
					t.Fatalf("missing deletion preconditions: %+v", opts)
				}
				deleted = true
				return false, nil, nil
			})
			client.PrependReactor("get", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
				reads++
				if !deleted {
					t.Fatal("checked dependents before delete")
				}
				if reads == 1 {
					return true, pod.DeepCopy(), nil
				}
				if policy != "Orphan" {
					_ = client.Tracker().Delete(recreationResources["Pod"], "team", "db-0")
					return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "db-0")
				}
				result := pod.DeepCopy()
				result.SetOwnerReferences(nil)
				_ = client.Tracker().Update(recreationResources["Pod"], result, "team")
				if created {
					result.SetOwnerReferences([]metav1.OwnerReference{stsOwner("sts-new")})
				}
				return true, result, nil
			})
			client.PrependReactor("create", "statefulsets", func(a ktesting.Action) (bool, runtime.Object, error) {
				if reads < 2 {
					t.Fatal("recreated before dependents settled")
				}
				created = true
				obj := a.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
				if obj.GetResourceVersion() != "" || obj.GetUID() != "" {
					t.Fatal("create carries old identity")
				}
				obj.SetUID("sts-new")
				return false, nil, nil
			})
			deps := []core.RecreationDependency{{Kind: "Pod", Name: "db-0", UID: "pod-old", Owners: pod.GetOwnerReferences()}}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := replaceStatefulSet(ctx, client, client.Resource(stsGVR).Namespace("team"), live, desired, change, "app", deps)
			if err != nil {
				t.Fatal(err)
			}
			if result.GetUID() != "sts-new" || !created {
				t.Fatal("replacement not created")
			}
			if policy == "Orphan" && reads < 3 {
				t.Fatal("did not verify adoption")
			}
		})
	}
}
func TestReplaceStatefulSetDoesNotCreateWhileBackgroundDeletionPending(t *testing.T) {
	live, desired, change := recreationFixture(t)
	change.Recreation = &core.StatefulSetRecreation{PropagationPolicy: "Background"}
	pod := dependent("Pod", "db-0", "pod-old", stsOwner("sts-old"))
	client := recreationClient(live, pod)
	client.PrependReactor("create", "statefulsets", func(ktesting.Action) (bool, runtime.Object, error) {
		t.Fatal("must wait for old Pod deletion")
		return true, nil, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := replaceStatefulSet(ctx, client, client.Resource(stsGVR).Namespace("team"), live, desired, change, "app", []core.RecreationDependency{{Kind: "Pod", Name: "db-0", UID: "pod-old"}})
	if err == nil {
		t.Fatal("expected deletion wait timeout")
	}
}
func TestRecreationFailureNeverRetriesOrLeaksCause(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled, apierrors.NewServiceUnavailable("private-value"), errors.New("private-value")} {
		err := &recreationFailure{cause: cause}
		classification := ClassifyRetryError(err)
		if classification.Retryable || classification.TerminalReason != "statefulset_recreation_requires_review" {
			t.Fatalf("unsafe retry: %+v", classification)
		}
		if strings.Contains(safeApplyFailure(err, store.OperationProgress{}), "private-value") {
			t.Fatal("unsafe public failure")
		}
	}
}

func TestRecreationDependenciesSurviveJSONStorage(t *testing.T) {
	live, _, change := recreationFixture(t)
	client := recreationClient(live, dependent("Pod", "db-0", "pod-old", stsOwner("sts-old")))
	deps, err := recreationDependencies(context.Background(), client, live)
	if err != nil {
		t.Fatal(err)
	}
	change.Recreation = &core.StatefulSetRecreation{PropagationPolicy: "Orphan", Dependencies: deps}
	raw, err := json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	var stored core.Change
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored.Recreation.Dependencies, deps) {
		t.Fatal("stored dependency metadata no longer matches live snapshot")
	}
	progress, err := store.NewOperationProgress(core.Plan{Changes: []core.Change{stored}})
	if err != nil {
		t.Fatal(err)
	}
	if progress.Resources[0].Action != "recreate" || progress.Resources[0].PropagationPolicy != "Orphan" {
		t.Fatalf("recreation missing from progress: %+v", progress)
	}
	if core.RequiredApprovalCount(core.Plan{Changes: []core.Change{stored}}) != 1 {
		t.Fatal("recreation must require approval even without resolved policy")
	}
}
func TestRecreationWaitFailsWhenClaimDisappears(t *testing.T) {
	live, desired, change := recreationFixture(t)
	change.Recreation = &core.StatefulSetRecreation{PropagationPolicy: "Foreground"}
	client := recreationClient(live)
	client.PrependReactor("create", "statefulsets", func(ktesting.Action) (bool, runtime.Object, error) {
		t.Fatal("must not recreate after a retained claim disappears")
		return true, nil, nil
	})
	_, err := replaceStatefulSet(context.Background(), client, client.Resource(stsGVR).Namespace("team"), live, desired, change, "app", []core.RecreationDependency{{Kind: "PersistentVolumeClaim", Name: "data-db-0", UID: "pvc"}})
	if err == nil || !strings.Contains(err.Error(), "PVC disappeared") {
		t.Fatalf("unsafe missing-claim handling: %v", err)
	}
}
