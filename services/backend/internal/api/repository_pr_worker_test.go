package api

import (
	"github.com/justlab/justcd/services/backend/internal/core"
	"testing"
)

func TestRepositoryPRResourcesStayInApprovedNamespace(t *testing.T) {
	for _, identity := range []core.Identity{{Namespace: "dev", Kind: "ConfigMap"}, {Namespace: "other", Kind: "ConfigMap"}, {ClusterScoped: true, Kind: "ClusterRole"}} {
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
