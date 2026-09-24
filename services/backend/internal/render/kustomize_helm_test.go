package render

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestKustomizeHelmRequiresApplicationOptIn(t *testing.T) {
	root := t.TempDir()
	writeRenderTestFile(t, filepath.Join(root, "kustomization.yml"), "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nhelmCharts:\n  - name: ntfy\n    version: 0.1.0\n")
	if err := validateKustomizeSources(context.Background(), root, root, false); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected opt-in error, got %v", err)
	}
	if err := validateKustomizeSources(context.Background(), root, root, true); err != nil {
		t.Fatalf("enabled local chart rejected: %v", err)
	}
}

func TestKustomizeHelmRendersVendoredChart(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is not installed")
	}
	if _, err := exec.LookPath("kustomize"); err != nil {
		if _, err := exec.LookPath("kubectl"); err != nil {
			t.Skip("kustomize and kubectl are not installed")
		}
	}
	root := t.TempDir()
	writeRenderTestFile(t, filepath.Join(root, "kustomization.yml"), "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nhelmCharts:\n  - name: ntfy\n    releaseName: ntfy-demo\n    version: 0.1.0\n")
	writeRenderTestFile(t, filepath.Join(root, "charts", "ntfy", "Chart.yaml"), "apiVersion: v2\nname: ntfy\nversion: 0.1.0\n")
	writeRenderTestFile(t, filepath.Join(root, "charts", "ntfy", "values.yaml"), "{}\n")
	writeRenderTestFile(t, filepath.Join(root, "charts", "ntfy", "templates", "configmap.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: ntfy-rendered\ndata:\n  source: helm\n")
	if err := validateKustomizeSources(context.Background(), root, root, true); err != nil {
		t.Fatal(err)
	}
	output, err := runKustomizeRenderer(context.Background(), root, root, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "name: ntfy-rendered") {
		t.Fatalf("Helm chart not rendered: %s", output)
	}
}

func TestKustomizeHelmAllowsChartOutsideOverlayInsideRepository(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is not installed")
	}
	if _, err := exec.LookPath("kustomize"); err != nil {
		if _, err := exec.LookPath("kubectl"); err != nil {
			t.Skip("kustomize and kubectl are not installed")
		}
	}
	repo := t.TempDir()
	overlay := filepath.Join(repo, "envs", "demo", "dev", "ntfy")
	writeRenderTestFile(t, filepath.Join(overlay, "kustomization.yml"), "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nhelmGlobals:\n  chartHome: ../../../../charts\nhelmCharts:\n  - name: ntfy\n    releaseName: ntfy-demo\n    version: 0.1.0\n")
	writeRenderTestFile(t, filepath.Join(repo, "charts", "ntfy", "Chart.yaml"), "apiVersion: v2\nname: ntfy\nversion: 0.1.0\n")
	writeRenderTestFile(t, filepath.Join(repo, "charts", "ntfy", "values.yaml"), "{}\n")
	writeRenderTestFile(t, filepath.Join(repo, "charts", "ntfy", "templates", "configmap.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: ntfy-rendered\ndata:\n  source: helm\n")
	if err := validateKustomizeSources(context.Background(), overlay, repo, true); err != nil {
		t.Fatal(err)
	}
	output, err := runKustomizeRenderer(context.Background(), overlay, repo, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "name: ntfy-rendered") {
		t.Fatalf("Helm chart not rendered: %s", output)
	}
}

func TestKustomizePathsCannotEscapeRepository(t *testing.T) {
	repo := t.TempDir()
	overlay := filepath.Join(repo, "envs", "demo", "dev", "ntfy")
	for _, field := range []string{"helmGlobals:\n  chartHome: ../../../../../outside", "resources:\n  - ../../../../../outside.yaml", "helmCharts:\n  - name: ntfy\n    valuesFile: ../../../../../outside.yaml"} {
		writeRenderTestFile(t, filepath.Join(overlay, "kustomization.yml"), "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n"+field+"\n")
		if err := validateKustomizeSources(context.Background(), overlay, repo, true); err == nil || !strings.Contains(err.Error(), "escapes the repository") {
			t.Errorf("accepted %q: %v", field, err)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo, "linked")); err != nil {
		t.Fatal(err)
	}
	writeRenderTestFile(t, filepath.Join(overlay, "kustomization.yml"), "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nhelmGlobals:\n  chartHome: ../../../../linked\n")
	if err := validateKustomizeSources(context.Background(), overlay, repo, true); err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("accepted symlink escape: %v", err)
	}
}

