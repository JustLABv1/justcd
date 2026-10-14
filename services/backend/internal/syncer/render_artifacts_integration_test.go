//go:build integration

package syncer

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/render"
	"github.com/justlab/justcd/services/backend/internal/store"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIntegrationHelmApprovalRenderStability(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is required")
	}
	ctx, db := isolatedPlannerStore(t)
	app := store.Application{ID: "helm-app", WorkspaceID: "workspace", Name: "Helm regression", SourceID: "source", Revision: "main", ManifestPath: ".", Renderer: "helm", ClusterID: "cluster", SyncPolicy: "manual", PollSeconds: 300}
	if err := db.CreateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "templates"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"Chart.yaml": "apiVersion: v2\nname: grafana-regression\nversion: 0.1.0\n",
		"templates/resources.yaml": `{{- $password := randAlphaNum 32 -}}
apiVersion: v1
kind: Secret
metadata:
  name: grafana
data:
  admin-password: {{ $password | b64enc | quote }}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: checksum
  annotations:
    checksum/secret: {{ $password | sha256sum | quote }}
data:
  source: helm
`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gv := schema.GroupVersion{Version: "v1"}
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gv})
	for _, kind := range []string{"Secret", "ConfigMap"} {
		mapper.Add(gv.WithKind(kind), meta.RESTScopeNamespace)
	}
	options := render.Options{RepositoryRoot: root, ManifestPath: ".", Renderer: "helm", ApplicationID: app.ID, ClusterID: "cluster", Namespaces: []core.Binding{{ClusterID: "cluster", Namespace: "default"}}, Mapper: mapper}
	source := store.GitSource{ID: "source", RepositoryURL: "https://example.invalid/repo"}
	fingerprint, err := adoptionReviewFingerprint(source, "main", "commit-a", options)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: db}
	first, err := svc.renderPlanResources(ctx, options, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	unfrozen, err := render.Render(ctx, options)
	if err != nil || reflect.DeepEqual(first, unfrozen) {
		t.Fatalf("fixture did not generate a fresh password/checksum: %v", err)
	}
	// Approval and apply use separate service instances/processes in production.
	second, err := (&Service{Store: db}).renderPlanResources(ctx, options, fingerprint)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("approval changed the reviewed render: %v", err)
	}
	// A racing renderer must use the persisted winner, never its new output.
	winner, err := db.SaveRenderArtifact(ctx, app.ID, fingerprint, unfrozen)
	if err != nil || !reflect.DeepEqual(first, winner) {
		t.Fatalf("concurrent rendering replaced a reviewed artifact: %v", err)
	}
	fingerprint, err = adoptionReviewFingerprint(source, "main", "commit-b", options)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := svc.renderPlanResources(ctx, options, fingerprint)
	if err != nil || reflect.DeepEqual(first, changed) {
		t.Fatalf("new Git revision reused the old render: %v", err)
	}
}
