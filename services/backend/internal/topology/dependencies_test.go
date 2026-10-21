package topology

import (
	"encoding/json"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	"strings"
	"testing"
)

func TestObservedDependencyNamesSurviveWithoutDesiredManifests(t *testing.T) {
	id := func(kind, name string) core.Identity {
		return core.Identity{APIVersion: "v1", Kind: kind, Namespace: "app", Name: name}
	}
	workload := id("Deployment", "web")
	account := id("ServiceAccount", "web-account")
	claim := id("PersistentVolumeClaim", "data-web-0")
	config := id("ConfigMap", "web-config")
	secret := id("Secret", "web-secret")
	role := id("Role", "read-pods")
	binding := id("RoleBinding", "read-pods-binding")
	snapshot := func(kind, raw string) json.RawMessage {
		value, err := json.Marshal(ObservationSpec(kind, parse([]byte(raw))))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	observed := []store.ObservedResource{
		{Identity: workload, Source: "kubernetes", UID: "deployment", TopologySpec: snapshot("Deployment", `{"spec":{"template":{"spec":{"serviceAccountName":"web-account","containers":[{"env":[{"name":"TOKEN","valueFrom":{"secretKeyRef":{"name":"web-secret","key":"token"}}},{"name":"LITERAL","value":"DO-NOT-STORE"}]}],"volumes":[{"projected":{"sources":[{"configMap":{"name":"web-config"}}]}}]}}}}`)},
		{Identity: id("ReplicaSet", "web-rs"), Source: "kubernetes", UID: "rs", OwnerUIDs: []string{"deployment"}},
		{Identity: id("Pod", "web-0"), Source: "kubernetes", UID: "pod", OwnerUIDs: []string{"rs"}, TopologySpec: snapshot("Pod", `{"spec":{"volumes":[{"persistentVolumeClaim":{"claimName":"data-web-0"}}]}}`)},
		{Identity: binding, Source: "kubernetes", TopologySpec: snapshot("RoleBinding", `{"roleRef":{"kind":"Role","name":"read-pods"},"subjects":[{"kind":"ServiceAccount","name":"web-account","namespace":"app"}]}`)},
		{Identity: account}, {Identity: claim}, {Identity: config}, {Identity: secret}, {Identity: role},
	}
	if strings.Contains(string(observed[0].TopologySpec), "DO-NOT-STORE") {
		t.Fatal("literal environment value leaked into topology snapshot")
	}
	graph := Build(nil, nil, observed)
	for _, pair := range [][2]core.Identity{{workload, account}, {workload, claim}, {workload, config}, {workload, secret}, {binding, role}, {binding, account}} {
		found := false
		for _, edge := range graph.Edges {
			if edge.From == ID(pair[0]) && edge.To == ID(pair[1]) && edge.Relation == "uses" {
				found = true
			}
		}
		if !found {
			t.Errorf("missing dependency %s/%s → %s/%s", pair[0].Kind, pair[0].Name, pair[1].Kind, pair[1].Name)
		}
	}
}
func TestLiveRemovedDependencyDoesNotKeepStaleDesiredConnection(t *testing.T) {
	workload := core.Identity{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "app", Name: "web"}
	config := core.Identity{APIVersion: "v1", Kind: "ConfigMap", Namespace: "app", Name: "old-config"}
	plan := &store.PlanRecord{Desired: []core.Resource{{Identity: workload, Manifest: []byte(`{"spec":{"template":{"spec":{"volumes":[{"configMap":{"name":"old-config"}}]}}}}`)}, {Identity: config}}}
	snapshot, _ := json.Marshal(ObservationSpec("Deployment", parse([]byte(`{"spec":{"template":{"spec":{"containers":[{"name":"web"}]}}}}`))))
	graph := Build(plan, nil, []store.ObservedResource{{Identity: workload, TopologySpec: snapshot}})
	for _, edge := range graph.Edges {
		if edge.From == ID(workload) && edge.To == ID(config) {
			t.Fatal("removed live dependency still connected through stale desired manifest")
		}
	}
}
