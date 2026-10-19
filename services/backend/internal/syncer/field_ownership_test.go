package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/render"
	jsonpatch "gopkg.in/evanphx/json-patch.v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/apimachinery/pkg/util/managedfields/managedfieldstest"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestImageTagUpdateMigratesOwnFieldOwnershipWithoutTakeover(t *testing.T) {
	testImageOwnershipMigration(t, false, false, false)
}

func TestImageTagUpdateMigratesAdoptedClientSideOwnershipWithoutTakeover(t *testing.T) {
	testImageOwnershipMigration(t, true, false, false)
}

func TestConfigMapUpdateMigratesAdoptedClientSideOwnershipWithoutTakeover(t *testing.T) {
	testImageOwnershipMigration(t, true, true, false)
}

func TestConfigMapRemovalOnlyMigratesAdoptedOwnership(t *testing.T) {
	testImageOwnershipMigration(t, true, true, true)
}

func testImageOwnershipMigration(t *testing.T, legacy, configMap, removalOnly bool) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	plural := "deployments"
	if configMap {
		gvk = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
		plural = "configmaps"
	}
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gvk.GroupVersion()})
	mapper.Add(gvk, meta.RESTScopeNamespace)
	manager := managedfieldstest.NewFakeFieldManager(managedfields.NewDeducedTypeConverter(), gvk)
	before := &unstructured.Unstructured{Object: map[string]interface{}{}}
	if err := json.Unmarshal([]byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"api","namespace":"team","uid":"uid","resourceVersion":"1","labels":{"justcd.io/application-id":"app"}},"spec":{"template":{"spec":{"containers":[{"name":"api","image":"api:0.41.0"}]}}}}`), &before.Object); err != nil {
		t.Fatal(err)
	}
	if configMap {
		before.SetAPIVersion("v1")
		before.SetKind("ConfigMap")
		delete(before.Object, "spec")
		before.Object["data"] = map[string]interface{}{"grafana.ini": "allowed_targets = instance", "removed.ini": "obsolete"}
	}
	empty := &unstructured.Unstructured{}
	empty.SetGroupVersionKind(gvk)
	initialManager := "justcd/app"
	if legacy {
		initialManager = "kubectl-client-side-apply"
		before.SetLabels(nil)
	}
	created, err := manager.Update(empty, before, initialManager)
	if err != nil {
		t.Fatal(err)
	}
	live := created.(*unstructured.Unstructured)
	if legacy {
		labeled := live.DeepCopy()
		labeled.SetLabels(map[string]string{"justcd.io/application-id": "app"})
		adopted, err := manager.Update(live, labeled, "justcd-adoption/app")
		if err != nil {
			t.Fatal(err)
		}
		live = adopted.(*unstructured.Unstructured)
	}
	resource := core.Resource{Identity: core.Identity{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "api"}, Manifest: []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"api","namespace":"team"},"spec":{"template":{"spec":{"containers":[{"name":"api","image":"api:0.42.0"}]}}}}`)}
	if configMap {
		resource.Identity.APIVersion = "v1"
		resource.Identity.Kind = "ConfigMap"
		resource.Manifest = []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"api","namespace":"team"},"data":{"grafana.ini":"allowed_targets = folder"}}`)
	}

	if removalOnly {
		resource.Manifest = []byte(strings.ReplaceAll(string(resource.Manifest), "allowed_targets = folder", "allowed_targets = instance"))
	}
	wantCalls := 2
	if removalOnly {
		wantCalls = 1
	}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	calls := 0
	wantDryRun := true
	var lastApplied *unstructured.Unstructured
	client.PrependReactor("get", plural, func(action ktesting.Action) (bool, runtime.Object, error) { return true, live.DeepCopy(), nil })
	client.PrependReactor("patch", plural, func(action ktesting.Action) (bool, runtime.Object, error) {
		patch := action.(ktesting.PatchAction)
		if patch.GetPatchType() == types.MergePatchType {
			raw, _ := live.MarshalJSON()
			if !wantDryRun {
				raw, _ = lastApplied.MarshalJSON()
			}
			merged, err := jsonpatch.MergePatch(raw, patch.GetPatch())
			if err != nil {
				t.Fatal(err)
			}
			result := &unstructured.Unstructured{}
			if err := result.UnmarshalJSON(merged); err != nil {
				t.Fatal(err)
			}
			return true, result, nil
		}
		calls++
		options := action.(interface{ GetPatchOptions() metav1.PatchOptions }).GetPatchOptions()
		if (len(options.DryRun) == 1 && options.DryRun[0] == metav1.DryRunAll) != wantDryRun {
			t.Fatalf("unexpected dry-run options: %+v", options)
		}
		object := &unstructured.Unstructured{}
		if err := json.Unmarshal(patch.GetPatch(), &object.Object); err != nil {
			t.Fatal(err)
		}
		if object.GetResourceVersion() != "1" {
			t.Fatal("ownership migration must preserve resource-version preconditions")
		}
		applied, err := manager.Apply(live.DeepCopy(), object, options.FieldManager, options.Force != nil && *options.Force)
		if !removalOnly && calls%2 == 1 && !fieldOwnershipConflict(err) {
			t.Fatalf("expected real Kubernetes Update/Apply conflict, got %v", err)
		}
		if err == nil {
			lastApplied = applied.(*unstructured.Unstructured)
		}
		return true, applied, err
	})
	clients := &kube.Clients{Dynamic: client, Mapper: mapper}
	plan := core.Plan{ApplicationID: "app", Changes: []core.Change{{Kind: core.Update, Identity: resource.Identity, LiveResourceVersion: "1"}}}
	if err := (&Service{}).reconcileUpdatePlan(context.Background(), planInput{Mapper: clients, NamespaceClients: map[string]*kube.Clients{"team": clients}}, &plan, []core.Resource{resource}, []core.Resource{{Identity: resource.Identity, Manifest: mustCanonicalLive(t, live)}}); err != nil {
		t.Fatal(err)
	}
	if plan.Changes[0].Takeover || calls != wantCalls {
		t.Fatalf("image tag update classified as takeover: %+v (calls=%d)", plan.Changes[0], calls)
	}
	// Exercise the same migration in the real apply path, without dry-run.
	wantDryRun = false
	object, err := render.NormalizedObject(resource)
	if err != nil {
		t.Fatal(err)
	}
	object.SetLabels(mergeOwnerLabel(object.GetLabels(), "app"))
	object.SetResourceVersion("1")
	body, err := object.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	applied, err := applyWithOwnershipMigration(context.Background(), client.Resource(schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: plural}).Namespace("team"), "api", body, applyPatchOptions(plan.Changes[0], "justcd/app"))
	if err != nil || calls != wantCalls*2 {
		t.Fatalf("apply ownership migration failed: calls=%d, err=%v", calls, err)
	}
	if configMap {
		data, _, _ := unstructured.NestedStringMap(applied.Object, "data")
		want := "allowed_targets = folder"
		if removalOnly {
			want = "allowed_targets = instance"
		}
		if data["grafana.ini"] != want || len(data) != 1 {
			t.Fatalf("ConfigMap update/removal failed: %v", data)
		}
	} else {
		containers, _, _ := unstructured.NestedSlice(applied.Object, "spec", "template", "spec", "containers")
		if containers[0].(map[string]interface{})["image"] != "api:0.42.0" {
			t.Fatal("image tag was not updated")
		}
	}
	for _, entry := range applied.GetManagedFields() {
		if entry.Manager == initialManager && entry.Operation == metav1.ManagedFieldsOperationUpdate {
			t.Fatal("initial ownership was not retired")
		}
	}
}

func TestOwnershipMigrationRefusesOtherOrUnknownManagers(t *testing.T) {
	for _, messages := range [][]string{
		{`conflict with "kubectl" using apps/v1`},
		{`conflict with "justcd/other-app" using apps/v1`},
		{`conflict with "justcd/app-extra" using apps/v1`},
		{`conflict with "justcd/app" using apps/v1`, `conflict with "kubectl"`},
		{`conflict with "justcd/app" with subresource "scale" using apps/v1`},
		{""},
	} {
		t.Run(messages[0], func(t *testing.T) {
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			causes := make([]metav1.StatusCause, 0, len(messages))
			for _, message := range messages {
				causes = append(causes, metav1.StatusCause{Type: metav1.CauseTypeFieldManagerConflict, Message: message})
			}
			conflict := apierrors.NewApplyConflict(causes, "field conflicts")
			calls := 0
			client.PrependReactor("patch", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
				calls++
				return true, nil, conflict
			})
			_, err := applyWithOwnershipMigration(context.Background(), client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace("team"), "api", []byte(`{}`), metav1.PatchOptions{FieldManager: "justcd/app"})
			if err != conflict || calls != 1 {
				t.Fatalf("unexpected automatic ownership transfer: calls=%d, err=%v", calls, err)
			}
		})
	}
}

func TestPlannerDetectsFieldTakeoverUsingDryRunOnly(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary update", true: "field ownership conflict"}[conflict], func(t *testing.T) {
			mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "apps", Version: "v1"}})
			mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			calls := 0
			client.PrependReactor("patch", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
				calls++
				options := action.(interface{ GetPatchOptions() metav1.PatchOptions }).GetPatchOptions()
				if len(options.DryRun) != 1 || options.DryRun[0] != metav1.DryRunAll {
					t.Fatal("planning must never mutate Kubernetes")
				}
				if calls == 1 && options.Force != nil {
					t.Fatal("first dry-run must not force field ownership")
				}
				if conflict && calls == 1 {
					return true, nil, &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonConflict, Code: 409, Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: metav1.CauseTypeFieldManagerConflict, Field: ".spec.replicas"}}}}}
				}
				if conflict && (options.Force == nil || !*options.Force) {
					t.Fatal("takeover validation must use forced dry-run")
				}
				object := &unstructured.Unstructured{}
				if err := object.UnmarshalJSON(action.(ktesting.PatchAction).GetPatch()); err != nil {
					t.Fatal(err)
				}
				return true, object, nil
			})
			clients := &kube.Clients{Dynamic: client, Mapper: mapper}
			id := core.Identity{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "api"}
			plan := core.Plan{ApplicationID: "app", Changes: []core.Change{{Kind: core.Update, Identity: id}}}
			resource := core.Resource{Identity: id, Manifest: []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"api","namespace":"team"},"spec":{"replicas":1}}`)}
			err := (&Service{}).reconcileUpdatePlan(context.Background(), planInput{Mapper: clients, NamespaceClients: map[string]*kube.Clients{"team": clients}}, &plan, []core.Resource{resource}, []core.Resource{{Identity: id, Manifest: []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"api","namespace":"team","labels":{"justcd.io/application-id":"app"}},"spec":{"replicas":2}}`)}})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Changes[0].Takeover != conflict {
				t.Fatalf("takeover=%v", plan.Changes[0].Takeover)
			}
			expected := 1
			if conflict {
				expected = 2
			}
			if calls != expected {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}

func TestResourceVersionConflictIsNotFieldTakeover(t *testing.T) {
	err := apierrors.NewConflict(schema.GroupResource{Resource: "deployments"}, "api", errors.New("resource version changed"))
	if fieldOwnershipConflict(err) {
		t.Fatal("stale resource versions must not enable force")
	}
}

func TestClientSideMigrationRequiresPendingAdoption(t *testing.T) {
	for _, scenario := range []string{"not adopted", "other application", "already applied", "stale version", "mixed managers"} {
		t.Run(scenario, func(t *testing.T) {
			live := &unstructured.Unstructured{}
			live.SetResourceVersion("1")
			live.SetLabels(map[string]string{"justcd.io/application-id": "app"})
			entries := []metav1.ManagedFieldsEntry{
				{Manager: "kubectl-client-side-apply", Operation: metav1.ManagedFieldsOperationUpdate},
				{Manager: "justcd-adoption/app", Operation: metav1.ManagedFieldsOperationUpdate},
			}
			switch scenario {
			case "not adopted":
				entries = entries[:1]
			case "other application":
				live.SetLabels(map[string]string{"justcd.io/application-id": "other"})
			case "already applied":
				entries = append(entries, metav1.ManagedFieldsEntry{Manager: "justcd/app", Operation: metav1.ManagedFieldsOperationApply})
			case "stale version":
				live.SetResourceVersion("2")
			}
			live.SetManagedFields(entries)
			causes := []metav1.StatusCause{{Type: metav1.CauseTypeFieldManagerConflict, Message: `conflict with "kubectl-client-side-apply" using v1`, Field: ".data.config"}}
			if scenario == "mixed managers" {
				causes = append(causes, metav1.StatusCause{Type: metav1.CauseTypeFieldManagerConflict, Message: `conflict with "operator"`, Field: ".data.other"})
			}
			conflict := apierrors.NewApplyConflict(causes, "field conflicts")
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			calls := 0
			client.PrependReactor("get", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) { return true, live, nil })
			client.PrependReactor("patch", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
				calls++
				options := action.(interface{ GetPatchOptions() metav1.PatchOptions }).GetPatchOptions()
				if options.Force != nil && *options.Force {
					t.Fatal("unreviewed external ownership transfer")
				}
				return true, nil, conflict
			})
			_, err := applyWithOwnershipMigration(context.Background(), client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("team"), "api", []byte(`{"metadata":{"resourceVersion":"1"}}`), metav1.PatchOptions{FieldManager: "justcd/app"})
			if err != conflict || calls != 1 {
				t.Fatalf("calls=%d, err=%v", calls, err)
			}
		})
	}
}

func mustCanonicalLive(t *testing.T, object *unstructured.Unstructured) json.RawMessage {
	t.Helper()
	resource, err := render.CanonicalLive(object, "", "app", false)
	if err != nil {
		t.Fatal(err)
	}
	return resource.Manifest
}
