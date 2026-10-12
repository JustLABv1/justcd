package topology

import (
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/apphealth"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestBuildCarriesHealthSummaryOnObservedNode(t *testing.T) {
	identity := core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "demo", Name: "web"}
	ready, desired := int64(2), int64(3)
	observed := []store.ObservedResource{{
		Identity:   identity,
		Source:     "kubernetes",
		ObservedAt: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC),
		HealthSummary: apphealth.ResourceDetails{
			ReadyReplicas:   &ready,
			DesiredReplicas: &desired,
			Conditions:      []apphealth.KubernetesCondition{{Type: "Available", Status: "False", Reason: "MinimumReplicasUnavailable"}},
		},
	}}
	graph := Build(nil, nil, observed)
	if len(graph.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(graph.Nodes))
	}
	node := graph.Nodes[0]
	if node.HealthSummary == nil || node.HealthSummary.ReadyReplicas == nil || *node.HealthSummary.ReadyReplicas != 2 {
		t.Fatalf("health summary not carried to topology node: %#v", node)
	}
	if len(node.HealthSummary.Conditions) != 1 || node.HealthSummary.Conditions[0].Reason != "MinimumReplicasUnavailable" {
		t.Fatalf("conditions not carried to topology node: %#v", node.HealthSummary)
	}
}

func TestObservedServiceRetainsDesiredRelationships(t *testing.T) {
	service := core.Identity{ClusterID: "cluster", APIVersion: "v1", Kind: "Service", Namespace: "demo", Name: "web"}
	pod := core.Identity{ClusterID: "cluster", APIVersion: "v1", Kind: "Pod", Namespace: "demo", Name: "web-pod"}
	plan := &store.PlanRecord{Desired: []core.Resource{{Identity: service, Manifest: []byte(`{"spec":{"selector":{"app":"web"}}}`)}}}
	graph := Build(plan, nil, []store.ObservedResource{{Identity: service, Source: "kubernetes", UID: "service-uid"}, {Identity: pod, Source: "kubernetes", UID: "pod-uid", Labels: map[string]string{"app": "web"}}})
	for _, edge := range graph.Edges {
		if edge.From == ID(service) && edge.To == ID(pod) && edge.Relation == "selects" {
			return
		}
	}
	t.Fatalf("observed Service lost its selector relationships: %#v", graph.Edges)
}

func TestBuildPreservesTrackingAndSharedHealth(t *testing.T) {
	cluster := core.Identity{APIVersion: "stackgres.io/v1", Kind: "SGCluster", Namespace: "demo", Name: "db"}
	pod := core.Identity{APIVersion: "v1", Kind: "Pod", Namespace: "demo", Name: "db-0"}
	graph := Build(nil, []store.ManagedResource{{Identity: cluster, UID: "cluster"}}, []store.ObservedResource{
		{Identity: cluster, Source: "kubernetes", UID: "cluster", HealthSummary: apphealth.ResourceDetails{Conditions: []apphealth.KubernetesCondition{{Type: "Bootstrapped", Status: "True"}, {Type: "Failed", Status: "False"}}}},
		{Identity: pod, Source: "kubernetes", UID: "pod", OwnerUIDs: []string{"cluster"}},
	})
	for _, node := range graph.Nodes {
		if node.Identity.Kind == "SGCluster" {
			if !node.Tracked || node.Health == nil || node.Health.Status != apphealth.Healthy {
				t.Fatalf("cluster=%#v", node)
			}
		} else if node.Tracked {
			t.Fatalf("generated pod counted as tracked: %#v", node)
		}
	}
}
