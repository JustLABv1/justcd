package publishing

import (
	"strings"
	"testing"
)

const ingressPolicyTemplate = `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ .Name }}-ingress
  namespace: {{ .Namespace }}
spec:
  podSelector:
    matchLabels:
      app: {{ .Name }}
  policyTypes: [Ingress]
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: {{ quote (default "ingress-system" (index .Parameters "ingressNamespace")) }}
      ports:
        - protocol: TCP
          port: {{ .Port }}
`

func TestOptionalManifestGeneration(t *testing.T) {
	files, _ := BuiltinFilesForTemplate("shiny")
	files[".justcd/manifests/network-policy.yaml"] = ingressPolicyTemplate
	if err := ValidateTemplate("shiny", files); err != nil {
		t.Fatal(err)
	}
	c := config()
	c.TemplateFiles = &TemplateFileSet{Files: files}
	c.TemplateParameters = "ingressNamespace=platform-router"
	out, err := Generate(c, goldenSource([]string{"app.R", "packages.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	var manifest string
	for _, f := range out {
		if f.Path == FileDeployment {
			manifest = f.Content
		}
	}
	for _, want := range []string{"kind: NetworkPolicy", `kubernetes.io/metadata.name: "platform-router"`, "port: 3838"} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("missing %q in generated deployment", want)
		}
	}
	// Removing an optional file removes it from the managed manifest on the next build.
	delete(files, ".justcd/manifests/network-policy.yaml")
	rendered, err := RenderConfig(c, SourceFacts{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered[FileDeployment], "NetworkPolicy") {
		t.Fatal("removed policy is still rendered")
	}
}

func TestOptionalManifestGuardrails(t *testing.T) {
	for name, edit := range map[string]func(map[string]string){
		"path traversal": func(f map[string]string) { f[".justcd/manifests/../policy.yaml"] = ingressPolicyTemplate },
		"nested path":    func(f map[string]string) { f[".justcd/manifests/sub/policy.yaml"] = ingressPolicyTemplate },
		"whole namespace": func(f map[string]string) {
			f[".justcd/manifests/policy.yaml"] = strings.Replace(ingressPolicyTemplate, "matchLabels:\n      app: {{ .Name }}", "matchLabels: {}", 1)
		},
		"foreign namespace": func(f map[string]string) {
			f[".justcd/manifests/policy.yaml"] = strings.Replace(ingressPolicyTemplate, "namespace: {{ .Namespace }}", "namespace: other", 1)
		},
		"invalid spec": func(f map[string]string) {
			f[".justcd/manifests/policy.yaml"] = strings.Replace(ingressPolicyTemplate, "policyTypes: [Ingress]", "policyTypes: [Invalid]", 1)
		},
		"invalid port": func(f map[string]string) {
			f[".justcd/manifests/policy.yaml"] = strings.Replace(ingressPolicyTemplate, "port: {{ .Port }}", "port: 70000", 1)
		},
		"duplicate identity": func(f map[string]string) {
			f[".justcd/manifests/a.yaml"] = ingressPolicyTemplate
			f[".justcd/manifests/b.yaml"] = ingressPolicyTemplate
		},
		"unknown field": func(f map[string]string) {
			f[".justcd/manifests/policy.yaml"] = strings.Replace(ingressPolicyTemplate, "policyTypes:", "typo:", 1)
		},
		"extra workload": func(f map[string]string) { f[".justcd/manifests/extra.yaml"] = f[FileDeployment] },
	} {
		t.Run(name, func(t *testing.T) {
			f, _ := BuiltinFilesForTemplate("shiny")
			edit(f)
			c := config()
			c.TemplateFiles = &TemplateFileSet{Files: f}
			if _, err := RenderConfig(c, SourceFacts{}); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestTemplateParameters(t *testing.T) {
	for _, text := range []string{"not a pair", "a=x\na=y", "1key=x", "key=\x00", strings.Repeat("x", 8193)} {
		if _, err := ParseTemplateParameters(text); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	parsed, err := ParseTemplateParameters("\n ingressNamespace = router \n label = a=b\n")
	if err != nil || parsed["ingressNamespace"] != "router" || parsed["label"] != "a=b" {
		t.Fatal(parsed, err)
	}
	// Profiles preserve parameters in resolved configs; defaults remain empty.
	s := ProfileSettings{TemplateID: "shiny", Namespace: "analytics", BaseImage: "registry.example.com/base:1", BuilderImage: "registry.example.com/builder:1", RegistryPrefix: "registry.example.com/apps", SecretMode: "variables", TemplateParameters: "ingressNamespace=router"}
	c, err := s.Resolve("test-app", "")
	if err != nil || c.TemplateParameters != s.TemplateParameters {
		t.Fatal(c, err)
	}
}
