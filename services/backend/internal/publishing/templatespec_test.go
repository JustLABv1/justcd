package publishing

import (
	"strings"
	"testing"
)

func customServer() Template {
	return Template{ID: "dash-app", Name: "Dash", Runtime: RuntimeServer, EntryPoint: "app.py", Port: 8050, Custom: true,
		Layout: []LayoutRule{
			{Message: "Dash needs app.py", AnyOf: [][]string{{"{entry}"}}},
			{AnyOf: [][]string{{"requirements.txt"}, {"pyproject.toml"}}},
		}}
}

func TestValidateCustomTemplate(t *testing.T) {
	if err := ValidateCustomTemplate(customServer()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Template){
		"builtin id":   func(x *Template) { x.ID = "shiny" },
		"short id":     func(x *Template) { x.ID = "ab" },
		"uppercase id": func(x *Template) { x.ID = "Dash" },
		"no name":      func(x *Template) { x.Name = " " },
		"bad runtime":  func(x *Template) { x.Runtime = "ruby" },
		"bad entry":    func(x *Template) { x.EntryPoint = "../app.py" },
		"bad port":     func(x *Template) { x.Port = 70000 },
		"traversal":    func(x *Template) { x.Layout = []LayoutRule{{AnyOf: [][]string{{"../secret"}}}} },
		"empty rule":   func(x *Template) { x.Layout = []LayoutRule{{}} },
		"absolute":     func(x *Template) { x.Layout = []LayoutRule{{AnyOf: [][]string{{"/etc/passwd"}}}} },
	} {
		x := customServer()
		mutate(&x)
		if ValidateCustomTemplate(x) == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestCustomLayoutDecidesUploads(t *testing.T) {
	tpl := customServer()
	c := SampleConfigFor(tpl)
	c.Layout = &TemplateLayout{Rules: tpl.Layout}
	files := func(names ...string) []File {
		out := []File{}
		for _, n := range names {
			out = append(out, File{Path: "source/" + n})
		}
		return out
	}
	if _, _, err := NormalizeSource(c, files("app.py", "requirements.txt")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NormalizeSource(c, files("app.py", "pyproject.toml")); err != nil {
		t.Fatal("any alternative should pass:", err)
	}
	if _, _, err := NormalizeSource(c, files("main.py", "requirements.txt")); err == nil || err.Error() != "Dash needs app.py" {
		t.Fatalf("custom message expected, got %v", err)
	}
	if _, _, err := NormalizeSource(c, files("app.py")); err == nil || !strings.Contains(err.Error(), "requirements.txt or pyproject.toml") {
		t.Fatalf("missing alternatives should be listed, got %v", err)
	}
	// One enclosing folder is still removed.
	if _, root, err := NormalizeSource(c, files("proj/app.py", "proj/requirements.txt")); err != nil || root != "proj" {
		t.Fatalf("enclosing folder: root=%q err=%v", root, err)
	}
	// {entry} follows the application's entry point.
	c.EntryPoint = "src/app.py"
	if _, _, err := NormalizeSource(c, files("src/app.py", "requirements.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestServerStarterTemplateRendersWithPort(t *testing.T) {
	tpl := customServer()
	files, err := BuiltinTemplateFiles(tpl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTemplateFor(tpl, files); err != nil {
		t.Fatal(err)
	}
	c := SampleConfigFor(tpl)
	c.TemplateFiles = &TemplateFileSet{Files: files}
	out, err := RenderConfig(c, SourceFacts{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out[FileDockerfile], "EXPOSE 8050") || !strings.Contains(out[FileDeployment], "containerPort: 8050") {
		t.Fatal("custom port is not rendered")
	}
}

func TestResolveWithCustomTemplate(t *testing.T) {
	tpl := customServer()
	lookup := func(id string) (Template, bool) {
		if id == tpl.ID {
			return tpl, true
		}
		return BuiltinLookup(id)
	}
	s := ProfileSettings{TemplateID: tpl.ID, GitLabURL: "https://gitlab.example.com", Group: "team/apps", ClusterID: "c", Namespace: "ns",
		BaseImage: "registry.example.com/base:1", BuilderImage: "registry.example.com/builder:1", RegistryPrefix: "registry.example.com/apps", SecretMode: "variables"}
	if _, err := s.ResolveWith(lookup, "demo", ""); err == nil {
		t.Fatal("a custom template needs a pinned version")
	}
	s.TemplateVersion = 1
	c, err := s.ResolveWith(lookup, "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Runtime != RuntimeServer || c.Port != 8050 || c.EntryPoint != "app.py" || c.Layout == nil {
		t.Fatalf("custom template not applied: %+v", c)
	}
	if _, err := s.Resolve("demo", ""); err == nil {
		t.Fatal("built-in lookup must not know custom templates")
	}
}
