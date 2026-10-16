package syncer

import (
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
)

func TestHealthForPlanResourceDeletionIsOutOfSync(t *testing.T) {
	for _, requiresApproval := range []bool{false, true} {
		for _, changes := range [][]core.Change{
			{{Kind: core.Delete}},
			{{Kind: core.Delete}, {Kind: core.Update}},
		} {
			plan := core.Plan{RequiresApproval: requiresApproval, Changes: changes}
			if got := healthForPlan(plan); got != "out_of_sync" {
				t.Errorf("resource changes %v (requires approval: %t): got %q, want out_of_sync", changes, requiresApproval, got)
			}
		}
	}
}

func TestHealthForPlanIgnoredDeletionIsSynced(t *testing.T) {
	plan := core.Plan{Ignored: []core.Change{{Kind: core.Delete}}}
	if got := healthForPlan(plan); got != "synced" {
		t.Errorf("ignored deletion: got %q, want synced", got)
	}
}
