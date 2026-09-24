package render

import (
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIgnoredKindDoesNotRequireKubernetesDiscovery(t *testing.T) {
	manifest := []byte("apiVersion: secrets.hashicorp.com/v1beta1\nkind: VaultStaticSecret\nmetadata:\n  name: ntfy-secret\n  namespace: demo\n  labels:\n    justcd.io/skip: 'true'\n")
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{})
	_, err := parseAndNormalize(manifest, Options{ApplicationID: "app", ClusterID: "cluster", Namespaces: []core.Binding{{Namespace: "demo"}}, Mapper: mapper})
	if err == nil || !strings.Contains(err.Error(), "does not recognize") {
		t.Fatalf("expected discovery failure without rule, got %v", err)
	}
	for _, opts := range []Options{
		{ApplicationID: "app", ClusterID: "cluster", Namespaces: []core.Binding{{Namespace: "demo"}}, Mapper: mapper, IgnoredSelectors: []core.IgnoreSelector{{APIVersion: "secrets.hashicorp.com/v1beta1", Kind: "VaultStaticSecret"}}},
		{ApplicationID: "app", ClusterID: "cluster", Namespaces: []core.Binding{{Namespace: "demo"}}, Mapper: mapper, IgnoredSelectors: []core.IgnoreSelector{{LabelKey: "justcd.io/skip", LabelValue: "true"}}},
		{ApplicationID: "app", ClusterID: "cluster", Namespaces: []core.Binding{{Namespace: "demo"}}, Mapper: mapper, IgnoredResources: []core.Identity{{ClusterID: "cluster", APIVersion: "secrets.hashicorp.com/v1beta1", Kind: "VaultStaticSecret", Namespace: "demo", Name: "ntfy-secret"}}},
	} {
		resources, err := parseAndNormalize(manifest, opts)
		if err != nil || len(resources) != 0 {
			t.Fatalf("ignored resource reached discovery: %v, %#v", err, resources)
		}
	}
}

func TestIgnoredClusterScopedResourceDoesNotNeedDiscovery(t *testing.T) {
	manifest := []byte("apiVersion: example.io/v1\nkind: GlobalPolicy\nmetadata:\n  name: policy\n")
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{})
	resources, err := parseAndNormalize(manifest, Options{
		ApplicationID: "app", ClusterID: "cluster", Namespaces: []core.Binding{{Namespace: "demo"}}, Mapper: mapper,
		IgnoredResources: []core.Identity{{ClusterID: "cluster", APIVersion: "example.io/v1", Kind: "GlobalPolicy", Name: "policy", ClusterScoped: true}},
	})
	if err != nil || len(resources) != 0 {
		t.Fatalf("cluster-scoped ignore reached discovery: %v, %#v", err, resources)
	}
}
