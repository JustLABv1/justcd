package syncer

import (
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

// These reasons describe the already resolved approval rule; they never alter
// eligibility. Identities come from the immutable applied changes, not ignored
// resources or a browser's draft selection.
func setReviewReasons(plan *core.Plan, app store.Application, clusterFallback bool) {
	plan.ApprovalReasons = nil
	if !plan.RequiresApproval {
		return
	}
	all, deleted, scoped, takeover := []core.Identity{}, []core.Identity{}, []core.Identity{}, []core.Identity{}
	for _, change := range plan.Changes {
		all = append(all, change.Identity)
		if change.Kind == core.Delete {
			deleted = append(deleted, change.Identity)
		}
		if change.Identity.ClusterScoped {
			scoped = append(scoped, change.Identity)
		}
		if change.Takeover {
			takeover = append(takeover, change.Identity)
		}
	}
	policyRule := func(kind string) string {
		overridden := app.ApprovalPolicyOverride != nil && ((kind == "sync" && app.ApprovalPolicyOverride.Sync != nil) || (kind == "deletion" && app.ApprovalPolicyOverride.Deletion != nil))
		if overridden {
			return "application." + kind
		}
		return "workspace." + kind
	}
	add := func(code, rule, description string, resources []core.Identity) {
		plan.ApprovalReasons = append(plan.ApprovalReasons, core.ApprovalReason{Code: code, Rule: rule, Description: description, Resources: resources})
	}
	switch plan.ApprovalKind {
	case "rollback":
		add("rollback", "safeguard.rollback", "Every rollback requires distinct workspace owner approval, including a target with no resource changes.", all)
		if len(deleted) > 0 {
			add("deletion", policyRule("deletion"), "Rollback deletions raise the owner approval count to the deletion policy count when that count is higher.", deleted)
		}
	case "takeover":
		rule := "sync"
		if len(deleted) > 0 {
			rule = "deletion"
		}
		if !clusterFallback {
			add("policy", policyRule(rule), "The effective policy determines the approval count; field ownership takeover requires at least one owner and restricts all approvers to owners.", all)
		}
		add("takeover", "safeguard.takeover", "These changes transfer Kubernetes field ownership and require owner approval for the entire plan.", takeover)
	case "deletion":
		add("deletion", policyRule("deletion"), "These deletions select the effective deletion policy for the entire plan.", deleted)
	default:
		if !clusterFallback {
			add("sync", policyRule("sync"), "The effective sync policy requires approval of this entire plan.", all)
		}
	}
	if clusterFallback {
		add("cluster", "safeguard.cluster_scope", "Cluster-wide changes require one owner even when the sync policy disables ordinary approvals.", scoped)
	}
}
