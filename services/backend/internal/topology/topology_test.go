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
