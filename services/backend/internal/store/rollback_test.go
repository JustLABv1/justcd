package store

import "testing"

func TestSameApplicationNamespacesPreservesScopeWithoutRebindingCredentials(t *testing.T) {
	bindings := []NamespaceBinding{
		{Namespace: "observability"},
		{Namespace: "apps"},
	}
	if !sameApplicationNamespaces(bindings, []string{"apps", "observability"}) {
		t.Fatal("namespace order should not change scope equality")
	}
	if sameApplicationNamespaces(bindings, []string{"apps"}) {
		t.Fatal("a narrower rollback namespace scope must not be pinned")
	}
	if sameApplicationNamespaces(bindings, []string{"apps", "apps"}) {
		t.Fatal("duplicate namespaces must not match an application scope")
	}
}
