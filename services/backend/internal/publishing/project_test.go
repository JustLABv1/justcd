package publishing

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func archive(t *testing.T, names ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, n := range names {
		f, e := w.Create(n)
		if e != nil {
			t.Fatal(e)
		}
		f.Write([]byte("test"))
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func config() Config {
	return Config{Name: "statistics", Runtime: "shiny", EntryPoint: ".", BaseImage: "registry.example.com/base/shiny:1", BuilderImage: "registry.example.com/base/builder:1", RegistryImage: "registry.example.com/apps/statistics", Namespace: "analytics", SecretMode: "variables"}
}
func TestArchiveRejectsUnsafeSources(t *testing.T) {
	for _, n := range []string{"../app.R", "/app.R", "a/../../app.R", "a\\app.R", ".git/config", ".env", "x/.Renviron", ".streamlit/secrets.toml", "."} {
		t.Run(n, func(t *testing.T) {
			if _, e := readArchive(t, archive(t, n)); e == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	if _, e := readArchive(t, archive(t, "app.R", "app.R")); e == nil {
		t.Fatal("duplicate accepted")
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: "app.R"}
	h.SetMode(os.ModeSymlink | 0777)
	f, _ := w.CreateHeader(h)
	f.Write([]byte("/etc/passwd"))
	w.Close()
	if _, e := readArchive(t, b.Bytes()); e == nil {
		t.Fatal("symlink accepted")
	}
	var bomb bytes.Buffer
	zw := zip.NewWriter(&bomb)
	bf, _ := zw.Create("data")
	bf.Write(bytes.Repeat([]byte{'a'}, (2 << 20)))
	zw.Close()
	if _, e := ExtractArchive(context.Background(), bytes.NewReader(bomb.Bytes()), int64(bomb.Len()), t.TempDir(), Limits{ArchiveBytes: 10 << 20, ExpandedBytes: 1 << 20}); e == nil {
		t.Fatal("expanded size limit bypassed")
	}
}
func TestGenerateShinyAndStreamlitProjects(t *testing.T) {
	for _, runtime := range []string{"shiny", "streamlit"} {
		t.Run(runtime, func(t *testing.T) {
			c := config()
			c.Runtime = runtime
			names := []string{"app.R", "packages.txt", "data.bin"}
			if runtime == "streamlit" {
				c.EntryPoint = "app.py"
				names = []string{"app.py", "requirements.txt", "data.bin"}
			}
			source, e := readArchive(t, archive(t, names...))
			if e != nil {
				t.Fatal(e)
			}
			files, e := Generate(c, source)
			if e != nil {
				t.Fatal(e)
			}
			found := map[string]string{}
			for _, f := range files {
				found[f.Path] = f.Content
				if strings.HasSuffix(f.Path, ".yaml") || f.Path == ".gitlab-ci.yml" {
					for _, doc := range strings.Split(f.Content, "\n---\n") {
						if _, e := yaml.YAMLToJSON([]byte(doc)); e != nil {
							t.Fatalf("invalid %s: %v", f.Path, e)
						}
					}
				}
			}
			if strings.Contains(found["Dockerfile"], "REGISTRY_PASSWORD") {
				t.Fatal("secret in Dockerfile")
			}
			if !strings.Contains(found[".gitlab-ci.yml"], "containerimage.digest") || !strings.Contains(found[".gitlab-ci.yml"], "git rev-parse FETCH_HEAD") {
				t.Fatal("missing immutable image or stale build guard")
			}
			var raw []byte
			for _, f := range files {
				if f.Path == "source/data.bin" {
					raw, e = os.ReadFile(f.DiskPath)
				}
			}
			if e != nil || string(raw) != "test" {
				t.Fatal("source content changed")
			}
			if _, ok := found["deploy/application.yaml"]; ok {
				t.Fatal("unbuilt image must not be deployed")
			}
		})
	}
}
func TestValidateRejectsTemplateInjection(t *testing.T) {
	for _, change := range []func(*Config){func(c *Config) { c.BaseImage = "base\nRUN evil" }, func(c *Config) { c.EntryPoint = "x'); system('evil')" }, func(c *Config) { c.RegistryImage = "registry/x:latest" }, func(c *Config) { c.Host = "example.com\nevil" }, func(c *Config) { c.RunnerTag = "x\nscript: evil" }, func(c *Config) { c.SecretMode = "git" }, func(c *Config) { c.SecretMode = "vault"; c.VaultAddress = "http://vault" }} {
		c := config()
		change(&c)
		if c.Validate() == nil {
			t.Fatal("invalid config accepted")
		}
	}
	c := config()
	c.SecretMode = "vault"
	c.VaultAddress = "https://vault.example.com"
	c.VaultRole = "analytics"
	c.VaultPath = "secret/data/apps/registry"
	files, e := Generate(c, []File{{Path: "source/app.R"}, {Path: "source/packages.txt"}})
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		if f.Path == ".gitlab-ci.yml" {
			var v map[string]any
			if e = yaml.Unmarshal([]byte(f.Content), &v); e != nil {
				t.Fatal(e)
			}
			publish := v["publish"].(map[string]any)
			script := publish["script"].([]any)[0].(string)
			cmd := exec.Command("sh", "-n")
			cmd.Stdin = strings.NewReader(script)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("invalid pipeline shell: %s %v", out, err)
			}
			if publish["id_tokens"] == nil || publish["secrets"] == nil {
				t.Fatal("missing Vault OIDC configuration")
			}
		}
	}
}

func readArchive(t *testing.T, data []byte) ([]File, error) {
	t.Helper()
	return ExtractArchive(context.Background(), bytes.NewReader(data), int64(len(data)), t.TempDir(), Limits{})
}
func TestArchiveLimitsAndDiskFingerprint(t *testing.T) {
	data := archive(t, "app.R")
	if _, err := ExtractArchive(context.Background(), bytes.NewReader(data), int64(len(data)), t.TempDir(), Limits{ArchiveBytes: int64(len(data) - 1), ExpandedBytes: 100}); err == nil {
		t.Fatal("compressed limit bypassed")
	}
	files, err := ExtractArchive(context.Background(), bytes.NewReader(data), int64(len(data)), t.TempDir(), Limits{ArchiveBytes: int64(len(data)), ExpandedBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Content != "" || files[0].DiskPath == "" {
		t.Fatal("source was buffered instead of stored on disk")
	}
	second, err := readArchive(t, data)
	if err != nil {
		t.Fatal(err)
	}
	firstHash, err := Fingerprint(context.Background(), config(), files, "cluster")
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := Fingerprint(context.Background(), config(), second, "cluster")
	if err != nil || firstHash != secondHash {
		t.Fatal("temporary paths changed source identity")
	}
	os.WriteFile(second[0].DiskPath, []byte("changed"), 0600)
	changed, _ := Fingerprint(context.Background(), config(), second, "cluster")
	if changed == firstHash {
		t.Fatal("content change did not change fingerprint")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = ExtractArchive(ctx, bytes.NewReader(data), int64(len(data)), t.TempDir(), Limits{}); err == nil {
		t.Fatal("cancellation ignored")
	}
}

// A stored (uncompressed) entry ensures both the ZIP and source exceed the old
// 10 MiB ceiling without building the test payload in memory.
func TestArchiveAboveFormerLimit(t *testing.T) {
	zipFile, err := os.CreateTemp(t.TempDir(), "large-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer zipFile.Close()
	writer := zip.NewWriter(zipFile)
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: "dataset.bin", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.CopyN(entry, zeroReader{}, 11<<20); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	stat, err := zipFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	files, err := ExtractArchive(context.Background(), zipFile, stat.Size(), t.TempDir(), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := os.Stat(files[0].DiskPath)
	if err != nil || extracted.Size() != 11<<20 || files[0].Content != "" {
		t.Fatal("large source was not extracted to disk", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestCatalogTemplatesMatchRuntime(t *testing.T) {
	for _, template := range Templates() {
		cfg := config()
		cfg.TemplateID = template.ID
		cfg.Runtime = template.Runtime
		cfg.EntryPoint = template.EntryPoint
		if err := cfg.Validate(); err != nil {
			t.Fatal(template.ID, err)
		}
	}
	cfg := config()
	cfg.TemplateID = "streamlit"
	if cfg.Validate() == nil {
		t.Fatal("mismatched template runtime accepted")
	}
}
