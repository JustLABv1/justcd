package publishing

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const testSources = `[
 {"ecosystem":"pip","url":"https://art.example.com/artifactory/api/pypi/pypi-remote/simple","authenticated":true},
 {"ecosystem":"cran","url":"https://art.example.com/artifactory/api/cran/cran-remote","authenticated":true},
 {"ecosystem":"npm","url":"https://art.example.com/artifactory/api/npm/npm-remote","authenticated":true},
 {"ecosystem":"conda","url":"https://art.example.com/artifactory/api/conda/conda-remote","authenticated":true},
 {"ecosystem":"apt","url":"https://art.example.com/artifactory/ubuntu-remote","authenticated":true},
 {"ecosystem":"apk","url":"https://art.example.com/artifactory/alpine-remote","authenticated":true}
]`

func sourcesConfig(t *testing.T, runtimeID, sources string) Config {
	t.Helper()
	tpl, ok := BuiltinLookup(runtimeID)
	if !ok {
		t.Fatal("unknown template", runtimeID)
	}
	c := SampleConfigFor(tpl)
	c.PackageSources = sources
	return c
}

func TestValidatePackageSources(t *testing.T) {
	if err := ValidatePackageSources(testSources, PackagePolicyEnforce); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePackageSources("", ""); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct{ sources, policy string }{
		"not json":         {"nope", ""},
		"unknown":          {`[{"ecosystem":"gem","url":"https://x.example.com"}]`, ""},
		"duplicate":        {`[{"ecosystem":"pip","url":"https://a.example.com"},{"ecosystem":"pip","url":"https://b.example.com"}]`, ""},
		"http":             {`[{"ecosystem":"pip","url":"http://a.example.com/simple"}]`, ""},
		"credentials":      {`[{"ecosystem":"pip","url":"https://user:pw@a.example.com/simple"}]`, ""},
		"query":            {`[{"ecosystem":"pip","url":"https://a.example.com/simple?token=1"}]`, ""},
		"quote injection":  {`[{"ecosystem":"pip","url":"https://a.example.com/x'; rm -rf /; '"}]`, ""},
		"dollar":           {`[{"ecosystem":"pip","url":"https://a.example.com/$(id)"}]`, ""},
		"policy no source": {``, "enforce"},
		"bad policy":       {`[{"ecosystem":"pip","url":"https://a.example.com/simple"}]`, "maybe"},
	} {
		if ValidatePackageSources(tc.sources, tc.policy) == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// The generated script must be valid POSIX sh and, when sourced the way the
// build does, route every ecosystem through the mirror with encoded credentials.
func TestPackageScriptConfiguresEveryEcosystem(t *testing.T) {
	c := sourcesConfig(t, "streamlit", testSources)
	script := packageScript(c)
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "pkg.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-n", scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("script is not valid sh: %v\n%s", err, out)
	}
	cfgDir := filepath.Join(dir, "cfg")
	secret := "a@b.com"
	token := "to ken'x/9"
	run := exec.Command("sh", "-c", `JUSTCD_PKG_USER=`+shellQuote(secret)+`; JUSTCD_PKG_TOKEN=`+shellQuote(token)+`; . "$0" && env`, scriptPath)
	run.Env = append(os.Environ(), "JUSTCD_PKG_DIR="+cfgDir)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("sourcing failed: %v\n%s", err, out)
	}
	env := string(out)
	encUser, encToken := "a%40b.com", "to%20ken'x%2F9"
	for _, want := range []string{
		"PIP_INDEX_URL=https://" + encUser + ":" + encToken + "@art.example.com/artifactory/api/pypi/pypi-remote/simple",
		"R_REPOSITORY=https://" + encUser + ":" + encToken + "@art.example.com/artifactory/api/cran/cran-remote",
		"RENV_CONFIG_REPOS_OVERRIDE=CRAN=https://",
		"NPM_CONFIG_USERCONFIG=" + cfgDir + "/npmrc",
		"CONDARC=" + cfgDir + "/condarc",
		"APT_CONFIG=" + cfgDir + "/apt.conf",
		"NETRC=" + cfgDir + "/netrc",
		"R_PROFILE_USER=" + cfgDir + "/Rprofile",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("environment is missing %q", want)
		}
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(cfgDir, name))
		if err != nil {
			t.Fatalf("%s was not written: %v", name, err)
		}
		return string(b)
	}
	if npmrc := read("npmrc"); !strings.Contains(npmrc, "registry=https://art.example.com/artifactory/api/npm/npm-remote/") || !strings.Contains(npmrc, "//art.example.com/artifactory/api/npm/npm-remote/:_auth=") || !strings.Contains(npmrc, "always-auth=true") {
		t.Errorf("npmrc: %s", npmrc)
	}
	if condarc := read("condarc"); !strings.Contains(condarc, "default_channels:") || !strings.Contains(condarc, "@art.example.com/artifactory/api/conda/conda-remote") {
		t.Errorf("condarc: %s", condarc)
	}
	if auth := read("auth.d/justcd.conf"); !strings.Contains(auth, "machine art.example.com login a@b.com password to ken'x/9") {
		t.Errorf("apt auth: %s", auth)
	}
	if !strings.Contains(read("netrc"), "machine art.example.com login a@b.com password to ken'x/9") {
		t.Error("netrc")
	}
	if strings.Contains(script, token) || strings.Contains(script, "a@b.com") {
		t.Error("the committed script must not contain credentials")
	}
	if info, _ := os.Stat(cfgDir); info == nil || info.Mode().Perm() != 0o700 {
		t.Errorf("settings directory must be private: %v", info)
	}
}

