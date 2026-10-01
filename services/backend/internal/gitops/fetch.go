package gitops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
)

type Credentials struct {
	Username   string `json:"username"`
	Token      string `json:"token"`
	PrivateKey string `json:"privateKey"`
	KnownHosts string `json:"knownHosts"`
}

type Checkout struct {
	Root    string
	Commit  string
	cleanup func()
}

type FetchError struct {
	operation string
	retryable bool
}

func (e *FetchError) Error() string     { return "could not " + e.operation }
func (e *FetchError) Retryable() bool   { return e.retryable }
func (e *FetchError) ErrorCode() string { return "git.fetch_failed" }

func (c *Checkout) Close() {
	if c != nil && c.cleanup != nil {
		c.cleanup()
	}
}

func Fetch(ctx context.Context, db *store.Store, key []byte, source store.GitSource, revision string) (*Checkout, error) {
	if !validRevision(revision) {
		return nil, errors.New("Git revision is invalid")
	}
	if !validURL(source.RepositoryURL) {
		return nil, errors.New("Git source URL is not supported")
	}
	workRoot, err := os.MkdirTemp("", "justcd-git-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(workRoot) }
	checkoutPath := filepath.Join(workRoot, "repository")
	homePath := filepath.Join(workRoot, "home")
	if err := os.Mkdir(homePath, 0700); err != nil {
		cleanup()
		return nil, err
	}
	env := cleanGitEnvironment(os.Environ(), homePath)
	var sshCommand string
	if source.CredentialID == nil && (strings.HasPrefix(source.RepositoryURL, "git@") || strings.HasPrefix(source.RepositoryURL, "ssh://")) {
		cleanup()
		return nil, errors.New("SSH Git sources require a credential with a pinned known_hosts file")
	}
	if source.CredentialID != nil {
		credential, err := db.CredentialByID(ctx, *source.CredentialID)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("load Git credential: %w", err)
		}
		if credential.ExpiresAt != nil && !credential.ExpiresAt.After(time.Now()) {
			cleanup()
			return nil, errors.New("Git credential has expired")
		}
		plain, err := security.Decrypt(key, credential.Cipher, "credential:"+credential.ID)
		if err != nil {
			cleanup()
			return nil, err
		}
		var secrets Credentials
		if err := json.Unmarshal(plain, &secrets); err != nil {
			cleanup()
			return nil, errors.New("Git credential payload is invalid")
		}
		switch credential.Kind {
		case "git-https":
			if secrets.Token == "" {
				cleanup()
				return nil, errors.New("Git HTTPS token is empty")
			}
			askpass := filepath.Join(workRoot, "askpass.sh")
			username := secrets.Username
			if username == "" {
				username = "justcd"
			}
			script := "#!/bin/sh\ncase \"$1\" in *Username*) printf '%s' " + shellQuote(username) + " ;; *) printf '%s' " + shellQuote(secrets.Token) + " ;; esac\n"
			if err := os.WriteFile(askpass, []byte(script), 0700); err != nil {
				cleanup()
				return nil, err
			}
			env = append(env, "GIT_ASKPASS="+askpass, "GIT_ASKPASS_REQUIRE=force")
		case "git-ssh":
			if secrets.PrivateKey == "" || secrets.KnownHosts == "" {
				cleanup()
				return nil, errors.New("SSH key and known-hosts data are required")
			}
			identityPath := filepath.Join(workRoot, "identity")
			knownHostsPath := filepath.Join(workRoot, "known_hosts")
			if err := os.WriteFile(identityPath, []byte(secrets.PrivateKey), 0600); err != nil {
				cleanup()
				return nil, err
			}
			if err := os.WriteFile(knownHostsPath, []byte(secrets.KnownHosts), 0600); err != nil {
				cleanup()
				return nil, err
			}
			sshCommand = "ssh -i " + shellQuote(identityPath) + " -o IdentitiesOnly=yes -o UserKnownHostsFile=" + shellQuote(knownHostsPath) + " -o StrictHostKeyChecking=yes -o BatchMode=yes"
			env = append(env, "GIT_SSH_COMMAND="+sshCommand)
		default:
			cleanup()
			return nil, errors.New("credential is not a supported Git credential")
		}
	}
	cloneCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := runGit(cloneCtx, env, "clone", "--no-checkout", "--filter=blob:none", "--", source.RepositoryURL, checkoutPath); err != nil {
		cleanup()
		return nil, commandFailure(cloneCtx, "clone Git source", err)
	}
	if err := runGit(cloneCtx, env, "-C", checkoutPath, "config", "core.hooksPath", "/dev/null"); err != nil {
		cleanup()
		return nil, errors.New("could not isolate repository configuration")
	}
	if err := runGit(cloneCtx, env, "-C", checkoutPath, "fetch", "--depth=1", "origin", revision); err != nil {
		cleanup()
		return nil, commandFailure(cloneCtx, "fetch requested Git revision", err)
	}
	commitBytes, err := runGitOutput(cloneCtx, env, "-C", checkoutPath, "rev-parse", "--verify", "--end-of-options", "FETCH_HEAD^{commit}")
	if err != nil {
		cleanup()
		return nil, errors.New("requested Git revision does not resolve to a commit")
	}
	commit := strings.TrimSpace(string(commitBytes))
	if !validCommit(commit) {
		cleanup()
		return nil, errors.New("Git returned an invalid commit id")
	}
	if err := runGit(cloneCtx, env, "-C", checkoutPath, "checkout", "--detach", commit); err != nil {
		cleanup()
		return nil, errors.New("could not check out requested Git revision")
	}
	return &Checkout{Root: checkoutPath, Commit: commit, cleanup: cleanup}, nil
}

