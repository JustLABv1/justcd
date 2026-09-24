package core

import (
	"testing"
	"time"
)

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
