package render

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestQuantityComparison(t *testing.T) {
	shapes := []struct {
		kind, apiVersion, containerField string
		path                             []string
	}{
		{"Pod", "v1", "containers", []string{"spec"}},
		{"Pod", "v1", "initContainers", []string{"spec"}},
		{"Pod", "v1", "ephemeralContainers", []string{"spec"}},
		{"PodTemplate", "v1", "containers", []string{"template", "spec"}},
		{"Deployment", "apps/v1", "containers", []string{"spec", "template", "spec"}},
		{"StatefulSet", "apps/v1", "containers", []string{"spec", "template", "spec"}},
		{"DaemonSet", "apps/v1", "containers", []string{"spec", "template", "spec"}},
		{"ReplicaSet", "apps/v1", "containers", []string{"spec", "template", "spec"}},
		{"ReplicationController", "v1", "containers", []string{"spec", "template", "spec"}},
		{"Job", "batch/v1", "containers", []string{"spec", "template", "spec"}},
		{"CronJob", "batch/v1", "containers", []string{"spec", "jobTemplate", "spec", "template", "spec"}},
		{"Pod", "v1", "", []string{"spec", "resources", "limits"}},
		{"Pod", "v1", "", []string{"spec", "overhead"}},
		{"PersistentVolumeClaim", "v1", "", []string{"spec", "resources", "requests"}},
		{"PersistentVolume", "v1", "", []string{"spec", "capacity"}},
		{"ResourceQuota", "v1", "", []string{"spec", "hard"}},
	}
	values := []struct {
		name, desired, live string
		equal               bool
	}{
		{"quoted CPU", `{"cpu":2}`, `{"cpu":"2"}`, true},
		{"GPU", `{"nvidia.com/gpu":1}`, `{"nvidia.com/gpu":"1"}`, true},
		{"CPU units", `{"cpu":"2000m"}`, `{"cpu":"2"}`, true},
		{"fractional CPU", `{"cpu":0.5}`, `{"cpu":"500m"}`, true},
		{"binary memory", `{"memory":"1Gi"}`, `{"memory":"1024Mi"}`, true},
		{"decimal and binary memory", `{"memory":"1073741824"}`, `{"memory":"1Gi"}`, true},
		{"actual CPU change", `{"cpu":3}`, `{"cpu":"2"}`, false},
		{"actual GPU change", `{"nvidia.com/gpu":2}`, `{"nvidia.com/gpu":"1"}`, false},
		{"actual memory change", `{"memory":"1G"}`, `{"memory":"1Gi"}`, false},
		{"invalid quantity", `{"cpu":"bad"}`, `{"cpu":"2"}`, false},
		{"invalid type", `{"cpu":true}`, `{"cpu":"1"}`, false},
		{"removed quantity", `{}`, `{"cpu":"2"}`, false},
		{"added quantity", `{"cpu":2}`, `{}`, false},
	}
	for _, shape := range shapes {
		for _, value := range values {
			t.Run(shape.kind+"/"+shape.containerField+"/"+strings.Join(shape.path, "/")+"/"+value.name, func(t *testing.T) {
				manifest := func(raw string) []byte {
					var quantities map[string]interface{}
					if err := json.Unmarshal([]byte(raw), &quantities); err != nil {
						t.Fatal(err)
					}
					object := map[string]interface{}{"apiVersion": shape.apiVersion, "kind": shape.kind, "metadata": map[string]interface{}{"name": "test", "namespace": "team"}}
					if shape.containerField != "" {
						containers := []interface{}{map[string]interface{}{"name": "test", "resources": map[string]interface{}{"limits": quantities, "requests": quantities}}}
						path := append(append([]string{}, shape.path...), shape.containerField)
						if err := unstructured.SetNestedSlice(object, containers, path...); err != nil {
							t.Fatal(err)
						}
					} else if err := unstructured.SetNestedMap(object, quantities, shape.path...); err != nil {
						t.Fatal(err)
					}
					encoded, err := json.Marshal(object)
					if err != nil {
						t.Fatal(err)
					}
					return encoded
				}
				desired, live := manifest(value.desired), manifest(value.live)
				// Keep the old field in the managed set to exercise deletions.
				assertQuantityComparison(t, desired, live, live, value.equal)
			})
		}
	}
}

