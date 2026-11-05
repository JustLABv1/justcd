package publishing

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"text/template/parse"
)

// Application templates are sets of files (Dockerfile, build pipeline,
// Kubernetes manifest, ...) with placeholders. JustCD ships built-in versions;
// workspace owners can copy and edit them, which produces a new immutable
// template version. A profile pins the template version it uses.
//
// Template files use Go text/template syntax restricted to a sandbox: only the
// data in TemplateData and a few pure helper functions are available, there is
// no include/define/block, and rendering is size-limited. The rendered result
// is validated before it is ever committed or applied.

//go:embed templates
var builtinFS embed.FS

// Template file paths (as committed to the application repository).
const (
	FileDockerfile   = "Dockerfile"
	FileDockerignore = ".dockerignore"
	FilePipeline     = ".gitlab-ci.yml"
	FileDeployment   = ".justcd/deployment.yaml"
	FileReadme       = ".justcd/README.md"

	imagePlaceholer  = "JUSTCD_IMAGE_DIGEST"
	maxTemplateFile  = 128 << 10
	maxRenderedBytes = 1 << 20
)

// TemplateFilePaths lists, in display order, the files every template contains.
var TemplateFilePaths = []string{FileDockerfile, FilePipeline, FileDeployment, FileDockerignore, FileReadme}

var builtinSources = map[string]string{
	FileDockerfile:   "Dockerfile.tmpl",
	FilePipeline:     "common/gitlab-ci.yml.tmpl",
	FileDeployment:   "common/deployment.yaml.tmpl",
	FileDockerignore: "common/dockerignore.tmpl",
	FileReadme:       "common/README.md.tmpl",
}

// KV is one label or annotation.
type KV struct{ Key, Value string }

// TemplateData is everything a template can reference, as {{ .Field }}.
type TemplateData struct {
	HasDatabase         bool
	DatabaseClusterName string
	DatabaseName        string
	Parameters          map[string]string
	RecipeVersion       string
	// Application
	Name, Namespace, Host, Runtime, EntryPoint string
	Port                                       int
	StaticDir                                  string
	BuildCommand, OutputDir                    string
	StaticOutputScript                         string
	HasRenvLock                                bool
	// Images
	BaseImage, WebServerImage, BuilderImage, GitImage, RegistryImage, PullSecret string
	ImagePlaceholder                                                             string
	// Publishing
	ManagedCompletion                   bool
	Exposure, IngressClass, IngressTLS  string
	ExposureLabels, ExposureAnnotations []KV
	// Build job
	BuildEngine, CAVariable                                          string
	RunnerTag, RunnerServiceAccount, RunnerSCC, BuilderSetup         string
	SecretMode, VaultAddress, VaultRole, VaultMount, VaultSecretPath string
}

// Placeholder documents one TemplateData field for the editor.
type Placeholder struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

