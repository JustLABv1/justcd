package api

import (
	"context"
	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/repoconfig"
	"github.com/justlab/justcd/services/backend/internal/store"
	"testing"
)

func TestRepositoryPRResourcesStayInApprovedNamespace(t *testing.T) {
	for _, identity := range []core.Identity{{Namespace: "dev", Kind: "ConfigMap"}, {Namespace: "other", Kind: "ConfigMap"}, {ClusterScoped: true, Kind: "ClusterRole"}, {APIVersion: "v1", Kind: "Namespace", Name: "dev", ClusterScoped: true}} {
		err := validateRepositoryPRResourceScope([]core.Resource{{Identity: identity}}, "dev")
		if (identity.Namespace == "dev" && !identity.ClusterScoped) != (err == nil) {
			t.Fatalf("unexpected scope validation for %+v: %v", identity, err)
		}
	}
}

func TestRepositoryPRDefinitionPaths(t *testing.T) {
	for _, test := range []struct {
		paths []string
		want  bool
	}{{nil, false}, {[]string{"README.md", "apps/shop/kustomization.yaml"}, false}, {[]string{"apps/new/justcd.yaml"}, true}, {[]string{"justcd.yml"}, true}, {[]string{"notjustcd.yaml"}, false}} {
		if got := repositoryPRHasDefinitions(test.paths); got != test.want {
			t.Fatalf("%v: got %v", test.paths, got)
		}
	}
}

func TestRepositoryPRExcludedDestinationsDoNotResolveAccess(t *testing.T) {
	for _, mode := range []string{"existing", "isolated", "review-only"} {
		t.Run(mode, func(t *testing.T) {
			repo := store.RepositoryConfiguration{PRSettings: store.RepositoryPRSettings{Mode: mode, Destinations: []store.PRDestination{{ClusterID: "allowed", NamespacePattern: ".*-dev"}}}}
			for _, destination := range []struct{ cluster, namespace string }{{"allowed", "mimir-prd"}, {"allowed", "mimir-dev-extra"}, {"other", "mimir-dev"}, {"", "mimir-dev"}} {
				var definition repoconfig.Definition
				definition.Spec.Destination.Namespace = destination.namespace
				definition.Spec.Destination.CreateNamespaces = true
				// No store or syncer: exclusions must return before deployment resolution.
				_, eligible, err := (&Server{}).resolveRepositoryPRDefinition(context.Background(), repo, definition, destination.cluster)
				if err != nil || eligible {
					t.Fatalf("excluded destination resolved: %+v %v", destination, err)
				}
			}
		})
	}
}

func TestRepositoryPRClusterIdentity(t *testing.T) {
	clusters := []store.Cluster{{ID: "cluster-id", Name: "cluster-name"}}
	for _, reference := range []string{"cluster-id", "cluster-name"} {
		id, err := repositoryPRClusterID(clusters, reference)
		if err != nil || id != "cluster-id" {
			t.Fatalf("identity %q: %q %v", reference, id, err)
		}
	}
	if id, err := repositoryPRClusterID(clusters, "other"); err != nil || id != "" {
		t.Fatalf("unavailable cluster: %q %v", id, err)
	}
	clusters = append(clusters, store.Cluster{ID: "second-id", Name: "cluster-name"})
	if _, err := repositoryPRClusterID(clusters, "cluster-name"); err == nil {
		t.Fatal("ambiguous cluster name accepted")
	}
}
