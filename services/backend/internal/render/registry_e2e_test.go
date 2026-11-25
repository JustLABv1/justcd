package render

import (
	"context"
	"crypto/subtle"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/core"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestE2EHelmRegistryAuthentication renders charts from an authenticated OCI
// registry and HTTPS chart repository with private CAs. The runner provides a
// TLS registry with basic auth that already holds chart e2e-app 0.1.0 under
// oci://$JUSTCD_E2E_OCI_REGISTRY/charts, and must not trust its CA system-wide:
// the CA reaches Helm only through the credential.
//
//	JUSTCD_E2E_OCI_REGISTRY=registry.e2e:5000
//	JUSTCD_E2E_OCI_USERNAME / JUSTCD_E2E_OCI_PASSWORD
//	JUSTCD_E2E_OCI_CA_FILE=/path/to/ca.pem
func TestE2EHelmRegistryAuthentication(t *testing.T) {
	registry := os.Getenv("JUSTCD_E2E_OCI_REGISTRY")
	if registry == "" {
		t.Skip("set JUSTCD_E2E_OCI_REGISTRY to run the Helm registry end-to-end test")
	}
	for _, tool := range []string{"helm", "kustomize"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required: %v", tool, err)
		}
	}
	ca, err := os.ReadFile(os.Getenv("JUSTCD_E2E_OCI_CA_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	ociAuth := RegistryAuth{Endpoint: "oci://" + registry, Username: os.Getenv("JUSTCD_E2E_OCI_USERNAME"), Password: os.Getenv("JUSTCD_E2E_OCI_PASSWORD"), CAData: string(ca)}
	ociChart := core.HelmChartSource{Repository: "oci://" + registry + "/charts", Chart: "e2e-app", Version: "0.1.0"}
	ctx := context.Background()
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	newOptions := func(repo string) Options {
		return Options{RepositoryRoot: repo, ManifestPath: ".", Renderer: "helm", ApplicationID: "app", ClusterID: "cluster", Namespaces: []core.Binding{{Namespace: "demo"}}, Mapper: mapper}
	}
	expectMessage := func(t *testing.T, resources []core.Resource, name, message string) {
		t.Helper()
		for _, resource := range resources {
			if resource.Identity.Name == name {
				if !strings.Contains(string(resource.Manifest), message) {
					t.Fatalf("%s rendered without %q: %s", name, message, resource.Manifest)
				}
				return
			}
		}
		t.Fatalf("%s missing from %d rendered resources", name, len(resources))
	}

	t.Run("oci registry chart", func(t *testing.T) {
		repo := t.TempDir()
		writeRenderTestFile(t, filepath.Join(repo, "values", "prod.yaml"), "message: from-git-values\n")
		options := newOptions(repo)
		options.HelmChart = &ociChart
		options.HelmValuesFiles = []string{"values/prod.yaml"}
		if _, err := Render(ctx, options); err == nil {
			t.Fatal("private registry rendered without a credential")
		}
		withoutCA := ociAuth
		withoutCA.CAData = ""
		options.RegistryCredentials = []RegistryAuth{withoutCA}
		if _, err := Render(ctx, options); err == nil {
			t.Fatal("registry with a private CA rendered without trusting it")
		}
		wrongPassword := ociAuth
		wrongPassword.Password = "wrong"
		options.RegistryCredentials = []RegistryAuth{wrongPassword}
		if _, err := Render(ctx, options); err == nil {
			t.Fatal("registry accepted a wrong password")
		}
		options.RegistryCredentials = []RegistryAuth{ociAuth}
		resources, err := Render(ctx, options)
		if err != nil {
			t.Fatalf("%v: %s", err, rendererOutput(err))
		}
		expectMessage(t, resources, "justcd-e2e", "from-git-values")
	})

	t.Run("https chart repository", func(t *testing.T) {
		archive := packageE2EChart(t)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, password, ok := r.BasicAuth()
			if !ok || subtle.ConstantTimeCompare([]byte(user+":"+password), []byte("reader:repo-secret")) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="charts"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			switch r.URL.Path {
			case "/stable/index.yaml":
				fmt.Fprintf(w, "apiVersion: v1\nentries:\n  e2e-app:\n    - apiVersion: v2\n      name: e2e-app\n      version: 0.1.0\n      urls:\n        - e2e-app-0.1.0.tgz\n")
			case "/stable/e2e-app-0.1.0.tgz":
				http.ServeFile(w, r, archive)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()
		serverCA := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
		endpoint := server.URL + "/stable"
		repo := t.TempDir()
		options := newOptions(repo)
		options.HelmChart = &core.HelmChartSource{Repository: endpoint, Chart: "e2e-app", Version: "0.1.0"}
		options.HelmValuesYAML = "message: from-https\n"
		options.RegistryCredentials = []RegistryAuth{{Endpoint: endpoint, Username: "reader", Password: "repo-secret", CAData: serverCA}}
		resources, err := Render(ctx, options)
		if err != nil {
			t.Fatalf("%v: %s", err, rendererOutput(err))
		}
		expectMessage(t, resources, "justcd-e2e", "from-https")
	})

	t.Run("kustomize helmCharts", func(t *testing.T) {
		repo := t.TempDir()
		writeRenderTestFile(t, filepath.Join(repo, "app", "kustomization.yaml"), fmt.Sprintf("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nhelmCharts:\n  - name: e2e-app\n    repo: oci://%s/charts\n    version: 0.1.0\n    releaseName: kustomized\n    valuesInline:\n      message: from-kustomize\n", registry))
		options := newOptions(repo)
		options.Renderer, options.ManifestPath, options.KustomizeHelmEnabled = "kustomize", "app", true
		options.RegistryCredentials = []RegistryAuth{ociAuth}
		resources, err := Render(ctx, options)
		if err != nil {
			t.Fatalf("%v: %s", err, rendererOutput(err))
		}
		expectMessage(t, resources, "kustomized-e2e", "from-kustomize")
	})

	t.Run("chart dependency from registry", func(t *testing.T) {
		repo := t.TempDir()
		writeRenderTestFile(t, filepath.Join(repo, "umbrella", "Chart.yaml"), fmt.Sprintf("apiVersion: v2\nname: umbrella\nversion: 1.0.0\ndependencies:\n  - name: e2e-app\n    version: 0.1.0\n    repository: oci://%s/charts\n", registry))
		writeRenderTestFile(t, filepath.Join(repo, "umbrella", "values.yaml"), "e2e-app:\n  message: from-umbrella\n")
		options := newOptions(repo)
		options.ManifestPath = "umbrella"
		options.RegistryCredentials = []RegistryAuth{ociAuth}
		resources, err := Render(ctx, options)
		if err != nil {
			t.Fatalf("%v: %s", err, rendererOutput(err))
		}
		expectMessage(t, resources, "justcd-e2e", "from-umbrella")
	})
}

func rendererOutput(err error) string {
	var diagnostic interface{ LogDiagnostic() string }
	if errors.As(err, &diagnostic) {
		return diagnostic.LogDiagnostic()
	}
	return ""
}

// packageE2EChart builds the same chart the runner pushes to the registry.
func packageE2EChart(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeE2EChart(t, filepath.Join(dir, "e2e-app"))
	cmd := exec.Command("helm", "package", filepath.Join(dir, "e2e-app"), "--destination", dir)
	cmd.Env = cleanEnvironment(os.Environ(), dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helm package: %v: %s", err, output)
	}
	return filepath.Join(dir, "e2e-app-0.1.0.tgz")
}

func writeE2EChart(t *testing.T, dir string) {
	t.Helper()
	writeRenderTestFile(t, filepath.Join(dir, "Chart.yaml"), "apiVersion: v2\nname: e2e-app\nversion: 0.1.0\n")
	writeRenderTestFile(t, filepath.Join(dir, "values.yaml"), "message: chart-default\n")
	writeRenderTestFile(t, filepath.Join(dir, "templates", "configmap.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}-e2e\ndata:\n  message: {{ .Values.message | quote }}\n")
}

// TestE2EWriteChart lets the runner create the chart it pushes.
func TestE2EWriteChart(t *testing.T) {
	dir := os.Getenv("JUSTCD_E2E_WRITE_CHART")
	if dir == "" {
		t.Skip("set JUSTCD_E2E_WRITE_CHART to write the end-to-end chart")
	}
	writeE2EChart(t, dir)
}