var placeholderDocs = map[string]string{
	"HasDatabase":          "True when this application selected the optional StackGres database.",
	"DatabaseClusterName":  "The generated SGCluster name, also used as app label on its database pods.",
	"DatabaseName":         "The SQL database name from the selected database plan.",
	"Parameters":           `Non-secret profile parameters, for example {{ quote (default "ingress-system" (index .Parameters "ingressNamespace")) }}.`,
	"RecipeVersion":        "JustCD recipe version of the built-in templates.",
	"Name":                 "Application name (DNS label).",
	"Namespace":            "Target namespace.",
	"Host":                 "Application host name; empty when the profile has no application domain.",
	"Runtime":              "shiny, streamlit, static or python-static.",
	"EntryPoint":           "Entry directory or file inside source/.",
	"Port":                 "Container port of the runtime.",
	"StaticDir":            "Static website directory inside the build context (source/<entry point>).",
	"BuildCommand":         "Python static build command.",
	"OutputDir":            "Python static output directory.",
	"StaticOutputScript":   "Python helper that validates and copies static build output.",
	"HasRenvLock":          "True when the source contains renv.lock (Shiny).",
	"BaseImage":            "Runtime or build base image from the profile.",
	"WebServerImage":       "Static web server image from the profile.",
	"BuilderImage":         "CI builder image from the profile.",
	"GitImage":             "Deployment update image (legacy pipelines); defaults to the builder image.",
	"RegistryImage":        "Image repository the build pushes to (registry prefix + application name).",
	"PullSecret":           "Name of the image pull secret, if configured.",
	"ImagePlaceholder":     "Token JustCD replaces with the built image reference. The app container must use it.",
	"ManagedCompletion":    "True when JustCD commits the deployment manifest after the build (default).",
	"Exposure":             "route (OpenShift Route) or ingress.",
	"IngressClass":         "ingressClassName for Ingress exposure.",
	"IngressTLS":           "hosts or none.",
	"ExposureLabels":       "Labels for the Route or Ingress: range over .Key and .Value.",
	"ExposureAnnotations":  "Annotations for the Route or Ingress: range over .Key and .Value.",
	"BuildEngine":          "buildkit or podman.",
	"CAVariable":           "Name of the CI variable holding custom CA certificates.",
	"RunnerTag":            "GitLab runner tag.",
	"RunnerServiceAccount": "Kubernetes service account override for the build job.",
	"RunnerSCC":            "OpenShift SCC annotation value for the build pod.",
	"BuilderSetup":         "Executable in the builder image run before the build.",
	"SecretMode":           "variables or vault.",
	"VaultAddress":         "Vault URL.",
	"VaultRole":            "Vault JWT role.",
	"VaultMount":           "Vault KV v2 mount (first segment of the secret path).",
	"VaultSecretPath":      "Vault KV v2 secret path without the mount and data/ prefix.",
}

// TemplatePlaceholders lists every placeholder and helper function.
func TemplatePlaceholders() []Placeholder {
	t := reflect.TypeOf(TemplateData{})
	out := make([]Placeholder, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		out = append(out, Placeholder{Name: "." + f.Name, Type: f.Type.String(), Description: placeholderDocs[f.Name]})
	}
	for _, fn := range []Placeholder{
		{"quote", "func(string) string", "Quotes a value as a YAML/JSON string: {{ quote .Name }}."},
		{"json", "func(any) string", "Renders a value as JSON, for example a Dockerfile CMD: {{ json (list \"a\" \"b\") }}."},
		{"list", "func(...any) []any", "Builds a list: {{ list \"a\" \"b\" }}."},
		{"default", "func(string, string) string", "Fallback for empty values: {{ default \"x\" .Host }}."},
	} {
		out = append(out, fn)
	}
	return out
}

var templateFuncs = template.FuncMap{
	"quote": func(s string) string { return yamlString(s) },
	"json": func(v any) (string, error) {
		b, err := json.Marshal(v)
		return string(b), err
	},
	"list": func(v ...any) []any { return v },
	"default": func(def, v string) string {
		if v == "" {
			return def
		}
		return v
	},
}

func yamlString(s string) string { b, _ := json.Marshal(s); return string(b) }

// BuiltinTemplateFiles returns the built-in, read-only files of a runtime.
func BuiltinTemplateFiles(runtime string) (map[string]string, error) {
	switch runtime {
	case "shiny", "streamlit", "static", "python-static":
	default:
		return nil, errors.New("unsupported runtime " + runtime)
	}
	out := map[string]string{}
	for target, source := range builtinSources {
		p := "templates/" + source
		if !strings.HasPrefix(source, "common/") {
			p = "templates/" + runtime + "/" + source
		}
		b, err := builtinFS.ReadFile(p)
		if err != nil {
			return nil, err
		}
		out[target] = string(b)
	}
	return out, nil
}

// BuiltinFilesForTemplate resolves a template ID (for example
// shiny-geospatial) to the built-in files of its runtime.
func BuiltinFilesForTemplate(templateID string) (map[string]string, error) {
	for _, t := range Templates() {
		if t.ID == templateID {
			return BuiltinTemplateFiles(t.Runtime)
		}
	}
	return nil, errors.New("unknown application template " + templateID)
}

func runtimePort(c Config) int {
	switch c.Runtime {
	case "streamlit":
		return 8501
	case "static", "python-static":
		return 8080
	}
	return 3838
}

// SourceFacts are properties of the uploaded source that templates can branch on.
type SourceFacts struct{ HasRenvLock bool }

