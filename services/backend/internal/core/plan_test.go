package core

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPlanDigestIgnoresManifestKeyOrderAfterDatabaseRoundTrip(t *testing.T) {
	plan := Plan{ApplicationID: "app", Revision: "commit", Bindings: []Binding{{ClusterID: "cluster", Namespace: "demo", CredentialRef: "credential"}}, Changes: []Change{{Kind: Create, Identity: Identity{APIVersion: "v1", Kind: "ConfigMap", Namespace: "demo", Name: "example"}, After: json.RawMessage(`{"apiVersion":"v1","data":{"mode":"desired"},"kind":"ConfigMap","metadata":{"name":"example","namespace":"demo"}}`)}}}
	if err := RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	original := plan.Digest
	plan.Changes[0].After = json.RawMessage(`{"metadata":{"namespace":"demo","name":"example"},"kind":"ConfigMap","data":{"mode":"desired"},"apiVersion":"v1"}`)
	if err := RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	if plan.Digest != original {
		t.Fatal("JSONB key ordering changed the immutable plan digest")
	}
}

func TestRollbackPlanRequiresOwnerAndDistinctApprovals(t *testing.T) {
	plan := Plan{Digest: "rollback-digest", RequiredApprovals: 2, RequiresApproval: true, ApproverRoles: []string{"owner"}, Rollback: &RollbackTarget{Kind: "successful_sync", ID: "snapshot-1", Revision: "abc123"}}
	if RequiredApprovalCount(plan) != 2 {
		t.Fatal("rollback deletion policy count should remain enforced")
	}
	if !ApprovalRoleAllows(plan, "owner", "owner-1") || ApprovalRoleAllows(plan, "deployer", "deployer-1") {
		t.Fatal("rollback approval must be restricted to workspace owners")
	}
	now := time.Now()
	first := DeletionApproval{PlanDigest: plan.Digest, ActorID: "owner-1", ExpiresAt: now.Add(time.Minute)}
	if err := AuthorizeApplyMany(plan, []DeletionApproval{first}, now); err == nil {
		t.Fatal("a single approval must not satisfy the stricter two-owner rule")
	}
	second := DeletionApproval{PlanDigest: plan.Digest, ActorID: "owner-2", ExpiresAt: now.Add(time.Minute)}
	if err := AuthorizeApplyMany(plan, []DeletionApproval{first, second}, now); err != nil {
		t.Fatalf("two distinct approvals should satisfy rollback: %v", err)
	}
}

func TestRollbackTargetIsBoundIntoPlanDigest(t *testing.T) {
	plan := Plan{ApplicationID: "app", Revision: "commit-a", Bindings: []Binding{{ClusterID: "cluster", Namespace: "demo", CredentialRef: "credential"}}}
	plan.Rollback = &RollbackTarget{Kind: "successful_sync", ID: "snapshot-a", Revision: "commit-a"}
	if err := RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	original := plan.Digest
	plan.Rollback.Revision = "commit-b"
	if err := RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	if plan.Digest == original {
		t.Fatal("changing the rollback target must invalidate the plan digest")
	}
}

