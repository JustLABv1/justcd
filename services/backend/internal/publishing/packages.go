package publishing

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Package sources let a platform route the package managers used inside image
// builds through its own mirror, for example JFrog Artifactory remote
// repositories, optionally with authentication and a custom CA.
//
// JustCD generates .justcd/package-sources.sh. The pipeline joins it with the
// credentials into one build secret (justcd_pkg) that only the install steps
// of the Dockerfile mount, so tokens never reach an image layer, the source or
// the repository. The script keeps its configuration in tmpfs and in the
// environment of that single step.

const (
	// FilePackageSources is generated next to the other managed build files.
	FilePackageSources = ".justcd/package-sources.sh"

	// PackagePolicyEnforce rejects uploads that reach for other sources.
	PackagePolicyEnforce = "enforce"

	maxPackageSources = 8
)

// PackageSource routes one ecosystem through a mirror.
type PackageSource struct {
	// Ecosystem is pip, cran, npm, conda, apt or apk.
	Ecosystem string `json:"ecosystem"`
	// URL is the https location of the mirror (the index or repository root).
	URL string `json:"url"`
	// Authenticated sources sign in with the profile's package credentials.
	Authenticated bool `json:"authenticated,omitempty"`
}

var packageEcosystems = map[string]string{
	"pip":   "Python (pip)",
	"cran":  "R (CRAN, renv)",
	"npm":   "Node.js (npm)",
	"conda": "Conda",
	"apt":   "Debian and Ubuntu (apt)",
	"apk":   "Alpine (apk)",
}

// PackageEcosystems lists the supported ecosystems with display names.
func PackageEcosystems() map[string]string {
	out := make(map[string]string, len(packageEcosystems))
	for k, v := range packageEcosystems {
		out[k] = v
	}
	return out
}

// ParsePackageSources decodes the JSON list stored in profile settings.
func ParsePackageSources(raw string) ([]PackageSource, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var sources []PackageSource
	if err := json.Unmarshal([]byte(raw), &sources); err != nil {
		return nil, errors.New("package sources must be a JSON list")
	}
	return sources, nil
}

// ValidatePackageSources checks the list, the policy and URL safety.
func ValidatePackageSources(raw, policy string) error {
	if policy != "" && policy != PackagePolicyEnforce {
		return errors.New("package policy must be empty or enforce")
	}
	sources, err := ParsePackageSources(raw)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		if policy != "" {
			return errors.New("a package policy needs at least one package source")
		}
		return nil
	}
	if len(sources) > maxPackageSources {
		return fmt.Errorf("at most %d package sources are allowed", maxPackageSources)
	}
	seen := map[string]bool{}
	for _, s := range sources {
		label, ok := packageEcosystems[s.Ecosystem]
		if !ok {
			return fmt.Errorf("unsupported package ecosystem %q", s.Ecosystem)
		}
		if seen[s.Ecosystem] {
			return fmt.Errorf("%s has more than one package source", label)
		}
		seen[s.Ecosystem] = true
		u, err := url.Parse(s.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(s.URL) > 512 || strings.ContainsAny(s.URL, " \t\r\n'\"\\`$") {
			return fmt.Errorf("%s: use an https URL without credentials, query or spaces", label)
		}
	}
	return nil
}

func (c Config) packageSources() []PackageSource {
	sources, _ := ParsePackageSources(c.PackageSources)
	return sources
}

// PackagesAuthenticated reports whether any source signs in.
func (c Config) PackagesAuthenticated() bool {
	for _, s := range c.packageSources() {
		if s.Authenticated {
			return true
		}
	}
	return false
}

func (c Config) packageHosts() map[string]bool {
	hosts := map[string]bool{}
	for _, s := range c.packageSources() {
		if u, err := url.Parse(s.URL); err == nil {
			hosts[strings.ToLower(u.Host)] = true
		}
	}
	return hosts
}

