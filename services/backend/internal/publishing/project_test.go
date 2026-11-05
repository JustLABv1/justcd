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
		if cfg.Runtime == "static" {
			cfg.WebServerImage = "registry.example.com/nginx:1"
		}
		if cfg.Runtime == "python-static" {
			cfg.BuildCommand = "python build_html.py"
			cfg.OutputDir = "dist"
			cfg.WebServerImage = "registry.example.com/nginx:1"
		}
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

func TestStaticWebsiteRecipe(t *testing.T) {
	c := config()
	c.TemplateID, c.Runtime, c.EntryPoint = "python-static", "python-static", "build_html.py"
	c.BuildCommand, c.OutputDir, c.WebServerImage = "python build_html.py\n# FROM evil", "dist", "registry.example.com/nginx:1"
	files, err := Generate(c, []File{{Path: "source/build_html.py", Content: "print('hello')", Encoding: "text"}})
	if err != nil {
		t.Fatal(err)
	}
	var docker, deployment string
	for _, f := range files {
		if f.Path == "Dockerfile" {
			docker = f.Content
		}
		if f.Path == ".justcd/deployment.yaml" {
			deployment = f.Content
		}
	}
	if !strings.Contains(docker, `RUN ["sh","-c","python build_html.py\n# FROM evil"]`) || !strings.Contains(docker, "COPY --from=site-build /justcd-site/ /usr/share/nginx/html/") || !strings.Contains(deployment, "containerPort: 8080") {
		t.Fatal("incorrect static recipe", docker, deployment)
	}
	for _, dir := range []string{".", "../dist", "/dist", "dist/../out", ""} {
		c.OutputDir = dir
		if c.Validate() == nil {
			t.Fatal("unsafe output accepted", dir)
		}
	}
}
func TestStaticOutputPreparation(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	for _, mode := range []string{"valid", "missing-index", "symlink", "escape"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			out := root + "/dist"
			target := t.TempDir() + "/site"
			if err := os.Mkdir(out, 0700); err != nil {
				t.Fatal(err)
			}
			if mode != "missing-index" {
				if err := os.WriteFile(out+"/index.html", []byte("hello"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "symlink" {
				if err := os.Symlink("index.html", out+"/linked.html"); err != nil {
					t.Fatal(err)
				}
			}
			dir := "dist"
			if mode == "escape" {
				dir = "../"
			}
			cmd := exec.Command(python, "-c", staticOutputScript, dir, target)
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			if mode != "valid" {
				if err == nil {
					t.Fatal("unsafe output accepted", mode)
				}
				return
			}
			if err != nil {
				t.Fatal(err, string(output))
			}
			info, err := os.Stat(target + "/index.html")
			if err != nil || info.Mode().Perm() != 0644 {
				t.Fatal("output not readable by arbitrary UID", err)
			}
		})
	}
}

func TestReadyMadeStaticWebsite(t *testing.T) {
	c := config()
	c.TemplateID = "static"
	c.Runtime = "static"
	c.BaseImage = ""
	c.WebServerImage = "registry.example.com/nginx:1"
	for _, dir := range []string{".", "public"} {
		t.Run(dir, func(t *testing.T) {
			c.EntryPoint = dir
			source := []File{{Path: "source/" + strings.TrimPrefix(dir+"/", "./") + "index.html", Content: "<h1>Hello</h1>"}}
			files, err := Generate(c, source)
			if err != nil {
				t.Fatal(err)
			}
			var docker, manifest string
			for _, f := range files {
				if f.Path == "Dockerfile" {
					docker = f.Content
				}
				if f.Path == ".justcd/deployment.yaml" {
					manifest = f.Content
				}
			}
			expected := "source/"
			if dir != "." {
				expected += dir + "/"
			}
			if !strings.Contains(docker, "COPY --chmod=0755 "+expected+" /usr/share/nginx/html/") || !strings.Contains(docker, "FROM "+c.WebServerImage) || !strings.Contains(manifest, "containerPort: 8080") {
				t.Fatal("incorrect static recipe", docker)
			}
			for _, unused := range []string{"RUN ", "python", "pip install", "site-build"} {
				if strings.Contains(docker, unused) {
					t.Fatal("static files require a build", docker)
				}
			}
			if _, err := Generate(c, []File{{Path: "source/other/index.html"}, {Path: "source/README.md"}}); err == nil {
				t.Fatal("missing selected index accepted")
			}
		})
	}
	c.EntryPoint = "."
	c.BuildCommand = "python build.py"
	if c.Validate() == nil {
		t.Fatal("build command accepted for ready-made HTML")
	}
	c.BuildCommand = ""
	c.WebServerImage = ""
	if c.Validate() == nil {
		t.Fatal("missing webserver accepted")
	}
}

func TestArchiveReportsEmptyAndFoldersOnly(t *testing.T) {
	for _, names := range [][]string{nil, {"site/", "site/assets/"}} {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		for _, name := range names {
			if _, err := writer.Create(name); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		_, err := readArchive(t, buffer.Bytes())
		expected := "ZIP archive is empty"
		if len(names) > 0 {
			expected = "contains only folders (2 entries)"
		}
		if err == nil || !strings.Contains(err.Error(), expected) {
			t.Fatal("unhelpful archive error", err)
		}
	}
	// A containing folder is valid when the archive also includes real files.
	files, err := readArchive(t, archive(t, "site/index.html", "site/assets/main.css"))
	if err != nil || len(files) != 2 {
		t.Fatal("nested website files rejected", err)
	}
}

func TestOptionalCAVariable(t *testing.T) {
	c := config()
	if strings.Contains(pipeline(c), "SSL_CERT_FILE") || strings.Contains(pipeline(c), "PLAIN2_ROOT_CA") {
		t.Fatal("default pipeline must not require platform CA variables")
	}
	c.CAVariable = "PLAIN2_ROOT_CA"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	ci := pipeline(c)
	for _, expected := range []string{"${PLAIN2_ROOT_CA:-}", `"$PLAIN2_ROOT_CA"`, "export SSL_CERT_FILE=", "export GIT_SSL_CAINFO=", `cat "$system_ca"`, "is empty or unavailable"} {
		if !strings.Contains(ci, expected) {
			t.Fatalf("missing CA configuration %q", expected)
		}
	}
	var v map[string]any
	if err := yaml.Unmarshal([]byte(ci), &v); err != nil {
		t.Fatal(err)
	}
	script := v["publish"].(map[string]any)["script"].([]any)[0].(string)
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invalid CA shell: %s: %v", out, err)
	}
	for _, invalid := range []string{"CA;echo bad", "$(whoami)", "CA\nOTHER", "1CA", "CA-VAR"} {
		c.CAVariable = invalid
		if c.Validate() == nil {
			t.Fatalf("accepted invalid CA variable %q", invalid)
		}
	}
}

func TestPodmanPipeline(t *testing.T) {
	c := config()
	c.BuildEngine = "podman"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	ci := pipeline(c)
	for _, forbidden := range []string{"buildctl-daemonless.sh", "jq -", "--tls-verify=false"} {
		if strings.Contains(ci, forbidden) {
			t.Fatalf("Podman job contains %s", forbidden)
		}
	}
	var v map[string]any
	if err := yaml.Unmarshal([]byte(ci), &v); err != nil {
		t.Fatal(err)
	}
	script := v["publish"].(map[string]any)["script"].([]any)[0].(string)
	prefix := strings.Split(script, "# A newer source commit")[0]
	dir := t.TempDir()
	fake := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$TEST_LOG"
case "$1" in
login) test "$(cat)" = "$REGISTRY_PASSWORD" ;;
push)
 while [ "$#" -gt 0 ]; do
  if [ "$1" = --digestfile ]; then shift; printf '%s' "$TEST_DIGEST" > "$1"; break; fi
  shift
 done ;;
