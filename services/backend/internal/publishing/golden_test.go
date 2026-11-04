package publishing

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite generated-file golden files")

type goldenCase struct {
	name   string
	mutate func(*Config)
	source []string
}

func goldenCases() []goldenCase {
	shinyPackages := []string{"app.R", "packages.txt"}
	shinyRenv := []string{"app.R", "renv.lock"}
	full := func(c *Config) {
		c.BuildEngine = "podman"
		c.CAVariable = "PLAIN2_ROOT_CA"
		c.RunnerTag = "ocp"
		c.RunnerServiceAccount = "gitlab-runner-app-sa"
		c.RunnerSCC = "gitlab-runner-scc"
		c.BuilderSetup = "/home/user/entrypoint.sh"
		c.SecretMode = "vault"
		c.VaultAddress = "https://vault.example.com"
		c.VaultRole = "publisher"
		c.VaultPath = "secret/data/analytics/registry"
		c.Host = "app.apps.example.com"
		c.PullSecret = "pull-secret"
		c.ExposureLabels = "router=external\nexample.com/team=data"
		c.ExposureAnnotations = "haproxy.router.openshift.io/timeout=5m"
	}
	ingress := func(c *Config) {
		c.Host = "app.apps.example.com"
		c.Exposure = "ingress"
		c.IngressClass = "openshift-default"
		c.IngressTLS = "none"
		c.ExposureLabels = "router=external"
	}
	managed := func(m func(*Config)) func(*Config) {
		return func(c *Config) {
			c.ManagedCompletion = true
			if m != nil {
				m(c)
			}
		}
	}
	return []goldenCase{
		{"shiny-packages-legacy", nil, shinyPackages},
		{"shiny-packages-managed", managed(nil), shinyPackages},
		{"shiny-renv-managed-full", managed(full), shinyRenv},
		{"shiny-renv-legacy-full", full, shinyRenv},
		{"shiny-ingress-managed", managed(ingress), shinyPackages},
		{"streamlit-managed", func(c *Config) { c.Runtime, c.EntryPoint = "streamlit", "app.py"; c.ManagedCompletion = true }, []string{"app.py", "requirements.txt"}},
		{"streamlit-legacy-ingress", func(c *Config) { c.Runtime, c.EntryPoint = "streamlit", "app.py"; ingress(c) }, []string{"app.py", "requirements.txt"}},
		{"static-managed", func(c *Config) {
			c.Runtime, c.EntryPoint, c.WebServerImage = "static", ".", "registry.example.com/base/nginx:1"
			managed(full)(c)
		}, []string{"index.html"}},
		{"static-subdir-legacy", func(c *Config) {
			c.Runtime, c.EntryPoint, c.WebServerImage = "static", "site", "registry.example.com/base/nginx:1"
		}, []string{"site/index.html"}},
		{"python-static-managed", func(c *Config) {
			c.Runtime, c.EntryPoint, c.WebServerImage = "python-static", "build_html.py", "registry.example.com/base/nginx:1"
			c.BuildCommand, c.OutputDir = "python build_html.py", "dist"
			managed(ingress)(c)
		}, []string{"build_html.py", "requirements.txt"}},
		{"python-static-legacy", func(c *Config) {
			c.Runtime, c.EntryPoint, c.WebServerImage = "python-static", "build_html.py", "registry.example.com/base/nginx:1"
			c.BuildCommand, c.OutputDir = "python build_html.py", "dist"
		}, []string{"build_html.py"}},
		{"gitimage-legacy", func(c *Config) { c.GitImage = "registry.example.com/base/git:1" }, shinyPackages},
		{"buildkit-ca-managed", managed(func(c *Config) { c.CAVariable = "ROOT_CA"; c.RunnerTag = "linux" }), shinyRenv},
	}
}

func goldenSource(paths []string) []File {
	out := []File{}
	for _, p := range paths {
		out = append(out, File{Path: "source/" + p, Content: "x", Encoding: "text"})
	}
	return out
}

func TestGeneratedFilesMatchGolden(t *testing.T) {
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			c := config()
			c.Name = "statistics"
			if tc.mutate != nil {
				tc.mutate(&c)
			}
			files, err := Generate(c, goldenSource(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join("testdata", "golden", tc.name)
			got := map[string]string{}
			for _, f := range files {
				if strings.HasPrefix(f.Path, "source/") {
					continue
				}
				got[strings.ReplaceAll(f.Path, "/", "__")] = f.Content
			}
			if *updateGolden {
				_ = os.RemoveAll(dir)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				for name, content := range got {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				return
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			names := []string{}
			for _, e := range entries {
				names = append(names, e.Name())
				want, _ := os.ReadFile(filepath.Join(dir, e.Name()))
				if got[e.Name()] != string(want) {
					t.Errorf("%s differs from golden:\n--- want\n%s\n--- got\n%s", e.Name(), want, got[e.Name()])
				}
			}
			sort.Strings(names)
			if len(names) != len(got) {
				t.Errorf("generated %d files, golden has %d", len(got), len(names))
			}
		})
	}
}
