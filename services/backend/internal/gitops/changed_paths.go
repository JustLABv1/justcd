package gitops

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"
)

// ChangedPaths compares immutable commits. GitLab supplies the diff merge base;
// GitHub supplies the target tip, for which we calculate the merge base locally.
// Disable rename detection: a rename yields both the deleted and added paths.
func (c *Checkout) ChangedPaths(parent context.Context, baseSHA, headSHA string, baseIsMergeBase bool) ([]string, error) {
	if !validCommit(baseSHA) || !validCommit(headSHA) || c.Commit != headSHA {
		return nil, errors.New("Git diff commits are invalid or no longer match the PR")
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()
	fetchArgs := []string{"-C", c.Root, "fetch"}
	if baseIsMergeBase {
		fetchArgs = append(fetchArgs, "--depth=1")
	} else {
		shallow, err := runGitOutput(ctx, c.env, "-C", c.Root, "rev-parse", "--is-shallow-repository")
		if err != nil {
			return nil, errors.New("could not verify Git comparison history")
		}
		if strings.TrimSpace(string(shallow)) == "true" {
			fetchArgs = append(fetchArgs, "--unshallow")
		}
	}
	fetchArgs = append(fetchArgs, "origin", baseSHA, headSHA)
	if err := runGit(ctx, c.env, fetchArgs...); err != nil {
		return nil, errors.New("could not fetch the PR comparison commits")
	}
	if !baseIsMergeBase {
		shallow, err := runGitOutput(ctx, c.env, "-C", c.Root, "rev-parse", "--is-shallow-repository")
		if err != nil || strings.TrimSpace(string(shallow)) != "false" {
			return nil, errors.New("the Git comparison history is incomplete")
		}
	}
	if !baseIsMergeBase {
		output, err := runGitOutput(ctx, c.env, "-C", c.Root, "merge-base", baseSHA, headSHA)
		if err != nil {
			return nil, errors.New("could not establish the PR merge base")
		}
		baseSHA = strings.TrimSpace(string(output))
		if !validCommit(baseSHA) {
			return nil, errors.New("Git returned an invalid merge base")
		}
	}
	output, err := runGitOutput(ctx, c.env, "-C", c.Root, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z", baseSHA, headSHA, "--")
	if err != nil {
		return nil, errors.New("could not collect complete PR filenames from Git")
	}
	paths := []string{}
	if len(output) == 0 {
		return paths, nil
	}
	if output[len(output)-1] != 0 {
		return nil, errors.New("Git returned an incomplete filename list")
	}
	for _, raw := range bytes.Split(output[:len(output)-1], []byte{0}) {
		value := string(raw)
		if value == "" || strings.HasPrefix(value, "/") || value == ".." || strings.HasPrefix(value, "../") {
			return nil, errors.New("Git returned an invalid filename")
		}
		paths = append(paths, value)
	}
	return paths, nil
}