esac
`
	if err := os.WriteFile(dir+"/podman", []byte(fake), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		digest string
		valid  bool
	}{{"sha256:" + strings.Repeat("a", 64), true}, {"malformed", false}} {
		cmd := exec.Command("sh", "-c", prefix+"\nprintf '%s' \"$digest\"")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "TEST_LOG="+dir+"/commands", "TEST_DIGEST="+tc.digest, "REGISTRY_USER=test", "REGISTRY_PASSWORD=test-password", "JUSTCD_IMAGE=registry.example.com/apps/test", "CI_COMMIT_SHA=abc123", "SSL_CERT_FILE=")
		out, err := cmd.CombinedOutput()
		if tc.valid && (err != nil || string(out) != tc.digest) {
			t.Fatalf("Podman script failed: %s %v", out, err)
		}
		if !tc.valid && err == nil {
			t.Fatal("invalid digest accepted")
		}
	}
	log, err := os.ReadFile(dir + "/commands")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "test-password") {
		t.Fatal("password exposed in arguments")
	}
	for _, expected := range []string{"login --authfile", "--password-stdin", "build --authfile", "--format docker", "push --authfile", "--digestfile", "--tls-verify=true"} {
		if !strings.Contains(string(log), expected) {
			t.Fatalf("missing Podman argument %q", expected)
		}
	}
	c.BuildEngine = "unknown"
	if c.Validate() == nil {
		t.Fatal("unknown build engine accepted")
	}
	settings := profileSettings()
	settings.BuildEngine = "podman"
	resolved, err := settings.Resolve("test", ".")
	if err != nil || resolved.BuildEngine != "podman" {
		t.Fatalf("engine not resolved: %v", err)
	}
}

func TestSplitPipelineDeployment(t *testing.T) {
	c := config()
	c.BuildEngine = "podman"
	c.GitImage = "registry.example.com/tools/git:1"
	var ci map[string]any
	if err := yaml.Unmarshal([]byte(pipeline(c)), &ci); err != nil {
		t.Fatal(err)
	}
	build := ci["publish"].(map[string]any)
	update := ci["update-deployment"].(map[string]any)
	buildScript := build["script"].([]any)[0].(string)
	updateScript := update["script"].([]any)[0].(string)
	if strings.Contains(buildScript, "git ") || strings.Contains(buildScript, "git;") {
		t.Fatal("builder requires git")
	}
	if update["image"].(map[string]any)["name"] != c.GitImage {
		t.Fatal("wrong deployment image")
	}
	if build["artifacts"] == nil || update["needs"] == nil || update["resource_group"] != "justcd-publish" {
		t.Fatal("missing artifact dependency or serialization")
	}
	if update["secrets"] != nil || update["id_tokens"] != nil {
		t.Fatal("deployment job requests registry credentials")
	}
	dir := t.TempDir()
	for _, name := range []string{".justcd-build", ".justcd", "deploy"} {
		if err := os.Mkdir(dir+"/"+name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	digest := "sha256:" + strings.Repeat("b", 64)
	os.WriteFile(dir+"/.justcd/deployment.yaml", []byte("image: JUSTCD_IMAGE_DIGEST\n"), 0600)
	fakeGit := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$TEST_LOG"
if [ "$1" = rev-parse ]; then printf '%s' "$REMOTE_SHA"; fi
`
	if err := os.WriteFile(dir+"/git", []byte(fakeGit), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		digest, sha string
		valid       bool
	}{{digest, "abc123", true}, {"bad", "abc123", false}, {digest, "newer", false}} {
		os.WriteFile(dir+"/.justcd-build/image-digest", []byte(tc.digest), 0600)
		os.WriteFile(dir+"/commands", nil, 0600)
		cmd := exec.Command("sh", "-c", updateScript)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "TEST_LOG="+dir+"/commands", "REMOTE_SHA="+tc.sha, "CI_COMMIT_SHA=abc123", "CI_DEFAULT_BRANCH=main", "CI_REPOSITORY_URL=https://git.example.com/apps/test.git", "JUSTCD_IMAGE="+c.RegistryImage)
		out, err := cmd.CombinedOutput()
		if tc.valid && err != nil {
			t.Fatalf("deployment failed: %s %v", out, err)
		}
		log, _ := os.ReadFile(dir + "/commands")
		if !tc.valid && (err == nil || strings.Contains(string(log), "push")) {
			t.Fatal("invalid or stale build published")
		}
		if tc.valid {
			manifest, _ := os.ReadFile(dir + "/deploy/application.yaml")
			if !strings.Contains(string(manifest), c.RegistryImage+"@"+digest) || !strings.Contains(string(log), "push https://git.example.com/apps/test.git HEAD:main") {
				t.Fatal("digest not committed/pushed")
			}
		}
	}
	c.GitImage = "invalid\nimage"
	if c.Validate() == nil {
		t.Fatal("invalid Git image accepted")
	}
}