// PackageMount is the RUN flag prefix that makes the sources available to one
// install step, for example `RUN {{ .PackageMount }}pip install ...`. It is
// empty when no sources are configured, so templates render as before.
func packageMount(c Config) string {
	if len(c.packageSources()) == 0 {
		return ""
	}
	mount := "--mount=type=secret,id=justcd_pkg "
	if c.CAVariable != "" {
		mount += "--mount=type=secret,id=justcd_ca "
	}
	return mount + ". /run/secrets/justcd_pkg && "
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// packageScript renders the static, secret-free part of the build secret.
func packageScript(c Config) string {
	sources := c.packageSources()
	if len(sources) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`# JustCD package sources (generated; changes are overwritten by profile updates).
# Sourced inside one build step that mounts the justcd_pkg secret. Settings live
# in tmpfs and in this step's environment only; nothing is written to the image.
__jd_dir="${JUSTCD_PKG_DIR:-/dev/shm/justcd-pkg}"
mkdir -p "$__jd_dir" && chmod 700 "$__jd_dir" || { printf 'JustCD: cannot create %s for package settings.\n' "$__jd_dir" >&2; return 1; }
# Percent-encode what a URL cannot carry, and keep letters, digits and common
# punctuation readable: GitLab masks a token in job logs only when it appears
# literally, and R prints the download URL.
__jd_enc() {
  if command -v awk >/dev/null 2>&1; then
    printf '%s' "$1" | od -An -v -tu1 | awk '{ for (i = 1; i <= NF; i++) { c = $i; if ((c >= 48 && c <= 57) || (c >= 65 && c <= 90) || (c >= 97 && c <= 122) || c == 45 || c == 46 || c == 95 || c == 126 || c == 33 || c == 36 || c == 38 || c == 39 || c == 40 || c == 41 || c == 42 || c == 43 || c == 44 || c == 59 || c == 61) printf "%c", c; else printf "%%%02X", c } }'
  else
    printf '%s' "$1" | od -An -v -tx1 | tr -d ' \n' | sed 's/../%&/g'
  fi
}
__jd_auth_url() { printf 'https://%s:%s@%s' "$(__jd_enc "$JUSTCD_PKG_USER")" "$(__jd_enc "$JUSTCD_PKG_TOKEN")" "${1#https://}"; }
__jd_ca=""
if [ -r /run/secrets/justcd_ca ]; then
  __jd_ca=/run/secrets/justcd_ca
  export SSL_CERT_FILE="$__jd_ca" CURL_CA_BUNDLE="$__jd_ca" REQUESTS_CA_BUNDLE="$__jd_ca" PIP_CERT="$__jd_ca" NODE_EXTRA_CA_CERTS="$__jd_ca" GIT_SSL_CAINFO="$__jd_ca"
fi
`)
	if c.PackagesAuthenticated() {
		b.WriteString(`if [ -z "${JUSTCD_PKG_USER:-}" ] || [ -z "${JUSTCD_PKG_TOKEN:-}" ]; then
  printf 'JustCD: the profile needs package credentials but none were provided to this build.\n' >&2
  return 1
fi
`)
	}
	sorted := append([]PackageSource(nil), sources...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Ecosystem < sorted[j].Ecosystem })
	for _, s := range sorted {
		quoted := shellQuote(s.URL)
		// The URL a tool uses: with credentials for authenticated sources.
		target := quoted
		if s.Authenticated {
			target = `"$(__jd_auth_url ` + quoted + `)"`
		}
		host := ""
		if u, err := url.Parse(s.URL); err == nil {
			host = u.Host
		}
		fmt.Fprintf(&b, "# %s\n", packageEcosystems[s.Ecosystem])
		switch s.Ecosystem {
		case "pip":
			fmt.Fprintf(&b, "export PIP_INDEX_URL=%s PIP_DISABLE_PIP_VERSION_CHECK=1\nunset PIP_EXTRA_INDEX_URL PIP_FIND_LINKS\n", target)
		case "cran":
			fmt.Fprintf(&b, "export R_REPOSITORY=%s\nexport RENV_CONFIG_REPOS_OVERRIDE=\"CRAN=$R_REPOSITORY\"\nprintf 'options(repos=c(CRAN=\"%%s\"), download.file.method=\"libcurl\")\\n' \"$R_REPOSITORY\" > \"$__jd_dir/Rprofile\"\nexport R_PROFILE_USER=\"$__jd_dir/Rprofile\"\n", target)
		case "npm":
			registry := strings.TrimSuffix(s.URL, "/") + "/"
			fmt.Fprintf(&b, "printf 'registry=%%s\\n' %s > \"$__jd_dir/npmrc\"\n", shellQuote(registry))
			if s.Authenticated {
				fmt.Fprintf(&b, "printf '//%%s:_auth=%%s\\nalways-auth=true\\n' %s \"$(printf '%%s:%%s' \"$JUSTCD_PKG_USER\" \"$JUSTCD_PKG_TOKEN\" | base64 | tr -d '\\n')\" >> \"$__jd_dir/npmrc\"\n", shellQuote(strings.TrimPrefix(registry, "https://")))
			}
			b.WriteString("export NPM_CONFIG_USERCONFIG=\"$__jd_dir/npmrc\"\n")
		case "conda":
			fmt.Fprintf(&b, "printf 'channel_alias: %%s\\ndefault_channels:\\n  - %%s\\nchannel_priority: strict\\n' %s %s > \"$__jd_dir/condarc\"\n", target, target)
			b.WriteString("if [ -n \"$__jd_ca\" ]; then printf 'ssl_verify: %s\\n' \"$__jd_ca\" >> \"$__jd_dir/condarc\"; fi\nexport CONDARC=\"$__jd_dir/condarc\"\n")
		case "apt":
			b.WriteString("mkdir -p \"$__jd_dir/auth.d\"\n")
			if s.Authenticated {
				fmt.Fprintf(&b, "printf 'machine %%s login %%s password %%s\\n' %s \"$JUSTCD_PKG_USER\" \"$JUSTCD_PKG_TOKEN\" > \"$__jd_dir/auth.d/justcd.conf\"\n", shellQuote(host))
			}
			b.WriteString("printf 'Dir::Etc::netrcparts \"%s\";\\n' \"$__jd_dir/auth.d\" > \"$__jd_dir/apt.conf\"\nif [ -n \"$__jd_ca\" ]; then printf 'Acquire::https::CaInfo \"%s\";\\n' \"$__jd_ca\" >> \"$__jd_dir/apt.conf\"; fi\nexport APT_CONFIG=\"$__jd_dir/apt.conf\"\n")
		case "apk":
			if s.Authenticated {
				fmt.Fprintf(&b, "printf 'machine %%s login %%s password %%s\\n' %s \"$JUSTCD_PKG_USER\" \"$JUSTCD_PKG_TOKEN\" >> \"$__jd_dir/netrc\"\nchmod 600 \"$__jd_dir/netrc\"\nexport NETRC=\"$__jd_dir/netrc\"\n", shellQuote(host))
			}
		}
	}
	b.WriteString("unset -f __jd_enc __jd_auth_url 2>/dev/null || true\n")
	return b.String()
}

