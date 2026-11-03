package publishing

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/scm"
)

type GitLab struct {
	URL     string
	Token   string
	HTTP    *http.Client
	TempDir string
}
type Project struct {
	CIConfigPath  string `json:"ci_config_path"`
	ID            int    `json:"id"`
	WebURL        string `json:"web_url"`
	RepositoryURL string `json:"http_url_to_repo"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	JobPush       bool   `json:"ci_push_repository_for_job_token_allowed"`
}
type Variable struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Masked    bool   `json:"masked"`
	Protected bool   `json:"protected"`
	Raw       bool   `json:"raw"`
}
type ProviderError struct {
	Status    int
	Operation string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("GitLab %s failed (HTTP %d); verify token permissions, GitLab version and project settings", e.Operation, e.Status)
}

func (g GitLab) Validate() error {
	u, e := url.Parse(g.URL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimRight(u.Path, "/") != "" {
		return errors.New("GitLab URL must be an HTTPS origin without credentials, path or query")
	}
	_, e = scm.Details(strings.TrimRight(g.URL, "/")+"/group/project.git", "gitlab", "")
	if e != nil {
		return e
	}
	if len(g.Token) < 8 || strings.ContainsAny(g.Token, "\r\n") {
		return errors.New("a GitLab API token is required")
	}
	return nil
}
func (g GitLab) request(ctx context.Context, method, p string, body any, out any) error {
	var input io.Reader
	if reader, ok := body.(io.Reader); ok {
		input = reader
	} else if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(g.URL, "/")+"/api/v4"+p, input)
	if err != nil {
		return errors.New("invalid GitLab request")
	}
	if f, ok := body.(*os.File); ok {
		if stat, e := f.Stat(); e == nil {
			req.ContentLength = stat.Size()
		}
	}
	req.Header.Set("PRIVATE-TOKEN", g.Token)
	req.Header.Set("Content-Type", "application/json")
	client := g.HTTP
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("GitLab request failed; verify network access and trusted certificates")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ProviderError{Status: resp.StatusCode, Operation: method + " " + strings.Split(p, "?")[0]}
	}
	if out != nil {
		if err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out); err != nil {
			return errors.New("GitLab returned an invalid response")
		}
	}
	return nil
}

// EnsureProject only resumes a repository bearing this workspace's exact marker.
// Existing unrelated repositories are never adopted or overwritten.
func (g GitLab) EnsureProject(ctx context.Context, group, name, marker string) (Project, error) {
	var result Project
	p := "/projects/" + url.PathEscape(group+"/"+name)
	err := g.request(ctx, "GET", p, nil, &result)
	if err == nil {
		if result.Description != marker {
			return result, errors.New("repository already exists and is not managed by this publishing request")
		}
		return result, nil
	}
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Status != 404 {
		return result, err
	}
	var ns struct {
		ID int `json:"id"`
	}
	if err = g.request(ctx, "GET", "/groups/"+url.PathEscape(group), nil, &ns); err != nil {
		return result, err
	}
	err = g.request(ctx, "POST", "/projects", map[string]any{"name": name, "path": name, "namespace_id": ns.ID, "visibility": "private", "description": marker, "initialize_with_readme": false, "default_branch": "main", "ci_push_repository_for_job_token_allowed": true}, &result)
	if err != nil {
		return result, err
	}
	if result.ID <= 0 || result.WebURL == "" || result.RepositoryURL == "" {
		return result, errors.New("GitLab returned an incomplete project")
	}
	return result, nil
}
func (g GitLab) Prepare(ctx context.Context, p Project, files []File, variables []Variable, fingerprint string) error {
	prefix := "/projects/" + strconv.Itoa(p.ID)
	// This setting is required for the build job to commit the image digest without a long-lived Git write token.
	var settings Project
	if err := g.request(ctx, "PUT", prefix, map[string]any{"ci_push_repository_for_job_token_allowed": true}, &settings); err != nil {
		return err
	}
	if !settings.JobPush {
		return errors.New("GitLab must support and enable CI job-token repository pushes (GitLab 18.4+ recommended)")
	}
	if err := g.SetVariables(ctx, p, variables); err != nil {
		return err
	}
	var branch struct {
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	err := g.request(ctx, "GET", prefix+"/repository/branches/main", nil, &branch)
	if err == nil {
		if strings.TrimSpace(branch.Commit.Message) != "JustCD source "+fingerprint+" [skip ci]" {
			return errors.New("managed repository has changed; refusing to overwrite it")
		}
		return nil
	}
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Status != 404 {
		return err
	}
	actions := make([]commitAction, 0, len(files))
	for _, f := range files {
		actions = append(actions, commitAction{Action: "create", File: f})
	}
	return g.commit(ctx, prefix, map[string]string{"branch": "main", "commit_message": "JustCD source " + fingerprint + " [skip ci]"}, actions)
}
func (g GitLab) StartPipeline(ctx context.Context, project int) (string, error) {
	var p struct {
		WebURL string `json:"web_url"`
	}
	err := g.request(ctx, "POST", "/projects/"+strconv.Itoa(project)+"/pipeline", map[string]string{"ref": "main"}, &p)
	return p.WebURL, err
}

// ReplaceSource commits a complete source snapshot while retaining the currently
// deployed manifest until the new build succeeds. Generated CI remains managed.
func (g GitLab) ReplaceSource(ctx context.Context, p Project, files []File, fingerprint string) error {
	prefix := "/projects/" + strconv.Itoa(p.ID)
	existing := map[string]bool{}
	for page := 1; page <= 20; page++ {
		var tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		}
		if err := g.request(ctx, "GET", prefix+"/repository/tree?ref=main&recursive=true&per_page=100&page="+strconv.Itoa(page), nil, &tree); err != nil {
			return err
		}
		for _, f := range tree {
			if f.Type == "blob" {
				existing[f.Path] = true
			}
		}
		if len(tree) < 100 {
			break
		}
		if page == 20 {
			return errors.New("managed repository contains too many files")
		}
	}
	var branch struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := g.request(ctx, "GET", prefix+"/repository/branches/main", nil, &branch); err != nil {
		return err
	}
	actions := []commitAction{}
	next := map[string]bool{}
	for _, f := range files {
		if f.Path == "deploy/.gitkeep" {
			continue
		}
		action := "create"
		if existing[f.Path] {
			action = "update"
		}
		actions = append(actions, commitAction{Action: action, File: f})
		next[f.Path] = true
	}
	// Only analyst source files can be removed by replacing an upload.
	for p := range existing {
		if strings.HasPrefix(p, "source/") && !next[p] {
			actions = append(actions, commitAction{Action: "delete", File: File{Path: p}})
		}
	}
	return g.commit(ctx, prefix, map[string]string{"branch": "main", "start_sha": branch.Commit.ID, "commit_message": "JustCD source " + fingerprint + " [skip ci]"}, actions)
}

func (g GitLab) ProtectPublishingBranch(ctx context.Context, p Project) error {
	prefix := "/projects/" + strconv.Itoa(p.ID) + "/protected_branches"
	var branch any
	err := g.request(ctx, "GET", prefix+"/main", nil, &branch)
	if err == nil {
		return nil
	}
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Status != 404 {
		return err
	}
	return g.request(ctx, "POST", prefix, map[string]any{"name": "main", "push_access_level": 40, "merge_access_level": 40, "allow_force_push": false}, nil)
}

// GitLab's commit API requires JSON/base64. Stage that representation on disk
// incrementally, then stream it with Content-Length for registry/proxy compatibility.
type commitAction struct {
	Action string
	File   File
}

func (g GitLab) commit(ctx context.Context, prefix string, metadata map[string]string, actions []commitAction) error {
	payload, err := os.CreateTemp(g.TempDir, "justcd-commit-*.json")
	if err != nil {
		return errors.New("cannot allocate temporary GitLab request storage")
	}
	defer os.Remove(payload.Name())
	defer payload.Close()
	if err = writeCommit(ctx, payload, metadata, actions); err != nil {
		return errors.New("cannot prepare GitLab source commit; check temporary storage capacity")
	}
	if _, err = payload.Seek(0, io.SeekStart); err != nil {
		return errors.New("cannot read temporary GitLab request")
	}
	return g.request(ctx, "POST", prefix+"/repository/commits", payload, nil)
}
func writeCommit(ctx context.Context, w io.Writer, metadata map[string]string, actions []commitAction) error {
	head, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	if _, err = w.Write(head[:len(head)-1]); err != nil {
		return err
	}
	if _, err = io.WriteString(w, `,"actions":[`); err != nil {
		return err
	}
	for i, a := range actions {
		if err = ctx.Err(); err != nil {
			return err
		}
		if i > 0 {
			if _, err = io.WriteString(w, ","); err != nil {
				return err
			}
		}
		fields := map[string]string{"action": a.Action, "file_path": a.File.Path}
		if a.Action != "delete" {
			fields["encoding"] = a.File.Encoding
		}
		data, _ := json.Marshal(fields)
		if a.Action == "delete" {
			if _, err = w.Write(data); err != nil {
				return err
			}
			continue
		}
		if _, err = w.Write(data[:len(data)-1]); err != nil {
			return err
		}
		if _, err = io.WriteString(w, `,"content":`); err != nil {
			return err
		}
		if a.File.DiskPath != "" {
			if _, err = io.WriteString(w, `"`); err != nil {
				return err
			}
			source, e := os.Open(a.File.DiskPath)
			if e != nil {
				return e
			}
			encoder := base64.NewEncoder(base64.StdEncoding, w)
			_, copyErr := io.Copy(encoder, contextReader{ctx, source})
			closeErr := encoder.Close()
			source.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if _, err = io.WriteString(w, `"`); err != nil {
				return err
			}
		} else {
			data, _ = json.Marshal(a.File.Content)
			if _, err = w.Write(data); err != nil {
				return err
			}
		}
		if _, err = io.WriteString(w, "}"); err != nil {
			return err
		}
	}
	_, err = io.WriteString(w, "]}")
	return err
}

func (g GitLab) SetVariables(ctx context.Context, p Project, variables []Variable) error {
	prefix := "/projects/" + strconv.Itoa(p.ID)
	for _, v := range variables {
		err := g.request(ctx, "POST", prefix+"/variables", v, nil)
		var pe *ProviderError
		if errors.As(err, &pe) && pe.Status == 400 {
			err = g.request(ctx, "PUT", prefix+"/variables/"+url.PathEscape(v.Key), v, nil)
		}
		if err != nil {
			return err
		}
	}

	return nil
}