func templateData(c Config, facts SourceFacts) TemplateData {
	parameters, _ := ParseTemplateParameters(c.TemplateParameters)
	d := TemplateData{Parameters: parameters,
		RecipeVersion: Version, Name: c.Name, Namespace: c.Namespace, Host: c.Host, Runtime: c.Runtime, EntryPoint: c.EntryPoint,
		Port: runtimePort(c), StaticDir: path.Join("source", c.EntryPoint), BuildCommand: c.BuildCommand, OutputDir: c.OutputDir,
		StaticOutputScript: staticOutputScript, HasRenvLock: facts.HasRenvLock,
		BaseImage: c.BaseImage, WebServerImage: c.WebServerImage, BuilderImage: c.BuilderImage, GitImage: c.GitImage, RegistryImage: c.RegistryImage,
		PullSecret: c.PullSecret, ImagePlaceholder: imagePlaceholer, ManagedCompletion: c.ManagedCompletion,
		Exposure: c.Exposure, IngressClass: c.IngressClass, IngressTLS: c.IngressTLS,
		BuildEngine: c.BuildEngine, CAVariable: c.CAVariable, RunnerTag: c.RunnerTag, RunnerServiceAccount: c.RunnerServiceAccount,
		RunnerSCC: c.RunnerSCC, BuilderSetup: c.BuilderSetup, SecretMode: c.SecretMode, VaultAddress: c.VaultAddress, VaultRole: c.VaultRole,
	}
	if services, err := resolvedServices(c); err == nil && services.Database != nil {
		d.HasDatabase = true
		d.DatabaseClusterName = ServiceResourceName(c.Name, "-db")
		d.DatabaseName = services.Database.DatabaseName
	}
	if d.GitImage == "" {
		d.GitImage = c.BuilderImage
	}
	if d.Exposure == "" {
		d.Exposure = "route"
	}
	if d.IngressTLS == "" {
		d.IngressTLS = "hosts"
	}
	if d.BuildEngine == "" {
		d.BuildEngine = "buildkit"
	}
	if c.VaultPath != "" {
		parts := strings.Split(c.VaultPath, "/")
		d.VaultMount = parts[0]
		d.VaultSecretPath = strings.TrimPrefix(strings.Join(parts[1:], "/"), "data/")
	}
	d.ExposureLabels = parseMetadataLines(c.ExposureLabels)
	d.ExposureAnnotations = parseMetadataLines(c.ExposureAnnotations)
	return d
}

func parseMetadataLines(text string) []KV {
	items, _ := parseMetadata(text, false)
	out := make([]KV, 0, len(items))
	for _, kv := range items {
		out = append(out, KV{kv[0], kv[1]})
	}
	return out
}

type limitedBuffer struct {
	bytes.Buffer
	max int
}

var errOutputLimit = errors.New("rendered output exceeds 1 MiB")

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.Len()+len(p) > l.max {
		return 0, errOutputLimit
	}
	return l.Buffer.Write(p)
}

