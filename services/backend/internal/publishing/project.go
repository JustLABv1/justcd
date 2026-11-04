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
	ManagedCompletion bool   `json:"managedCompletion,omitempty"`
	GitImage          string `json:"gitImage,omitempty"`
	BuildEngine       string `json:"buildEngine,omitempty"`
	CAVariable        string `json:"caVariable,omitempty"`
	CreateNamespace   bool   `json:"createNamespace,omitempty"`
	BuildCommand      string `json:"buildCommand,omitempty"`
	OutputDir         string `json:"outputDir,omitempty"`
	WebServerImage    string `json:"webServerImage,omitempty"`
	TemplateID        string `json:"templateId,omitempty"`
	Name              string `json:"name"`
	Runtime           string `json:"runtime"`
	EntryPoint        string `json:"entryPoint"`
	BaseImage         string `json:"baseImage"`
	BuilderImage      string `json:"builderImage"`
	RegistryImage     string `json:"registryImage"`
	Namespace         string `json:"namespace"`
	Host              string `json:"host"`
	PullSecret        string `json:"pullSecret"`
	SecretMode        string `json:"secretMode"`
	VaultAddress      string `json:"vaultAddress"`
	VaultRole         string `json:"vaultRole"`
	VaultPath         string `json:"vaultPath"`
	RunnerTag         string `json:"runnerTag"`
	// Optional Kubernetes-executor settings for runners that only allow rootless
	// builds under a dedicated service account and OpenShift SCC.
	RunnerServiceAccount string `json:"runnerServiceAccount,omitempty"`
	RunnerSCC            string `json:"runnerScc,omitempty"`
	BuilderSetup         string `json:"builderSetup,omitempty"`
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
	if c.BuildEngine != "" && c.BuildEngine != "buildkit" && c.BuildEngine != "podman" {
		return errors.New("build engine must be buildkit or podman")
	}
	if c.CAVariable != "" && !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`).MatchString(c.CAVariable) {
		return errors.New("CA variable must be a CI variable name containing letters, numbers and underscores")
	}
	if !namePattern.MatchString(c.Name) || !namePattern.MatchString(c.Namespace) {
		return errors.New("application and namespace must be lowercase DNS names (at most 63 characters)")
	}
	if c.CreateNamespace {
		if err := ValidateNewNamespace(c.Namespace); err != nil {
			return err
		}
	}
	if c.Runtime != "shiny" && c.Runtime != "streamlit" && c.Runtime != "python-static" && c.Runtime != "static" {
		return errors.New("select Shiny, Streamlit, or a static website")
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
	images := []string{c.BuilderImage, c.RegistryImage}
	if c.GitImage != "" {
		images = append(images, c.GitImage)
	}
	if c.Runtime != "static" {
		images = append(images, c.BaseImage)
	}
	for _, image := range images {
		if !imagePattern.MatchString(image) {
			return errors.New("runtime, builder, and destination image references are required and must not contain whitespace")
		}
	}
	if c.Runtime == "python-static" {
		if strings.TrimSpace(c.BuildCommand) == "" || len(c.BuildCommand) > 2048 || strings.ContainsRune(c.BuildCommand, '\x00') {
			return errors.New("a static website build command of at most 2048 characters is required")
		}
		if c.OutputDir == "." || !safePath(c.OutputDir) || !filePattern.MatchString(c.OutputDir) {
			return errors.New("output directory must be a relative subdirectory such as dist, without traversal or spaces")
		}
		if !imagePattern.MatchString(c.WebServerImage) {
			return errors.New("an approved unprivileged webserver image is required for static websites")
		}
	} else if c.Runtime == "static" {
		if c.BuildCommand != "" || c.OutputDir != "" {
			return errors.New("ready-made static websites do not use a build command or output directory; select the website directory instead")
		}
		if !imagePattern.MatchString(c.WebServerImage) {
			return errors.New("an approved unprivileged webserver image is required for static websites")
		}
	} else if c.BuildCommand != "" || c.OutputDir != "" || c.WebServerImage != "" {
		return errors.New("static build settings are only supported by the Python static website template")
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
	for _, v := range []struct{ value, label string }{{c.RunnerServiceAccount, "runner service account"}, {c.RunnerSCC, "runner security context constraint"}} {
		if v.value != "" && !regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`).MatchString(v.value) {
			return errors.New(v.label + " must be a Kubernetes resource name")
		}
	}
	if c.BuilderSetup != "" && !regexp.MustCompile(`^/[A-Za-z0-9_./-]{1,199}$`).MatchString(c.BuilderSetup) {
		return errors.New("builder setup must be the absolute path of an executable in the builder image (for example /home/user/entrypoint.sh), without arguments")
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
		if len(z.File) == 0 {
			return nil, errors.New("the ZIP archive is empty; recreate it with your application files, such as index.html, app.py, or app.R")
		}
		return nil, fmt.Errorf("the ZIP archive contains only folders (%d entries), with no application files; recreate it including the files inside your project directory", len(z.File))
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
	if c.Runtime == "static" && !has(path.Join(c.EntryPoint, "index.html")) {
		return nil, errors.New("static website directory must contain index.html")
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
	} else if c.Runtime == "streamlit" {
		port = 8501
		docker += "RUN python -m pip install --no-cache-dir -r requirements.txt\nRUN chgrp -R 0 /app && chmod -R g=u /app\nENV HOME=/tmp\nUSER 1001\nEXPOSE 8501\n"
		cmd, _ := json.Marshal([]string{"python", "-m", "streamlit", "run", c.EntryPoint, "--server.address=0.0.0.0", "--server.port=8501"})
		docker += "CMD " + string(cmd) + "\n"
	}
	if c.Runtime == "python-static" {
		port = 8080
		command, _ := json.Marshal([]string{"sh", "-c", c.BuildCommand})
		prepare, _ := json.Marshal([]string{"python", "-c", staticOutputScript, c.OutputDir, "/justcd-site"})
		docker = "FROM " + c.BaseImage + " AS site-build\nUSER 0\nWORKDIR /app\nCOPY source/ /app/\nRUN if [ -f requirements.txt ]; then python -m pip install --no-cache-dir -r requirements.txt; fi\nRUN " + string(command) + "\nRUN " + string(prepare) + "\n\nFROM " + c.WebServerImage + " AS site-runtime\nCOPY --from=site-build /justcd-site/ /usr/share/nginx/html/\nUSER 1001\nEXPOSE 8080\n"
	}
	if c.Runtime == "static" {
		port = 8080
		docker = "FROM " + c.WebServerImage + "\nCOPY --chmod=0755 " + path.Join("source", c.EntryPoint) + "/ /usr/share/nginx/html/\nUSER 1001\nEXPOSE 8080\n"
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
	ci := legacyPipeline(c)
	if c.ManagedCompletion {
		ci = strings.Split(ci, "update-deployment:\n")[0]
		ci = strings.Replace(ci, "stages: [build, publish]", "stages: [build]", 1)
	}
	return ci
}

func legacyPipeline(c Config) string {
	gitImage := c.GitImage
	if gitImage == "" {
		gitImage = c.BuilderImage
	}
	tools := "buildctl-daemonless.sh buildctl buildkitd jq"
	engine := "BuildKit"
	buildSteps := `      jq -n --arg host "$registry" --arg user "$REGISTRY_USER" --arg password "$REGISTRY_PASSWORD" '{auths:{($host):{auth:(($user+":"+$password)|@base64)}}}' > "$DOCKER_CONFIG/config.json"
      buildctl-daemonless.sh build --frontend dockerfile.v0 --local context=. --local dockerfile=. --output "type=image,name=$JUSTCD_IMAGE:$CI_COMMIT_SHA,push=true" --metadata-file /tmp/justcd-image.json
      digest="$(jq -er '."containerimage.digest" | select(test("^sha256:[a-f0-9]{64}$"))' /tmp/justcd-image.json)"
`
	if c.BuildEngine == "podman" {
		tools = "podman grep cat cp mkdir"
		engine = "Podman"
		buildSteps = `      export REGISTRY_AUTH_FILE="$DOCKER_CONFIG/auth.json"
      mkdir -p "$DOCKER_CONFIG/certs"
      if [ -n "${SSL_CERT_FILE:-}" ]; then cp "$SSL_CERT_FILE" "$DOCKER_CONFIG/certs/ca.crt"; fi
      printf '%s' "$REGISTRY_PASSWORD" | podman login --authfile "$REGISTRY_AUTH_FILE" --cert-dir "$DOCKER_CONFIG/certs" --tls-verify=true --username "$REGISTRY_USER" --password-stdin "$registry"
      podman build --authfile "$REGISTRY_AUTH_FILE" --cert-dir "$DOCKER_CONFIG/certs" --tls-verify=true --format docker -f Dockerfile -t "$JUSTCD_IMAGE:$CI_COMMIT_SHA" .
      podman push --authfile "$REGISTRY_AUTH_FILE" --cert-dir "$DOCKER_CONFIG/certs" --tls-verify=true --digestfile "$DOCKER_CONFIG/digest" "$JUSTCD_IMAGE:$CI_COMMIT_SHA" "docker://$JUSTCD_IMAGE:$CI_COMMIT_SHA"
      digest="$(cat "$DOCKER_CONFIG/digest")"
      if ! printf '%s\n' "$digest" | grep -Eq '^sha256:[a-f0-9]{64}$'; then
        printf 'JustCD: Podman push returned an invalid image digest.\n' >&2
        exit 1
      fi
`
	}
	caSetup := ""
	if c.CAVariable != "" {
		caSetup = `      # Optional platform CA, supplied as PEM text by an inherited CI variable.
      if [ -z "${` + c.CAVariable + `:-}" ]; then
        printf 'JustCD: configured CA variable ` + c.CAVariable + ` is empty or unavailable. Check GitLab variable inheritance and protected-branch scope.\n' >&2
        exit 1
      fi
      ca_bundle="$DOCKER_CONFIG/ca-bundle.pem"
      system_ca=""
      for candidate in /etc/ssl/certs/ca-certificates.crt /etc/pki/tls/certs/ca-bundle.crt; do
        if [ -r "$candidate" ]; then system_ca="$candidate"; break; fi
      done
      if [ -z "$system_ca" ]; then printf 'JustCD: builder image has no supported system CA bundle.\n' >&2; exit 1; fi
      cat "$system_ca" > "$ca_bundle"
      printf '\n%s\n' "$` + c.CAVariable + `" >> "$ca_bundle"
      export SSL_CERT_FILE="$ca_bundle"
      export GIT_SSL_CAINFO="$ca_bundle"
      export CURL_CA_BUNDLE="$ca_bundle"
      export REQUESTS_CA_BUNDLE="$ca_bundle"
`
	}

	tags := ""
	if c.RunnerTag != "" {
		tags = "  tags: [" + yamlString(c.RunnerTag) + "]\n"
	}
	runnerVars, setup := "", ""
	if c.RunnerServiceAccount != "" {
		runnerVars += "    KUBERNETES_SERVICE_ACCOUNT_OVERWRITE: " + yamlString(c.RunnerServiceAccount) + "\n"
	}
	if c.RunnerSCC != "" {
		runnerVars += "    KUBERNETES_POD_ANNOTATIONS_1: " + yamlString("openshift.io/required-scc="+c.RunnerSCC) + "\n"
	}
	if c.BuilderSetup != "" {
		setup = "  before_script:\n    - " + yamlString(c.BuilderSetup) + "\n"
	}
	vault := ""
	if c.SecretMode == "vault" {
		vault = "  id_tokens:\n    VAULT_ID_TOKEN:\n      aud: " + yamlString(c.VaultAddress) + "\n  secrets:\n    REGISTRY_USER:\n      vault:\n        engine: {name: kv-v2, path: " + yamlString(strings.Split(c.VaultPath, "/")[0]) + "}\n        path: " + yamlString(strings.TrimPrefix(strings.Join(strings.Split(c.VaultPath, "/")[1:], "/"), "data/")) + "\n        field: username\n      token: $VAULT_ID_TOKEN\n      file: false\n    REGISTRY_PASSWORD:\n      vault:\n        engine: {name: kv-v2, path: " + yamlString(strings.Split(c.VaultPath, "/")[0]) + "}\n        path: " + yamlString(strings.TrimPrefix(strings.Join(strings.Split(c.VaultPath, "/")[1:], "/"), "data/")) + "\n        field: token\n      token: $VAULT_ID_TOKEN\n      file: false\n"
	}
	return "# JustCD template v" + Version + ". Builder image must support the selected engine and CI tools.\nworkflow:\n  rules:\n    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'\n      changes: [source/**/*, Dockerfile, .gitlab-ci.yml, .justcd/**/*]\n    - when: never\nstages: [build, publish]\npublish:\n  stage: build\n  image:\n    name: " + yamlString(c.BuilderImage) + "\n    entrypoint: [\"\"]\n" + tags + vault + "  variables:\n    GIT_DEPTH: '0'\n" + runnerVars + "    BUILDKITD_FLAGS: '--oci-worker-no-process-sandbox'\n    JUSTCD_IMAGE: " + yamlString(c.RegistryImage) + "\n    VAULT_SERVER_URL: " + yamlString(c.VaultAddress) + "\n    VAULT_AUTH_ROLE: " + yamlString(c.VaultRole) + "\n" + setup + `  script:
    - |
      set -eu
      for tool in ` + tools + `; do
        if ! command -v "$tool" >/dev/null 2>&1; then
          printf 'JustCD: CI builder image is missing %s. The selected ` + engine + ` engine requires: ` + tools + `. Update the publishing profile builder image or build engine and rebuild.\n' "$tool" >&2
          exit 1
        fi
      done
      test -n "$REGISTRY_USER"
      test -n "$REGISTRY_PASSWORD"
      export DOCKER_CONFIG="$(mktemp -d)"
      trap 'rm -rf "$DOCKER_CONFIG"' EXIT
` + caSetup + `      registry="${JUSTCD_IMAGE%%/*}"
` + buildSteps + `      mkdir -p .justcd-build
      printf '%s\n' "$digest" > .justcd-build/image-digest
  artifacts:
    paths: [.justcd-build/image-digest]
    expire_in: 1 day
update-deployment:
  stage: publish
  resource_group: justcd-publish
  needs:
    - job: publish
      artifacts: true
  image:
    name: ` + yamlString(gitImage) + `
    entrypoint: [""]
` + tags + `  variables:
    GIT_DEPTH: '0'
    JUSTCD_IMAGE: ` + yamlString(c.RegistryImage) + `
  script:
    - |
      set -eu
      for tool in git sed grep; do
        if ! command -v "$tool" >/dev/null 2>&1; then
          printf 'JustCD: deployment update image is missing %s. Configure a Git-capable deployment update image in the publishing profile.\n' "$tool" >&2
          exit 1
        fi
      done
      export DOCKER_CONFIG="$(mktemp -d)"
      trap 'rm -rf "$DOCKER_CONFIG"' EXIT
` + caSetup + `      digest="$(cat .justcd-build/image-digest)"
      if ! printf '%s\n' "$digest" | grep -Eq '^sha256:[a-f0-9]{64}$'; then
        printf 'JustCD: build artifact has an invalid image digest.\n' >&2
        exit 1
      fi
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

// Runs inside the isolated Python build stage, never in the JustCD API. Only
// validated static output is copied into the final webserver image.
const staticOutputScript = `import itertools, shutil, sys
from pathlib import Path
root = Path.cwd().resolve()
source = root / sys.argv[1]
resolved = source.resolve()
if resolved == root or root not in resolved.parents:
    raise SystemExit("Static output must stay inside the project directory")
if not source.is_dir() or not (source / "index.html").is_file():
    raise SystemExit("Static build must produce index.html inside the configured output directory")
for item in itertools.chain((source,), source.rglob("*")):
    if item.is_symlink() or not (item.is_file() or item.is_dir()):
        raise SystemExit("Static output cannot contain links or special files")
target = Path(sys.argv[2])
shutil.copytree(source, target)
for item in itertools.chain((target,), target.rglob("*")):
    item.chmod(0o755 if item.is_dir() else 0o644)
`

// Reserved platform namespaces cannot be provisioned through app publishing.
func ValidateNewNamespace(name string) error {
	if !namePattern.MatchString(name) || name == "default" || name == "kube" || name == "openshift" || strings.HasPrefix(name, "kube-") || strings.HasPrefix(name, "openshift-") {
		return errors.New("choose a non-system namespace using lowercase DNS letters, numbers and hyphens (at most 63 characters)")
	}
	return nil
}
