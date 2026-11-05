package publishing

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
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
		var head bytes.Buffer
		body := io.TeeReader(io.LimitReader(resp.Body, 2<<20), &limitedWriter{buf: &head, max: 4096})
		if err = json.NewDecoder(body).Decode(out); err != nil {
			// The decoder stops at the first bad byte; read on for the excerpt.
			_, _ = io.Copy(&limitedWriter{buf: &head, max: 4096}, io.LimitReader(resp.Body, 4096))
			// Name the call: a proxy, login page or size limit in front of GitLab
			// otherwise looks identical to a malformed reply.
			kind, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";")
			if kind == "" {
				kind = "no content type"
			}
			message := fmt.Sprintf("GitLab returned an invalid response to %s %s (HTTP %d, %s)", method, strings.Split(p, "?")[0], resp.StatusCode, kind)
			if excerpt := textExcerpt(head.Bytes(), 200); excerpt != "" {
				message += `: "` + excerpt + `"`
			}
			return errors.New(message + "; check that the GitLab URL points directly at GitLab and that no login page, firewall or proxy answers instead")
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
	err = g.request(ctx, "POST", "/projects", map[string]any{"name": name, "path": name, "namespace_id": ns.ID, "visibility": "private", "description": marker, "initialize_with_readme": false, "default_branch": "main", "ci_push_repository_for_job_token_allowed": false}, &result)
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
	managed := false
	for _, f := range files {
		if f.Path == ".gitlab-ci.yml" {
			managed = !strings.Contains(f.Content, "update-deployment:")
		}
	}
	if !managed {
		var settings Project
		if err := g.request(ctx, "PUT", prefix, map[string]any{"ci_push_repository_for_job_token_allowed": true}, &settings); err != nil {
			return err
		}
		if !settings.JobPush {
			return errors.New("GitLab must support CI job-token repository pushes for legacy publishing")
		}
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
	// start_sha creates a new branch on GitLab; source uploads target existing main.
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
	return g.commit(ctx, prefix, map[string]string{"branch": "main", "commit_message": "JustCD source " + fingerprint + " [skip ci]"}, actions)
}

// SourceMatches verifies retry identity against Git, not only cached database metadata.
// Git tree IDs are hashes of raw blob contents, including for disk-backed uploads.
func (g GitLab) SourceMatches(ctx context.Context, p Project, files []File) (bool, error) {
	existing := map[string]string{}
	for page := 1; page <= 20; page++ {
		var tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := g.request(ctx, "GET", "/projects/"+strconv.Itoa(p.ID)+"/repository/tree?ref=main&recursive=true&per_page=100&page="+strconv.Itoa(page), nil, &tree); err != nil {
			return false, err
		}
		for _, f := range tree {
			if f.Type == "blob" && strings.HasPrefix(f.Path, "source/") {
				existing[f.Path] = f.ID
			}
		}
		if len(tree) < 100 {
			break
		}
		if page == 20 {
			return false, errors.New("managed repository contains too many files")
		}
	}
	count := 0
	for _, f := range files {
		if !strings.HasPrefix(f.Path, "source/") {
			continue
		}
		count++
		var reader io.Reader
		var size int64
		var input *os.File
		if f.DiskPath != "" {
			var err error
			input, err = os.Open(f.DiskPath)
			if err != nil {
				return false, err
			}
			stat, err := input.Stat()
			if err != nil {
				input.Close()
				return false, err
			}
			size = stat.Size()
			reader = input
		} else {
			b := []byte(f.Content)
			if f.Encoding == "base64" {
				var err error
				b, err = base64.StdEncoding.DecodeString(f.Content)
				if err != nil {
					return false, err
				}
			}
			size = int64(len(b))
			reader = bytes.NewReader(b)
		}
		h := sha1.New()
		fmt.Fprintf(h, "blob %d\x00", size)
		_, err := io.Copy(h, contextReader{ctx, reader})
		if input != nil {
			input.Close()
		}
		if err != nil {
			return false, err
		}
		if existing[f.Path] != fmt.Sprintf("%x", h.Sum(nil)) {
			return false, nil
		}
	}
	return count == len(existing), nil
}

func (g GitLab) ProtectPublishingBranch(ctx context.Context, p Project) error {
	prefix := "/projects/" + strconv.Itoa(p.ID)
	var rule any
	err := g.request(ctx, "GET", prefix+"/protected_branches/main", nil, &rule)
	if err == nil {
		return nil
	}
	var provider *ProviderError
	if errors.As(err, &provider) && provider.Status == 404 {
		err = g.request(ctx, "POST", prefix+"/protected_branches", map[string]any{"name": "main", "push_access_level": 40, "merge_access_level": 40, "allow_force_push": false}, nil)
		if err == nil {
			return nil
		}
	}
	// An exact-name rule may be inaccessible or absent when a wildcard/group rule
	// already protects main. Also tolerate another actor protecting it concurrently.
	// Only GitLab's effective branch status can satisfy this fallback.
	var branch struct {
		Protected bool `json:"protected"`
	}
	if check := g.request(ctx, "GET", prefix+"/repository/branches/main", nil, &branch); check == nil && branch.Protected {
		return nil
	}
	return err
}

