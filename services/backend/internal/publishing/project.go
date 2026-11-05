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
	Services          string `json:"services,omitempty"`
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
	// Exposure selects the resource that publishes the app: an OpenShift Route
	// (default) or a networking.k8s.io Ingress. Labels and annotations are
	// "key=value" lines applied to that resource only.
	Exposure            string `json:"exposure,omitempty"`
	ExposureLabels      string `json:"exposureLabels,omitempty"`
	ExposureAnnotations string `json:"exposureAnnotations,omitempty"`
	IngressClass        string `json:"ingressClass,omitempty"`
	// IngressTLS is "hosts" (default: list the host, use the controller default
	// certificate) or "none" (tls: []).
	IngressTLS string `json:"ingressTls,omitempty"`
	// TemplateVersion pins a custom template version (0 = built-in).
	TemplateVersion    int    `json:"templateVersion,omitempty"`
	TemplateParameters string `json:"templateParameters,omitempty"`
	// TemplateFiles are the resolved files of that version; never persisted.
	TemplateFiles *TemplateFileSet `json:"-"`
	// PackageSources is a JSON list of mirrors for package managers; see packages.go.
	PackageSources string `json:"packageSources,omitempty"`
	// PackagePolicy is empty or "enforce" (reject uploads naming other sources).
	PackagePolicy string `json:"packagePolicy,omitempty"`
	// Port is the container port; zero selects the runtime default.
	Port int `json:"port,omitempty"`
	// Layout holds the upload rules of a template that defines its own.
	Layout               *TemplateLayout `json:"-"`
	RunnerServiceAccount string          `json:"runnerServiceAccount,omitempty"`
	RunnerSCC            string          `json:"runnerScc,omitempty"`
	BuilderSetup         string          `json:"builderSetup,omitempty"`
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
	if _, err := resolvedServices(c); err != nil {
		return err
	}
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
	if !validRuntime(c.Runtime) {
		return errors.New("select an application template")
	}
	// Built-in IDs must match their runtime. Workspace templates are resolved
	// and checked against the workspace's own list by the server.
	if template, builtin := BuiltinLookup(c.TemplateID); c.TemplateID != "" && builtin && template.Runtime != c.Runtime {
		return errors.New("selected application template does not match the runtime")
	}
	if c.Port < 0 || c.Port > 65535 {
		return errors.New("container port is invalid")
	}
	if err := ValidatePackageSources(c.PackageSources, c.PackagePolicy); err != nil {
		return err
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
		return errors.New("static build settings are only supported by static website templates")
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
	if err := c.validateExposure(); err != nil {
		return err
	}
	if _, err := ParseTemplateParameters(c.TemplateParameters); err != nil {
		return err
	}
	if c.TemplateVersion < 0 {
		return errors.New("template version is invalid")
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
		if n == "__MACOSX" || strings.HasPrefix(n, "__MACOSX/") || path.Base(n) == ".DS_Store" {
			continue
		}
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
	normalized, _, err := NormalizeSource(c, source)
	if err != nil {
		return nil, err
	}
	source = normalized
	if err := CheckPackageSources(c, source); err != nil {
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
	files := append([]File{}, source...)
	rendered, err := RenderConfig(c, SourceFacts{HasRenvLock: has("renv.lock")})
	if err != nil {
		return nil, err
	}
	for _, p := range TemplateFilePaths {
		files = append(files, File{Path: p, Content: rendered[p], Encoding: "text"})
	}
	if script := rendered[FilePackageSources]; script != "" {
		files = append(files, File{Path: FilePackageSources, Content: script, Encoding: "text"})
	}
	files = append(files, File{Path: "deploy/.gitkeep", Content: "", Encoding: "text"})
	return files, nil
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

var (
	metadataKeyPattern  = regexp.MustCompile(`^([a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?/)?[A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$`)
	labelValuePattern   = regexp.MustCompile(`^([A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?)?$`)
	ingressClassPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
)

// parseMetadata reads "key=value" lines. Labels get Kubernetes label-value
// rules; annotations only need a single line of bounded length.
func parseMetadata(text string, label bool) ([][2]string, error) {
	var out [][2]string
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || !metadataKeyPattern.MatchString(key) {
			return nil, errors.New("labels and annotations must be lines of the form key=value with Kubernetes-valid keys")
		}
		if label && !labelValuePattern.MatchString(value) {
			return nil, errors.New("label value for " + key + " is not a valid Kubernetes label value")
		}
		if !label && (len(value) > 1024 || strings.ContainsAny(value, "\r\x00")) {
			return nil, errors.New("annotation value for " + key + " is too long or contains control characters")
		}
		if seen[key] {
			return nil, errors.New("duplicate key " + key)
		}
		seen[key] = true
		if out = append(out, [2]string{key, value}); len(out) > 20 {
			return nil, errors.New("at most 20 labels or annotations are allowed")
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out, nil
}

func (c Config) validateExposure() error {
	if c.Exposure != "" && c.Exposure != "route" && c.Exposure != "ingress" {
		return errors.New("exposure must be route or ingress")
	}
	if c.IngressTLS != "" && c.IngressTLS != "hosts" && c.IngressTLS != "none" {
		return errors.New("ingress TLS must be hosts or none")
	}
	if c.IngressTLS == "none" && c.Exposure != "ingress" {
		return errors.New("ingress TLS requires the ingress exposure")
	}
	if c.IngressClass != "" && (c.Exposure != "ingress" || !ingressClassPattern.MatchString(c.IngressClass)) {
		return errors.New("ingress class requires the ingress exposure and a valid class name")
	}
	if _, err := parseMetadata(c.ExposureLabels, true); err != nil {
		return err
	}
	_, err := parseMetadata(c.ExposureAnnotations, false)
	return err
}
