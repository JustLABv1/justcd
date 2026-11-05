package publishing

import (
	"reflect"
	"strings"
	"testing"
)

func editedFiles(t *testing.T, templateID string, edit func(map[string]string)) map[string]string {
	t.Helper()
	files, err := BuiltinFilesForTemplate(templateID)
	if err != nil {
		t.Fatal(err)
	}
	edit(files)
	return files
}

func TestBuiltinTemplatesPassValidation(t *testing.T) {
	for _, tpl := range Templates() {
		files, err := BuiltinFilesForTemplate(tpl.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != len(TemplateFilePaths) {
			t.Fatalf("%s: expected %d template files, got %d", tpl.ID, len(TemplateFilePaths), len(files))
		}
		if err := ValidateTemplate(tpl.ID, files); err != nil {
			t.Fatalf("built-in template %s fails its own validation: %v", tpl.ID, err)
		}
	}
}

func TestEveryPlaceholderIsDocumented(t *testing.T) {
	ty := reflect.TypeOf(TemplateData{})
	for i := 0; i < ty.NumField(); i++ {
		if placeholderDocs[ty.Field(i).Name] == "" {
			t.Errorf("placeholder %s has no description", ty.Field(i).Name)
		}
	}
}

func TestCustomTemplateIsRendered(t *testing.T) {
	files := editedFiles(t, "shiny", func(f map[string]string) {
		f[FileDeployment] = strings.Replace(f[FileDeployment], "  replicas: 1\n", "  replicas: 3\n  revisionHistoryLimit: 2\n", 1)
		f[FileDeployment] = strings.Replace(f[FileDeployment], "      labels: {app: {{ .Name }}}\n", "      labels: {app: {{ .Name }}, cost-center: \"4711\"}\n", 1)
		f[FileDeployment] += "---\napiVersion: networking.k8s.io/v1\nkind: NetworkPolicy\nmetadata:\n  name: {{ .Name }}-allow-router\n  namespace: {{ .Namespace }}\nspec:\n  podSelector: {matchLabels: {app: {{ .Name }}}}\n  policyTypes: [Ingress]\n"
		f[FileDockerfile] = strings.Replace(f[FileDockerfile], "ENV HOME=/tmp", "ENV HOME=/tmp\nENV TZ=Europe/Berlin", 1)
	})
	c := config()
	c.Name = "statistics"
	c.TemplateFiles = &TemplateFileSet{Files: files}
	out, err := Generate(c, goldenSource([]string{"app.R", "packages.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	var manifest, docker string
	for _, f := range out {
		switch f.Path {
		case FileDeployment:
			manifest = f.Content
		case FileDockerfile:
			docker = f.Content
		}
	}
	for _, want := range []string{"replicas: 3", "revisionHistoryLimit: 2", "cost-center", "kind: NetworkPolicy", "name: statistics-allow-router", "image: JUSTCD_IMAGE_DIGEST"} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest missing %q:\n%s", want, manifest)
		}
	}
	if !strings.Contains(docker, "ENV TZ=Europe/Berlin") {
		t.Fatal("custom Dockerfile was not used")
	}
}

func TestCustomTemplateGuardrails(t *testing.T) {
	deploymentEdits := map[string]func(string) string{
		"privileged": func(s string) string {
			return strings.Replace(s, "allowPrivilegeEscalation: false", "privileged: true", 1)
		},
		"escalation": func(s string) string {
			return strings.Replace(s, "allowPrivilegeEscalation: false", "allowPrivilegeEscalation: true", 1)
		},
		"root": func(s string) string {
			return strings.Replace(s, "allowPrivilegeEscalation: false", "allowPrivilegeEscalation: false, runAsUser: 0", 1)
		},
		"capabilities": func(s string) string {
			return strings.Replace(s, "capabilities: {drop: [ALL]}", "capabilities: {add: [NET_ADMIN]}", 1)
		},
		"host network": func(s string) string {
			return strings.Replace(s, "      automountServiceAccountToken: false\n", "      automountServiceAccountToken: false\n      hostNetwork: true\n", 1)
		},
		"service token": func(s string) string {
			return strings.Replace(s, "automountServiceAccountToken: false", "automountServiceAccountToken: true", 1)
		},
		"service account": func(s string) string {
			return strings.Replace(s, "      automountServiceAccountToken: false\n", "      automountServiceAccountToken: false\n      serviceAccountName: admin\n", 1)
		},
		"hostPath": func(s string) string {
			return strings.Replace(s, "      containers:\n", "      volumes: [{name: x, hostPath: {path: /}}]\n      containers:\n", 1)
		},
		"app image": func(s string) string {
			return strings.Replace(s, "image: {{ .ImagePlaceholder }}", "image: evil.example.com/x:1", 1)
		},
		"other namespace": func(s string) string {
			return strings.Replace(s, "  name: {{ .Name }}\n  namespace: {{ .Namespace }}\nspec:\n  selector", "  name: {{ .Name }}\n  namespace: kube-system\nspec:\n  selector", 1)
		},
		"foreign sidecar": func(s string) string {
			return strings.Replace(s, "      containers:\n", "      containers:\n        - {name: proxy, image: \"evil.example.com/proxy:1\"}\n", 1)
		},
		"secret kind": func(s string) string {
			return s + "---\napiVersion: v1\nkind: Secret\nmetadata: {name: x, namespace: {{ .Namespace }}}\n"
		},
		"cluster role": func(s string) string {
			return s + "---\napiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRoleBinding\nmetadata: {name: x, namespace: {{ .Namespace }}}\n"
		},
		"load balancer": func(s string) string {
			return strings.Replace(s, "spec:\n  selector: {app: {{ .Name }}}", "spec:\n  type: LoadBalancer\n  selector: {app: {{ .Name }}}", 1)
		},
		"selector change": func(s string) string {
			return strings.Replace(s, "matchLabels: {app: {{ .Name }}}", "matchLabels: {app: other}", 1)
		},
		"second deployment": func(s string) string { return s + "---\n" + s },
		"invalid yaml":      func(s string) string { return s + "\n  : [" },
	}
	for name, edit := range deploymentEdits {
		files := editedFiles(t, "shiny", func(f map[string]string) { f[FileDeployment] = edit(f[FileDeployment]) })
		if err := ValidateTemplate("shiny", files); err == nil {
			t.Errorf("%s: unsafe manifest accepted", name)
		}
	}
	for name, edit := range map[string]func(map[string]string){
		"no digest artifact": func(f map[string]string) {
			f[FilePipeline] = strings.ReplaceAll(f[FilePipeline], ".justcd-build/image-digest", ".other")
		},
		"no FROM":        func(f map[string]string) { f[FileDockerfile] = "RUN true\n" },
		"empty pipeline": func(f map[string]string) { f[FilePipeline] = "" },
		"missing file":   func(f map[string]string) { delete(f, FileReadme) },
		"extra file":     func(f map[string]string) { f["evil.sh"] = "x" },
		"syntax error":   func(f map[string]string) { f[FileDockerfile] = "FROM {{ .BaseImage" },
		"unknown field":  func(f map[string]string) { f[FileDockerfile] = "FROM {{ .Missing }}\n" },
		"define":         func(f map[string]string) { f[FileDockerfile] = "{{ define \"x\" }}a{{ end }}FROM {{ .BaseImage }}\n" },
		"include":        func(f map[string]string) { f[FileDockerfile] = "{{ template \"x\" }}FROM {{ .BaseImage }}\n" },
		"block":          func(f map[string]string) { f[FileDockerfile] = "{{ block \"x\" . }}a{{ end }}FROM {{ .BaseImage }}\n" },
		"range number": func(f map[string]string) {
			f[FileDockerfile] = "FROM {{ .BaseImage }}\n{{ range 1000000000 }}{{ end }}"
		},
		"huge output": func(f map[string]string) {
			f[FileDockerfile] = "FROM x\n{{ range $i := .ExposureLabels }}{{ end }}" + strings.Repeat("{{ .BaseImage }}", 9000)
		},
		"oversized file": func(f map[string]string) { f[FileDockerfile] = "FROM x\n" + strings.Repeat("# comment\n", 20000) },
	} {
		files := editedFiles(t, "shiny", edit)
		if err := ValidateTemplate("shiny", files); err == nil {
			t.Errorf("%s: unsafe template accepted", name)
		}
	}
	sidecar := editedFiles(t, "shiny", func(f map[string]string) {
		f[FileDeployment] = strings.Replace(f[FileDeployment], "      containers:\n", "      containers:\n        - {name: proxy, image: \"registry.example.com/tools/proxy:1.2\"}\n", 1)
	})
	// Sidecars may come from the profile registry host.
	c, _ := SampleConfig("shiny")
	c.RegistryImage = "registry.example.com/apps/sample-app"
	c.TemplateFiles = &TemplateFileSet{Files: sidecar}
	if _, err := RenderConfig(c, SourceFacts{}); err != nil {
		t.Fatalf("sidecar from the profile registry rejected: %v", err)
	}
}

func TestTemplateRenderingIsLimitedAndDeterministic(t *testing.T) {
	c, _ := SampleConfig("shiny")
	a, err := RenderConfig(c, SourceFacts{HasRenvLock: true})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := RenderConfig(c, SourceFacts{HasRenvLock: true})
	if !reflect.DeepEqual(a, b) {
		t.Fatal("rendering is not deterministic")
	}
	files := editedFiles(t, "shiny", func(f map[string]string) { f[FileReadme] = strings.Repeat("{{ .StaticOutputScript }}", 3000) })
	if _, err := RenderFiles(files, templateData(c, SourceFacts{})); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("expected output limit error, got %v", err)
	}
}
