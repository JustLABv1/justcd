package syncer

import (
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestSelectedResourceRemainsInManagedLiveSnapshot(t *testing.T) {
	identity := core.Identity{ClusterID: "cluster", APIVersion: "v1", Kind: "Secret", Namespace: "team", Name: "ntfy-env"}
	resource := store.ManagedResource{Identity: identity, Manifest: []byte(`{"metadata":{"name":"ntfy-env"}}`)}
	input := planInput{Selection: core.PlanSelection{Resources: []core.Identity{identity}}}
	if !resourceExcluded(input, identity) {
		t.Fatal("the selected change should be excluded from the plan")
	}
	if !shouldTrackManagedResource(input, resource) {
		t.Fatal("a selected resource must remain visible to ownership and live-state checks")
	}
	input.IgnoreRules = []store.ApplicationIgnoreRule{{IgnoreRule: core.IgnoreRule{Identity: identity}}}
	if shouldTrackManagedResource(input, resource) {
		t.Fatal("a permanently ignored resource should not be tracked in the live snapshot")
	}
}

func TestGitKindExclusionSkipsPreviouslyManagedResource(t *testing.T) {
	resource := store.ManagedResource{Identity: core.Identity{APIVersion: "secrets.hashicorp.com/v1beta1", Kind: "VaultStaticSecret", Namespace: "ntfy-dev", Name: "oauth2-proxy-secret"}, Manifest: []byte(`{"metadata":{"name":"oauth2-proxy-secret"}}`)}
	input := planInput{IgnoreSelectors: []core.IgnoreSelector{{APIVersion: resource.Identity.APIVersion, Kind: resource.Identity.Kind, ManagedByGit: true}}}
	if shouldTrackManagedResource(input, resource) {
		t.Fatal("Git-excluded resources must not enter live reads or pruning")
	}
}