func TestKustomizeAllowsSharedComponentInsideRepository(t *testing.T) {
	if _, err := exec.LookPath("kustomize"); err != nil {
		if _, err := exec.LookPath("kubectl"); err != nil {
			t.Skip("kustomize and kubectl are not installed")
		}
	}
	repo := t.TempDir()
	overlay := filepath.Join(repo, "envs", "demo", "dev", "ntfy")
	writeRenderTestFile(t, filepath.Join(overlay, "kustomization.yml"), "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\ncomponents:\n  - ../../../../components/common\nresources:\n  - configmap.yaml\n")
	writeRenderTestFile(t, filepath.Join(overlay, "configmap.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: ntfy\n")
	writeRenderTestFile(t, filepath.Join(repo, "components", "common", "kustomization.yml"), "apiVersion: kustomize.config.k8s.io/v1alpha1\nkind: Component\ncommonLabels:\n  app.kubernetes.io/part-of: justcd\n")
	if err := validateKustomizeSources(context.Background(), overlay, repo, false); err != nil {
		t.Fatal(err)
	}
	output, err := runKustomizeRenderer(context.Background(), overlay, repo, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "app.kubernetes.io/part-of: justcd") {
		t.Fatalf("component not applied: %s", output)
	}
}

func TestKustomizeIgnoresUnrelatedKustomizations(t *testing.T) {
	repo := t.TempDir()
	overlay := filepath.Join(repo, "envs", "demo", "dev", "ntfy")
	writeRenderTestFile(t, filepath.Join(overlay, "kustomization.yml"), "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - configmap.yaml\n")
	writeRenderTestFile(t, filepath.Join(overlay, "configmap.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: ntfy\n")
	writeRenderTestFile(t, filepath.Join(repo, ".guide", "netpol", "kustomization.yaml"), "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - ../../../../../outside.yaml\n")
	if err := validateKustomizeSources(context.Background(), overlay, repo, false); err != nil {
		t.Fatalf("unrelated kustomization blocked application: %v", err)
	}
}

func TestKustomizeHelmRejectsNonPublicChartRepos(t *testing.T) {
	for _, repo := range []string{"http://charts.example.com", "https://127.0.0.1/charts", "https://localhost/charts", "https://user:secret@charts.example.com"} {
		if err := validateHelmRepository(context.Background(), repo); err == nil {
			t.Errorf("accepted unsafe chart repo %q", repo)
		}
	}
}

func writeRenderTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestKustomizeNamespaceOverrideBuildsForBoundTarget(t *testing.T) {
	if _, err := exec.LookPath("kustomize"); err != nil {
		if _, err := exec.LookPath("kubectl"); err != nil {
			t.Skip("kustomize and kubectl are not installed")
		}
	}
	repo := t.TempDir()
	overlay := filepath.Join(repo, "envs", "demo", "dev", "ntfy")
	writeRenderTestFile(t, filepath.Join(overlay, "kustomization.yaml"), "namespace: ntfy-dev\nresources:\n  - configmap.yaml\n  - rolebinding.yaml\n")
	writeRenderTestFile(t, filepath.Join(overlay, "configmap.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: oauth2-proxy-config\n")
	writeRenderTestFile(t, filepath.Join(overlay, "rolebinding.yaml"), "apiVersion: rbac.authorization.k8s.io/v1\nkind: RoleBinding\nmetadata:\n  name: access\nsubjects:\n  - kind: ServiceAccount\n    name: ntfy\n    namespace: ntfy-dev\nroleRef:\n  apiGroup: rbac.authorization.k8s.io\n  kind: Role\n  name: access\n")
	writeRenderTestFile(t, filepath.Join(repo, ".guide", "unrelated", "kustomization.yaml"), "resources:\n  - ../../../../../outside.yaml\n")
	if err := validateKustomizeSources(context.Background(), overlay, repo, false); err != nil {
		t.Fatalf("unrelated guide blocked application overlay: %v", err)
	}
	declared, err := KustomizationNamespace(repo, "envs/demo/dev/ntfy")
	if err != nil || declared != "ntfy-dev" {
		t.Fatalf("namespace = %q, err = %v", declared, err)
	}
	wrapper, cleanup, err := namespaceOverrideOverlay(repo, overlay, "justintest")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	output, err := runKustomizeRenderer(context.Background(), wrapper, repo, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(output), "namespace: justintest") != 2 || !strings.Contains(string(output), "namespace: ntfy-dev") {
		t.Fatalf("resource namespaces were not transformed or explicit subject was unexpectedly changed: %s", output)
	}
	cleanup()
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	options := Options{RepositoryRoot: repo, ManifestPath: "envs/demo/dev/ntfy", Renderer: "kustomize", ApplicationID: "app", ClusterID: "cluster", Namespaces: []core.Binding{{Namespace: "justintest"}}, Mapper: mapper}
	// Keep the end-to-end assertion focused on the ConfigMap. The RoleBinding above
	// demonstrates that explicit subject references remain unchanged by Kustomize.
	writeRenderTestFile(t, filepath.Join(overlay, "kustomization.yaml"), "namespace: ntfy-dev\nresources:\n  - configmap.yaml\n")
	if _, err := Render(context.Background(), options); err == nil || !strings.Contains(err.Error(), "unbound namespace") {
		t.Fatalf("expected unbound namespace before override, got %v", err)
	}
	options.KustomizeNamespaceOverride = true
	resources, err := Render(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].Identity.Namespace != "justintest" {
		t.Fatalf("override rendered unexpected resources: %+v", resources)
	}
}