func TestPackageScriptWithoutAuthHasNoCredentialUse(t *testing.T) {
	c := sourcesConfig(t, "streamlit", `[{"ecosystem":"pip","url":"https://pypi.example.com/simple"}]`)
	script := packageScript(c)
	if strings.Contains(script, "JUSTCD_PKG_TOKEN:-") || strings.Contains(script, "__jd_auth_url '") {
		t.Fatal("an anonymous mirror must not require credentials")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "pkg.sh")
	_ = os.WriteFile(path, []byte(script), 0o600)
	run := exec.Command("sh", "-c", `. "$0" && printf %s "$PIP_INDEX_URL"`, path)
	run.Env = append(os.Environ(), "JUSTCD_PKG_DIR="+filepath.Join(dir, "c"))
	out, err := run.CombinedOutput()
	if err != nil || string(out) != "https://pypi.example.com/simple" {
		t.Fatalf("%v %q", err, out)
	}
}

func TestPipelineAndDockerfileUseThePackageSecret(t *testing.T) {
	c := sourcesConfig(t, "streamlit", `[{"ecosystem":"pip","url":"https://art.example.com/api/pypi/p/simple","authenticated":true}]`)
	c.CAVariable = "PLATFORM_CA"
	for _, engine := range []string{"buildkit", "podman"} {
		c.BuildEngine = engine
		out, err := RenderConfig(c, SourceFacts{})
		if err != nil {
			t.Fatal(engine, err)
		}
		if !strings.Contains(out[FileDockerfile], "RUN --mount=type=secret,id=justcd_pkg --mount=type=secret,id=justcd_ca . /run/secrets/justcd_pkg && python -m pip install") {
			t.Errorf("%s: Dockerfile does not mount the package secret:\n%s", engine, out[FileDockerfile])
		}
		ci := out[FilePipeline]
		for _, want := range []string{`--secret id=justcd_pkg,src="$pkg_file"`, `--secret id=justcd_ca,src="$ca_bundle"`, "cat .justcd/package-sources.sh", "JUSTCD_PKG_TOKEN"} {
			if !strings.Contains(ci, want) {
				t.Errorf("%s: pipeline is missing %q", engine, want)
			}
		}
		if out[FilePackageSources] == "" {
			t.Errorf("%s: the package script is not generated", engine)
		}
	}
	// Without sources nothing changes.
	plain, err := RenderConfig(sourcesConfig(t, "streamlit", ""), SourceFacts{})
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range plain {
		if strings.Contains(content, "justcd_pkg") || strings.Contains(content, "PackageMount") {
			t.Errorf("%s mentions package sources without any configured", path)
		}
	}
	if _, ok := plain[FilePackageSources]; ok {
		t.Error("script generated without sources")
	}
}

func TestPackageCredentialsFromVault(t *testing.T) {
	c := sourcesConfig(t, "streamlit", `[{"ecosystem":"pip","url":"https://art.example.com/api/pypi/p/simple","authenticated":true}]`)
	c.SecretMode, c.VaultAddress, c.VaultRole, c.VaultPath = "vault", "https://vault.example.com", "publisher", "secret/data/team/registry"
	out, err := RenderConfig(c, SourceFacts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"JUSTCD_PKG_USER:", "field: package_username", "JUSTCD_PKG_TOKEN:", "field: package_token"} {
		if !strings.Contains(out[FilePipeline], want) {
			t.Errorf("vault pipeline is missing %q", want)
		}
	}
	c.PackageSources = `[{"ecosystem":"pip","url":"https://art.example.com/api/pypi/p/simple"}]`
	out, _ = RenderConfig(c, SourceFacts{})
	if strings.Contains(out[FilePipeline], "package_token") {
		t.Error("anonymous mirrors must not read package credentials from Vault")
	}
}

