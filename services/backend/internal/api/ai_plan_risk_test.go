package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/syncer"
)

func raw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func deployment(replicas int, image, memory string) map[string]any {
	return map[string]any{"kind": "Deployment", "spec": map[string]any{"replicas": replicas, "template": map[string]any{"spec": map[string]any{"containers": []any{
		map[string]any{"name": "api", "image": image, "resources": map[string]any{"limits": map[string]any{"memory": memory}}},
	}}}}}
}

func TestPlanRisksFindsObjectiveReviewPoints(t *testing.T) {
	plan := core.Plan{Changes: []core.Change{
		{Kind: "update", Identity: core.Identity{Kind: "Deployment", Namespace: "prod", Name: "api"}, ChangedPaths: []string{"/spec/replicas"},
			Before: raw(t, deployment(3, "registry/api:1.4", "1Gi")), After: raw(t, deployment(0, "registry/api:latest", "512Mi"))},
		{Kind: "delete", Identity: core.Identity{Kind: "PersistentVolumeClaim", Namespace: "prod", Name: "data"}},
		{Kind: "create", Identity: core.Identity{Kind: "ConfigMap", Namespace: "prod", Name: "settings"}},
	}}
	summary := planRisks(plan)
	if summary.Level != "high" || summary.Create != 1 || summary.Update != 1 || summary.Delete != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	titles := map[string]bool{}
	for _, risk := range summary.Risks {
		titles[risk.Title] = true
	}
	for _, want := range []string{"Scales a workload to zero", "Deletes persisted storage", "Uses a floating image tag", "Lowers resource limits"} {
		if !titles[want] {
			t.Errorf("missing risk %q in %v", want, titles)
		}
	}
	if len(summary.Images) != 1 {
		t.Fatalf("expected one image change, got %v", summary.Images)
	}
}

func TestPlanRisksQuietForOrdinaryChange(t *testing.T) {
	plan := core.Plan{Changes: []core.Change{{Kind: "create", Identity: core.Identity{Kind: "ConfigMap", Name: "settings"}}}}
	if summary := planRisks(plan); summary.Level != "low" || len(summary.Risks) != 0 {
		t.Fatalf("ordinary plans must not be flagged: %+v", summary)
	}
}

func TestQuantityValueComparesKubernetesQuantities(t *testing.T) {
	if !(quantityValue("512Mi") < quantityValue("1Gi")) || !(quantityValue("250m") < quantityValue("1")) {
		t.Fatal("quantities must compare by magnitude")
	}
}

func TestGroupConflictsNeverSuggestsAdoptingControllerChildren(t *testing.T) {
	server := &Server{}
	conflicts := []*syncer.OwnershipConflict{
		{Identity: core.Identity{Kind: "ConfigMap", Name: "a"}, LiveFingerprint: "x", DesiredFingerprint: "x"},
		{Identity: core.Identity{Kind: "ConfigMap", Name: "b"}, LiveFingerprint: "x", DesiredFingerprint: "x"},
		{Identity: core.Identity{Kind: "Pod", Name: "c"}, HasOwnerReferences: true},
		{Identity: core.Identity{Kind: "Service", Name: "d"}, Owner: "gone", OwnerMissing: true},
	}
	groups := server.groupConflicts(context.Background(), conflicts)
	byCategory := map[string]conflictGroup{}
	for _, group := range groups {
		byCategory[group.Category] = group
	}
	if g := byCategory["unmanaged"]; g.Count != 2 || !g.Safe || g.Recommendation != "adopt" {
		t.Errorf("identical unmanaged resources are safe to adopt: %+v", g)
	}
	if g := byCategory["controller_child"]; g.Recommendation != "exclude" || g.Safe {
		t.Errorf("controller children must be excluded, not adopted: %+v", g)
	}
	if g := byCategory["orphaned"]; g.Recommendation != "adopt" || g.Safe {
		t.Errorf("orphaned resources need review when they differ: %+v", g)
	}
	if groups[len(groups)-1].Recommendation == "adopt" {
		t.Errorf("adoptable groups must sort first: %+v", groups)
	}
}

func TestDigestWindow(t *testing.T) {
	if _, ok := digestWindow("month"); ok {
		t.Fatal("only day and week are supported")
	}
	if d, ok := digestWindow("week"); !ok || d.Hours() != 168 {
		t.Fatalf("week window = %v", d)
	}
}
