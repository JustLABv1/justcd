package syncer

import (
	"github.com/justlab/justcd/services/backend/internal/core"
	"testing"
)

func TestDecommissionNamespaceSelectionPreservesWorkloadDeletes(t *testing.T) {
	namespace := core.Change{Kind: core.Delete, Identity: core.Identity{ClusterID: "cluster", APIVersion: "v1", Kind: "Namespace", Name: "app", ClusterScoped: true}, LiveUID: "ns-uid"}
	workload := core.Change{Kind: core.Delete, Identity: core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "app", Name: "web"}, LiveUID: "web-uid"}
	plan := core.Plan{Decommission: true, Changes: []core.Change{namespace, workload}}
	selectDecommissionNamespaces(&plan, false)
	if err := core.RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	original := plan.Digest
	selectDecommissionNamespaces(&plan, true)
	if !plan.Selection.KeepNamespaces || len(plan.Changes) != 1 || plan.Changes[0].LiveUID != "web-uid" || len(plan.Ignored) != 1 || plan.Ignored[0].LiveUID != "ns-uid" {
		t.Fatalf("namespace retention changed workload deletion: %+v", plan)
	}
	if err := core.RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	if plan.Digest == original {
		t.Fatal("namespace choice did not invalidate prior approval digest")
	}
	selectDecommissionNamespaces(&plan, true)
	if len(plan.Changes) != 1 || len(plan.Ignored) != 1 {
		t.Fatal("repeated namespace selection duplicated resources")
	}
	selectDecommissionNamespaces(&plan, false)
	if err := core.RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	if plan.Digest != original || len(plan.Ignored) != 0 {
		t.Fatal("restoring namespace deletion did not restore the full plan")
	}
}
