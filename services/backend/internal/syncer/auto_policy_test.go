package syncer

import (
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	"testing"
)

func TestAutoSyncApprovalPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, mode           string
		change               core.Change
		sync, deletion, want int
	}{
		{"create", "auto", core.Change{Kind: core.Create}, 2, 1, 0},
		{"update", "auto", core.Change{Kind: core.Update}, 2, 1, 0},
		{"cluster scoped", "auto", core.Change{Kind: core.Update, Identity: core.Identity{ClusterScoped: true}}, 0, 1, 0},
		{"takeover", "auto", core.Change{Kind: core.Update, Takeover: true}, 2, 1, 0},
		{"delete minimum", "auto", core.Change{Kind: core.Delete}, 0, 0, 1},
		{"delete configured", "auto", core.Change{Kind: core.Delete, Takeover: true}, 0, 2, 2},
		{"safe cluster approval", "auto-safe", core.Change{Kind: core.Update, Identity: core.Identity{ClusterScoped: true}}, 0, 1, 1},
		{"safe takeover approval", "auto-safe", core.Change{Kind: core.Update, Takeover: true}, 0, 1, 1},
		{"safe sync policy", "auto-safe", core.Change{Kind: core.Update}, 2, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := core.Plan{Changes: []core.Change{tc.change}}
			setSyncApprovalPolicy(&plan, store.Application{SyncPolicy: tc.mode}, store.ApprovalPolicy{Sync: store.ApprovalRule{RequiredApprovals: tc.sync}, Deletion: store.ApprovalRule{RequiredApprovals: tc.deletion}})
			if core.RequiredApprovalCount(plan) != tc.want {
				t.Fatalf("approvals=%d want %d", core.RequiredApprovalCount(plan), tc.want)
			}
			if tc.mode == "auto" && tc.change.Kind == core.Delete && plan.ApprovalKind != "deletion" {
				t.Fatal("delete must use deletion policy")
			}
		})
	}
}
