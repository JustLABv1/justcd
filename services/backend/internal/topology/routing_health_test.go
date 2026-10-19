package topology

import (
	"testing"

	"github.com/justlab/justcd/services/backend/internal/apphealth"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestServiceReadinessUsesMatchingReadyNonTerminatingPods(t *testing.T) {
	service := core.Identity{APIVersion: "v1", Kind: "Service", Namespace: "demo", Name: "web"}
	route := core.Identity{APIVersion: "networking.k8s.io/v1", Kind: "Ingress", Namespace: "demo", Name: "entry"}
	for _, tt := range []struct {
		name, phase, ready string
		terminating        bool
		reason             string
		available          bool
	}{
		{"ready", "Running", "Ready", false, "ReadyBackends", true},
		{"pending", "Pending", "Not ready", false, "PodsStarting", false},
		{"failed", "Failed", "Not ready", false, "NoReadyPods", false},
		{"completed is not serving", "Succeeded", "Ready", false, "NoReadyPods", false},
		{"terminating is not serving", "Running", "Ready", true, "NoReadyPods", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pod := core.Identity{APIVersion: "v1", Kind: "Pod", Namespace: "demo", Name: "pod"}
			other := pod
			other.Name = "unrelated-ready"
			observed := []store.ObservedResource{
				{Identity: route, Source: "kubernetes", UID: "route", TopologySpec: []byte(`{"spec":{"rules":[{"http":{"paths":[{"backend":{"service":{"name":"web","port":{"number":80}}}}]}}]}}`)},
				{Identity: service, Source: "kubernetes", UID: "service", TopologySpec: []byte(`{"spec":{"selector":{"app":"web"},"ports":[{"port":80}]}}`)},
				{Identity: pod, Source: "kubernetes", UID: "pod", Labels: map[string]string{"app": "web"}, Phase: tt.phase, Readiness: tt.ready, HealthSummary: apphealth.ResourceDetails{Terminating: tt.terminating}},
				{Identity: other, Source: "kubernetes", UID: "other", Labels: map[string]string{"app": "other"}, Phase: "Running", Readiness: "Ready"},
			}
			for _, node := range Build(nil, nil, observed).Nodes {
				if node.Identity.Kind == "Ingress" || node.Identity.Kind == "Service" {
					h := node.RoutingHealth
					if h == nil || h.Reason != tt.reason || h.Available == nil || *h.Available != tt.available || h.TotalPods != 1 {
						t.Fatalf("%s health = %#v", node.Identity.Kind, h)
					}
				}
			}
		})
	}
}

func TestMissingServiceIsAReferenceAndIncompleteObservationsStayUnknown(t *testing.T) {
	identity := core.Identity{APIVersion: "networking.k8s.io/v1", Kind: "Ingress", Namespace: "demo", Name: "entry"}
	observed := []store.ObservedResource{{Identity: identity, UID: "ingress", Source: "kubernetes", TopologySpec: []byte(`{"spec":{"defaultBackend":{"service":{"name":"absent"}}}}`)}}
	for _, incomplete := range []bool{false, true} {
		graph := BuildWithOptions(nil, nil, observed, BuildOptions{ObservationIncomplete: incomplete})
		if len(graph.Nodes) != 2 || len(graph.Edges) != 1 {
			t.Fatalf("missing backend reference not connected: %#v", graph)
		}
		for _, node := range graph.Nodes {
			if node.Identity.Kind == "Service" && (node.Source != "reference" || node.Tracked) {
				t.Fatalf("missing reference treated as managed: %#v", node)
			}
			want := "ServiceMissing"
			if incomplete {
				want = "ObservationIncomplete"
			}
			if node.RoutingHealth == nil || node.RoutingHealth.Reason != want {
				t.Fatalf("incomplete=%v: %#v", incomplete, node.RoutingHealth)
			}
		}
	}
}

func TestUnobservedServicePortsAreUnknownRatherThanMissing(t *testing.T) {
	id := func(kind, name, version string) core.Identity {
		return core.Identity{APIVersion: version, Kind: kind, Namespace: "demo", Name: name}
	}
	graph := Build(nil, nil, []store.ObservedResource{
		{Identity: id("Ingress", "entry", "networking.k8s.io/v1"), UID: "entry", Source: "kubernetes", TopologySpec: []byte(`{"spec":{"defaultBackend":{"service":{"name":"web","port":{"number":80}}}}}`)},
		{Identity: id("Service", "web", "v1"), UID: "web", Source: "kubernetes", TopologySpec: []byte(`{"spec":{"selector":{"app":"web"}}}`)},
		{Identity: id("Pod", "pod", "v1"), UID: "pod", Source: "kubernetes", Labels: map[string]string{"app": "web"}, Phase: "Running", Readiness: "Ready"},
	})
	for _, node := range graph.Nodes {
		if node.Identity.Kind == "Ingress" {
			if node.RoutingHealth == nil || node.RoutingHealth.Status != "unknown" || node.RoutingHealth.Reason != "ServicePortsUnavailable" {
				t.Fatalf("route readiness = %#v", node.RoutingHealth)
			}
		}
	}
}
