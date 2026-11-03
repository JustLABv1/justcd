package api

import (
	"testing"

	"github.com/justlab/justcd/services/backend/internal/apphealth"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func ptr(value int64) *int64 { return &value }

func workload(name, uid string, desired, available, updated int64) store.ObservedResource {
	return store.ObservedResource{
		Identity: core.Identity{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "prod", Name: name}, UID: uid, Source: "kubernetes",
		HealthSummary: apphealth.ResourceDetails{DesiredReplicas: ptr(desired), ReadyReplicas: ptr(available), AvailableReplicas: ptr(available), UpdatedReplicas: ptr(updated)},
	}
}

func pod(name, uid, owner, reason string) store.ObservedResource {
	return store.ObservedResource{
		Identity: core.Identity{APIVersion: "v1", Kind: "Pod", Namespace: "prod", Name: name}, UID: uid, OwnerUIDs: []string{owner}, Source: "kubernetes", Phase: "Pending",
		HealthSummary: apphealth.ResourceDetails{WaitingReason: reason},
	}
}

func replicaSet(name, uid, owner string) store.ObservedResource {
	return store.ObservedResource{Identity: core.Identity{APIVersion: "apps/v1", Kind: "ReplicaSet", Namespace: "prod", Name: name}, UID: uid, OwnerUIDs: []string{owner}, Source: "kubernetes"}
}

func TestClassifyStillServingWhileNewPodCannotStart(t *testing.T) {
	app := store.Application{ID: "a1", Name: "api", Health: "synced", HealthCondition: apphealth.ApplicationCondition{Status: apphealth.Degraded}}
	signal := classifyApplication(app, []store.ObservedResource{
		workload("api", "d1", 3, 3, 1),
		replicaSet("api-new", "rs2", "d1"),
		pod("api-new-x", "p1", "rs2", "ImagePullBackOff"),
	})
	if signal.Category != categoryDegradedServing || signal.Severity != "high" {
		t.Fatalf("want degraded_serving/high, got %s/%s", signal.Category, signal.Severity)
	}
	if signal.ServingTraffic == nil || !*signal.ServingTraffic || !signal.RolloutStalled {
		t.Fatalf("expected a stalled rollout that is still serving: %+v", signal)
	}
	if len(signal.Workloads) != 1 || len(signal.Workloads[0].Problems) != 1 {
		t.Fatalf("the failing pod must be attributed to its Deployment through the ReplicaSet: %+v", signal.Workloads)
	}
}

func TestClassifyOutageOutranksEverything(t *testing.T) {
	app := store.Application{ID: "a2", Name: "web", Health: "synced", HealthCondition: apphealth.ApplicationCondition{Status: apphealth.Degraded}}
	down := classifyApplication(app, []store.ObservedResource{workload("web", "d1", 2, 0, 2)})
	if down.Category != categoryOutage || down.Severity != "critical" {
		t.Fatalf("want outage/critical, got %s/%s", down.Category, down.Severity)
	}
	blocked := classifyApplication(store.Application{ID: "a3", Name: "blocked", Health: "synced", StatusIssues: []store.ApplicationStatusIssue{{Summary: "could not clone"}}}, nil)
	paused := classifyApplication(store.Application{ID: "a4", Name: "paused", Health: "synced", AutoSyncPaused: true}, nil)
	signals := []triageSignal{paused, blocked, down}
	sortSignals(signals)
	if signals[0].Name != "web" || signals[1].Name != "blocked" || signals[2].Name != "paused" {
		t.Fatalf("unexpected order: %s, %s, %s", signals[0].Name, signals[1].Name, signals[2].Name)
	}
}

func TestMergeTriageKeepsAIWordingAndAddsNewApplications(t *testing.T) {
	stored := []triageItem{{ApplicationID: "gone", Severity: "high"}, {ApplicationID: "a1", ApplicationName: "api", Severity: "low", Headline: "AI wording"}}
	signals := []triageSignal{
		{ApplicationID: "a1", Name: "api", Category: categoryDegradedServing, Severity: "high", Tab: "topology"},
		{ApplicationID: "new", Name: "new", Category: categoryOutage, Severity: "critical", Tab: "topology"},
	}
	items := mergeTriage(stored, signals)
	if len(items) != 2 || items[0].ApplicationID != "new" || items[1].Headline != "AI wording" || items[1].Severity != "high" {
		t.Fatalf("unexpected merge result: %+v", items)
	}
}
