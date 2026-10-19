//go:build integration

package store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
)

func testTopologyObservationPersistence(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	items := []ObservedResource{
		{Identity: core.Identity{ClusterID: "integration-cluster", APIVersion: "route.openshift.io/v1", Kind: "Route", Namespace: "default", Name: "web"}, UID: "web-route", TopologySpec: json.RawMessage(`{"spec":{"host":"web.example.com","to":{"name":"web"},"tls":{"termination":"edge"}}}`)},
		{Identity: core.Identity{ClusterID: "integration-cluster", APIVersion: "v1", Kind: "Pod", Namespace: "default", Name: "web-pod"}, UID: "web-pod"},
	}
	for _, host := range []string{"web.example.com", "updated.example.com"} {
		items[0].TopologySpec = json.RawMessage(`{"spec":{"host":"` + host + `","to":{"name":"web"},"tls":{"termination":"edge"}}}`)
		if err := s.ReplaceObservedResources(ctx, "integration-app", items); err != nil {
			t.Fatal(err)
		}
		got, err := s.ObservedResources(ctx, "integration-app")
		if err != nil || len(got) != 2 {
			t.Fatalf("observations = %#v, %v", got, err)
		}
		for _, item := range got {
			var spec map[string]any
			if err := json.Unmarshal(item.TopologySpec, &spec); err != nil {
				t.Fatal(err)
			}
			if item.Identity.Kind == "Route" && spec["spec"].(map[string]any)["host"] != host {
				t.Fatalf("routing snapshot was not updated: %s", item.TopologySpec)
			}
			if item.Identity.Kind == "Pod" && len(spec) != 0 {
				t.Fatalf("empty snapshot default = %s", item.TopologySpec)
			}
		}
	}
	if err := s.ReplaceObservedResources(ctx, "integration-app", nil); err != nil {
		t.Fatal(err)
	}
}