// ---- Upload guardrail -------------------------------------------------------

const maxPackageFile = 2 << 20

var (
	requirementURL   = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*)://([^\s/;#]+)`)
	requirementFlags = regexp.MustCompile(`(?i)^(-i\b|--index-url|--extra-index-url|-f\b|--find-links|--trusted-host)`)
	githubShorthand  = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(#.*)?$`)
)

// configOverrides lists, per file at the project root, what would redirect a
// package manager to another source. Matching is on lower-cased content.
var configOverrides = map[string][]*regexp.Regexp{
	"pip.conf":       {regexp.MustCompile(`index-url|find-links`)},
	"pip.ini":        {regexp.MustCompile(`index-url|find-links`)},
	".npmrc":         {regexp.MustCompile(`(^|\n)\s*(@[^:\s]+:)?registry\s*=`)},
	".condarc":       {regexp.MustCompile(`channel_alias|channels:`)},
	".rprofile":      {regexp.MustCompile(`repos\s*=|r_repository|renv_config_repos`)},
	".renviron":      {regexp.MustCompile(`repos\s*=|r_repository|renv_config_repos`)},
	"rprofile.site":  {regexp.MustCompile(`repos\s*=|r_repository|renv_config_repos`)},
	"pyproject.toml": {regexp.MustCompile(`index-url|find-links|\[\[tool\.uv\.index\]\]|\[tool\.poetry\.source\]|\[\[tool\.pdm\.source\]\]`)},
	"pipfile":        {regexp.MustCompile(`\[\[source\]\]`)},
}