func TestDeletionNeedsExactApproval(t *testing.T) {
	binding := []Binding{{ClusterID: "cluster-a", Namespace: "team-a", CredentialRef: "secret/team-a"}}
	id := Identity{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team-a", Name: "web"}
	live := []Resource{{Identity: id, Fingerprint: "old", UID: "uid-1", Owner: "app-a"}}
	plan, err := BuildPlan("app-a", "commit-1", binding, nil, live)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Kind != Delete {
		t.Fatalf("unexpected changes: %#v", plan.Changes)
	}
	now := time.Now()
	if err := AuthorizeApply(plan, nil, now); err == nil {
		t.Fatal("deletion was authorized without approval")
	}
	approval := &DeletionApproval{PlanDigest: plan.Digest, ActorID: "user-1", Deletes: []Change{plan.Changes[0]}, ExpiresAt: now.Add(time.Minute)}
	if err := AuthorizeApply(plan, approval, now); err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeApply(plan, approval, now.Add(2*time.Minute)); err == nil {
		t.Fatal("expired approval was accepted")
	}
	changed, err := BuildPlan("app-a", "commit-2", binding, nil, live)
	if err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeApply(changed, approval, now); err == nil {
		t.Fatal("approval carried over to a different revision")
	}
	approval.Deletes[0].LiveUID = "different-uid"
	if err := AuthorizeApply(plan, approval, now); err == nil {
		t.Fatal("approval accepted a different resource UID")
	}
}

func TestPlanScopesResourcesAndIgnoresUnownedDeletions(t *testing.T) {
	binding := []Binding{{ClusterID: "cluster-a", Namespace: "team-a", CredentialRef: "secret/team-a"}}
	id := Identity{APIVersion: "v1", Kind: "ConfigMap", Namespace: "team-a", Name: "settings"}
	live := []Resource{{Identity: id, Fingerprint: "old", UID: "uid-1", Owner: "other-app"}}
	plan, err := BuildPlan("app-a", "commit-1", binding, nil, live)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 0 {
		t.Fatalf("unowned resource was targeted: %#v", plan.Changes)
	}
	id.Namespace = "team-b"
	_, err = BuildPlan("app-a", "commit-1", binding, []Resource{{Identity: id, Fingerprint: "new"}}, nil)
	if err == nil {
		t.Fatal("out-of-scope desired namespace was accepted")
	}
}

func TestPlanDigestIsStableAcrossInputOrder(t *testing.T) {
	bindings := []Binding{
		{ClusterID: "cluster-a", Namespace: "team-b", CredentialRef: "secret/team-b"},
		{ClusterID: "cluster-a", Namespace: "team-a", CredentialRef: "secret/team-a"},
	}
	a := Resource{Identity: Identity{APIVersion: "v1", Kind: "ConfigMap", Namespace: "team-a", Name: "a"}, Fingerprint: "a"}
	b := Resource{Identity: Identity{APIVersion: "v1", Kind: "ConfigMap", Namespace: "team-b", Name: "b"}, Fingerprint: "b"}
	first, err := BuildPlan("app", "commit", bindings, []Resource{a, b}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlan("app", "commit", []Binding{bindings[1], bindings[0]}, []Resource{b, a}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("digest changed with ordering: %s vs %s", first.Digest, second.Digest)
	}
}

func TestClusterScopedChangesRequireExactApproval(t *testing.T) {
	binding := []Binding{{ClusterID: "cluster-a", CredentialRef: "credential/global", ClusterScope: true}}
	identity := Identity{ClusterID: "cluster-a", APIVersion: "v1", Kind: "Namespace", Name: "team-a", ClusterScoped: true}
	desired := []Resource{{Identity: identity, Fingerprint: "desired"}}
	plan, err := BuildPlan("app-a", "commit-1", binding, desired, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.RequiresApproval {
		t.Fatal("cluster-scoped changes must require approval")
	}
	now := time.Now()
	approval := &DeletionApproval{PlanDigest: plan.Digest, ActorID: "owner-a", Privileged: []Change{plan.Changes[0]}, ExpiresAt: now.Add(time.Minute)}
	if err := AuthorizeApply(plan, nil, now); err == nil {
		t.Fatal("cluster-scoped change was authorized without approval")
	}
	if err := AuthorizeApply(plan, approval, now); err != nil {
		t.Fatalf("exact cluster-scoped approval should be accepted: %v", err)
	}
	approval.Privileged[0].Identity.Name = "different"
	if err := AuthorizeApply(plan, approval, now); err == nil {
		t.Fatal("approval for a different cluster-scoped resource was accepted")
	}
}

func TestApprovalDoesNotCarryAcrossPlanSelection(t *testing.T) {
	binding := []Binding{{ClusterID: "cluster-a", Namespace: "team-a", CredentialRef: "secret/team-a"}}
	identity := Identity{ClusterID: "cluster-a", APIVersion: "v1", Kind: "ConfigMap", Namespace: "team-a", Name: "settings"}
	plan, err := BuildPlan("app-a", "commit-1", binding, nil, []Resource{{Identity: identity, Fingerprint: "old", UID: "uid-1", Owner: "app-a"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	approval := &DeletionApproval{PlanDigest: plan.Digest, ActorID: "owner-a", Deletes: []Change{plan.Changes[0]}, ExpiresAt: now.Add(time.Minute)}
	if err := AuthorizeApply(plan, approval, now); err != nil {
		t.Fatal(err)
	}
	plan.Selection.Resources = []Identity{identity}
	if err := RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeApply(plan, approval, now); err == nil {
		t.Fatal("approval from the unselected plan carried over")
	}
}

func TestSelectedPlanApprovesOnlyItsExecutableChanges(t *testing.T) {
	binding := []Binding{{ClusterID: "cluster-a", Namespace: "team-a", CredentialRef: "secret/team-a"}}
	delete := Change{Kind: Delete, Identity: Identity{ClusterID: "cluster-a", APIVersion: "v1", Kind: "ConfigMap", Namespace: "team-a", Name: "old"}, LiveUID: "uid-old"}
	update := Change{Kind: Update, Identity: Identity{ClusterID: "cluster-a", APIVersion: "v1", Kind: "ConfigMap", Namespace: "team-a", Name: "current"}, LiveUID: "uid-current"}
	plan := Plan{ApplicationID: "app-a", Revision: "commit-1", Bindings: binding, Changes: []Change{update}, Ignored: []Change{delete}, Selection: PlanSelection{Resources: []Identity{delete.Identity}}}
	if err := RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeApply(plan, nil, time.Now()); err != nil {
		t.Fatalf("a selected plan without executable deletions should not need deletion approval: %v", err)
	}
	if plan.RequiresApproval {
		t.Fatal("ignored deletion should not require approval")
	}
}
