package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeRepositoryPathAllowsCheckoutRootAlias(t *testing.T) {
	parent := t.TempDir()
	realBase := filepath.Join(parent, "real")
	root := filepath.Join(realBase, "checkout")
	manifest := filepath.Join(root, "envs", "demo", "dev", "ntfy")
	if err := os.MkdirAll(manifest, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(realBase, alias); err != nil {
		t.Fatal(err)
	}
	got, err := safeRepositoryPath(filepath.Join(alias, "checkout"), "envs/demo/dev/ntfy")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("resolved path = %q, want %q", got, want)
	}
}

func TestSafeRepositoryPathStillRejectsSymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "checkout")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "external")); err != nil {
		t.Fatal(err)
	}
	if _, err := safeRepositoryPath(root, "external"); err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("expected symlink escape rejection, got %v", err)
	}
	if _, err := safeRepositoryPath(root, "../outside"); err == nil || !strings.Contains(err.Error(), "escapes the repository") {
		t.Fatalf("expected lexical escape rejection, got %v", err)
	}
}