func assertQuantityComparison(t *testing.T, desired, live, previous []byte, equal bool) {
	t.Helper()
	desiredOriginal, liveOriginal := bytes.Clone(desired), bytes.Clone(live)
	before, after, err := ComparisonManifests(live, desired, previous)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after) != equal {
		t.Fatalf("review equality should be %v: before=%s after=%s", equal, before, after)
	}
	object := &unstructured.Unstructured{}
	if err := object.UnmarshalJSON(live); err != nil {
		t.Fatal(err)
	}
	objectOriginal := object.DeepCopy()
	resource, desiredFingerprint, err := CanonicalLiveAgainst(object, "cluster", "app", false, desired, previous)
	if err != nil {
		t.Fatal(err)
	}
	if (resource.Fingerprint == desiredFingerprint) != equal {
		t.Fatalf("fingerprint equality should be %v", equal)
	}
	if !bytes.Equal(desired, desiredOriginal) || !bytes.Equal(live, liveOriginal) || !reflect.DeepEqual(object, objectOriginal) {
		t.Fatal("comparison mutated an input manifest or live object")
	}
	var original, inventory map[string]interface{}
	if err := json.Unmarshal(live, &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(resource.Manifest, &inventory); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, inventory) {
		t.Fatalf("comparison rewrote live inventory: %s", resource.Manifest)
	}
}

func TestQuantityComparisonPreservesOtherTypes(t *testing.T) {
	for _, desired := range []string{
		`{"apiVersion":"example.com/v1","kind":"Deployment","metadata":{"name":"test"},"spec":{"template":{"spec":{"containers":[{"name":"test","resources":{"limits":{"cpu":2}}}]}}}}`,
		`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"test"},"spec":{"replicas":2}}`,
		`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"test"},"spec":{"template":{"spec":{"containers":[{"name":"test","env":[{"name":"CPU","value":2}]}]}}}}`,
		`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test"},"data":{"cpu":2}}`,
	} {
		live := bytes.ReplaceAll([]byte(desired), []byte(`:2`), []byte(`:"2"`))
		assertQuantityComparison(t, []byte(desired), live, []byte(desired), false)
	}
}

func TestEquivalentIgnoredQuantitiesAreNotReportedAsChanges(t *testing.T) {
	desired := []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"test"},"spec":{"template":{"spec":{"containers":[{"name":"test","resources":{"limits":{"cpu":2}}}]}}}}`)
	live := bytes.ReplaceAll(desired, []byte(`:2`), []byte(`:"2000m"`))
	object := &unstructured.Unstructured{}
	if err := object.UnmarshalJSON(live); err != nil {
		t.Fatal(err)
	}
	path := "/spec/template/spec/containers"
	_, _, changed, err := CanonicalLiveAgainstIgnoring(object, "cluster", "app", false, desired, desired, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("equivalent ignored quantities reported as changes: %v", changed)
	}
}

func TestQuantityComparisonMatchesContainersByNameAndKeepsOtherChanges(t *testing.T) {
	desired := []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"test"},"spec":{"template":{"spec":{"containers":[{"name":"api","image":"new","resources":{"limits":{"cpu":2}}},{"name":"sidecar","resources":{"limits":{"cpu":"500m"}}}]}}}}`)
	live := []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"test"},"spec":{"template":{"spec":{"containers":[{"name":"sidecar","resources":{"limits":{"cpu":"0.5"}}},{"name":"api","image":"old","resources":{"limits":{"cpu":"2000m"}}}]}}}}`)
	before, after, err := ComparisonManifests(live, desired, desired)
	if err != nil {
		t.Fatal(err)
	}
	var wanted, actual map[string]interface{}
	if err := json.Unmarshal(after, &wanted); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(before, &actual); err != nil {
		t.Fatal(err)
	}
	wantList := quantityMapAt(wanted, "spec", "template", "spec")["containers"].([]interface{})
	liveList := quantityMapAt(actual, "spec", "template", "spec")["containers"].([]interface{})
	for _, wantItem := range wantList {
		wantContainer := wantItem.(map[string]interface{})
		for _, liveItem := range liveList {
			liveContainer := liveItem.(map[string]interface{})
			if wantContainer["name"] == liveContainer["name"] && !reflect.DeepEqual(wantContainer["resources"], liveContainer["resources"]) {
				t.Fatalf("quantity representation still differs for %s: before=%s after=%s", wantContainer["name"], before, after)
			}
		}
	}
	assertQuantityComparison(t, desired, live, desired, false)
}
