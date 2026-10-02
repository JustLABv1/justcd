package agentprotocol

import (
	"testing"
	"time"
)

func TestLocalScopeBoundary(t *testing.T) {
	p := Profile{Name: "default", WorkspaceIDs: []string{"w"}, Namespaces: []string{"dev", "preview-*"}}
	for _, tc := range []struct {
		uri, method, workspace, ns string
		scope, ok                  bool
	}{
		{"/api/v1/namespaces/dev/configmaps", "GET", "w", "dev", false, true},
		{"/apis/apps/v1/namespaces/dev/deployments/x?dryRun=All&fieldManager=justcd", "PATCH", "w", "dev", false, true},
		{"/api/v1/namespaces/preview-12/pods", "GET", "w", "preview-12", false, true},
		{"/api/v1/namespaces/prod/secrets", "GET", "w", "dev", false, false},
		{"/api/v1/namespaces/dev/secrets", "GET", "other", "dev", false, false},
		{"/api/v1/secrets", "GET", "w", "dev", false, false},
		{"/api/v1/namespaces", "POST", "w", "dev", false, false},
		{"/api/v1/namespaces/dev/pods/x/exec", "POST", "w", "dev", false, false},
		{"/api/v1/namespaces/dev/pods/x/log", "GET", "w", "dev", false, false},
		{"/api/v1/namespaces/dev/pods?watch=true", "GET", "w", "dev", false, false},
		{"https://evil.invalid/api/v1/pods", "GET", "w", "dev", false, false},
		{"//evil.invalid/api/v1/pods", "GET", "w", "dev", false, false},
		{"/api/v1/namespaces/dev/../prod/secrets", "GET", "w", "dev", false, false},
		{"/api/v1/namespaces/dev%2fprod/secrets", "GET", "w", "dev", false, false},
		{"/version", "GET", "w", "dev", false, true},
		{"/apis/apps/v1", "GET", "w", "dev", false, true},
		{"/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", "POST", "w", "dev", false, true},
	} {
		t.Run(tc.uri+tc.workspace, func(t *testing.T) {
			err := Validate(Request{Profile: "default", WorkspaceID: tc.workspace, Namespace: tc.ns, ClusterScope: tc.scope, Method: tc.method, URI: tc.uri, Deadline: time.Now().Add(time.Minute)}, p)
			if (err == nil) != tc.ok {
				t.Fatalf("allowed=%v, error=%v", tc.ok, err)
			}
		})
	}
	r := Request{Profile: "default", WorkspaceID: "w", Method: "POST", URI: "/api/v1/namespaces", ClusterScope: true, Deadline: time.Now().Add(time.Minute)}
	p.ClusterScope = true
	if err := Validate(r, p); err != nil {
		t.Fatal(err)
	}
	r.Deadline = time.Now().Add(-time.Second)
	if Validate(r, p) == nil {
		t.Fatal("expired task accepted")
	}
}

func TestProtectedNamespaceOverridesClusterScope(t *testing.T) {
	p := Profile{Name: "admin", WorkspaceIDs: []string{"w"}, ClusterScope: true, DeniedNamespaces: []string{"agent", "kube-system"}}
	for _, uri := range []string{"/api/v1/namespaces/agent/secrets/x", "/api/v1/namespaces/kube-system/pods/x", "/api/v1/namespaces/agent", "/api/v1/secrets"} {
		for _, method := range []string{"GET", "PATCH"} {
			r := Request{Profile: "admin", WorkspaceID: "w", ClusterScope: true, Method: method, URI: uri, Deadline: time.Now().Add(time.Minute)}
			if Validate(r, p) == nil {
				t.Fatalf("protected namespace accepted: %s %s", method, uri)
			}
		}
	}
}

func TestWildcardNamespacesAllowConnectionChecksAndKeepBoundaries(t *testing.T) {
	p := Profile{Name: "default", WorkspaceIDs: []string{"w"}, Namespaces: []string{"*"}, ClusterScope: true, DeniedNamespaces: []string{"agent", "kube-system"}}
	for _, tc := range []struct {
		name, uri, workspace, namespace string
		clusterScope, allowed           bool
	}{
		{"discovery", "/apis/apps/v1", "w", "grafana-dev", false, true},
		{"permission review", "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", "w", "grafana-dev", false, true},
		{"application resources", "/api/v1/namespaces/grafana-dev/pods", "w", "grafana-dev", false, true},
		{"other workspace", "/api/v1/namespaces/grafana-dev/pods", "other", "grafana-dev", false, false},
		{"scope mismatch", "/api/v1/namespaces/prod/pods", "w", "grafana-dev", false, false},
		{"protected namespace", "/api/v1/namespaces/kube-system/pods", "w", "kube-system", false, false},
		{"protected cluster request", "/api/v1/namespaces/agent/secrets", "w", "agent", true, false},
		{"cross namespace secrets", "/api/v1/secrets", "w", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			method := "GET"
			if tc.name == "permission review" {
				method = "POST"
			}
			err := Validate(Request{Profile: "default", WorkspaceID: tc.workspace, Namespace: tc.namespace, ClusterScope: tc.clusterScope, Method: method, URI: tc.uri, Deadline: time.Now().Add(time.Minute)}, p)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v: %v", tc.allowed, err)
			}
		})
	}
}
