package syncer

import (
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/kube"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestBranchPreviewResourceAccessScope(t *testing.T) {
	namespaceClient, clusterClient := &kube.Clients{}, &kube.Clients{}
	input := planInput{
		Application:        store.Application{ClusterID: "cluster", Namespaces: []store.NamespaceBinding{{Namespace: "preview"}}, BranchTest: &store.BranchTest{Mode: "isolated"}},
		NamespaceClients:   map[string]*kube.Clients{"preview": namespaceClient, "shared": namespaceClient},
		ClusterScopeClient: clusterClient,
	}
	for _, identity := range []core.Identity{
		{ClusterID: "cluster", Namespace: "shared"},
		{ClusterID: "other-cluster", Namespace: "preview"},
		{ClusterID: "cluster", Kind: "Namespace", Name: "preview", ClusterScoped: true},
		{ClusterID: "cluster", Kind: "ClusterRole", ClusterScoped: true},
		{ClusterID: "cluster"},
	} {
		if _, err := clientFor(input, identity); err == nil {
			t.Fatalf("preview allowed out-of-scope resource: %+v", identity)
		}
	}
	identity := core.Identity{ClusterID: "cluster", Namespace: "preview"}
	if client, err := clientFor(input, identity); err != nil || client != namespaceClient {
		t.Fatalf("preview namespace unavailable: %v", err)
	}
	// Existing-environment tests retain the application's normal cluster access.
	input.Application.BranchTest.Mode = "existing"
	if client, err := clientFor(input, core.Identity{ClusterID: "cluster", ClusterScoped: true}); err != nil || client != clusterClient {
		t.Fatalf("existing branch test lost cluster access: %v", err)
	}
}