// parseTemplate parses one file and enforces the sandbox rules.
func parseTemplate(name, text string) (*template.Template, error) {
	if len(text) > maxTemplateFile {
		return nil, fmt.Errorf("%s is larger than 128 KiB", name)
	}
	if strings.ContainsRune(text, 0) {
		return nil, fmt.Errorf("%s contains a NUL character", name)
	}
	t, err := template.New(name).Funcs(templateFuncs).Option("missingkey=error").Parse(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if len(t.Templates()) > 1 {
		return nil, fmt.Errorf("%s: define and block are not allowed", name)
	}
	if t.Tree != nil {
		if err := checkNodes(t.Tree.Root); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	return t, nil
}

func checkNodes(n parse.Node) error {
	switch n := n.(type) {
	case nil:
		return nil
	case *parse.ListNode:
		if n == nil {
			return nil
		}
		for _, c := range n.Nodes {
			if err := checkNodes(c); err != nil {
				return err
			}
		}
	case *parse.TemplateNode:
		return errors.New("template includes are not allowed")
	case *parse.IfNode:
		return checkBranch(&n.BranchNode, false)
	case *parse.WithNode:
		return checkBranch(&n.BranchNode, false)
	case *parse.RangeNode:
		return checkBranch(&n.BranchNode, true)
	}
	return nil
}

func checkBranch(b *parse.BranchNode, isRange bool) error {
	if isRange && b.Pipe != nil {
		for _, cmd := range b.Pipe.Cmds {
			for _, arg := range cmd.Args {
				if _, ok := arg.(*parse.NumberNode); ok {
					return errors.New("range over a number is not allowed")
				}
			}
		}
	}
	if err := checkNodes(b.List); err != nil {
		return err
	}
	if b.ElseList != nil {
		return checkNodes(b.ElseList)
	}
	return nil
}

// RenderFiles renders required files and optional manifests for a configuration.
func RenderFiles(files map[string]string, d TemplateData) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range OrderedTemplatePaths(files) {
		text, ok := files[p]
		if !ok {
			return nil, errors.New("template file " + p + " is missing")
		}
		t, err := parseTemplate(p, text)
		if err != nil {
			return nil, err
		}
		var buf limitedBuffer
		buf.max = maxRenderedBytes
		if err := t.Execute(&buf, d); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out[p] = buf.String()
	}
	return out, nil
}

// CheckTemplateFiles verifies the file set and syntax without rendering.
func CheckTemplateFiles(files map[string]string) error {
	if len(files) > len(TemplateFilePaths)+10 {
		return errors.New("a template may contain at most 10 additional manifest files")
	}
	for p, text := range files {
		if isRequiredTemplatePath(p) {
			continue
		}
		if !extraManifestPath.MatchString(p) {
			return fmt.Errorf("unsupported template path %q; additional manifests must use .justcd/manifests/<name>.yaml", p)
		}
		if _, err := parseTemplate(p, text); err != nil {
			return err
		}
	}
	for _, p := range TemplateFilePaths {
		text, ok := files[p]
		if !ok {
			return errors.New("template file " + p + " is missing")
		}
		if strings.TrimSpace(text) == "" && p != FileReadme && p != FileDockerignore {
			return errors.New(p + " must not be empty")
		}
		if _, err := parseTemplate(p, text); err != nil {
			return err
		}
	}
	return nil
}

var extraManifestPath = regexp.MustCompile(`^\.justcd/manifests/[a-z0-9][a-z0-9-]{0,62}\.yaml$`)

func isRequiredTemplatePath(p string) bool {
	for _, required := range TemplateFilePaths {
		if p == required {
			return true
		}
	}
	return false
}

// OrderedTemplatePaths keeps required files first and optional manifests sorted.
func OrderedTemplatePaths(files map[string]string) []string {
	out := append([]string{}, TemplateFilePaths...)
	var extra []string
	for p := range files {
		if !isRequiredTemplatePath(p) {
			extra = append(extra, p)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// ParseTemplateParameters parses bounded, non-secret key=value profile values.
func ParseTemplateParameters(text string) (map[string]string, error) {
	if len(text) > 8192 {
		return nil, errors.New("template parameters exceed 8 KiB")
	}
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`).MatchString(key) || strings.ContainsRune(value, 0) {
			return nil, errors.New("template parameters must be key=value lines with alphanumeric keys starting with a letter")
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("duplicate template parameter %s", key)
		}
		out[key] = value
	}
	if len(out) > 50 {
		return nil, errors.New("at most 50 template parameters are allowed")
	}
	return out, nil
}

// TemplateFileSet holds resolved custom template files. It is a pointer in
// Config so configurations stay comparable.
type TemplateFileSet struct{ Files map[string]string }

// filesFor returns the template files a configuration renders with: the pinned
// custom version when the caller resolved it, else the built-in runtime files.
func filesFor(c Config) (map[string]string, error) {
	if c.TemplateFiles != nil {
		return c.TemplateFiles.Files, nil
	}
	return BuiltinTemplateFiles(c.Runtime)
}

// RenderConfig renders and validates all files for a configuration.
func RenderConfig(c Config, facts SourceFacts) (map[string]string, error) {
	files, err := filesFor(c)
	if err != nil {
		return nil, err
	}
	if err := CheckTemplateFiles(files); err != nil {
		return nil, err
	}
	if _, err := ParseTemplateParameters(c.TemplateParameters); err != nil {
		return nil, err
	}
	out, err := RenderFiles(files, templateData(c, facts))
	if err != nil {
		return nil, err
	}
	// Include optional resources in the single manifest consumed by both build engines
	// and JustCD completion. This also makes additions/removals visible in upgrades.
	for _, p := range OrderedTemplatePaths(files) {
		if isRequiredTemplatePath(p) || strings.TrimSpace(out[p]) == "" {
			continue
		}
		docs, err := parseDocuments(out[p])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		for _, r := range docs {
			if _, ok := extraKinds[r.kind]; !ok {
				return nil, fmt.Errorf("%s: additional manifests may only contain ConfigMap, NetworkPolicy, PodDisruptionBudget or HorizontalPodAutoscaler", p)
			}
		}
		out[FileDeployment] = strings.TrimSpace(out[FileDeployment]) + "\n---\n" + strings.TrimSpace(out[p]) + "\n"
	}
	if len(out[FileDeployment]) > maxRenderedBytes {
		return nil, errOutputLimit
	}
	if err := AddServices(c, out); err != nil {
		return nil, err
	}
	if err := ValidateRendered(c, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ValidateTemplate renders a template for a matrix of representative
// configurations and validates every result, so conditionals that only trigger
// for some settings are checked when the template is saved.
func ValidateTemplate(templateID string, files map[string]string) error {
	if err := CheckTemplateFiles(files); err != nil {
		return err
	}
	base, err := SampleConfig(templateID)
	if err != nil {
		return err
	}
	base.TemplateFiles = &TemplateFileSet{Files: files}
	seen := map[string]bool{}
	var problems []string
	for _, managed := range []bool{true, false} {
		for _, engine := range []string{"buildkit", "podman"} {
			for _, vault := range []bool{false, true} {
				for _, ca := range []bool{false, true} {
					for _, exposure := range []string{"", "route", "ingress"} {
						for _, renv := range []bool{false, true} {
							c := base
							c.ManagedCompletion, c.BuildEngine = managed, engine
							if vault {
								c.SecretMode, c.VaultAddress, c.VaultRole, c.VaultPath = "vault", "https://vault.example.com", "publisher", "secret/data/team/registry"
							}
							if ca {
								c.CAVariable, c.RunnerTag, c.RunnerServiceAccount, c.RunnerSCC, c.BuilderSetup = "ROOT_CA", "ocp", "runner-sa", "runner-scc", "/home/user/setup.sh"
							}
							if exposure == "" {
								c.Host = ""
							} else {
								c.Exposure = exposure
								c.ExposureLabels, c.ExposureAnnotations = "router=external", "example.com/note=hello"
								if exposure == "ingress" {
									c.IngressClass, c.IngressTLS = "openshift-default", "none"
								}
							}
							if _, err := RenderConfig(c, SourceFacts{HasRenvLock: renv}); err != nil {
								msg := err.Error()
								if !seen[msg] {
									seen[msg] = true
									problems = append(problems, msg)
								}
							}
						}
					}
				}
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		if len(problems) > 5 {
			problems = problems[:5]
		}
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// SampleConfig is a representative configuration for previews and validation.
func SampleConfig(templateID string) (Config, error) {
	for _, t := range Templates() {
		if t.ID != templateID {
			continue
		}
		c := Config{
			TemplateID: t.ID, Name: "sample-app", Runtime: t.Runtime, EntryPoint: t.EntryPoint, Namespace: "sample-namespace",
			Host: "sample-app.apps.example.com", BaseImage: "registry.example.com/base/runtime:1", BuilderImage: "registry.example.com/base/builder:1",
			RegistryImage: "registry.example.com/apps/sample-app", SecretMode: "variables", ManagedCompletion: true,
		}
		if t.Runtime == "static" || t.Runtime == "python-static" {
			c.WebServerImage = "registry.example.com/base/nginx:1"
		}
		if t.Runtime == "python-static" {
			c.BuildCommand, c.OutputDir = "python "+t.EntryPoint, "dist"
		}
		return c, nil
	}
	return Config{}, errors.New("unknown application template " + templateID)
}
