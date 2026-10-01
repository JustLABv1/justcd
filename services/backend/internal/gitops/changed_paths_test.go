package gitops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangedPathsUsesPRMergeBaseAndCompleteNames(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = cleanGitEnvironment(os.Environ(), root)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	git(work, "init", "-b", "main")
	git(work, "config", "user.email", "fixture@example.invalid")
	git(work, "config", "user.name", "Fixture")
	write := func(name, value string) {
		t.Helper()
		p := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("apps/old.yaml", "old")
	git(work, "add", ".")
	git(work, "commit", "-m", "base")
	base := git(work, "rev-parse", "HEAD")
	git(work, "checkout", "-b", "feature")
	git(work, "mv", "apps/old.yaml", "apps/new.yaml")
	write("config/file with spaces\nand newline.yaml", "test")
	for i := 0; i < 450; i++ {
		write(fmt.Sprintf("bulk/file-%d.yaml", i), "data")
	}
	git(work, "add", ".")
	git(work, "commit", "-m", "PR")
	head := git(work, "rev-parse", "HEAD")
	git(work, "checkout", "main")
	write("target-only.yaml", "not a PR change")
	git(work, "add", ".")
	git(work, "commit", "-m", "target advances")
	target := git(work, "rev-parse", "HEAD")
	bare := filepath.Join(root, "remote.git")
	git(root, "clone", "--bare", work, bare)
	checkout := filepath.Join(root, "checkout")
	git(root, "clone", bare, checkout)
	c := Checkout{Root: checkout, Commit: head, env: cleanGitEnvironment(os.Environ(), root)}
	for _, test := range []struct {
		name, base string
		mergeBase  bool
	}{{"GitHub target tip", target, false}, {"GitLab diff base", base, true}, {"GitHub restores shallow history", target, false}} {
		t.Run(test.name, func(t *testing.T) {
			paths, err := c.ChangedPaths(context.Background(), test.base, head, test.mergeBase)
			if err != nil {
				t.Fatal(err)
			}
			set := map[string]bool{}
			for _, p := range paths {
				set[p] = true
			}
			if len(paths) != 453 || !set["apps/old.yaml"] || !set["apps/new.yaml"] || !set["config/file with spaces\nand newline.yaml"] || set["target-only.yaml"] {
				t.Fatalf("incomplete or incorrect PR paths: count=%d", len(paths))
			}
		})
	}
	paths, err := c.ChangedPaths(context.Background(), head, head, true)
	if err != nil || len(paths) != 0 {
		t.Fatalf("empty PR diff: %v %v", paths, err)
	}
	if _, err := c.ChangedPaths(context.Background(), "--upload-pack=bad", head, true); err == nil {
		t.Fatal("unsafe base accepted")
	}
	if _, err := c.ChangedPaths(context.Background(), base, base, true); err == nil {
		t.Fatal("changed head accepted")
	}
}
