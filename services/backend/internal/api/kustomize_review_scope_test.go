package api

import (
	"os"
	"path/filepath"
	"testing"
)

func scopeFile(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestKustomizeReviewDependencies(t *testing.T) {
	root := t.TempDir()
	scopeFile(t, root, "apps/api/kustomization.yaml", "resources:\n- ../../shared/base\ncomponents:\n- ../../shared/component\npatches:\n- path: ../../config/patch.yaml\nreplacements:\n- path: ../../config/replace.yaml\nconfigMapGenerator:\n- name: config\n  files:\n  - settings=../../config/settings.txt\n  envs:\n  - ../../config/settings.env\n")
	scopeFile(t, root, "shared/base/Kustomization", "resources:\n- nested\n")
	scopeFile(t, root, "shared/base/nested/kustomization.json", `{"resources":["deployment.yaml"]}`)
	scopeFile(t, root, "shared/base/nested/deployment.yaml", "kind: Deployment")
	scopeFile(t, root, "shared/component/kustomization.yml", "kind: Component\n")
	for _, p := range []string{"patch.yaml", "replace.yaml", "settings.txt", "settings.env"} {
		scopeFile(t, root, "config/"+p, "test")
	}
	paths, err := kustomizeDependencies(root, []string{"apps/api", ""})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"apps/api/kustomization.yaml", "shared/base/nested/deployment.yaml", "shared/component/kustomization.yml", "config/patch.yaml", "config/replace.yaml", "config/settings.txt", "config/settings.env"} {
		if !pathsAffectApplication([]string{file}, paths) {
			t.Fatalf("missed %s: %v", file, paths)
		}
	}
	for _, file := range []string{"apps/web/kustomization.yaml", "shared/other/deployment.yaml", "config/unrelated.env", "README.md"} {
		if pathsAffectApplication([]string{file}, paths) {
			t.Fatalf("included unrelated %s", file)
		}
	}
	// Removing a referenced input cannot cause a false exclusion.
	if err := os.Remove(filepath.Join(root, "config/settings.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := kustomizeDependencies(root, []string{"apps/api"}); err == nil {
		t.Fatal("missing input must fall back")
	}
}

func TestKustomizeReviewScopeFallback(t *testing.T) {
	for _, config := range []string{"resources:\n- https://example.com/base\n", "generators:\n- plugin.yaml\n", "helmCharts:\n- name: chart\n", "resources:\n- ../../../outside\n", "resources: broken", "resources: ["} {
		t.Run(config, func(t *testing.T) {
			root := t.TempDir()
			scopeFile(t, root, "app/kustomization.yaml", config)
			if _, err := kustomizeDependencies(root, []string{"app"}); err == nil {
				t.Fatal("incomplete scope must fail open")
			}
		})
	}
}

func TestKustomizeScopeIncludesAllOverlaysAndRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	for _, app := range []string{"dev", "qa", "preview"} {
		scopeFile(t, root, app+"/kustomization.yaml", "kind: Kustomization\n")
	}
	paths, err := kustomizeDependencies(root, []string{"dev", "qa", "preview"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"dev/a.yaml", "qa/a.yaml", "preview/a.yaml"} {
		if !pathsAffectApplication([]string{p}, paths) {
			t.Fatalf("missed %s", p)
		}
	}
	if err := os.Symlink(filepath.Join(root, "qa"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := kustomizeDependencies(root, []string{"linked"}); err == nil {
		t.Fatal("symlink must fall back")
	}
}
