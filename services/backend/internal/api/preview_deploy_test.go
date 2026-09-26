package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func previewResource(kind, namespace, name string, manifest map[string]any) core.Resource {
	data, _ := json.Marshal(manifest)
	return core.Resource{Identity: core.Identity{APIVersion: "v1", Kind: kind, Namespace: namespace, Name: name}, Manifest: data}
}

func TestPreviewResourceSafety(t *testing.T) {
	namespace := "preview-pr-7-aabbcc"
	profile := store.PreviewProfile{HostSuffix: "preview.example.com", AllowedSecrets: []string{"preview-db", "preview-tls"}}
	resources := []core.Resource{
		previewResource("Deployment", namespace, "web", map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"automountServiceAccountToken": false, "containers": []any{map[string]any{"env": []any{map[string]any{"valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "preview-db", "key": "dsn"}}}}}}}}}}),
		previewResource("Ingress", namespace, "web", map[string]any{"spec": map[string]any{"rules": []any{map[string]any{"host": "web." + namespace + ".preview.example.com"}, map[string]any{"host": "api." + namespace + ".preview.example.com"}}, "tls": []any{map[string]any{"hosts": []any{"web." + namespace + ".preview.example.com"}, "secretName": "preview-tls"}}}}),
	}
	if err := validatePreviewResources(resources, profile, namespace, false); err != nil {
		t.Fatal(err)
	}
	bad := append([]core.Resource{}, resources...)
	bad[1] = previewResource("Ingress", namespace, "web", map[string]any{"spec": map[string]any{"rules": []any{map[string]any{"host": "prod.example.com"}}}})
	if err := validatePreviewResources(bad, profile, namespace, false); err == nil {
		t.Fatal("production ingress accepted")
	}
	bad[1] = previewResource("Secret", namespace, "db", map[string]any{"data": map[string]any{"password": "secret"}})
	if err := validatePreviewResources(bad, profile, namespace, false); err == nil {
		t.Fatal("secret creation accepted")
	}
	bad[1] = previewResource("Deployment", namespace, "other", map[string]any{"spec": map[string]any{"secretName": "prod-token"}})
	if err := validatePreviewResources(bad, profile, namespace, false); err == nil {
		t.Fatal("non-allowlisted secret accepted")
	}
	bad[1] = previewResource("Service", "production", "web", map[string]any{"spec": map[string]any{}})
	if err := validatePreviewResources(bad, profile, namespace, false); err == nil {
		t.Fatal("namespace escape accepted")
	}
	bad[1] = previewResource("RoleBinding", namespace, "admin", map[string]any{})
	if err := validatePreviewResources(bad, profile, namespace, false); err == nil {
		t.Fatal("preview RBAC accepted")
	}
	bad[1] = previewResource("Pod", namespace, "danger", map[string]any{"spec": map[string]any{"hostNetwork": true}})
	if err := validatePreviewResources(bad, profile, namespace, false); err == nil {
		t.Fatal("preview host network accepted")
	}
	bad[1] = previewResource("Deployment", namespace, "token", map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{}}}}}})
	if err := validatePreviewResources(bad, profile, namespace, true); err == nil {
		t.Fatal("fork with default service account token accepted")
	}
}

func TestPreviewIdentityAndGitRefs(t *testing.T) {
	id, namespace := previewIdentity("connection-1", 42, "preview")
	if !strings.HasPrefix(id, "preview-") || !strings.Contains(namespace, "-pr-42-") {
		t.Fatalf("unexpected identity: %s %s", id, namespace)
	}
	if pullRequestRef("github", 42) != "refs/pull/42/head" || pullRequestRef("gitlab", 42) != "refs/merge-requests/42/head" {
		t.Fatal("incorrect pull request ref")
	}
}