func sourceText(f File) (string, error) {
	if f.DiskPath != "" {
		in, err := os.Open(f.DiskPath)
		if err != nil {
			return "", err
		}
		defer in.Close()
		data, err := io.ReadAll(io.LimitReader(in, maxPackageFile+1))
		if err != nil {
			return "", err
		}
		if len(data) > maxPackageFile {
			return "", errors.New("is too large to check")
		}
		return string(data), nil
	}
	if f.Encoding == "base64" {
		return "", nil
	}
	if len(f.Content) > maxPackageFile {
		return "", errors.New("is too large to check")
	}
	return f.Content, nil
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// CheckPackageSources enforces a profile's "only these sources" policy: files
// that name another package index, a Git dependency or a foreign host are
// rejected with the file and reason, so users can fix their project.
func CheckPackageSources(c Config, files []File) error {
	if c.PackagePolicy != PackagePolicyEnforce {
		return nil
	}
	allowed := c.packageHosts()
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	hostOK := func(h string) bool { return allowed[strings.ToLower(h)] }
	for _, f := range files {
		name, ok := strings.CutPrefix(f.Path, "source/")
		if !ok {
			continue
		}
		base := name[strings.LastIndex(name, "/")+1:]
		switch {
		case !strings.Contains(name, "/") && configOverrides[strings.ToLower(base)] != nil:
			text, err := sourceText(f)
			if err != nil {
				add("%s %v", name, err)
				continue
			}
			lower := strings.ToLower(text)
			for _, pattern := range configOverrides[strings.ToLower(base)] {
				if pattern.MatchString(lower) {
					add("%s changes where packages come from. Remove that setting; the publishing profile configures package sources.", name)
					break
				}
			}
		case strings.HasSuffix(name, ".txt") && (strings.HasPrefix(base, "requirements") || strings.HasPrefix(name, "requirements/")):
			text, err := sourceText(f)
			if err != nil {
				add("%s %v", name, err)
				continue
			}
			for i, line := range strings.Split(text, "\n") {
				line = strings.TrimSpace(line)
				if cut := strings.Index(line, " #"); cut >= 0 {
					line = strings.TrimSpace(line[:cut])
				}
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				if requirementFlags.MatchString(line) {
					add("%s line %d sets another package index.", name, i+1)
					continue
				}
				for _, m := range requirementURL.FindAllStringSubmatch(line, -1) {
					scheme := strings.ToLower(m[1])
					if (scheme != "https" && scheme != "http") || !hostOK(m[2]) {
						add("%s line %d installs from %s://%s, which is not an approved package source.", name, i+1, scheme, m[2])
					}
				}
			}
		case !strings.Contains(name, "/") && base == "renv.lock":
			text, err := sourceText(f)
			if err != nil {
				add("%s %v", name, err)
				continue
			}
			var lock struct {
				Packages map[string]struct{ Source string } `json:"Packages"`
			}
			if json.Unmarshal([]byte(text), &lock) == nil {
				var bad []string
				for pkg, p := range lock.Packages {
					if p.Source != "" && p.Source != "Repository" {
						bad = append(bad, pkg+" ("+p.Source+")")
					}
				}
				sort.Strings(bad)
				if len(bad) > 0 {
					if len(bad) > 3 {
						bad = append(bad[:3], "…")
					}
					add("renv.lock installs packages that do not come from a repository: %s. Use packages from the approved CRAN source.", strings.Join(bad, ", "))
				}
			}
		case !strings.Contains(name, "/") && base == "package.json":
			text, err := sourceText(f)
			if err != nil {
				add("%s %v", name, err)
				continue
			}
			var pkg map[string]json.RawMessage
			if json.Unmarshal([]byte(text), &pkg) != nil {
				continue
			}
			for _, section := range []string{"dependencies", "devDependencies", "optionalDependencies"} {
				var deps map[string]string
				if json.Unmarshal(pkg[section], &deps) != nil {
					continue
				}
				names := make([]string, 0, len(deps))
				for n := range deps {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					spec := deps[n]
					lower := strings.ToLower(spec)
					switch {
					case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
						if !hostOK(hostOf(spec)) {
							add("package.json: %s is installed from %s, which is not an approved package source.", n, spec)
						}
					case strings.HasPrefix(lower, "git") || strings.HasPrefix(lower, "github:") || strings.HasPrefix(lower, "gitlab:") || strings.HasPrefix(lower, "bitbucket:") || strings.HasPrefix(lower, "ssh:") || githubShorthand.MatchString(spec):
						add("package.json: %s is installed from Git (%s). Use a published package version.", n, spec)
					}
				}
			}
		case !strings.Contains(name, "/") && base == "package-lock.json":
			text, err := sourceText(f)
			if err != nil {
				add("%s %v", name, err)
				continue
			}
			var lock struct {
				Packages     map[string]struct{ Resolved string } `json:"packages"`
				Dependencies map[string]struct{ Resolved string } `json:"dependencies"`
			}
			if json.Unmarshal([]byte(text), &lock) != nil {
				continue
			}
			bad := map[string]bool{}
			check := func(resolved string) {
				if resolved == "" {
					return
				}
				h := hostOf(resolved)
				// npm swaps registry.npmjs.org for the configured registry.
				if h == "" || hostOK(h) || h == "registry.npmjs.org" {
					return
				}
				bad[h] = true
			}
			for _, p := range lock.Packages {
				check(p.Resolved)
			}
			for _, p := range lock.Dependencies {
				check(p.Resolved)
			}
			hosts := make([]string, 0, len(bad))
			for h := range bad {
				hosts = append(hosts, h)
			}
			sort.Strings(hosts)
			if len(hosts) > 0 {
				add("package-lock.json resolves packages from %s, which is not an approved package source.", strings.Join(hosts, ", "))
			}
		case !strings.Contains(name, "/") && (base == "environment.yml" || base == "environment.yaml"):
			text, err := sourceText(f)
			if err != nil {
				add("%s %v", name, err)
				continue
			}
			for _, m := range requirementURL.FindAllStringSubmatch(text, -1) {
				if !hostOK(m[2]) {
					add("%s uses %s://%s, which is not an approved package source.", name, strings.ToLower(m[1]), m[2])
				}
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	if len(problems) > 5 {
		problems = append(problems[:5], fmt.Sprintf("…and %d more.", len(problems)-5))
	}
	return errors.New("this project reaches for package sources your administrator has not approved: " + strings.Join(problems, " "))
}
