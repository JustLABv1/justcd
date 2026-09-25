package syncer

import (
	"errors"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
)

func TestValidateAdoptionConflict(t *testing.T) {
	base := OwnershipConflict{Identity: core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "ntfy"}, UID: "uid-1", ResourceVersion: "27", DesiredFingerprint: "git-hash"}
	if err := validateAdoptionConflict(&base, &base, "app-a"); err != nil {
		t.Fatalf("unchanged, unowned object should be claimable: %v", err)
	}
	for name, mutate := range map[string]func(*OwnershipConflict){
		"replacement":        func(c *OwnershipConflict) { c.UID = "uid-2" },
		"live change":        func(c *OwnershipConflict) { c.ResourceVersion = "28" },
		"Git change":         func(c *OwnershipConflict) { c.DesiredFingerprint = "new-git-hash" },
		"different identity": func(c *OwnershipConflict) { c.Identity.Namespace = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			fresh := base
			mutate(&fresh)
			if err := validateAdoptionConflict(&fresh, &base, "app-a"); !errors.Is(err, ErrAdoptionStale) {
				t.Fatalf("expected stale conflict, got %v", err)
			}
		})
	}
	owned := base
	owned.Owner = "app-b"
	if err := validateAdoptionConflict(&owned, &base, "app-a"); !errors.Is(err, ErrAdoptionUnsafe) {
		t.Fatalf("another application's label must prevent takeover: %v", err)
	}
	dependent := base
	dependent.HasOwnerReferences = true
	if err := validateAdoptionConflict(&dependent, &base, "app-a"); !errors.Is(err, ErrAdoptionUnsafe) {
		t.Fatalf("Kubernetes dependents must not be claimed: %v", err)
	}
}

func TestValidateAdoptionSelection(t *testing.T) {
	first := OwnershipConflict{Identity: core.Identity{ClusterID: "cluster", APIVersion: "v1", Kind: "Service", Namespace: "team", Name: "web"}, UID: "uid-1", ResourceVersion: "1", DesiredFingerprint: "git"}
	second := OwnershipConflict{Identity: core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "web"}, UID: "uid-2", ResourceVersion: "2", DesiredFingerprint: "git"}
	fresh := []*OwnershipConflict{&first, &second}
	if selected, err := validateAdoptionSelection(fresh, []OwnershipConflict{first, second}, "app-a"); err != nil || len(selected) != 2 {
		t.Fatalf("two reviewed resources should be claimable: %v", err)
	}
	if _, err := validateAdoptionSelection(fresh, []OwnershipConflict{first, first}, "app-a"); err == nil {
		t.Fatal("duplicate selection must fail before any resource is claimed")
	}
	stale := second
	stale.ResourceVersion = "3"
	if _, err := validateAdoptionSelection(fresh, []OwnershipConflict{first, stale}, "app-a"); !errors.Is(err, ErrAdoptionStale) {
		t.Fatalf("one stale resource must reject the entire review set: %v", err)
	}
	blocked := second
	blocked.Owner = "app-b"
	if _, err := validateAdoptionSelection([]*OwnershipConflict{&first, &blocked}, []OwnershipConflict{first, second}, "app-a"); !errors.Is(err, ErrAdoptionUnsafe) {
		t.Fatalf("another application's resource must reject the review set: %v", err)
	}
}

func TestOwnershipConflictsUnwrapFirst(t *testing.T) {
	first := &OwnershipConflict{Identity: core.Identity{Kind: "Service", Name: "web"}}
	all := &OwnershipConflicts{Items: []*OwnershipConflict{first, {Identity: core.Identity{Kind: "Deployment", Name: "web"}}}}
	var single *OwnershipConflict
	if !errors.As(all, &single) || single != first {
		t.Fatal("existing single-conflict callers must still see the first conflict")
	}
}

func TestTakeoverIsTheOnlyApplyThatForcesOwnership(t *testing.T) {
	ordinary := applyPatchOptions(core.Change{Kind: core.Update}, "justcd/app-a")
	if ordinary.Force != nil {
		t.Fatal("ordinary updates must never force field ownership")
	}
	takeover := applyPatchOptions(core.Change{Kind: core.Update, Takeover: true}, "justcd/app-a")
	if takeover.Force == nil || !*takeover.Force {
		t.Fatal("a reviewed takeover must request field ownership transfer")
	}
}

func TestTakeoverChangesPlanDigest(t *testing.T) {
	plan := core.Plan{ApplicationID: "app-a", Revision: "commit", Changes: []core.Change{{Kind: core.Update, Identity: core.Identity{ClusterID: "cluster", APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "ntfy"}}}}
	if err := core.RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	ordinaryDigest := plan.Digest
	plan.Changes[0].Takeover = true
	if err := core.RefreshDigest(&plan); err != nil {
		t.Fatal(err)
	}
	if plan.Digest == ordinaryDigest {
		t.Fatal("approval digest must bind the ownership transfer flag")
	}
}
