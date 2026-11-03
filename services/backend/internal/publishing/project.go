// Package publishing generates constrained application projects. Source is never executed here.
package publishing

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

const DefaultMaxArchive = 100 << 20
const DefaultMaxSource = 500 << 20
const MaxEntries = 1500

type Limits struct {
	ArchiveBytes  int64 `json:"archiveBytes"`
	ExpandedBytes int64 `json:"expandedBytes"`
}

func (l Limits) WithDefaults() Limits {
	if l.ArchiveBytes == 0 {
		l.ArchiveBytes = DefaultMaxArchive
	}
	if l.ExpandedBytes == 0 {
		l.ExpandedBytes = DefaultMaxSource
	}
	return l
}

// LimitError distinguishes oversized requests and ZIP bombs from invalid archives.
type LimitError struct{ Message string }

func (e *LimitError) Error() string { return e.Message }
func SizeLabel(n int64) string      { return fmt.Sprintf("%g MiB", float64(n)/(1<<20)) }

const Version = "1"

type Config struct {
	TemplateID    string `json:"templateId,omitempty"`
	Name          string `json:"name"`
	Runtime       string `json:"runtime"`
	EntryPoint    string `json:"entryPoint"`
	BaseImage     string `json:"baseImage"`
	BuilderImage  string `json:"builderImage"`
	RegistryImage string `json:"registryImage"`
	Namespace     string `json:"namespace"`
	Host          string `json:"host"`
	PullSecret    string `json:"pullSecret"`
	SecretMode    string `json:"secretMode"`
	VaultAddress  string `json:"vaultAddress"`
	VaultRole     string `json:"vaultRole"`
	VaultPath     string `json:"vaultPath"`
	RunnerTag     string `json:"runnerTag"`
}

type File struct {
	Path     string `json:"file_path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	DiskPath string `json:"-"`
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,61}[a-z0-9]$|^[a-z]$`)
var imagePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/@-]+$`)
var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
var filePattern = regexp.MustCompile(`^[a-zA-Z0-9_./-]+$`)

func (c Config) Validate() error {
	if !namePattern.MatchString(c.Name) || !namePattern.MatchString(c.Namespace) {
		return errors.New("application and namespace must be lowercase DNS names (at most 63 characters)")
	}
	if c.Runtime != "shiny" && c.Runtime != "streamlit" {
		return errors.New("select Shiny or Streamlit")
	}
	if c.TemplateID != "" {
		found := false
		for _, template := range Templates() {
			if template.ID == c.TemplateID && template.Runtime == c.Runtime {
				found = true
				break
			}
		}
		if !found {
			return errors.New("selected application template does not match the runtime")
		}
	}
	if !safePath(c.EntryPoint) || !filePattern.MatchString(c.EntryPoint) {
		return errors.New("entry point must be a relative application path without spaces")
	}
	for _, image := range []string{c.BaseImage, c.BuilderImage, c.RegistryImage} {
		if !imagePattern.MatchString(image) {
			return errors.New("runtime, builder, and destination image references are required and must not contain whitespace")
		}
	}
	// Destination has no tag: every build receives its own source revision tag.
	last := path.Base(c.RegistryImage)
	if strings.ContainsAny(last, ":@") || !strings.Contains(c.RegistryImage, "/") {
		return errors.New("destination image must include registry and repository, without a tag or digest")
	}
	if c.Host != "" && (!hostPattern.MatchString(c.Host) || strings.Contains(c.Host, "..")) {
		return errors.New("application hostname is invalid")
	}
	if c.PullSecret != "" && !namePattern.MatchString(c.PullSecret) {
		return errors.New("pull secret must be an existing namespace secret name")
	}
	if c.RunnerTag != "" && !regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,100}$`).MatchString(c.RunnerTag) {
		return errors.New("runner tag is invalid")
	}
	if c.SecretMode != "variables" && c.SecretMode != "vault" {
		return errors.New("select GitLab CI/CD variables or Vault")
	}
	if c.SecretMode == "vault" {
		u, err := url.Parse(c.VaultAddress)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("Vault address must be HTTPS without credentials or query parameters")
		}
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(c.VaultRole) || !regexp.MustCompile(`^[a-zA-Z0-9_-]+/data/[a-zA-Z0-9_/-]+$`).MatchString(c.VaultPath) || !safePath(c.VaultPath) {
			return errors.New("Vault JWT role and KV v2 API path are required")
		}
	}
	return nil
}
func safePath(p string) bool {
	return p != "" && path.Clean(p) == p && !path.IsAbs(p) && p != ".." && !strings.HasPrefix(p, "../") && !strings.ContainsAny(p, "\\\x00\r\n")
}

