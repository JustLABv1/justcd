package publishing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type BuildResult struct {
	ID     int64  `json:"id"`
	SHA    string `json:"sha"`
	Ref    string `json:"ref"`
	Status string `json:"status"`
}

var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)

// SuccessfulBuild reads an exact pipeline's successful publish job artifact, never
// the latest artifact by branch (which may belong to a different build).
func (g GitLab) SuccessfulBuild(ctx context.Context, project int, completed int64) (BuildResult, string, error) {
	prefix := "/projects/" + strconv.Itoa(project)
	var pipelines []BuildResult
	if err := g.request(ctx, "GET", prefix+"/pipelines?ref=main&order_by=id&sort=desc&per_page=20", nil, &pipelines); err != nil {
		return BuildResult{}, "", err
	}
	if len(pipelines) == 0 {
		return BuildResult{}, "", nil
	}
	p := BuildResult{}
	for _, candidate := range pipelines {
		if candidate.Status != "skipped" {
			p = candidate
			break
		}
	}
	if p.ID == 0 {
		return p, "", nil
	}
	if err := g.request(ctx, "GET", prefix+"/pipelines/"+strconv.FormatInt(p.ID, 10), nil, &p); err != nil {
		return p, "", err
	}
	if p.Ref != "main" || !commitPattern.MatchString(p.SHA) {
		return p, "", errors.New("unexpected publishing pipeline identity")
	}
	if p.ID == completed && completed > 0 {
		return p, "", nil
	}
	if p.Status != "success" {
		return p, "", nil
	}
	var jobs []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Ref    string `json:"ref"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
		Pipeline BuildResult `json:"pipeline"`
	}
	if err := g.request(ctx, "GET", prefix+"/pipelines/"+strconv.FormatInt(p.ID, 10)+"/jobs?include_retried=false&per_page=100", nil, &jobs); err != nil {
		return p, "", err
	}
	for _, j := range jobs {
		if j.Name != "publish" || j.Status != "success" || j.Ref != "main" || j.Commit.ID != p.SHA || j.Pipeline.ID != p.ID {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(g.URL, "/")+"/api/v4"+prefix+"/jobs/"+strconv.FormatInt(j.ID, 10)+"/artifacts/.justcd-build/image-digest", nil)
		if err != nil {
			return p, "", err
		}
		req.Header.Set("PRIVATE-TOKEN", g.Token)
		client := http.Client{Timeout: 30 * time.Second}
		if g.HTTP != nil {
			client = *g.HTTP
		}
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, err := client.Do(req)
		if err != nil {
			return p, "", errors.New("could not retrieve build digest artifact")
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 256))
		resp.Body.Close()
		if resp.StatusCode != 200 || readErr != nil {
			return p, "", errors.New("build digest artifact is unavailable; retry or rebuild if expired")
		}
		digest := strings.TrimSpace(string(data))
		if !digestPattern.MatchString(digest) {
			return p, "", errors.New("build artifact contains an invalid image digest")
		}
		return p, digest, nil
	}
	return p, "", errors.New("successful pipeline has no matching publish job")
}

// CommitDeployment builds one Git commit without checking out or executing user
// files. Normal fast-forward push atomically rejects concurrent source changes.
func CommitDeployment(ctx context.Context, repository, token, sha, digest, image string, pipelineID int64) error {
	return commitDeployment(ctx, repository, token, sha, digest, image, pipelineID, false)
}
func commitDeployment(ctx context.Context, repository, token, sha, digest, image string, pipelineID int64, localTest bool) error {
	if !commitPattern.MatchString(sha) || !digestPattern.MatchString(digest) || !imagePattern.MatchString(image) {
		return errors.New("invalid deployment build identity")
	}
	if !localTest {
		origin, _, err := RepositoryLocation(repository)
		if err != nil || origin == "" {
			return errors.New("invalid managed repository URL")
		}
	}
	root, err := os.MkdirTemp("", "justcd-completion-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	askpass := filepath.Join(root, "askpass")
	if err = os.WriteFile(askpass, []byte("#!/bin/sh\ncase \"$1\" in *Username*) printf '%s' oauth2 ;; *) printf '%s' "+quote(token)+" ;; esac\n"), 0700); err != nil {
		return err
	}
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=" + askpass, "GIT_ASKPASS_REQUIRE=force", "GIT_AUTHOR_NAME=JustCD Publisher", "GIT_AUTHOR_EMAIL=justcd@localhost", "GIT_COMMITTER_NAME=JustCD Publisher", "GIT_COMMITTER_EMAIL=justcd@localhost"}
	// Trust is configured on the backend; the CI CA variable is not a backend secret.
	for _, key := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR", "GIT_SSL_CAINFO", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	run := func(input string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "protocol.file.allow=never", "-C", root}, args...)...)
		if localTest {
			cmd = exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null", "-C", root}, args...)...)
		}
		cmd.Env = env
		cmd.Stdin = strings.NewReader(input)
		out, e := cmd.Output()
		if e != nil {
			return "", errors.New("publishing Git operation failed; verify write permission, backend CA trust, and branch state")
		}
		return strings.TrimSpace(string(out)), nil
	}
	if _, err = run("", "init", "--quiet"); err != nil {
		return err
	}
	if _, err = run("", "remote", "add", "origin", repository); err != nil {
		return err
	}
	if _, err = run("", "fetch", "--quiet", "--depth=2", "origin", "main"); err != nil {
		return err
	}
	head, err := run("", "rev-parse", "FETCH_HEAD")
	if err != nil {
		return err
	}
	message := fmt.Sprintf("JustCD publish pipeline %d [skip ci]", pipelineID)
	template, err := run("", "show", sha+":.justcd/deployment.yaml")
	if err != nil {
		return err
	}
	if len(template) > 1<<20 || !strings.Contains(template, "JUSTCD_IMAGE_DIGEST") {
		return errors.New("managed deployment template is missing or invalid")
	}
	manifest := strings.ReplaceAll(template, "JUSTCD_IMAGE_DIGEST", image+"@"+digest) + "\n"
	if head != sha {
		msg, e := run("", "show", "-s", "--format=%B", head)
		if e != nil {
			return e
		}
		parent, e := run("", "rev-parse", head+"^")
		if e != nil {
			return e
		}
		existing, e := run("", "show", head+":deploy/application.yaml")
		if e != nil {
			return e
		}
		if msg == message && parent == sha && existing == strings.TrimSpace(manifest) {
			return nil
		}
		return errors.New("publishing build is stale; main changed before deployment update")
	}
	if _, err = run("", "read-tree", sha); err != nil {
		return err
	}
	blob, err := run(manifest, "hash-object", "-w", "--stdin")
	if err != nil {
		return err
	}
	if _, err = run("", "update-index", "--add", "--cacheinfo", "100644,"+blob+",deploy/application.yaml"); err != nil {
		return err
	}
	tree, err := run("", "write-tree")
	if err != nil {
		return err
	}
	commit, err := run(message+"\n", "commit-tree", tree, "-p", sha)
	if err != nil {
		return err
	}
	_, err = run("", "push", "--quiet", "origin", commit+":refs/heads/main")
	return err
}

func (g GitLab) VerifyManagedProject(ctx context.Context, id int, repository, marker string) error {
	var p Project
	if err := g.request(ctx, "GET", "/projects/"+strconv.Itoa(id), nil, &p); err != nil {
		return err
	}
	if p.ID != id || p.RepositoryURL != repository || p.Description != marker || p.DefaultBranch != "main" {
		return errors.New("publishing project identity changed")
	}
	return nil
}
