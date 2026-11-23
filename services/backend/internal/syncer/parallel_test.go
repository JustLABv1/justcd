package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/store"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestForEachLimitBoundsConcurrency(t *testing.T) {
	var inFlight, peak atomic.Int32
	var calls atomic.Int32
	err := forEachLimit(40, 4, func(int) error {
		calls.Add(1)
		current := inFlight.Add(1)
		for {
			seen := peak.Load()
			if current <= seen || peak.CompareAndSwap(seen, current) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		inFlight.Add(-1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 40 {
		t.Fatalf("ran %d of 40 calls", calls.Load())
	}
	if peak.Load() > 4 || peak.Load() < 2 {
		t.Fatalf("peak concurrency %d, want between 2 and 4", peak.Load())
	}
}

func TestForEachLimitReportsLowestFailureAndStops(t *testing.T) {
	var calls atomic.Int32
	err := forEachLimit(100, 4, func(i int) error {
		calls.Add(1)
		switch i {
		case 5:
			time.Sleep(5 * time.Millisecond)
			return errors.New("five")
		case 6:
			return errors.New("six")
		}
		time.Sleep(time.Millisecond)
		return nil
	})
	if err == nil || err.Error() != "five" {
		t.Fatalf("got %v, want the lowest failing index", err)
	}
	if calls.Load() >= 100 {
		t.Fatal("work kept starting after a failure")
	}
}

func TestForEachLimitSequentialStopsAtFirstFailure(t *testing.T) {
	var order []int
	err := forEachLimit(5, 1, func(i int) error {
		order = append(order, i)
		if i == 2 {
			return errors.New("stop")
		}
		return nil
	})
	if err == nil || fmt.Sprint(order) != "[0 1 2]" {
		t.Fatalf("sequential mode must run in order and stop: %v %v", order, err)
	}
}

// Concurrent dry-runs keep the plan's order, and a snapshot object with the
// reviewed resourceVersion avoids a second read. (The fake client serializes
// reactors, so concurrency itself is covered by the forEachLimit tests.)
func TestReconcileUpdatePlanKeepsOrderAndReusesSnapshot(t *testing.T) {
	const count = 12
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	var gets atomic.Int32
	client.PrependReactor("get", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) {
		gets.Add(1)
		return false, nil, nil
	})
	client.PrependReactor("patch", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		result := &unstructured.Unstructured{}
		if err := result.UnmarshalJSON(action.(ktesting.PatchAction).GetPatch()); err != nil {
			return true, nil, err
		}
		result.SetResourceVersion("2")
		return true, result, nil
	})
	clients := &kube.Clients{Dynamic: client, Mapper: mapper}
	input := planInput{Application: store.Application{ID: "app"}, Mapper: clients, NamespaceClients: map[string]*kube.Clients{"team": clients}, Concurrency: 4, Live: newLiveObjects()}
	var desired, live []core.Resource
	for i := range count {
		name := fmt.Sprintf("config-%02d", i)
		id := core.Identity{ClusterID: "cluster", APIVersion: "v1", Kind: "ConfigMap", Namespace: "team", Name: name}
		manifest := func(value string) json.RawMessage {
			return json.RawMessage(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"` + name + `","namespace":"team","labels":{"justcd.io/application-id":"app"}},"data":{"value":"` + value + `"}}`)
		}
		object := &unstructured.Unstructured{}
		if err := object.UnmarshalJSON(manifest("old")); err != nil {
			t.Fatal(err)
		}
		object.SetUID(types.UID(fmt.Sprintf("uid-%d", i)))
		object.SetResourceVersion("1")
		input.Live.set(id.Key(), object)
		current, fingerprint, err := render.CanonicalLiveAgainst(object, "cluster", "app", false, manifest("new"), manifest("old"))
		if err != nil {
			t.Fatal(err)
		}
		desired = append(desired, core.Resource{Identity: id, Manifest: manifest("new"), Fingerprint: fingerprint})
		live = append(live, current)
	}
	plan, err := core.BuildPlan("app", "commit", []core.Binding{{ClusterID: "cluster", Namespace: "team", CredentialRef: "credential"}}, desired, live)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != count {
		t.Fatalf("fixture must produce %d updates: %+v", count, plan.Changes)
	}
	before := make([]string, len(plan.Changes))
	for i, change := range plan.Changes {
		before[i] = change.Identity.Key()
	}
	if err := (&Service{}).reconcileUpdatePlan(context.Background(), input, &plan, desired, live); err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != count {
		t.Fatalf("got %d updates, want %d", len(plan.Changes), count)
	}
	for i, change := range plan.Changes {
		if change.Identity.Key() != before[i] {
			t.Fatalf("update %d moved: %s, want %s", i, change.Identity.Key(), before[i])
		}
	}
	if gets.Load() != 0 {
		t.Fatalf("dry-run re-read %d objects already in the snapshot", gets.Load())
	}
}
