package repoconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validDefinition = `apiVersion: justcd.io/v1alpha1
kind: Application
metadata:
  name: shop
spec:
  source:
    renderer: kustomize
    path: .
  destination:
    cluster: production
    namespace: shop
`

func writeDefinition(t *testing.T, root, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, file)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, file), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestDiscoverPathsDefaultsAndSemanticHash(t *testing.T) {
	root := t.TempDir()
	writeDefinition(t, root, "apps/shop/justcd.yaml", validDefinition)
	first, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Spec.Source.Path != "apps/shop" || first[0].File != "apps/shop/justcd.yaml" || first[0].Spec.SyncPolicy != "manual" || first[0].Spec.PollSeconds != 300 {
		t.Fatalf("unexpected discovery: %+v", first)
	}
	writeDefinition(t, root, "apps/shop/justcd.yaml", "# harmless comment\n"+validDefinition)
	second, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Hash != second[0].Hash {
		t.Fatal("comments must not change semantic configuration hash")
	}
}
func TestDiscoverRejectsInvalidSnapshot(t *testing.T) {
	cases := map[string]string{
		"unknown field":              strings.Replace(validDefinition, "renderer:", "typo: true\n    renderer:", 1),
		"invalid policy":             validDefinition + "  syncPolicy: dangerous\n",
		"duplicate YAML key":         validDefinition + "  source: {}\n",
		"extra document":             validDefinition + "---\n" + validDefinition,
		"path escape":                strings.Replace(validDefinition, "path: .", "path: ../../..", 1),
		"absolute path":              strings.Replace(validDefinition, "path: .", "path: /tmp", 1),
		"renderer":                   strings.Replace(validDefinition, "kustomize", "unknown", 1),
		"helm settings on kustomize": strings.Replace(validDefinition, "path: .", "path: .\n    valuesFiles: [values.yaml]", 1),
		"bad namespace":              strings.Replace(validDefinition, "namespace: shop", "namespace: Bad", 1),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeDefinition(t, root, "apps/justcd.yaml", content)
			if _, err := Discover(root); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
func TestDiscoverDuplicateNamesAndSymlinks(t *testing.T) {
	t.Run("duplicate names", func(t *testing.T) {
		root := t.TempDir()
		writeDefinition(t, root, "a/justcd.yaml", validDefinition)
		writeDefinition(t, root, "b/justcd.yml", validDefinition)
		if _, err := Discover(root); err == nil {
			t.Fatal("duplicate names accepted")
		}
	})
	t.Run("config symlink", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		writeDefinition(t, outside, "config.yaml", validDefinition)
		if err := os.Symlink(filepath.Join(outside, "config.yaml"), filepath.Join(root, "justcd.yaml")); err != nil {
			t.Fatal(err)
		}
		if _, err := Discover(root); err == nil {
			t.Fatal("symlink config accepted")
		}
	})
	t.Run("source symlink escape", func(t *testing.T) {
		root := t.TempDir()
		writeDefinition(t, root, "justcd.yaml", strings.Replace(validDefinition, "path: .", "path: outside", 1))
		if err := os.Symlink(t.TempDir(), filepath.Join(root, "outside")); err != nil {
			t.Fatal(err)
		}
		if _, err := Discover(root); err == nil {
			t.Fatal("symlink escape accepted")
		}
	})
	t.Run("git metadata ignored", func(t *testing.T) {
		root := t.TempDir()
		writeDefinition(t, root, ".git/justcd.yaml", "bad: data")
		definitions, err := Discover(root)
		if err != nil || len(definitions) != 0 {
			t.Fatalf("Git metadata scanned: %+v %v", definitions, err)
		}
	})
}
func TestDiscoverHelmValuesRelativeToDefinition(t *testing.T) {
	root := t.TempDir()
	writeDefinition(t, root, "apps/values.yaml", "replicas: 2\n")
	writeDefinition(t, root, "apps/chart/Chart.yaml", "name: shop\n")
	writeDefinition(t, root, "apps/justcd.yaml", strings.Replace(strings.Replace(validDefinition, "kustomize", "helm", 1), "path: .", "path: chart\n    valuesFiles: [values.yaml]", 1))
	definitions, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if definitions[0].Spec.Source.Path != "apps/chart" || definitions[0].Spec.Source.ValuesFiles[0] != "apps/values.yaml" {
		t.Fatalf("incorrect relative paths: %+v", definitions)
	}
}

func TestDiscoverThroughSymlinkedCheckoutRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "real", "repository")
	writeDefinition(t, root, "envs/demo/dev/ntfy/justcd.yaml", validDefinition)
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(filepath.Join(parent, "real"), alias); err != nil {
		t.Fatal(err)
	}
	definitions, err := Discover(filepath.Join(alias, "repository"))
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || definitions[0].Spec.Source.Path != "envs/demo/dev/ntfy" {
		t.Fatalf("unexpected resolved path: %+v", definitions)
	}
}

func TestDiscoverCreateNamespaces(t *testing.T) {
	root := t.TempDir()
	writeDefinition(t, root, "justcd.yaml", validDefinition)
	before, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if before[0].Spec.Destination.CreateNamespaces {
		t.Fatal("must default to false")
	}
	writeDefinition(t, root, "justcd.yaml", strings.Replace(validDefinition, "namespace: shop", "namespace: shop\n    createNamespaces: true", 1))
	after, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if !after[0].Spec.Destination.CreateNamespaces || before[0].Hash == after[0].Hash {
		t.Fatal("option not tracked by semantic hash")
	}
}
