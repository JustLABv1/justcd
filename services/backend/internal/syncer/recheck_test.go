package syncer

import (
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func recheckObject(t *testing.T, mutate func(*unstructured.Unstructured)) *unstructured.Unstructured {
	t.Helper()
	object := &unstructured.Unstructured{}
	if err := object.UnmarshalJSON([]byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"api","namespace":"team","uid":"uid-1","resourceVersion":"10","labels":{"justcd.io/application-id":"app"}},"spec":{"replicas":2},"status":{"readyReplicas":2}}`)); err != nil {
		t.Fatal(err)
	}
	object.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "justcd/app", Operation: metav1.ManagedFieldsOperationApply, Time: &metav1.Time{}}})
	if mutate != nil {
		mutate(object)
	}
	return object
}

func TestLiveObjectDigestIgnoresOnlyVolatileFields(t *testing.T) {
	base, err := liveObjectDigest(recheckObject(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*unstructured.Unstructured){
		"status": func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, int64(1), "status", "readyReplicas")
		},
		"resourceVersion": func(o *unstructured.Unstructured) { o.SetResourceVersion("11") },
		"field manager time": func(o *unstructured.Unstructured) {
			fields := o.GetManagedFields()
			now := metav1.Now()
			fields[0].Time = &now
			o.SetManagedFields(fields)
		},
	} {
		got, err := liveObjectDigest(recheckObject(t, mutate))
		if err != nil || got != base {
			t.Errorf("%s must not change the digest", name)
		}
	}
	for name, mutate := range map[string]func(*unstructured.Unstructured){
		"spec": func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, int64(3), "spec", "replicas")
		},
		"label": func(o *unstructured.Unstructured) {
			o.SetLabels(map[string]string{"justcd.io/application-id": "other"})
		},
		"uid": func(o *unstructured.Unstructured) { o.SetUID("uid-2") },
		"field ownership": func(o *unstructured.Unstructured) {
			o.SetManagedFields(append(o.GetManagedFields(), metav1.ManagedFieldsEntry{Manager: "kubectl", Operation: metav1.ManagedFieldsOperationUpdate}))
		},
	} {
		got, err := liveObjectDigest(recheckObject(t, mutate))
		if err != nil || got == base {
			t.Errorf("%s must change the digest", name)
		}
	}
	if recheckObject(t, nil).Object["status"] == nil {
		t.Fatal("digest must not modify its input")
	}
}

func TestRecheckBasisCoversEveryInput(t *testing.T) {
	id := core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "api"}
	created := core.Identity{ClusterID: "cluster", APIVersion: "v1", Kind: "ConfigMap", Namespace: "team", Name: "new"}
	desired := []core.Resource{{Identity: id}, {Identity: created}}
	managed := []store.ManagedResource{{Identity: id, UID: "uid-1", Manifest: []byte(`{"a":1}`)}}
	build := func(change func(*planInput, *[]store.ManagedResource, *string)) (string, bool) {
		input := planInput{Application: store.Application{ID: "app"}, ReviewFingerprint: "source-v1:abc", ServerVersion: "v1.33.0", Bindings: []core.Binding{{ClusterID: "cluster", Namespace: "team", CredentialRef: "c"}}, Live: newLiveObjects()}
		input.Live.set(id.Key(), recheckObject(t, nil))
		input.Live.setAbsent(created.Key())
		records := append([]store.ManagedResource(nil), managed...)
		commit := "1111111111111111111111111111111111111111"
		if change != nil {
			change(&input, &records, &commit)
		}
		return recheckBasis(input, commit, records, desired)
	}
	base, ok := build(nil)
	if !ok || base == "" {
		t.Fatal("complete observation must produce a basis")
	}
	if again, _ := build(nil); again != base {
		t.Fatal("basis must be deterministic")
	}
	for name, change := range map[string]func(*planInput, *[]store.ManagedResource, *string){
		"commit": func(_ *planInput, _ *[]store.ManagedResource, commit *string) {
			*commit = "2222222222222222222222222222222222222222"
		},
		"render inputs": func(input *planInput, _ *[]store.ManagedResource, _ *string) {
			input.ReviewFingerprint = "source-v1:def"
		},
		"server version": func(input *planInput, _ *[]store.ManagedResource, _ *string) { input.ServerVersion = "v1.34.0" },
		"bindings": func(input *planInput, _ *[]store.ManagedResource, _ *string) {
			input.Bindings = append(input.Bindings, core.Binding{ClusterID: "cluster", Namespace: "other", CredentialRef: "c"})
		},
		"ignore rules": func(input *planInput, _ *[]store.ManagedResource, _ *string) {
			input.IgnoreRules = []store.ApplicationIgnoreRule{{IgnoreRule: core.IgnoreRule{Identity: id, Path: "/spec/replicas"}}}
		},
		"selection": func(input *planInput, _ *[]store.ManagedResource, _ *string) {
			input.Selection = core.PlanSelection{Resources: []core.Identity{id}}
		},
		"managed record": func(_ *planInput, records *[]store.ManagedResource, _ *string) {
			(*records)[0].Manifest = []byte(`{"a":2}`)
		},
		"live object": func(input *planInput, _ *[]store.ManagedResource, _ *string) {
			input.Live.set(id.Key(), recheckObject(t, func(o *unstructured.Unstructured) { o.SetUID("uid-2") }))
		},
		"create target appeared": func(input *planInput, _ *[]store.ManagedResource, _ *string) {
			input.Live.set(created.Key(), recheckObject(t, nil))
		},
	} {
		got, ok := build(change)
		if !ok || got == base {
			t.Errorf("%s must change the basis", name)
		}
	}
	if _, ok := build(func(input *planInput, _ *[]store.ManagedResource, _ *string) { input.Live.forget(created.Key()) }); ok {
		t.Fatal("an unobserved object must disable the fast recheck")
	}
}

func TestObservedManagedStateRequiresCompleteMatchingObservation(t *testing.T) {
	id := core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "api"}
	managed := []store.ManagedResource{{Identity: id, UID: "uid-1"}}
	input := planInput{Application: store.Application{ID: "app"}, Cluster: store.Cluster{ID: "cluster"}}
	if _, ok, _ := observedManagedState(input, managed); ok {
		t.Fatal("no observation must fall back to a scan")
	}
	input.Live = newLiveObjects()
	if _, ok, _ := observedManagedState(input, managed); ok {
		t.Fatal("an unobserved managed object must fall back to a scan")
	}
	input.Live.setAbsent(id.Key())
	if _, ok, _ := observedManagedState(input, managed); ok {
		t.Fatal("a missing managed object must fall back to a scan, which records its removal")
	}
	input.Live.set(id.Key(), recheckObject(t, func(o *unstructured.Unstructured) { o.SetUID("uid-2") }))
	if _, ok, _ := observedManagedState(input, managed); ok {
		t.Fatal("a replaced object must fall back to a scan, which reports the ownership change")
	}
	input.Live.set(id.Key(), recheckObject(t, nil))
	live, ok, err := observedManagedState(input, managed)
	if err != nil || !ok || len(live) != 1 || live[0].UID != "uid-1" || live[0].Identity.Key() != id.Key() {
		t.Fatalf("got %+v %v %v", live, ok, err)
	}
}
