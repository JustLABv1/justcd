package gitops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/justlab/justcd/services/backend/internal/store"
)

// Each plan used to clone its Git source from scratch, and a sync fetches the
// same revision again to recheck it. A local bare mirror per repository and
// credential keeps the objects of earlier fetches, so a repeated fetch only
// transfers what changed.
//
// The mirror never replaces the remote: every Fetch authenticates and fetches
// the requested revision from the remote first, so access is checked exactly
// as before and a revoked credential or deleted branch fails as before. The
// checkout is then filled from the mirror over the local transport and keeps
// the remote as origin with the original partial-clone settings.

// errMirrorUnusable means the cache failed for a local reason and the caller
// should fetch directly instead. Remote failures are returned as FetchError.
var errMirrorUnusable = errors.New("Git cache is unusable")

const (
	// mirrorMaxShallowCommits bounds a mirror's growth: each fetched revision
	// adds a shallow boundary. A mirror past it is rebuilt on its next use.
	mirrorMaxShallowCommits = 256
	// mirrorMaxIdle removes mirrors of repositories that are no longer used.
	mirrorMaxIdle       = 7 * 24 * time.Hour
	mirrorSweepInterval = time.Hour
	mirrorRef           = "refs/justcd/fetched"
)

var cacheConfig struct {
	mu        sync.RWMutex
	dir       string
	disabled  bool
	locks     sync.Map // mirror path -> *sync.Mutex
	lastSweep time.Time
}

// ConfigureCache sets the directory for cached Git mirrors. An empty dir uses
// a directory under the OS temporary directory; "off" disables the cache. The
// directory must belong to one backend process.
func ConfigureCache(dir string) {
	cacheConfig.mu.Lock()
	defer cacheConfig.mu.Unlock()
	cacheConfig.disabled = strings.EqualFold(strings.TrimSpace(dir), "off")
	cacheConfig.dir = strings.TrimSpace(dir)
	if cacheConfig.disabled {
		cacheConfig.dir = ""
	}
}

func cacheDir() string {
	cacheConfig.mu.RLock()
	defer cacheConfig.mu.RUnlock()
	if cacheConfig.disabled {
		return ""
	}
	if cacheConfig.dir != "" {
		return cacheConfig.dir
	}
	return filepath.Join(os.TempDir(), "justcd-git-cache")
}

// mirrorKey separates mirrors by repository and credential, so objects
// fetched with one credential are never reused under another.
func mirrorKey(source store.GitSource) string {
	credential := ""
	if source.CredentialID != nil {
		credential = *source.CredentialID
	}
	sum := sha256.Sum256([]byte(source.RepositoryURL + "\x00" + credential))
	return hex.EncodeToString(sum[:16])
}

func mirrorLock(path string) *sync.Mutex {
	lock, _ := cacheConfig.locks.LoadOrStore(path, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func fetchThroughMirror(ctx context.Context, dir string, env []string, source store.GitSource, revision, checkoutPath string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", errMirrorUnusable
	}
	sweepMirrors(dir)
	mirror := filepath.Join(dir, mirrorKey(source)+".git")
	lock := mirrorLock(mirror)
	lock.Lock()
	defer lock.Unlock()
	reused, err := ensureMirror(ctx, env, mirror)
	if err != nil {
		return "", err
	}
	if err := runGit(ctx, env, "-C", mirror, "fetch", "--depth=1", "--no-tags", "--", source.RepositoryURL, revision); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if reused {
			// A damaged mirror can look like a fetch failure. Rebuild it on the
			// next use and fetch directly now; a real remote failure repeats there.
			_ = os.RemoveAll(mirror)
			return "", errMirrorUnusable
		}
		return "", commandFailure(ctx, "fetch requested Git revision", err)
	}
	commitBytes, err := runGitOutput(ctx, env, "-C", mirror, "rev-parse", "--verify", "--end-of-options", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", commandFailure(ctx, "resolve requested Git revision to a commit", err)
	}
	commit := strings.TrimSpace(string(commitBytes))
	if !validCommit(commit) {
		return "", errors.New("Git returned an invalid commit id")
	}
	// The ref keeps fetched objects reachable and is advertised as a "have" on
	// the next fetch, so the remote only sends new objects.
	if err := runGit(ctx, env, "-C", mirror, "update-ref", mirrorRef, commit); err != nil {
		_ = os.RemoveAll(mirror)
		return "", errMirrorUnusable
	}
	if err := checkoutFromMirror(ctx, env, source, mirror, checkoutPath); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		_ = os.RemoveAll(mirror)
		return "", errMirrorUnusable
	}
	now := time.Now()
	_ = os.Chtimes(mirror, now, now)
	if shallowCommits(mirror) > mirrorMaxShallowCommits {
		_ = os.RemoveAll(mirror)
	}
	if err := runGit(ctx, env, "-C", checkoutPath, "checkout", "--detach", commit); err != nil {
		return "", commandFailure(ctx, "check out requested Git revision", err)
	}
	return commit, nil
}