// The shell quoting the pipeline uses for credentials must round-trip any value.
func TestPipelineShellQuotingRoundTrips(t *testing.T) {
	c := sourcesConfig(t, "streamlit", `[{"ecosystem":"pip","url":"https://art.example.com/api/pypi/p/simple","authenticated":true}]`)
	out, err := RenderConfig(c, SourceFacts{})
	if err != nil {
		t.Fatal(err)
	}
	fn := regexp.MustCompile(`(?m)^\s*(shq\(\).*)$`).FindStringSubmatch(out[FilePipeline])
	if fn == nil {
		t.Fatal("shq helper not found")
	}
	for _, value := range []string{"plain", "it's", `a"b$c\d`, "sp ace;rm -rf /", "x'; echo pwned; '"} {
		script := fn[1] + "\nJUSTCD_PKG_TOKEN=$(shq " + shellQuote(value) + ")\neval \"T=$JUSTCD_PKG_TOKEN\"\nprintf %s \"$T\""
		got, err := exec.Command("sh", "-c", script).CombinedOutput()
		if err != nil || string(got) != value {
			t.Errorf("value %q round-tripped to %q (%v)", value, got, err)
		}
	}
}

func uploaded(files map[string]string) []File {
	out := []File{}
	for name, content := range files {
		out = append(out, File{Path: "source/" + name, Content: content, Encoding: "text"})
	}
	return out
}

func TestEnforcedPackagePolicyRejectsForeignSources(t *testing.T) {
	c := sourcesConfig(t, "streamlit", `[{"ecosystem":"pip","url":"https://art.example.com/api/pypi/p/simple"},{"ecosystem":"npm","url":"https://art.example.com/api/npm/n"},{"ecosystem":"cran","url":"https://art.example.com/api/cran/c"}]`)
	c.PackagePolicy = PackagePolicyEnforce
	good := map[string]string{
		"requirements.txt":  "# deps\nstreamlit==1.40\npandas>=2 # data\nrequests @ https://art.example.com/files/requests.whl\n-r other.txt\n",
		"package.json":      `{"repository":"github.com/x/y","dependencies":{"left-pad":"^1.3.0"}}`,
		"package-lock.json": `{"packages":{"node_modules/a":{"resolved":"https://registry.npmjs.org/a/-/a-1.tgz"},"node_modules/b":{"resolved":"https://art.example.com/api/npm/n/b/-/b-1.tgz"}}}`,
		"renv.lock":         `{"Packages":{"dplyr":{"Source":"Repository","Repository":"CRAN"}}}`,
		"pyproject.toml":    "[project]\nname='x'\n[project.urls]\nrepository='https://github.com/x/y'\n",
		".npmrc":            "fund=false\n",
	}
	if err := CheckPackageSources(c, uploaded(good)); err != nil {
		t.Fatalf("a compliant project was rejected: %v", err)
	}
	bad := map[string]map[string]string{
		"index flag":      {"requirements.txt": "--extra-index-url https://pypi.org/simple\nstreamlit\n"},
		"short flag":      {"requirements.txt": "-i https://pypi.org/simple\n"},
		"foreign url":     {"requirements.txt": "pkg @ https://files.example.net/pkg.whl\n"},
		"git dependency":  {"requirements.txt": "pkg @ git+https://github.com/x/pkg.git\n"},
		"pip.conf":        {"pip.conf": "[global]\nindex-url = https://pypi.org/simple\n"},
		"npmrc registry":  {".npmrc": "registry=https://registry.npmjs.org/\n"},
		"scoped registry": {".npmrc": "@acme:registry=https://example.net/\n"},
		"condarc":         {".condarc": "channels:\n  - conda-forge\n"},
		"Rprofile":        {".Rprofile": `options(repos = c(CRAN = "https://cran.r-project.org"))`},
		"renv github":     {"renv.lock": `{"Packages":{"mypkg":{"Source":"GitHub"}}}`},
		"npm git":         {"package.json": `{"dependencies":{"a":"github:x/a","b":"x/b","c":"git+https://example.net/c.git"}}`},
		"npm url":         {"package.json": `{"dependencies":{"a":"https://example.net/a.tgz"}}`},
		"lock host":       {"package-lock.json": `{"packages":{"node_modules/a":{"resolved":"https://evil.example.net/a.tgz"}}}`},
		"conda env":       {"environment.yml": "channels:\n  - https://conda.example.net/channel\n"},
		"uv index":        {"pyproject.toml": "[[tool.uv.index]]\nurl='https://pypi.org/simple'\n"},
	}
	for name, files := range bad {
		if err := CheckPackageSources(c, uploaded(files)); err == nil {
			t.Errorf("%s: expected the upload to be rejected", name)
		}
	}
	// Without the policy nothing is checked.
	c.PackagePolicy = ""
	if err := CheckPackageSources(c, uploaded(bad["index flag"])); err != nil {
		t.Fatal("the default policy must not restrict uploads:", err)
	}
}

