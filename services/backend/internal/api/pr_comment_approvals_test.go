package api

import (
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestApprovalCommandIsBoundToExactPlanAndDigest(t *testing.T) {
	if !approvalCommand(" /justcd approve plan-1 digest-1\n", "plan-1", "digest-1") {
		t.Fatal("valid approval rejected")
	}
	for _, body := range []string{"/justcd approve plan-2 digest-1", "/justcd approve plan-1 digest-2", "/justcd approve", "/justcd approve plan-1 digest-1 extra", "quoted: /justcd approve plan-1 digest-1", "```\n/justcd approve plan-1 digest-1\n```"} {
		if approvalCommand(body, "plan-1", "digest-1") {
			t.Fatalf("unbound approval accepted: %q", body)
		}
	}
}

func TestPRPlanCommentShowsCommitDigestAndChanges(t *testing.T) {
	view := planView{ID: "plan-1", Status: "current", Plan: core.Plan{Digest: "digest-1", RequiresApproval: true, RequiredApprovals: 1, Changes: []core.Change{{Kind: core.Delete, Identity: core.Identity{APIVersion: "v1", Kind: "ConfigMap", Namespace: "preview", Name: "settings"}}}}}
	body := prPlanComment(store.PullRequestReview{HeadSHA: "head-sha", Phase: "approval_required"}, view, "https://justcd.example/plan")
	for _, value := range []string{"Changes:", "digest-1", "ConfigMap", "preview/settings", "/justcd approve plan-1 digest-1", "</details>"} {
		if !strings.Contains(body, value) {
			t.Fatalf("comment lacks %q: %s", value, body)
		}
	}
	if strings.Index(body, "/justcd approve") < strings.Index(body, "</details>") {
		t.Fatal("approval command must remain visible outside collapsed details")
	}
	view.Status = "review-only"
	if body = prPlanComment(store.PullRequestReview{}, view, "https://justcd.example/plan"); strings.Contains(body, "/justcd approve") {
		t.Fatal("review-only plan advertised an applyable approval")
	}
}
