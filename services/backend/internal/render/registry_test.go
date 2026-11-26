package render

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testCertificatePEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "registry test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestNormalizeRegistryEndpoint(t *testing.T) {
	for input, want := range map[string]string{
		"oci://Registry.Example.com/charts/": "oci://registry.example.com/charts",
		"https://charts.example.com":         "https://charts.example.com",
		"oci://registry.internal:5000/team":  "oci://registry.internal:5000/team",
	} {
		got, err := NormalizeRegistryEndpoint(input)
		if err != nil || got != want {
			t.Errorf("NormalizeRegistryEndpoint(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"http://charts.example.com", "oci://user:pw@registry.example.com", "https://charts.example.com/a/../b", "https://charts.example.com?x=1", "registry.example.com", "oci:///charts"} {
		if _, err := NormalizeRegistryEndpoint(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestMatchRegistryAuthUsesMostSpecificSegmentPrefix(t *testing.T) {
	auths := []RegistryAuth{
		{Endpoint: "oci://registry.example.com", Username: "host"},
		{Endpoint: "oci://registry.example.com/team", Username: "team"},
		{Endpoint: "https://charts.example.com/stable", Username: "https"},
	}
	cases := map[string]string{
		"oci://registry.example.com/team/app":    "team",
		"oci://registry.example.com/teamwork/x":  "host",
		"oci://REGISTRY.example.com/other/app":   "host",
		"https://charts.example.com/stable":      "https",
		"https://charts.example.com/stable/":     "https",
		"https://charts.example.com/stablex":     "",
		"oci://charts.example.com/stable":        "",
		"oci://registry.example.com:5000/team/x": "",
	}
	for reference, want := range cases {
		got := ""
		if auth := matchRegistryAuth(auths, reference); auth != nil {
			got = auth.Username
		}
		if got != want {
			t.Errorf("match(%q) = %q, want %q", reference, got, want)
		}
	}
}

func TestChartRepositoryPolicyTrustsConfiguredRegistries(t *testing.T) {
	ctx := context.Background()
	private := "oci://registry.internal:5000/charts"
	if err := validateChartRepository(ctx, private, private+"/app", nil); err == nil {
		t.Fatal("private registry accepted without a credential")
	}
	auths := []RegistryAuth{{Endpoint: "oci://registry.internal:5000/charts", Username: "u", Password: "p"}}
	if err := validateChartRepository(ctx, private, private+"/app", auths); err != nil {
		t.Fatalf("configured private registry rejected: %v", err)
	}
	for _, repository := range []string{"http://registry.internal:5000/charts", "oci://user:pw@registry.internal:5000/charts"} {
		if err := validateChartRepository(ctx, repository, repository+"/app", auths); err == nil {
			t.Errorf("accepted unsafe repository %q", repository)
		}
	}
}

func TestPrepareHelmRegistriesWritesOCIConfigAndCA(t *testing.T) {
	home := t.TempDir()
	ca := testCertificatePEM(t)
	auths := []RegistryAuth{{Endpoint: "oci://registry.example.com/charts", Username: "robot", Password: "s3cret", CAData: ca}}
	env := []string{"PATH=" + os.Getenv("PATH"), "SSL_CERT_DIR=/custom/certs"}
	setup, err := prepareHelmRegistries(context.Background(), home, env, home, auths, []string{"oci://registry.example.com/charts/app", "oci://public.example.com/app"})
	if err != nil {
		t.Fatal(err)
	}
	var configPath, certDir string
	for _, pair := range setup.env {
		if value, ok := strings.CutPrefix(pair, "HELM_REGISTRY_CONFIG="); ok {
			configPath = value
		}
		if value, ok := strings.CutPrefix(pair, "SSL_CERT_DIR="); ok {
			if certDir != "" {
				t.Fatal("SSL_CERT_DIR set twice")
			}
			certDir = value
		}
	}
	if !strings.HasSuffix(certDir, string(os.PathListSeparator)+"/custom/certs") {
		t.Fatalf("SSL_CERT_DIR = %q, want existing directory kept", certDir)
	}
	pem, err := os.ReadFile(filepath.Join(strings.Split(certDir, string(os.PathListSeparator))[0], "registry-0.pem"))
	if err != nil || string(pem) != ca {
		t.Fatalf("CA not written: %v", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Auths map[string]map[string]string `json:"auths"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Auths) != 1 || config.Auths["registry.example.com"]["auth"] != base64.StdEncoding.EncodeToString([]byte("robot:s3cret")) {
		t.Fatalf("registry config = %s", raw)
	}
	info, err := os.Stat(configPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("registry config permissions = %v, %v", info.Mode(), err)
	}
}

func TestPrepareHelmRegistriesRejectsTwoCredentialsForOneHost(t *testing.T) {
	auths := []RegistryAuth{{Endpoint: "oci://registry.example.com/a", Username: "a", Password: "a"}, {Endpoint: "oci://registry.example.com/b", Username: "b", Password: "b"}}
	_, err := prepareHelmRegistries(context.Background(), t.TempDir(), nil, t.TempDir(), auths, []string{"oci://registry.example.com/a/x", "oci://registry.example.com/b/y"})
	if err == nil || !strings.Contains(err.Error(), "two different registry credentials") {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestPrepareHelmRegistriesRejectsOCIChartOutsideCredentialScope(t *testing.T) {
	home := t.TempDir()
	auths := []RegistryAuth{{Endpoint: "oci://registry.example.com/team", Username: "robot", Password: "test-token"}}
	references := []string{"oci://registry.example.com/team/app", "oci://registry.example.com/public/app"}

	_, err := prepareHelmRegistries(context.Background(), home, nil, home, auths, references)
	if err == nil {
		t.Fatal("mixed OCI credential scopes were accepted for a host-level Helm credential")
	}
}

func TestEnsureHelmDependenciesSkipsVendoredCharts(t *testing.T) {
	root := t.TempDir()
	chart := filepath.Join(root, "chart")
	// An unresolvable private registry proves no network policy or download runs.
	writeRenderTestFile(t, filepath.Join(chart, "Chart.yaml"), "apiVersion: v2\nname: app\nversion: 0.1.0\ndependencies:\n  - name: redis\n    version: 1.0.0\n    repository: oci://registry.internal/charts\n  - name: common\n    version: 1.0.0\n    repository: oci://registry.internal/charts\n")
	writeRenderTestFile(t, filepath.Join(chart, "charts", "redis-1.0.0.tgz"), "archive")
	writeRenderTestFile(t, filepath.Join(chart, "charts", "common", "Chart.yaml"), "apiVersion: v2\nname: common\nversion: 1.0.0\n")
	if err := ensureHelmDependencies(context.Background(), chart, root, nil); err != nil {
		t.Fatalf("vendored dependencies should not be validated or rebuilt: %v", err)
	}
}

func TestEnsureHelmDependenciesRejectsUnsafeSources(t *testing.T) {
	root := t.TempDir()
	chart := filepath.Join(root, "chart")
	outside := t.TempDir()
	for repository, message := range map[string]string{
		"file://" + outside:         "inside the repository",
		"@stable":                   "oci://, https://, or file://",
		"http://charts.example.com": "oci://, https://, or file://",
	} {
		writeRenderTestFile(t, filepath.Join(chart, "Chart.yaml"), "apiVersion: v2\nname: app\nversion: 0.1.0\ndependencies:\n  - name: dep\n    version: 1.0.0\n    repository: \""+repository+"\"\n")
		if err := ensureHelmDependencies(context.Background(), chart, root, nil); err == nil || !strings.Contains(err.Error(), message) {
			t.Errorf("%s: got %v, want error containing %q", repository, err, message)
		}
	}
}

func TestInspectKustomizeSourcesPlansCredentialedChartDownloads(t *testing.T) {
	repo := t.TempDir()
	overlay := filepath.Join(repo, "apps", "demo")
	writeRenderTestFile(t, filepath.Join(overlay, "kustomization.yaml"), "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nhelmGlobals:\n  chartHome: ../../vendor\nhelmCharts:\n  - name: podinfo\n    repo: oci://registry.internal/charts\n    version: 6.5.0\n")
	if _, err := inspectKustomizeSources(context.Background(), overlay, repo, true, nil); err == nil {
		t.Fatal("private OCI chart accepted without a credential")
	}
	auths := []RegistryAuth{{Endpoint: "oci://registry.internal", Username: "u", Password: "p"}}
	charts, err := inspectKustomizeSources(context.Background(), overlay, repo, true, auths)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRepo, _ := filepath.EvalSymlinks(repo)
	want := filepath.Join(resolvedRepo, "vendor", "podinfo-6.5.0")
	if len(charts) != 1 || charts[0].untarDir != want || charts[0].chart.Reference() != "oci://registry.internal/charts/podinfo" {
		t.Fatalf("remote charts = %+v, want untarDir %s", charts, want)
	}
	// A chart already present is not downloaded again.
	writeRenderTestFile(t, filepath.Join(want, "podinfo", "Chart.yaml"), "apiVersion: v2\nname: podinfo\nversion: 6.5.0\n")
	if err := prefetchKustomizeHelmCharts(context.Background(), charts, resolvedRepo, auths); err != nil {
		t.Fatalf("existing chart triggered a download: %v", err)
	}
}

func TestInspectKustomizeSourcesRequiresExactVersionForCredentialedCharts(t *testing.T) {
	repo := t.TempDir()
	writeRenderTestFile(t, filepath.Join(repo, "kustomization.yaml"), "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nhelmCharts:\n  - name: podinfo\n    repo: oci://registry.internal/charts\n    version: \"^6.0.0\"\n")
	auths := []RegistryAuth{{Endpoint: "oci://registry.internal", Username: "u", Password: "p"}}
	if _, err := inspectKustomizeSources(context.Background(), repo, repo, true, auths); err == nil || !strings.Contains(err.Error(), "exact semantic version") {
		t.Fatalf("expected exact version error, got %v", err)
	}
}

func TestValidateRegistryCA(t *testing.T) {
	if err := ValidateRegistryCA(""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRegistryCA(testCertificatePEM(t)); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"not pem", "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"} {
		if err := ValidateRegistryCA(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}
