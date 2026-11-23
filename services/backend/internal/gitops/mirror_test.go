package gitops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/store"
)

// fetchThroughMirror is exercised with a local file:// remote; Fetch itself
// only accepts HTTPS and SSH URLs.
func TestFetchThroughMirrorReusesObjectsAndKeepsCheckoutUsable(t *testing.T) {
	root := t.TempDir()
	env := cleanGitEnvironment(os.Environ(), root)
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	git(work, "init", "-b", "main")
	git(work, "config", "user.email", "fixture@example.invalid")
	git(work, "config", "user.name", "Fixture")
	write := func(name, value string) {
		t.Helper()
		p := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("app/deployment.yaml", "v1")
	git(work, "add", ".")
	git(work, "commit", "-m", "one")
	first := git(work, "rev-parse", "HEAD")
	remote := filepath.Join(root, "remote.git")
	git(root, "clone", "--bare", work, remote)
	source := store.GitSource{RepositoryURL: "file://" + remote}
	cache := filepath.Join(root, "cache")
	ctx := context.Background()

	checkout := func(name, revision string) (string, string) {
		t.Helper()
		path := filepath.Join(root, name)
		commit, err := fetchThroughMirror(ctx, cache, env, source, revision, path)
		if err != nil {
			t.Fatal(err)
		}
		return path, commit
	}
	path, commit := checkout("first", "main")
	if commit != first {
		t.Fatalf("got commit %s, want %s", commit, first)
	}
	if data, _ := os.ReadFile(filepath.Join(path, "app/deployment.yaml")); string(data) != "v1" {
		t.Fatalf("checkout has %q", data)
	}
	if got := git(path, "config", "remote.origin.url"); got != source.RepositoryURL {
		t.Fatalf("origin must be the real remote, got %s", got)
	}
	if git(path, "config", "remote.origin.partialclonefilter") != "blob:none" || git(path, "config", "remote.origin.promisor") != "true" {
		t.Fatal("checkout lost its partial-clone settings")
	}
	mirror := filepath.Join(cache, mirrorKey(source)+".git")
	if _, err := os.Stat(filepath.Join(mirror, "HEAD")); err != nil {
		t.Fatalf("mirror was not kept: %v", err)
	}

	write("app/deployment.yaml", "v2")
	write("app/service.yaml", "new")
	git(work, "add", ".")
	git(work, "commit", "-m", "two")
	second := git(work, "rev-parse", "HEAD")
	git(work, "push", remote, "main")
	path, commit = checkout("second", "main")
	if commit != second {
		t.Fatalf("got commit %s, want %s", commit, second)
	}
	if data, _ := os.ReadFile(filepath.Join(path, "app/deployment.yaml")); string(data) != "v2" {
		t.Fatalf("checkout has %q after an update", data)
	}
	// Checkouts must work like a fresh clone: comparing commits fetches from
	// the real remote.
	c := Checkout{Root: path, Commit: second, env: env}
	paths, err := c.ChangedPaths(ctx, first, second, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, ",") != "app/deployment.yaml,app/service.yaml" {
		t.Fatalf("unexpected changed paths %v", paths)
	}

	// An older commit is still fetched by its id.
	path, commit = checkout("pinned", first)
	if commit != first {
		t.Fatalf("pinned revision resolved to %s", commit)
	}
	if data, _ := os.ReadFile(filepath.Join(path, "app/deployment.yaml")); string(data) != "v1" {
		t.Fatalf("pinned checkout has %q", data)
	}
}

func TestFetchThroughMirrorFallsBackWhenTheMirrorIsDamaged(t *testing.T) {
	root := t.TempDir()
	env := cleanGitEnvironment(os.Environ(), root)
	cache := filepath.Join(root, "cache")
	source := store.GitSource{RepositoryURL: "file://" + filepath.Join(root, "missing.git")}
	mirror := filepath.Join(cache, mirrorKey(source)+".git")
	if err := os.MkdirAll(mirror, 0o700); err != nil {
		t.Fatal(err)
	}
	// A HEAD file without a repository looks reusable but cannot fetch.
	if err := os.WriteFile(filepath.Join(mirror, "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := fetchThroughMirror(context.Background(), cache, env, source, "main", filepath.Join(root, "checkout"))
	if !errors.Is(err, errMirrorUnusable) {
		t.Fatalf("a damaged mirror must fall back to a direct fetch, got %v", err)
	}
	if _, err := os.Stat(mirror); !os.IsNotExist(err) {
		t.Fatal("damaged mirror was not removed")
	}
	// Without a mirror to blame, a remote failure is reported as such.
	_, err = fetchThroughMirror(context.Background(), cache, env, source, "main", filepath.Join(root, "checkout-2"))
	var fetch *FetchError
	if !errors.As(err, &fetch) {
		t.Fatalf("remote failure must be a FetchError, got %v", err)
	}
}

func TestMirrorKeySeparatesCredentials(t *testing.T) {
	a, b := "credential-a", "credential-b"
	url := "https://git.example.com/team/app.git"
	if mirrorKey(store.GitSource{RepositoryURL: url, CredentialID: &a}) == mirrorKey(store.GitSource{RepositoryURL: url, CredentialID: &b}) {
		t.Fatal("different credentials must not share a mirror")
	}
	if mirrorKey(store.GitSource{RepositoryURL: url}) == mirrorKey(store.GitSource{RepositoryURL: url, CredentialID: &a}) {
		t.Fatal("anonymous and authenticated fetches must not share a mirror")
	}
}

func TestConfigureCache(t *testing.T) {
	t.Cleanup(func() { ConfigureCache("") })
	ConfigureCache("off")
	if cacheDir() != "" {
		t.Fatal("off must disable the cache")
	}
	ConfigureCache("/var/cache/justcd/git")
	if cacheDir() != "/var/cache/justcd/git" {
		t.Fatalf("got %s", cacheDir())
	}
	ConfigureCache("")
	if !strings.HasPrefix(cacheDir(), os.TempDir()) {
		t.Fatalf("default cache must live in the temporary directory, got %s", cacheDir())
	}
}