func validURL(value string) bool {
	if strings.ContainsAny(value, " \t\r\n\x00") {
		return false
	}
	if strings.HasPrefix(value, "git@") {
		hostAndPath, ok := strings.CutPrefix(value, "git@")
		if !ok {
			return false
		}
		host, repository, hasPath := strings.Cut(hostAndPath, ":")
		return hasPath && host != "" && repository != "" && !strings.ContainsAny(host, "/@?#") && !strings.ContainsAny(repository, "?#")
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return u.User == nil && u.Path != ""
	}
	if u.Scheme != "ssh" || u.Path == "" {
		return false
	}
	if u.User != nil {
		_, hasPassword := u.User.Password()
		if u.User.Username() != "git" || hasPassword {
			return false
		}
	}
	return true
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func validRevision(value string) bool {
	return value != "" && len(value) <= 256 && !strings.HasPrefix(value, "-") && !strings.ContainsAny(value, " \t\r\n\x00")
}
func validCommit(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func cleanGitEnvironment(in []string, home string) []string {
	allowed := map[string]bool{"PATH": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true, "SYSTEMROOT": true, "WINDIR": true, "HTTPS_PROXY": true, "HTTP_PROXY": true, "NO_PROXY": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "GIT_SSL_CAINFO": true, "GIT_SSL_CAPATH": true}
	out := make([]string, 0, len(allowed)+10)
	for _, pair := range in {
		key, _, ok := strings.Cut(pair, "=")
		if !ok || !allowed[strings.ToUpper(key)] {
			continue
		}
		out = append(out, pair)
	}
	return append(out, "HOME="+home, "TMPDIR="+home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_LFS_SKIP_SMUDGE=1")
}
func runGit(ctx context.Context, env []string, args ...string) error {
	_, err := runGitOutput(ctx, env, args...)
	return err
}
func runGitOutput(ctx context.Context, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(output) > 1<<20 {
		return nil, errors.New("Git command output exceeds 1 MiB")
	}
	return output, nil
}

func commandFailure(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var exitErr *exec.ExitError
	transient := false
	if errors.As(err, &exitErr) {
		output := strings.ToLower(string(exitErr.Stderr))
		for _, marker := range []string{"could not resolve host", "connection timed out", "connection reset", "connection refused", "network is unreachable", "temporary failure", "temporarily unavailable", "service unavailable", "remote end hung up", "early eof", "unexpected eof", "tls handshake timeout", "i/o timeout", "returned error: 502", "returned error: 503", "returned error: 504", "http 502", "http 503", "http 504"} {
			if strings.Contains(output, marker) {
				transient = true
				break
			}
		}
	}
	return &FetchError{operation: operation, retryable: transient}
}
