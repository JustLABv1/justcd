package api

import (
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestReviewScopeUsesAllConfiguredInputs(t *testing.T) {
	app := store.Application{ManifestPath: "apps/api", TargetManifestPath: "apps/api/production", NamespaceManifestPaths: map[string]string{"qa": "apps/api/qa"}, Renderer: "helm", HelmValuesFiles: []string{"config/common.yaml"}, NamespaceHelmValues: map[string]core.HelmValuesOverride{"qa": {Files: []string{"config/qa.yaml"}}}}
	paths := reviewPaths(app, store.PreviewProfile{Enabled: true, ManifestPath: "apps/api/preview"})
	for _, file := range []string{"apps/api/templates/service.yaml", "config/common.yaml", "config/qa.yaml", "apps/api/preview/values.yaml"} {
		if !pathsAffectApplication([]string{file}, paths) {
			t.Fatalf("expected %q to affect application", file)
		}
	}
	for _, file := range []string{"apps/web/values.yaml", "apps/apifoo/template.yaml", "config/common.yaml.bak"} {
		if pathsAffectApplication([]string{file}, paths) {
			t.Fatalf("unexpected match for %q", file)
		}
	}
	if !pathsAffectApplication([]string{"anything"}, reviewPaths(store.Application{ManifestPath: ".", Renderer: "yaml"}, store.PreviewProfile{})) {
		t.Fatal("repository root must include all files")
	}
	if !pathsAffectApplication([]string{"shared/base/deployment.yaml"}, reviewPaths(store.Application{ManifestPath: "apps/api", Renderer: "kustomize"}, store.PreviewProfile{})) {
		t.Fatal("Kustomize external bases must remain in scope")
	}
}
