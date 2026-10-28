package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	jsonpatch "gopkg.in/evanphx/json-patch.v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	"k8s.io/client-go/util/retry"
)

func TestOwnershipCleanupConflictAfterSuccessfulApply(t *testing.T) {
	for _, kind := range []string{"Ingress", "Deployment"} {
		for _, concurrent := range []string{"status", "revision", "revision and spec", "controller ownership", "annotation", "timestamp", "spec", "labels", "replacement", "ownership", "persistent conflict", "forbidden"} {
			t.Run(kind+"/"+concurrent, func(t *testing.T) {
				apiVersion, group, plural := "networking.k8s.io/v1", "networking.k8s.io", "ingresses"
				spec := map[string]interface{}{"tls": []interface{}{map[string]interface{}{"secretName": "new-tls"}}}
				if kind == "Deployment" {
					apiVersion, group, plural = "apps/v1", "apps", "deployments"
					spec = map[string]interface{}{"replicas": int64(2)}
				}
				applied := &unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": apiVersion, "kind": kind,
					"metadata": map[string]interface{}{"name": "gateway", "namespace": "team", "uid": "uid", "resourceVersion": "2", "annotations": map[string]interface{}{"obsolete": "remove"}},
					"spec":     spec,
				}}
				applied.SetManagedFields([]metav1.ManagedFieldsEntry{
					{Manager: "justcd/app", Operation: metav1.ManagedFieldsOperationUpdate, APIVersion: apiVersion, FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:annotations":{"f:obsolete":{}}}}`)}},
					{Manager: "justcd/app", Operation: metav1.ManagedFieldsOperationApply, APIVersion: apiVersion, FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:spec":{}}`)}},
				})
				fresh := applied.DeepCopy()
				fresh.SetResourceVersion("3")
				fresh.Object["status"] = map[string]interface{}{"observedGeneration": int64(2)}
				fresh.SetManagedFields(append(fresh.GetManagedFields(), metav1.ManagedFieldsEntry{Manager: "controller", Operation: metav1.ManagedFieldsOperationUpdate, Subresource: "status", APIVersion: apiVersion, FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:status":{}}`)}}))
				switch concurrent {
				case "revision", "revision and spec", "controller ownership":
					annotations := fresh.GetAnnotations()
					annotations["deployment.kubernetes.io/revision"] = "10"
					fresh.SetAnnotations(annotations)
					fresh.SetManagedFields(append(fresh.GetManagedFields(), metav1.ManagedFieldsEntry{Manager: "kube-controller-manager", Operation: metav1.ManagedFieldsOperationUpdate, APIVersion: apiVersion, FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:annotations":{"f:deployment.kubernetes.io/revision":{}}}}`)}}))
					if concurrent == "controller ownership" {
						entries := fresh.GetManagedFields()
						entries[len(entries)-1].FieldsV1 = &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:annotations":{"f:deployment.kubernetes.io/revision":{}}},"f:spec":{}}`)}
						fresh.SetManagedFields(entries)
					}
					if concurrent == "revision and spec" {
						fresh.Object["spec"] = map[string]interface{}{"changed": "concurrently"}
					}
				case "annotation":
					fresh.SetAnnotations(map[string]string{"obsolete": "remove", "external": "changed"})
				case "timestamp":
					entries := fresh.GetManagedFields()
					now := metav1.Now()
					entries[0].Time = &now
					fresh.SetManagedFields(entries)
				case "spec":
					fresh.Object["spec"] = map[string]interface{}{"changed": "concurrently"}
				case "labels":
					fresh.SetLabels(map[string]string{"concurrent": "change"})
				case "replacement":
					fresh.SetUID("replacement")
				case "ownership":
					fresh.SetManagedFields(append(fresh.GetManagedFields(), metav1.ManagedFieldsEntry{Manager: "another-manager", Operation: metav1.ManagedFieldsOperationApply, APIVersion: apiVersion, FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:spec":{}}`)}}))
				}
				client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
				applyCalls, cleanupCalls, getCalls := 0, 0, 0
				client.PrependReactor("get", plural, func(action ktesting.Action) (bool, runtime.Object, error) {
					getCalls++
					return true, fresh.DeepCopy(), nil
				})
				client.PrependReactor("patch", plural, func(action ktesting.Action) (bool, runtime.Object, error) {
					patch := action.(ktesting.PatchAction)
					if patch.GetPatchType() == types.ApplyPatchType {
						applyCalls++
						return true, applied.DeepCopy(), nil
					}
					cleanupCalls++
					resource := schema.GroupResource{Group: group, Resource: plural}
					if concurrent == "forbidden" {
						return true, nil, apierrors.NewForbidden(resource, "gateway", nil)
					}
					if cleanupCalls == 1 || concurrent == "persistent conflict" {
						return true, nil, apierrors.NewConflict(resource, "gateway", nil)
					}
					options := action.(interface{ GetPatchOptions() metav1.PatchOptions }).GetPatchOptions()
					if options.Force != nil || len(options.DryRun) > 0 || options.FieldManager != "justcd/app" {
						t.Fatalf("unsafe cleanup options: %+v", options)
					}
					raw, _ := fresh.MarshalJSON()
					merged, err := jsonpatch.MergePatch(raw, patch.GetPatch())
					if err != nil {
						t.Fatal(err)
					}
					result := &unstructured.Unstructured{}
					if err := result.UnmarshalJSON(merged); err != nil {
						t.Fatal(err)
					}
					if result.GetResourceVersion() != "3" || result.GetUID() != "uid" || result.GetAnnotations()["obsolete"] != "" || !reflect.DeepEqual(result.Object["status"], fresh.Object["status"]) || !reflect.DeepEqual(result.Object["spec"], applied.Object["spec"]) {
						t.Fatalf("cleanup lost preconditions or concurrent status: %+v", result.Object)
					}
					for _, entry := range result.GetManagedFields() {
						if entry.Manager == "justcd/app" && entry.Operation == metav1.ManagedFieldsOperationUpdate {
							t.Fatal("stale ownership survived")
						}
					}
					return true, result, nil
				})
				rc := client.Resource(schema.GroupVersionResource{Group: group, Version: "v1", Resource: plural}).Namespace("team")
				result, err := applyWithOwnershipMigration(context.Background(), rc, "gateway", []byte(`{}`), metav1.PatchOptions{FieldManager: "justcd/app"})
				if applyCalls != 1 {
					t.Fatalf("Apply was replayed %d times", applyCalls)
				}
				if concurrent == "status" || concurrent == "timestamp" || (concurrent == "revision" && kind == "Deployment") {
					if err != nil || cleanupCalls != 2 || getCalls != 1 {
						t.Fatalf("controller bookkeeping update was not recovered: %v (%d patches, %d gets)", err, cleanupCalls, getCalls)
					}
					if concurrent == "revision" && result.GetAnnotations()["deployment.kubernetes.io/revision"] != "10" {
						t.Fatal("controller revision was not preserved")
					}
				} else {
					var appliedError *AppliedResourceError
					if !errors.As(err, &appliedError) {
						t.Fatalf("missing successful-apply context: %v", err)
					}
					if err == nil {
						t.Fatal("unsafe or failed cleanup reported success")
					}
					if concurrent == "persistent conflict" {
						if !apierrors.IsConflict(err) || cleanupCalls != retry.DefaultRetry.Steps {
							t.Fatalf("unbounded or incorrect retry: %v (%d calls)", err, cleanupCalls)
						}
					} else if cleanupCalls != 1 {
						t.Fatalf("unsafe cleanup was retried %d times", cleanupCalls)
					}
				}
			})
		}
	}
}

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
