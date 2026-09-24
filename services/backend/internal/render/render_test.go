package render

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestJSONPointerRejectsIdentityAndArrayIndexes(t *testing.T) {
	for _, pointer := range []string{"kind", "/kind", "/metadata/name", "/metadata/labels/justcd.io~1application-id", "/spec/containers/0/image", "/spec/replicas/~2bad"} {
		if _, err := ParseJSONPointer(pointer); err == nil {
			t.Errorf("expected invalid pointer %q to be rejected", pointer)
		}
	}
	if _, err := ParseJSONPointer("/spec/template/spec/replicas"); err != nil {
		t.Fatalf("valid field path rejected: %v", err)
	}
}

func TestIgnoredFieldIsOmittedFromDriftProjectionAndReported(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{"name": "settings", "namespace": "team-a", "labels": map[string]interface{}{"justcd.io/application-id": "app-a"}},
		"data":     map[string]interface{}{"mode": "live", "kept": "same"},
	}}
	object.SetUID("uid-1")
	object.SetResourceVersion("17")
	desired := json.RawMessage(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"team-a","labels":{"justcd.io/application-id":"app-a"}},"data":{"mode":"desired","kept":"same"}}`)
	previous := json.RawMessage(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"team-a","labels":{"justcd.io/application-id":"app-a"}},"data":{"mode":"previous","kept":"same"}}`)
	live, desiredFingerprint, changed, err := CanonicalLiveAgainstIgnoring(object, "cluster-a", "app-a", false, desired, previous, []string{"/data/mode"})
	if err != nil {
		t.Fatal(err)
	}
	if live.Fingerprint != desiredFingerprint {
		t.Fatalf("ignored field caused drift: live=%s desired=%s", live.Fingerprint, desiredFingerprint)
	}
	if len(changed) != 1 || changed[0] != "/data/mode" {
		t.Fatalf("ignored field difference was not surfaced for review: %v", changed)
	}
	without, err := RemoveJSONPointer(desired, "/data/mode")
	if err != nil {
		t.Fatal(err)
	}
	if JSONPointerExists(without, "/data/mode") {
		t.Fatal("ignored field was not removed from the apply manifest")
	}
}

func TestPersistentAndOneTimeIgnoredFieldsHaveDifferentDriftReporting(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{"name": "settings", "namespace": "team-a", "labels": map[string]interface{}{"justcd.io/application-id": "app-a"}},
		"data":     map[string]interface{}{"persistent": "live", "once": "live", "kept": "same"},
	}}
	object.SetUID("uid-1")
	object.SetResourceVersion("17")
	desired := json.RawMessage(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"team-a","labels":{"justcd.io/application-id":"app-a"}},"data":{"persistent":"desired","once":"desired","kept":"same"}}`)
	previous := json.RawMessage(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"team-a","labels":{"justcd.io/application-id":"app-a"}},"data":{"persistent":"previous","once":"previous","kept":"same"}}`)
	_, _, reported, err := CanonicalLiveAgainstIgnoring(object, "cluster-a", "app-a", false, desired, previous, []string{"/data/persistent", "/data/once"}, []string{"/data/once"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reported) != 1 || reported[0] != "/data/once" {
		t.Fatalf("expected only one-time selection drift to be reported, got %v", reported)
	}
}

func TestOneTimeIgnoredFieldDeletionRemainsVisible(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{"name": "settings", "namespace": "team-a", "labels": map[string]interface{}{"justcd.io/application-id": "app-a"}},
		"data":     map[string]interface{}{"removed": "still-live"},
	}}
	desired := json.RawMessage(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"team-a","labels":{"justcd.io/application-id":"app-a"}},"data":{}}`)
	previous := json.RawMessage(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"team-a","labels":{"justcd.io/application-id":"app-a"}},"data":{"removed":"still-live"}}`)
	_, _, reported, err := CanonicalLiveAgainstIgnoring(object, "cluster-a", "app-a", false, desired, previous, []string{"/data/removed"}, []string{"/data/removed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reported) != 1 || reported[0] != "/data/removed" {
		t.Fatalf("excluded Git deletion disappeared from plan: %v", reported)
	}
}

func TestFieldIgnoreRequiresAnotherManagedFieldsOwner(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": 3,
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{map[string]interface{}{"name": "api", "image": "example:v2"}},
				},
			},
		},
	}}
	object.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "deployment-controller", Operation: metav1.ManagedFieldsOperationApply, FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:replicas":{}}}`)}}})
	if !HasOtherManagerFieldOwnership(object, "app-a", "/spec/replicas") {
		t.Fatal("another field manager's ownership was not detected")
	}
	if HasOtherManagerFieldOwnership(object, "app-a", "/spec") {
		t.Fatal("ownership of one child must not authorize handing off an entire subtree")
	}
	if HasOtherManagerFieldOwnership(object, "app-a", "/spec/template/spec/containers") {
		t.Fatal("unowned field was incorrectly considered handed off")
	}
	object.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "justcd/app-a", Operation: metav1.ManagedFieldsOperationApply, FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:replicas":{}}}`)}}})
	if HasOtherManagerFieldOwnership(object, "app-a", "/spec/replicas") {
		t.Fatal("JustCD ownership should not count as an external handoff")
	}
}

func TestChangedJSONPointersTreatListsAsWholeFields(t *testing.T) {
	paths, err := ChangedJSONPointers([]byte(`{"spec":{"replicas":2,"containers":[{"name":"api","image":"v1"}]}}`), []byte(`{"spec":{"replicas":3,"containers":[{"name":"api","image":"v2"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"/spec/containers": false, "/spec/replicas": false}
	for _, path := range paths {
		if _, ok := want[path]; ok {
			want[path] = true
		}
		if path == "/spec/containers/0/image" {
			t.Fatal("array index was returned as a field path")
		}
	}
	for path, seen := range want {
		if !seen {
			t.Errorf("missing changed pointer %s in %v", path, paths)
		}
	}
}