func TestGenerateAddsPackageScriptAndEnforcesPolicy(t *testing.T) {
	c := sourcesConfig(t, "streamlit", `[{"ecosystem":"pip","url":"https://art.example.com/api/pypi/p/simple","authenticated":true}]`)
	c.PackagePolicy = PackagePolicyEnforce
	c.TemplateFiles = nil
	source := uploaded(map[string]string{"app.py": "import streamlit", "requirements.txt": "streamlit\n"})
	files, err := Generate(c, source)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f.Path == FilePackageSources && strings.Contains(f.Content, "PIP_INDEX_URL") {
			found = true
		}
	}
	if !found {
		t.Fatal("Generate did not include the package script")
	}
	bad := uploaded(map[string]string{"app.py": "import streamlit", "requirements.txt": "--index-url https://pypi.org/simple\n"})
	if _, err := Generate(c, bad); err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("Generate must enforce the policy, got %v", err)
	}
}

// Without awk the script still produces a valid, fully encoded URL.
func TestPackageScriptEncodingFallsBackWithoutAwk(t *testing.T) {
	c := sourcesConfig(t, "streamlit", `[{"ecosystem":"pip","url":"https://art.example.com/simple","authenticated":true}]`)
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"od", "tr", "sed", "mkdir", "chmod", "cat", "base64"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Skip(tool + " is unavailable")
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	scriptPath := filepath.Join(dir, "pkg.sh")
	_ = os.WriteFile(scriptPath, []byte(packageScript(c)), 0o600)
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is unavailable")
	}
	run := exec.Command(sh, "-c", `JUSTCD_PKG_USER=a@b; JUSTCD_PKG_TOKEN=tok; . "$0" && printf %s "$PIP_INDEX_URL"`, scriptPath)
	run.Env = []string{"PATH=" + bin, "JUSTCD_PKG_DIR=" + filepath.Join(dir, "cfg")}
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if want := "https://%61%40%62:%74%6f%6b@art.example.com/simple"; string(out) != want {
		t.Fatalf("fallback URL = %q, want %q", out, want)
	}
}

// A token made of ordinary characters must appear literally, so GitLab can mask it.
func TestPackageTokenStaysLiteralForLogMasking(t *testing.T) {
	c := sourcesConfig(t, "streamlit", `[{"ecosystem":"cran","url":"https://art.example.com/cran","authenticated":true}]`)
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "pkg.sh")
	_ = os.WriteFile(scriptPath, []byte(packageScript(c)), 0o600)
	// A made-up token of the usual base64 shape; never a real credential.
	token := "ZmFrZS10ZXN0LXRva2VuLW5vdC1hLXNlY3JldC0xMjM0NTY3OA"
	run := exec.Command("sh", "-c", `JUSTCD_PKG_USER=analyst@example.com; JUSTCD_PKG_TOKEN=`+token+`; . "$0" && printf %s "$R_REPOSITORY"`, scriptPath)
	run.Env = append(os.Environ(), "JUSTCD_PKG_DIR="+filepath.Join(dir, "cfg"))
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if want := "https://analyst%40example.com:" + token + "@art.example.com/cran"; string(out) != want {
		t.Fatalf("R_REPOSITORY = %q, want the token literally in the URL:\n%q", out, want)
	}
}

func TestShinyRenvRestoreModes(t *testing.T) {
	render := func(parameters string) string {
		c := sourcesConfig(t, "shiny", "")
		c.TemplateParameters = parameters
		out, err := RenderConfig(c, SourceFacts{HasRenvLock: true})
		if err != nil {
			t.Fatal(err)
		}
		return out[FileDockerfile]
	}
	full := render("")
	if !strings.Contains(full, "renv::restore(prompt=FALSE)") || strings.Contains(full, "renvRestore") {
		t.Fatalf("the default must restore the whole lockfile:\n%s", full)
	}
	missing := render("renvRestore=missing")
	if !strings.Contains(missing, "renv::restore(packages=absent, prompt=FALSE)") || strings.Contains(missing, "renv::restore(prompt=FALSE)") {
		t.Fatalf("renvRestore=missing must install only absent packages:\n%s", missing)
	}
	if err := ValidateTemplate("shiny", mustBuiltin(t, "shiny")); err != nil {
		t.Fatal(err)
	}
}

func mustBuiltin(t *testing.T, id string) map[string]string {
	t.Helper()
	files, err := BuiltinFilesForTemplate(id)
	if err != nil {
		t.Fatal(err)
	}
	return files
}
