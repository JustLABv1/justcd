package syncer

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	jsonpatch "gopkg.in/evanphx/json-patch.v5"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields/managedfieldstest"
	"k8s.io/client-go/applyconfigurations"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	ktesting "k8s.io/client-go/testing"
)

func TestMountReplacementMigratesUpdateOwnership(t *testing.T) {
	for _, samePath := range []bool{false, true} {
		t.Run(map[bool]string{false: "replace mount path", true: "remove subPath"}[samePath], func(t *testing.T) {
			gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
			fields := managedfieldstest.NewFakeFieldManager(applyconfigurations.NewTypeConverter(scheme.Scheme), gvk)
			before := &unstructured.Unstructured{}
			if err := before.UnmarshalJSON([]byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"mimir","namespace":"team","uid":"uid","resourceVersion":"1","labels":{"justcd.io/application-id":"app"}},"spec":{"template":{"spec":{"containers":[{"name":"mimir","image":"mimir:1","volumeMounts":[{"name":"custom-ca","mountPath":"/etc/mimir/certs/ca.crt","subPath":"ca.crt","readOnly":true},{"name":"config","mountPath":"/etc/mimir"}]}]}}}}`)); err != nil {
				t.Fatal(err)
			}
			empty := &unstructured.Unstructured{}
			empty.SetGroupVersionKind(gvk)
			created, err := fields.Update(empty, before, "justcd/app")
			if err != nil {
				t.Fatal(err)
			}
			// Another controller injects a mount and annotation. These must survive.
			withInjection := created.(*unstructured.Unstructured).DeepCopy()
			containers, _, _ := unstructured.NestedSlice(withInjection.Object, "spec", "template", "spec", "containers")
			container := containers[0].(map[string]interface{})
			container["volumeMounts"] = append(container["volumeMounts"].([]interface{}), map[string]interface{}{"name": "injected", "mountPath": "/injected"})
			unstructured.SetNestedSlice(withInjection.Object, containers, "spec", "template", "spec", "containers")
			withInjection.SetAnnotations(map[string]string{"injected": "keep"})
			injected, err := fields.Update(created, withInjection, "injector")
			if err != nil {
				t.Fatal(err)
			}
			live := injected.(*unstructured.Unstructured)
			mountPath := "/etc/custom-ca"
			if samePath {
				mountPath = "/etc/mimir/certs/ca.crt"
			}
			desired := before.DeepCopy()
			desired.SetManagedFields(nil)
			desired.SetUID("")
			desired.SetResourceVersion("1")
			containers, _, _ = unstructured.NestedSlice(desired.Object, "spec", "template", "spec", "containers")
			container = containers[0].(map[string]interface{})
			container["volumeMounts"] = []interface{}{map[string]interface{}{"name": "custom-ca", "mountPath": mountPath, "readOnly": true}, map[string]interface{}{"name": "config", "mountPath": "/etc/mimir"}}
			unstructured.SetNestedSlice(desired.Object, containers, "spec", "template", "spec", "containers")
			body, _ := desired.MarshalJSON()
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			wantDryRun := true
			client.PrependReactor("get", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) { return true, live.DeepCopy(), nil })
			client.PrependReactor("patch", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
				patch := action.(ktesting.PatchAction)
				options := action.(interface{ GetPatchOptions() metav1.PatchOptions }).GetPatchOptions()
				if (len(options.DryRun) > 0) != wantDryRun {
					t.Fatalf("unexpected dry-run options: %+v", options)
				}
				var result runtime.Object
				var err error
				if patch.GetPatchType() == types.ApplyPatchType {
					object := &unstructured.Unstructured{}
					if err := object.UnmarshalJSON(patch.GetPatch()); err != nil {
						t.Fatal(err)
					}
					result, err = fields.Apply(live.DeepCopy(), object, options.FieldManager, options.Force != nil && *options.Force)
				} else {
					if options.Force != nil {
						t.Fatal("merge patch must not force ownership")
					}
					raw, _ := live.MarshalJSON()
					merged, mergeErr := jsonpatch.MergePatch(raw, patch.GetPatch())
					if mergeErr != nil {
						t.Fatal(mergeErr)
					}
					object := &unstructured.Unstructured{}
					if err := object.UnmarshalJSON(merged); err != nil {
						t.Fatal(err)
					}
					if object.GetResourceVersion() != live.GetResourceVersion() {
						t.Fatal("missing resource-version precondition")
					}
					result, err = fields.Update(live.DeepCopy(), object, options.FieldManager)
				}
				if err == nil && !wantDryRun {
					live = result.(*unstructured.Unstructured)
				}
				return true, result, err
			})
			mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gvk.GroupVersion()})
			mapper.Add(gvk, meta.RESTScopeNamespace)
			clients := &kube.Clients{Dynamic: client, Mapper: mapper}
			id := core.Identity{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "mimir"}
			plan := core.Plan{ApplicationID: "app", Changes: []core.Change{{Kind: core.Update, Identity: id, LiveResourceVersion: "1"}}}
			original, _ := live.MarshalJSON()
			if err := (&Service{}).reconcileUpdatePlan(context.Background(), planInput{Mapper: clients, NamespaceClients: map[string]*kube.Clients{"team": clients}}, &plan, []core.Resource{{Identity: id, Manifest: body}}, []core.Resource{{Identity: id, Manifest: mustCanonicalLive(t, live)}}); err != nil {
				t.Fatal(err)
			}
			unchanged, _ := live.MarshalJSON()
			if !reflect.DeepEqual(original, unchanged) {
				t.Fatal("planning mutated the live object")
			}
			if len(plan.Changes) != 1 || plan.Changes[0].Takeover {
				t.Fatalf("unexpected plan: %+v", plan)
			}
			wantDryRun = false
			rc := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace("team")
			applied, err := applyWithOwnershipMigration(context.Background(), rc, "mimir", body, applyPatchOptions(plan.Changes[0], "justcd/app"))
			if err != nil {
				t.Fatal(err)
			}
			actual := mustCanonicalLive(t, applied)
			var predictedObject, actualObject interface{}
			json.Unmarshal(plan.Changes[0].After, &predictedObject)
			json.Unmarshal(actual, &actualObject)
			if !reflect.DeepEqual(predictedObject, actualObject) {
				t.Fatalf("dry-run differs from apply:\n%s\n%s", plan.Changes[0].After, actual)
			}
			containers, _, _ = unstructured.NestedSlice(applied.Object, "spec", "template", "spec", "containers")
			mounts := containers[0].(map[string]interface{})["volumeMounts"].([]interface{})
			if len(mounts) != 3 {
				t.Fatalf("expected replacement, config, and injected mounts: %+v", mounts)
			}
			for _, raw := range mounts {
				mount := raw.(map[string]interface{})
				if mount["name"] == "custom-ca" && (mount["mountPath"] != mountPath || mount["subPath"] != nil) {
					t.Fatalf("stale mount survived: %+v", mount)
				}
			}
			if applied.GetAnnotations()["injected"] != "keep" || applied.GetUID() != "uid" {
				t.Fatal("unrelated metadata changed")
			}
			for _, entry := range applied.GetManagedFields() {
				if entry.Manager == "justcd/app" && entry.Operation == metav1.ManagedFieldsOperationUpdate {
					t.Fatal("Update ownership was not retired")
				}
			}
		})
	}
}

func TestOwnershipCleanupPreservesSharedChildrenAndListKeys(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "example.io/v1", "kind": "Example",
		"spec": map[string]interface{}{
			"settings": map[string]interface{}{"keep": "shared", "remove": "old"},
			"items": []interface{}{
				map[string]interface{}{"name": "a", "port": int64(1)},
				map[string]interface{}{"name": "a", "port": int64(2)},
				map[string]interface{}{"name": "b", "port": int64(1)},
			},
			"tags": []interface{}{"old-a", "keep", "old-b"},
		},
	}}
	entry := func(manager string, operation metav1.ManagedFieldsOperationType, fields string) metav1.ManagedFieldsEntry {
		return metav1.ManagedFieldsEntry{Manager: manager, Operation: operation, APIVersion: "example.io/v1", FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(fields)}}
	}
	object.SetManagedFields([]metav1.ManagedFieldsEntry{
		entry("justcd/app", metav1.ManagedFieldsOperationUpdate, `{"f:spec":{"f:settings":{".":{},"f:keep":{},"f:remove":{}},"f:items":{"k:{\"name\":\"a\",\"port\":1}":{},"k:{\"name\":\"a\",\"port\":2}":{},"k:{\"name\":\"b\",\"port\":1}":{}},"f:tags":{"v:\"old-a\"":{},"v:\"old-b\"":{}}}}`),
		entry("justcd/app", metav1.ManagedFieldsOperationApply, `{"f:spec":{"f:tags":{"v:\"keep\"":{}}}}`),
		entry("controller", metav1.ManagedFieldsOperationApply, `{"f:spec":{"f:settings":{"f:keep":{}},"f:items":{"k:{\"name\":\"a\",\"port\":2}":{}}}}`),
	})
	cleaned, changed, err := cleanUpdateOwnership(object, "justcd/app")
	if err != nil || !changed {
		t.Fatalf("cleanup failed: %v", err)
	}
	want := map[string]interface{}{
		"settings": map[string]interface{}{"keep": "shared"},
		"items":    []interface{}{map[string]interface{}{"name": "a", "port": int64(2)}},
		"tags":     []interface{}{"keep"},
	}
	if !reflect.DeepEqual(cleaned.Object["spec"], want) {
		t.Fatalf("incorrect cleanup: %+v", cleaned.Object["spec"])
	}
	if len(cleaned.GetManagedFields()) != 2 || len(object.GetManagedFields()) != 3 {
		t.Fatal("cleanup changed original ownership or dropped another manager")
	}
}

func TestOwnershipMigrationRejectsConcurrentDryRunChange(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]interface{}{"name": "config", "uid": "uid", "resourceVersion": "1"}, "data": map[string]interface{}{"keep": "new", "old": "remove"}}}
	object.SetManagedFields([]metav1.ManagedFieldsEntry{
		{Manager: "justcd/app", Operation: metav1.ManagedFieldsOperationUpdate, APIVersion: "v1", FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:old":{}}}`)}},
		{Manager: "justcd/app", Operation: metav1.ManagedFieldsOperationApply, APIVersion: "v1", FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:keep":{}}}`)}},
	})
	for _, replaced := range []bool{false, true} {
		current := object.DeepCopy()
		if replaced {
			current.SetUID("replacement")
		} else {
			current.SetResourceVersion("2")
		}
		client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), current)
		rc := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"})
		_, err := finishOwnershipMigration(context.Background(), rc, "config", object, metav1.PatchOptions{FieldManager: "justcd/app", DryRun: []string{metav1.DryRunAll}})
		if err == nil {
			t.Fatal("concurrent change was not rejected")
		}
		for _, action := range client.Actions() {
			if action.GetVerb() != "get" {
				t.Fatal("migration patched a changed target")
			}
		}
	}
}
