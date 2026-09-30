package render

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
)

func TestYAMLRenderingExcludesJustCDDefinitions(t *testing.T) {
	root := t.TempDir()
	writeRenderTestFile(t, filepath.Join(root, "app.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: workload\n")
	writeRenderTestFile(t, filepath.Join(root, "justcd.yaml"), "apiVersion: justcd.io/v1alpha1\nkind: Application\n")
	writeRenderTestFile(t, filepath.Join(root, "other/justcd.yml"), "invalid: definition\n")
	output, err := readManifestFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "workload") || strings.Contains(string(output), "justcd.io") || strings.Contains(string(output), "invalid:") {
		t.Fatalf("JustCD definitions entered workload manifests: %s", output)
	}
}

func TestHelmRenderingUsesConfiguredReleaseName(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is required")
	}
	root := t.TempDir()
	writeRenderTestFile(t, filepath.Join(root, "Chart.yaml"), "apiVersion: v2\nname: shop\nversion: 0.1.0\n")
	writeRenderTestFile(t, filepath.Join(root, "templates/config.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}\n")
	output, err := renderHelmNamespace(context.Background(), Options{RepositoryRoot: root, HelmReleaseName: "shop-production"}, root, core.Binding{Namespace: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "name: shop-production") {
		t.Fatalf("configured release name not used: %s", output)
	}
}
