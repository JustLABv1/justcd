package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	"github.com/justlab/justcd/services/backend/internal/topology"
)

func TestHelmTopologyFixtureShowsRealRelationships(t *testing.T) {
	fixture := helmTopologyFixture("demo-cluster")
	if len(fixture.desired) < 9 || len(helmObservedResources("demo-cluster")) < 5 {
		t.Fatal("Helm topology fixture should cover a realistic multi-resource release")
	}
	managed := make([]store.ManagedResource, 0, len(fixture.live))
	for _, resource := range fixture.live {
		managed = append(managed, store.ManagedResource{Identity: resource.Identity, UID: resource.UID, ResourceVersion: resource.ResourceVersion, Manifest: resource.Manifest})
	}
	graph := topology.Build(&store.PlanRecord{ID: "plan", Desired: fixture.desired}, managed, helmObservedResources("demo-cluster"))
	links := map[string]bool{}
	byID := map[string]string{}
	for _, node := range graph.Nodes {
		byID[node.ID] = node.Identity.Kind + "/" + node.Identity.Name
	}
	for _, edge := range graph.Edges {
		links[byID[edge.From]+" -> "+byID[edge.To]] = true
	}
	for _, want := range []string{
		"Ingress/shop-public -> Service/shop-web",
		"Service/shop-web -> Deployment/shop-web",
		"Deployment/shop-web -> ReplicaSet/shop-web-8f6c",
		"ReplicaSet/shop-web-8f6c -> Pod/shop-web-8f6c-a12",
		"Deployment/shop-web -> ConfigMap/shop-feature-flags",
		"Deployment/shop-api -> Secret/shop-runtime",
	} {
		if !links[want] {
			t.Errorf("missing topology link %q", want)
		}
	}
	encoded, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "redacted") || strings.Contains(string(encoded), "stringData") {
		t.Fatal("topology response leaked Secret manifest")
	}
}

func TestDemoFixturesCoverReviewStates(t *testing.T) {
	fixtures := buildFixtures("demo-cluster", "demo-payments-credential")
	if len(fixtures) != 5 {
		t.Fatalf("got %d fixtures, want 5", len(fixtures))
	}
	for _, fixture := range fixtures {
		applicationID := "demo-application-" + fixture.namespace
		for i := range fixture.desired {
			fixture.desired[i] = addApplicationOwnership(fixture.desired[i], applicationID)
		}
		for i := range fixture.live {
			fixture.live[i] = addApplicationOwnership(fixture.live[i], applicationID)
		}
		_, _, current, _ := plansForFixture(applicationID, fixture, []core.Binding{{
			ClusterID: "demo-cluster", Namespace: fixture.namespace, CredentialRef: "demo-credential",
		}})
		switch fixture.name {
		case "Demo API · Synced":
			if len(current.Changes) != 0 || current.RequiresApproval {
				t.Fatalf("synced fixture should have an empty, safe plan: %#v", current)
			}
		case "Demo Web · Drifted":
			if len(current.Changes) != 3 || current.RequiresApproval {
				t.Fatalf("drift fixture should have three namespaced changes and no approval requirement: %#v", current)
			}
		case "Demo Worker · Deletion approval":
			if len(current.Changes) != 1 || current.Changes[0].Kind != core.Delete || !current.RequiresApproval {
				t.Fatalf("prune fixture should require approval for one delete: %#v", current)
			}
		case "Demo Metrics · Auto-safe":
			if fixture.syncPolicy != "auto-safe" || len(current.Changes) != 1 || current.Changes[0].Kind != core.Update || current.RequiresApproval {
				t.Fatalf("auto-safe fixture should contain one safe update: %#v", current)
			}
		case "Demo Checkout · Failed sync":
			if fixture.planStatus != "failed" || fixture.health != "degraded" || len(current.Changes) != 1 || current.Changes[0].Kind != core.Create {
				t.Fatalf("failure fixture should have a degraded failed create plan: %#v", current)
			}
		default:
			t.Fatalf("unexpected demo fixture %q", fixture.name)
		}
	}
}
