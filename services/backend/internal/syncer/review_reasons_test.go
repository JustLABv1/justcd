package syncer

import (
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
	"reflect"
	"testing"
)

func TestReviewReasonsScopeAndGoverningRule(t *testing.T) {
	deletion := core.Change{Kind: core.Delete, Identity: core.Identity{Kind: "ConfigMap", Name: "old"}}
	takeover := core.Change{Kind: core.Update, Takeover: true, Identity: core.Identity{Kind: "Deployment", Name: "api"}}
	scoped := core.Change{Kind: core.Update, Identity: core.Identity{Kind: "Namespace", Name: "team", ClusterScoped: true}}
	app := store.Application{ApprovalPolicyOverride: &store.ApprovalPolicyOverride{Deletion: &store.ApprovalRule{RequiredApprovals: 2}}}
	cases := []struct {
		kind         string
		changes      []core.Change
		fallback     bool
		codes, rules []string
	}{
		{"deletion", []core.Change{deletion, takeover}, false, []string{"deletion"}, []string{"application.deletion"}},
		{"takeover", []core.Change{deletion, takeover}, false, []string{"policy", "takeover"}, []string{"application.deletion", "safeguard.takeover"}},
		{"sync", []core.Change{scoped}, true, []string{"cluster"}, []string{"safeguard.cluster_scope"}},
		{"sync", []core.Change{scoped}, false, []string{"sync"}, []string{"workspace.sync"}},
		{"rollback", []core.Change{deletion}, false, []string{"rollback", "deletion"}, []string{"safeguard.rollback", "application.deletion"}},
		{"rollback", nil, false, []string{"rollback"}, []string{"safeguard.rollback"}},
	}
	for _, tc := range cases {
		t.Run(tc.kind+tc.rules[0], func(t *testing.T) {
			plan := core.Plan{ApprovalKind: tc.kind, RequiresApproval: true, Changes: tc.changes, Ignored: []core.Change{scoped}}
			setReviewReasons(&plan, app, tc.fallback)
			var codes, rules []string
			for _, r := range plan.ApprovalReasons {
				codes = append(codes, r.Code)
				rules = append(rules, r.Rule)
				for _, id := range r.Resources {
					found := false
					for _, c := range plan.Changes {
						found = found || id.Key() == c.Identity.Key()
					}
					if !found {
						t.Fatalf("reason includes ignored/unapplied resource: %+v", id)
					}
				}
			}
			if !reflect.DeepEqual(codes, tc.codes) || !reflect.DeepEqual(rules, tc.rules) {
				t.Fatalf("reasons: %+v", plan.ApprovalReasons)
			}
		})
	}
}
func TestLegacyReviewSchemaPreservesDigestButRejectsPolicyChange(t *testing.T) {
	stored := core.Plan{ApprovalKind: "sync", RequiresApproval: true, RequiredApprovals: 1, ApproverRoles: []string{"owner"}}
	if err := core.RefreshDigest(&stored); err != nil {
		t.Fatal(err)
	}
	fresh := stored
	setReviewReasons(&fresh, store.Application{}, false)
	if err := preserveReviewSchema(&fresh, stored); err != nil {
		t.Fatal(err)
	}
	if fresh.Digest != stored.Digest {
		t.Fatal("explanatory metadata invalidates legacy plan")
	}
	fresh.RequiredApprovals = 2
	if err := preserveReviewSchema(&fresh, stored); err != nil {
		t.Fatal(err)
	}
	if fresh.Digest == stored.Digest {
		t.Fatal("changed policy remained approved")
	}
	modern := stored
	setReviewReasons(&modern, store.Application{}, false)
	if err := core.RefreshDigest(&modern); err != nil {
		t.Fatal(err)
	}
	changed := modern
	changed.ApprovalReasons = append([]core.ApprovalReason{}, modern.ApprovalReasons...)
	changed.ApprovalReasons[0].Rule = "application.sync"
	if err := preserveReviewSchema(&changed, modern); err != nil {
		t.Fatal(err)
	}
	if changed.Digest == modern.Digest {
		t.Fatal("governing rule is not digest-bound")
	}
}