// ExtractArchive reads the ZIP's directory via ReaderAt, then streams each regular
// file into a private temporary file. Archive paths are never filesystem paths.
// The caller owns dir and removes it on every success/failure path.
func ExtractArchive(ctx context.Context, input io.ReaderAt, size int64, dir string, limits Limits) ([]File, error) {
	limits = limits.WithDefaults()
	if size <= 0 {
		return nil, errors.New("upload a non-empty ZIP archive")
	}
	if size > limits.ArchiveBytes {
		return nil, &LimitError{"ZIP archive exceeds " + SizeLabel(limits.ArchiveBytes)}
	}
	z, err := zip.NewReader(input, size)
	if err != nil {
		return nil, errors.New("invalid ZIP archive")
	}
	if len(z.File) > MaxEntries {
		return nil, errors.New("archive contains too many entries (maximum 1500)")
	}
	files := []File{}
	seen := map[string]bool{}
	var total int64
	for _, f := range z.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := strings.TrimSuffix(f.Name, "/")
		if n == "." || !safePath(n) {
			return nil, errors.New("archive contains an unsafe path or special file")
		}
		for _, p := range strings.Split(n, "/") {
			if p == ".git" || p == ".env" || strings.HasPrefix(p, ".env.") || p == ".Renviron" || p == ".Rhistory" || p == ".RData" || p == "secrets.toml" || p == "id_rsa" || p == "id_ed25519" {
				return nil, errors.New("remove local credentials or session files from the archive")
			}
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() || seen[n] {
			return nil, errors.New("archive contains duplicate paths or non-regular files")
		}
		seen[n] = true
		if f.UncompressedSize64 > uint64(limits.ExpandedBytes-total) {
			return nil, &LimitError{"archive exceeds " + SizeLabel(limits.ExpandedBytes) + " expanded size"}
		}
		r, e := f.Open()
		if e != nil {
			return nil, errors.New("cannot open archive entry")
		}
		out, e := os.CreateTemp(dir, "source-*")
		if e != nil {
			r.Close()
			return nil, errors.New("cannot allocate temporary source storage")
		}
		count, copyErr := io.Copy(out, io.LimitReader(contextReader{ctx, r}, limits.ExpandedBytes-total+1))
		r.Close()
		closeErr := out.Close()
		total += count
		if total > limits.ExpandedBytes {
			return nil, &LimitError{"archive exceeds " + SizeLabel(limits.ExpandedBytes) + " expanded size"}
		}
		if copyErr != nil || closeErr != nil {
			return nil, errors.New("cannot extract archive; check its integrity and temporary storage capacity")
		}
		files = append(files, File{Path: "source/" + n, DiskPath: out.Name(), Encoding: "base64"})
	}
	if len(files) == 0 {
		return nil, errors.New("archive contains no application files")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// Fingerprint hashes contents incrementally instead of marshaling the project.
func Fingerprint(ctx context.Context, c Config, files []File, cluster string) (string, error) {
	h := sha256.New()
	encoder := json.NewEncoder(h)
	if err := encoder.Encode(struct {
		Config  Config
		Cluster string
	}{c, cluster}); err != nil {
		return "", err
	}
	for _, f := range files {
		contentHash := sha256.New()
		if f.DiskPath != "" {
			input, err := os.Open(f.DiskPath)
			if err != nil {
				return "", errors.New("cannot read temporary source file")
			}
			_, err = io.Copy(contentHash, contextReader{ctx, input})
			input.Close()
			if err != nil {
				return "", err
			}
		} else {
			io.WriteString(contentHash, f.Content)
		}
		if err := encoder.Encode(struct {
			Path   string
			Digest string
		}{f.Path, hex.EncodeToString(contentHash.Sum(nil))}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func Generate(c Config, source []File) ([]File, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	has := func(p string) bool {
		for _, f := range source {
			if f.Path == "source/"+p {
				return true
			}
		}
		return false
	}
	if c.Runtime == "streamlit" && (!has(c.EntryPoint) || !has("requirements.txt")) {
		return nil, errors.New("Streamlit requires the entry point and requirements.txt at the archive root")
	}
	if c.Runtime == "shiny" && (!has(path.Join(c.EntryPoint, "app.R")) && !(has(path.Join(c.EntryPoint, "ui.R")) && has(path.Join(c.EntryPoint, "server.R")))) {
		return nil, errors.New("Shiny entry directory must contain app.R or ui.R and server.R")
	}
	if c.Runtime == "shiny" && !has("renv.lock") && !has("packages.txt") {
		return nil, errors.New("Shiny requires renv.lock or packages.txt at the archive root")
	}
	files := append([]File{}, source...)
	add := func(p, s string) { files = append(files, File{Path: p, Content: s, Encoding: "text"}) }
	port := 3838
	docker := "FROM " + c.BaseImage + "\nUSER 0\nWORKDIR /app\nCOPY source/ /app/\n"
	if c.Runtime == "shiny" {
		if has("renv.lock") {
			docker += "RUN R -e 'install.packages(\"renv\", repos=Sys.getenv(\"R_REPOSITORY\", \"https://cloud.r-project.org\")); renv::restore(prompt=FALSE)'\n"
		} else {
			docker += "RUN R -e 'p <- trimws(readLines(\"packages.txt\")); p <- p[nzchar(p) & !startsWith(p, \"#\")]; install.packages(p, repos=Sys.getenv(\"R_REPOSITORY\", \"https://cloud.r-project.org\")); stopifnot(all(p %in% rownames(installed.packages())))'\n"
		}
		cmd, _ := json.Marshal([]string{"R", "-e", fmt.Sprintf("shiny::runApp('%s', host='0.0.0.0', port=3838)", c.EntryPoint)})
		docker += "RUN chgrp -R 0 /app && chmod -R g=u /app\nENV HOME=/tmp\nUSER 1001\nEXPOSE 3838\nCMD " + string(cmd) + "\n"
	} else {
		port = 8501
		docker += "RUN python -m pip install --no-cache-dir -r requirements.txt\nRUN chgrp -R 0 /app && chmod -R g=u /app\nENV HOME=/tmp\nUSER 1001\nEXPOSE 8501\n"
		cmd, _ := json.Marshal([]string{"python", "-m", "streamlit", "run", c.EntryPoint, "--server.address=0.0.0.0", "--server.port=8501"})
		docker += "CMD " + string(cmd) + "\n"
	}
	add("Dockerfile", docker)
	add(".dockerignore", ".git\ndeploy\n.justcd\n")
	add(".justcd/deployment.yaml", manifest(c, port))
	add("deploy/.gitkeep", "")
	add(".gitlab-ci.yml", pipeline(c))
	add(".justcd/README.md", "# Managed application\nGenerated by JustCD publishing template v"+Version+". User application files live in source/.\nThe protected main branch builds and publishes an immutable image, then commits deploy/application.yaml.\nNo workload is deployed until JustCD plans and applies it under workspace policy.\n")
	return files, nil
}

func manifest(c Config, port int) string {
	pull := ""
	if c.PullSecret != "" {
		pull = "      imagePullSecrets:\n        - name: " + c.PullSecret + "\n"
	}
	s := fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
spec:
  replicas: 1
  selector:
    matchLabels: {app: %s}
  template:
    metadata:
      labels: {app: %s}
    spec:
      automountServiceAccountToken: false
%s      containers:
        - name: app
          image: JUSTCD_IMAGE_DIGEST
          ports:
            - {name: http, containerPort: %d}
          resources:
            requests: {cpu: 100m, memory: 256Mi}
            limits: {cpu: '1', memory: 1Gi}
          securityContext:
            allowPrivilegeEscalation: false
            capabilities: {drop: [ALL]}
          readinessProbe:
            tcpSocket: {port: http}
            initialDelaySeconds: 10
          livenessProbe:
            tcpSocket: {port: http}
            initialDelaySeconds: 60
---
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
spec:
  selector: {app: %s}
  ports:
    - {name: http, port: 80, targetPort: http}
`, c.Name, c.Namespace, c.Name, c.Name, pull, port, c.Name, c.Namespace, c.Name)
	if c.Host != "" {
		s += fmt.Sprintf(`---
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: %s
  namespace: %s
spec:
  host: %s
  to: {kind: Service, name: %s}
  port: {targetPort: http}
  tls:
    termination: edge
    insecureEdgeTerminationPolicy: Redirect
`, c.Name, c.Namespace, c.Host, c.Name)
	}
	return s
}
func yamlString(s string) string { b, _ := json.Marshal(s); return string(b) }
func pipeline(c Config) string {
	tags := ""
	if c.RunnerTag != "" {
		tags = "  tags: [" + yamlString(c.RunnerTag) + "]\n"
	}
	vault := ""
	if c.SecretMode == "vault" {
		vault = "  id_tokens:\n    VAULT_ID_TOKEN:\n      aud: " + yamlString(c.VaultAddress) + "\n  secrets:\n    REGISTRY_USER:\n      vault:\n        engine: {name: kv-v2, path: " + yamlString(strings.Split(c.VaultPath, "/")[0]) + "}\n        path: " + yamlString(strings.TrimPrefix(strings.Join(strings.Split(c.VaultPath, "/")[1:], "/"), "data/")) + "\n        field: username\n      token: $VAULT_ID_TOKEN\n      file: false\n    REGISTRY_PASSWORD:\n      vault:\n        engine: {name: kv-v2, path: " + yamlString(strings.Split(c.VaultPath, "/")[0]) + "}\n        path: " + yamlString(strings.TrimPrefix(strings.Join(strings.Split(c.VaultPath, "/")[1:], "/"), "data/")) + "\n        field: token\n      token: $VAULT_ID_TOKEN\n      file: false\n"
	}
	return "# JustCD template v" + Version + ". Builder image must contain rootless BuildKit, git, jq and a POSIX shell.\nworkflow:\n  rules:\n    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'\n      changes: [source/**/*, Dockerfile, .gitlab-ci.yml, .justcd/**/*]\n    - when: never\nstages: [publish]\npublish:\n  stage: publish\n  resource_group: justcd-publish\n  image:\n    name: " + yamlString(c.BuilderImage) + "\n    entrypoint: [\"\"]\n" + tags + vault + "  variables:\n    GIT_DEPTH: '0'\n    BUILDKITD_FLAGS: '--oci-worker-no-process-sandbox'\n    JUSTCD_IMAGE: " + yamlString(c.RegistryImage) + "\n    VAULT_SERVER_URL: " + yamlString(c.VaultAddress) + "\n    VAULT_AUTH_ROLE: " + yamlString(c.VaultRole) + ` 
  script:
    - |
      set -eu
      test -n "$REGISTRY_USER"
      test -n "$REGISTRY_PASSWORD"
      export DOCKER_CONFIG="$(mktemp -d)"
      trap 'rm -rf "$DOCKER_CONFIG"' EXIT
      registry="${JUSTCD_IMAGE%%/*}"
      jq -n --arg host "$registry" --arg user "$REGISTRY_USER" --arg password "$REGISTRY_PASSWORD" '{auths:{($host):{auth:(($user+":"+$password)|@base64)}}}' > "$DOCKER_CONFIG/config.json"
      buildctl-daemonless.sh build --frontend dockerfile.v0 --local context=. --local dockerfile=. --output "type=image,name=$JUSTCD_IMAGE:$CI_COMMIT_SHA,push=true" --metadata-file /tmp/justcd-image.json
      digest="$(jq -er '."containerimage.digest" | select(test("^sha256:[a-f0-9]{64}$"))' /tmp/justcd-image.json)"
      # A newer source commit must never be replaced by an older build.
      git fetch origin "$CI_DEFAULT_BRANCH"
      test "$(git rev-parse FETCH_HEAD)" = "$CI_COMMIT_SHA"
      sed "s|JUSTCD_IMAGE_DIGEST|$JUSTCD_IMAGE@$digest|g" .justcd/deployment.yaml > deploy/application.yaml
      git config user.name 'JustCD Publisher'
      git config user.email 'justcd@localhost'
      git add deploy/application.yaml
      git commit -m "Publish $CI_COMMIT_SHA [skip ci]"
      git push "$CI_REPOSITORY_URL" "HEAD:$CI_DEFAULT_BRANCH"
`
}