// GitLab's commit API requires JSON/base64. Stage that representation on disk
// incrementally, then stream it with Content-Length for registry/proxy compatibility.
type commitAction struct {
	LastCommitID string
	Action       string
	File         File
}

func (g GitLab) commit(ctx context.Context, prefix string, metadata map[string]string, actions []commitAction, result ...any) error {
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
	var out any
	if len(result) > 0 {
		out = result[0]
	}
	return g.request(ctx, "POST", prefix+"/repository/commits", payload, out)
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
		content, encoding := a.File.Content, a.File.Encoding
		// Shell-heavy content is sent encoded: GitLab stores identical bytes, and
		// request filters in front of it do not mistake the script for an attack.
		if encoding == "text" && a.File.DiskPath == "" && a.File.Path == FilePackageSources {
			content, encoding = base64.StdEncoding.EncodeToString([]byte(content)), "base64"
		}
		fields := map[string]string{"action": a.Action, "file_path": a.File.Path}
		if a.LastCommitID != "" {
			fields["last_commit_id"] = a.LastCommitID
		}
		if a.Action != "delete" {
			fields["encoding"] = encoding
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
			data, _ = json.Marshal(content)
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

// RepositoryFile reads a managed build file at the reviewed source commit.
func (g GitLab) RepositoryFile(ctx context.Context, project int, name, sha string) (string, string, bool, error) {
	var file struct {
		Content      string `json:"content"`
		Encoding     string `json:"encoding"`
		LastCommitID string `json:"last_commit_id"`
	}
	err := g.request(ctx, "GET", "/projects/"+strconv.Itoa(project)+"/repository/files/"+url.PathEscape(name)+"?ref="+url.QueryEscape(sha), nil, &file)
	var provider *ProviderError
	if errors.As(err, &provider) && provider.Status == 404 {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	if file.Encoding != "base64" || file.LastCommitID == "" {
		return "", "", false, errors.New("GitLab returned invalid managed file metadata")
	}
	data, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil {
		return "", "", false, errors.New("GitLab returned invalid managed file content")
	}
	return string(data), file.LastCommitID, true, nil
}

type RecipeChange struct {
	Path         string `json:"path"`
	Before       string `json:"before"`
	After        string `json:"after"`
	Exists       bool   `json:"exists"`
	LastCommitID string `json:"-"`
}

func (g GitLab) CommitRecipeUpgrade(ctx context.Context, snapshot RepositorySnapshot, changes []RecipeChange, hash string) error {
	prefix := "/projects/" + strconv.Itoa(snapshot.Project.ID)
	var branch struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := g.request(ctx, "GET", prefix+"/repository/branches/"+url.PathEscape(snapshot.Revision), nil, &branch); err != nil {
		return err
	}
	if branch.Commit.ID != snapshot.SHA {
		return errors.New("repository changed; review the profile upgrade again")
	}
	actions := []commitAction{}
	for _, c := range changes {
		if strings.HasPrefix(c.Path, "source/") || strings.HasPrefix(c.Path, "deploy/") {
			return errors.New("profile upgrades can only change generated build files")
		}
		action := "create"
		if c.Exists {
			action = "update"
		}
		actions = append(actions, commitAction{Action: action, File: File{Path: c.Path, Content: base64.StdEncoding.EncodeToString([]byte(c.After)), Encoding: "base64"}, LastCommitID: c.LastCommitID})
	}
	if len(actions) == 0 {
		return nil
	}
	var result struct {
		ParentIDs []string `json:"parent_ids"`
	}
	if err := g.commit(ctx, prefix, map[string]string{"branch": snapshot.Revision, "commit_message": "JustCD profile upgrade " + hash + " [skip ci]"}, actions, &result); err != nil {
		return err
	}
	// Per-file last_commit_id guards recipe edits. If source moved in the narrow
	// check/commit window, no source is overwritten and no build starts until a
	// fresh review acknowledges the new source commit.
	if len(result.ParentIDs) != 1 || result.ParentIDs[0] != snapshot.SHA {
		return errors.New("repository advanced during the recipe commit; source was preserved; review again before starting the build")
	}
	return nil
}

// limitedWriter keeps the first max bytes written to it.
type limitedWriter struct {
	buf *bytes.Buffer
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if room := w.max - w.buf.Len(); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		w.buf.Write(p[:room])
	}
	return len(p), nil
}

var htmlTag = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>|<[^>]*>`)

// textExcerpt reduces a non-JSON reply to a short readable line, without
// markup, so an error can show what answered in GitLab's place.
func textExcerpt(raw []byte, limit int) string {
	text := htmlTag.ReplaceAllString(string(raw), " ")
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return text
}
