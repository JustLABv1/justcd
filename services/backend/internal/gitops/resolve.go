package gitops

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/store"
)

// ResolveRevision returns the commit a revision currently names on the
// remote, without fetching any objects. It authenticates exactly like Fetch,
// so an expired or revoked credential fails here too. found is false when the
// revision cannot be resolved unambiguously from the advertised refs (for
// example an abbreviated commit id); callers should then Fetch.
func ResolveRevision(ctx context.Context, db *store.Store, key []byte, source store.GitSource, revision string) (commit string, found bool, err error) {
	if !validRevision(revision) {
		return "", false, errors.New("Git revision is invalid")
	}
	if !validURL(source.RepositoryURL) {
		return "", false, errors.New("Git source URL is not supported")
	}
	workRoot, err := os.MkdirTemp("", "justcd-git-")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(workRoot)
	env, err := gitEnvironment(ctx, db, key, source, workRoot)
	if err != nil {
		return "", false, err
	}
	resolveCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	// A full commit id is fetched as itself. Still contact the remote so the
	// credential and repository access are checked on every resolution.
	pattern := revision
	if validCommit(revision) {
		pattern = "HEAD"
	}
	output, err := runGitOutput(resolveCtx, env, "ls-remote", "--", source.RepositoryURL, pattern)
	if err != nil {
		return "", false, commandFailure(resolveCtx, "resolve requested Git revision", err)
	}
	if validCommit(revision) {
		return strings.ToLower(revision), true, nil
	}
	commit, found = matchAdvertisedRef(string(output), revision)
	return commit, found, nil
}

// matchAdvertisedRef applies git's ref_rev_parse_rules, the order git fetch
// uses to pick a remote ref for a short name, to ls-remote output. Annotated
// tags resolve to the commit they point at.
func matchAdvertisedRef(output, revision string) (string, bool) {
	refs := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		id, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || !validCommit(id) {
			continue
		}
		refs[name] = strings.ToLower(id)
	}
	for _, rule := range []string{"%s", "refs/%s", "refs/tags/%s", "refs/heads/%s", "refs/remotes/%s", "refs/remotes/%s/HEAD"} {
		name := strings.Replace(rule, "%s", revision, 1)
		id, ok := refs[name]
		if !ok {
			continue
		}
		if peeled, ok := refs[name+"^{}"]; ok {
			return peeled, true
		}
		if strings.HasPrefix(name, "refs/tags/") {
			// A tag without a peeled entry may still be an annotated tag
			// object; only Fetch can tell.
			return "", false
		}
		return id, true
	}
	return "", false
}