// ensureMirror creates the bare mirror if needed and reports whether an
// existing one was reused.
func ensureMirror(ctx context.Context, env []string, mirror string) (bool, error) {
	if info, err := os.Stat(filepath.Join(mirror, "HEAD")); err == nil && info.Mode().IsRegular() {
		return true, nil
	}
	_ = os.RemoveAll(mirror)
	for _, args := range [][]string{
		{"init", "--quiet", "--bare", "--", mirror},
		{"-C", mirror, "config", "core.hooksPath", "/dev/null"},
		// Fetched objects stay reachable from mirrorRef only; never let an
		// automatic gc run while another checkout reads them.
		{"-C", mirror, "config", "gc.auto", "0"},
	} {
		if err := runGit(ctx, env, args...); err != nil {
			_ = os.RemoveAll(mirror)
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, errMirrorUnusable
		}
	}
	return false, nil
}

// checkoutFromMirror creates the checkout repository the way a blob-less
// clone of the remote would, then takes the fetched commit from the mirror.
func checkoutFromMirror(ctx context.Context, env []string, source store.GitSource, mirror, checkoutPath string) error {
	for _, args := range [][]string{
		{"init", "--quiet", "--", checkoutPath},
		{"-C", checkoutPath, "config", "core.hooksPath", "/dev/null"},
		{"-C", checkoutPath, "config", "core.repositoryformatversion", "1"},
		{"-C", checkoutPath, "remote", "add", "origin", source.RepositoryURL},
		{"-C", checkoutPath, "config", "remote.origin.promisor", "true"},
		{"-C", checkoutPath, "config", "remote.origin.partialclonefilter", "blob:none"},
		{"-C", checkoutPath, "fetch", "--quiet", "--depth=1", "--no-tags", "--", mirror, mirrorRef},
	} {
		if err := runGit(ctx, env, args...); err != nil {
			return err
		}
	}
	return nil
}

func shallowCommits(mirror string) int {
	data, err := os.ReadFile(filepath.Join(mirror, "shallow"))
	if err != nil {
		return 0
	}
	return bytes.Count(data, []byte("\n"))
}

// sweepMirrors removes idle mirrors at most once per mirrorSweepInterval.
// Mirrors in use are skipped.
func sweepMirrors(dir string) {
	cacheConfig.mu.Lock()
	if time.Since(cacheConfig.lastSweep) < mirrorSweepInterval {
		cacheConfig.mu.Unlock()
		return
	}
	cacheConfig.lastSweep = time.Now()
	cacheConfig.mu.Unlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".git") {
			continue
		}
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < mirrorMaxIdle {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		lock := mirrorLock(path)
		if !lock.TryLock() {
			continue
		}
		_ = os.RemoveAll(path)
		lock.Unlock()
	}
}
