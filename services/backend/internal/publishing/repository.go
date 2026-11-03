package publishing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

type RepositorySnapshot struct {
	Project  Project           `json:"project"`
	Revision string            `json:"revision"`
	SHA      string            `json:"sha"`
	Entries  []RepositoryEntry `json:"-"`
}
type RepositoryEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}
type RepositoryPlan struct {
	SHA           string   `json:"sha"`
	Hash          string   `json:"hash"`
	HasDockerfile bool     `json:"hasDockerfile"`
	HasPipeline   bool     `json:"hasPipeline"`
	ManifestPaths []string `json:"manifestPaths"`
	Files         []File   `json:"files"`
	ManifestPath  string   `json:"manifestPath"`
	NeedsImage    bool     `json:"needsImage"`
	Notes         []string `json:"notes"`
}

var branchPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,199}$`)

func ValidBranch(s string) bool {
	return branchPattern.MatchString(s) && !strings.Contains(s, "..") && !strings.Contains(s, "//") && !strings.HasSuffix(s, "/") && !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, ".lock")
}
func RepositoryLocation(raw string) (string, string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", errors.New("existing GitLab repositories require an HTTPS URL without embedded credentials")
	}
	project := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), ".git")
	if !safePath(project) || !strings.Contains(project, "/") || strings.ContainsAny(project, " %?#") {
		return "", "", errors.New("invalid GitLab project path")
	}
	return "https://" + u.Host, project, nil
}
func (g GitLab) InspectRepository(ctx context.Context, project, revision string) (RepositorySnapshot, error) {
	s := RepositorySnapshot{Revision: revision}
	if !ValidBranch(revision) {
		return s, errors.New("select a branch name without traversal, whitespace or special characters")
	}
	prefix := "/projects/" + url.PathEscape(project)
	if err := g.request(ctx, "GET", prefix, nil, &s.Project); err != nil {
		return s, err
	}
	origin, p, err := RepositoryLocation(s.Project.RepositoryURL)
	if err != nil || origin != strings.TrimRight(g.URL, "/") || p != project || s.Project.ID <= 0 {
		return s, errors.New("GitLab returned a different repository")
	}
	var branch struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	prefix = "/projects/" + strconv.Itoa(s.Project.ID)
	if err := g.request(ctx, "GET", prefix+"/repository/branches/"+url.PathEscape(revision), nil, &branch); err != nil {
		return s, err
	}
	if !regexp.MustCompile(`^([a-fA-F0-9]{40}|[a-fA-F0-9]{64})$`).MatchString(branch.Commit.ID) {
		return s, errors.New("GitLab returned an invalid commit")
	}
	s.SHA = branch.Commit.ID
	for page := 1; page <= 50; page++ {
		var entries []RepositoryEntry
		if err := g.request(ctx, "GET", prefix+"/repository/tree?recursive=true&per_page=100&ref="+url.QueryEscape(s.SHA)+"&page="+strconv.Itoa(page), nil, &entries); err != nil {
			return s, err
		}
		s.Entries = append(s.Entries, entries...)
		if len(entries) < 100 {
			return s, nil
		}
	}
	return s, errors.New("repository exceeds the inspection limit of 5000 entries; use the existing manifest import instead")
}

// PlanRepository creates additions only. Existing source, Dockerfiles, pipelines,
// manifests and ignore rules are never modified or imported into managed ZIP publishing.
func PlanRepository(c Config, s RepositorySnapshot, appDir, image string) (RepositoryPlan, error) {
	p := RepositoryPlan{SHA: s.SHA, Files: []File{}, ManifestPaths: []string{}, Notes: []string{}}
	if !safePath(appDir) || !filePattern.MatchString(appDir) {
		return p, errors.New("application directory must be a relative path without spaces")
	}
	entries := map[string]RepositoryEntry{}
	for _, e := range s.Entries {
		entries[e.Path] = e
	}
	file := func(name string) bool {
		e, ok := entries[name]
		return ok && e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755" || e.Mode == "")
	}
	p.HasDockerfile = file(path.Join(appDir, "Dockerfile"))
	p.HasPipeline = file(".gitlab-ci.yml") || (s.Project.CIConfigPath != "" && s.Project.CIConfigPath != ".gitlab-ci.yml")
	// Values unused by an existing build are not required from the user.
	if p.HasPipeline || p.HasDockerfile {
		if c.BaseImage == "" {
			c.BaseImage = "unused.invalid/base:1"
		}
		if c.Runtime == "python-static" && c.WebServerImage == "" {
			c.WebServerImage = "unused.invalid/server:1"
		}
	}
	if p.HasPipeline {
		if c.BuilderImage == "" {
			c.BuilderImage = "unused.invalid/builder:1"
		}
		if c.RegistryImage == "" {
			c.RegistryImage = "unused.invalid/image"
		}
	}
	if err := c.Validate(); err != nil {
		return p, err
	}

	for _, e := range s.Entries {
		if e.Type == "blob" && (strings.HasSuffix(e.Path, ".yaml") || strings.HasSuffix(e.Path, ".yml")) && (strings.HasPrefix(e.Path, "deploy/") || strings.HasPrefix(e.Path, "k8s/") || strings.HasPrefix(e.Path, "manifests/") || path.Base(e.Path) == "kustomization.yaml" || path.Base(e.Path) == "Chart.yaml") {
			p.ManifestPaths = append(p.ManifestPaths, e.Path)
		}
	}
	// Build context traversing a symlink/submodule is ambiguous and must be chosen explicitly elsewhere.
	if appDir != "." {
		for dir := appDir; dir != "."; dir = path.Dir(dir) {
			if e, ok := entries[dir]; ok && e.Type != "tree" {
				return p, errors.New("application directory crosses a symlink, file or submodule")
			}
		}
	}
	source := []File{}
	for _, e := range s.Entries {
		rel := e.Path
		if appDir != "." {
			if !strings.HasPrefix(rel, appDir+"/") {
				continue
			}
			rel = strings.TrimPrefix(rel, appDir+"/")
		}
		if file(e.Path) {
			source = append(source, File{Path: "source/" + rel})
		}
	}
	if !p.HasDockerfile && !p.HasPipeline {
		if _, err := Generate(c, source); err != nil {
			return p, err
		}
	}
	p.ManifestPath = "deploy/" + c.Name
	templatePath := ".justcd/" + c.Name + "/deployment.yaml"
	readmePath := ".justcd/" + c.Name + "/README.md"
	add := func(name, content string) error {
		// Reject both exact conflicts and file/directory ancestor conflicts.
		for existing := range entries {
			if existing == name || strings.HasPrefix(existing, name+"/") {
				return errors.New("proposed file already exists: " + name)
			}
		}
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			if e, ok := entries[dir]; ok && e.Type != "tree" {
				return errors.New("proposed file crosses an existing file or link: " + dir)
			}
		}
		p.Files = append(p.Files, File{Path: name, Content: content, Encoding: "text"})
		return nil
	}
	port := 3838
	if c.Runtime == "streamlit" {
		port = 8501
	}
	if c.Runtime == "python-static" {
		port = 8080
	}
	manifest := manifest(c, port)
	if p.HasPipeline {
		p.Notes = append(p.Notes, "Existing CI is preserved. Supply its published image digest; update the deployment manifest through your existing pipeline for future releases.")
		if image == "" {
			p.NeedsImage = true
		} else {
			if !strings.Contains(strings.Split(image, "@")[0], "/") || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*@sha256:[a-f0-9]{64}$`).MatchString(image) {
				return p, errors.New("existing CI requires an immutable registry image reference ending in @sha256:<64 hex characters>")
			}
			if err := add(p.ManifestPath+"/application.yaml", strings.ReplaceAll(manifest, "JUSTCD_IMAGE_DIGEST", image)); err != nil {
				return p, err
			}
		}
	} else {
		if !p.HasDockerfile {
			generated, err := Generate(c, source)
			if err != nil {
				return p, err
			}
			for _, f := range generated {
				if f.Path == "Dockerfile" {
					if err := add(path.Join(appDir, "Dockerfile"), strings.ReplaceAll(f.Content, "COPY source/ /app/", "COPY . /app/")); err != nil {
						return p, err
					}
				}
			}
			if _, exists := entries[path.Join(appDir, ".dockerignore")]; !exists {
				if err := add(path.Join(appDir, ".dockerignore"), ".git\ndeploy\n.justcd\n"); err != nil {
					return p, err
				}
			}
		}
		ci := pipeline(c)
		ci = strings.ReplaceAll(ci, "$CI_DEFAULT_BRANCH", "$JUSTCD_PUBLISH_BRANCH")
		ci = strings.Replace(ci, "changes: [source/**/*, Dockerfile, .gitlab-ci.yml, .justcd/**/*]", "changes: ['**/*']", 1)
		ci = strings.Replace(ci, "  variables:\n", "  variables:\n    JUSTCD_PUBLISH_BRANCH: "+yamlString(s.Revision)+"\n", 1)
		// Workflow variables must be available before the job is selected.
		ci = strings.Replace(ci, "workflow:\n", "variables:\n  JUSTCD_PUBLISH_BRANCH: "+yamlString(s.Revision)+"\nworkflow:\n", 1)
		ci = strings.ReplaceAll(ci, "--local context=. --local dockerfile=.", "--local context="+appDir+" --local dockerfile="+appDir)
		ci = strings.ReplaceAll(ci, ".justcd/deployment.yaml", templatePath)
		ci = strings.ReplaceAll(ci, "deploy/application.yaml", p.ManifestPath+"/application.yaml")
		ci = strings.ReplaceAll(ci, "git add ", "git add -f ")
		ci = strings.Replace(ci, "sed \"s|JUSTCD_IMAGE_DIGEST|", "mkdir -p "+p.ManifestPath+"\n      sed \"s|JUSTCD_IMAGE_DIGEST|", 1)
		if err := add(".gitlab-ci.yml", ci); err != nil {
			return p, err
		}
		if err := add(templatePath, manifest); err != nil {
			return p, err
		}
		p.Notes = append(p.Notes, "Before merging: configure protected REGISTRY_USER / REGISTRY_PASSWORD CI variables (or the profile's Vault integration), allow CI job-token repository pushes, and protect the selected branch. Existing settings and secrets are not changed. After merging, run the selected branch pipeline once if it did not start automatically; the proposal commit skips CI.")
	}
	if err := add(readmePath, "# JustCD existing project onboarding\n\n"+strings.Join(p.Notes, "\n\n")+"\n\nApplication directory: "+appDir+"\nDeployment path: "+p.ManifestPath+"\nMerge the proposal before deploying. For generated CI, the first successful branch pipeline writes the immutable image digest to the deployment path.\n"); err != nil {
		return p, err
	}
	payload, _ := json.Marshal(struct {
		Config                                   Config
		SHA, Revision, AppDir, Image, Repository string
		Files                                    []File
	}{c, s.SHA, s.Revision, appDir, image, s.Project.RepositoryURL, p.Files})
	sum := sha256.Sum256(payload)
	p.Hash = hex.EncodeToString(sum[:])
	return p, nil
}

type MergeRequest struct {
	WebURL string `json:"web_url"`
	IID    int    `json:"iid"`
}

func (g GitLab) ProposeRepository(ctx context.Context, s RepositorySnapshot, p RepositoryPlan) (MergeRequest, error) {
	if len(p.Hash) != 64 || p.SHA != s.SHA || p.NeedsImage || len(p.Files) == 0 {
		return MergeRequest{}, errors.New("a complete reviewed repository plan is required")
	}
	prefix := "/projects/" + strconv.Itoa(s.Project.ID)
	branch := "justcd/setup-" + p.Hash[:20]
	var existing []MergeRequest
	query := "?state=opened&source_branch=" + url.QueryEscape(branch) + "&target_branch=" + url.QueryEscape(s.Revision)
	if err := g.request(ctx, "GET", prefix+"/merge_requests"+query, nil, &existing); err != nil {
		return MergeRequest{}, err
	}
	if len(existing) > 0 {
		if err := validateProposalURL(s, existing[0]); err != nil {
			return MergeRequest{}, err
		}
		return existing[0], nil
	}
	// Atomic create-only actions on a new branch starting at the reviewed SHA.
	// A retry after an interrupted commit can reuse that deterministic branch.
	var b struct {
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	err := g.request(ctx, "GET", prefix+"/repository/branches/"+url.PathEscape(branch), nil, &b)
	var pe *ProviderError
	if errors.As(err, &pe) && pe.Status == 404 {
		actions := []commitAction{}
		for _, f := range p.Files {
			actions = append(actions, commitAction{Action: "create", File: f})
		}
		if err = g.commit(ctx, prefix, map[string]string{"branch": branch, "start_sha": s.SHA, "commit_message": "JustCD setup " + p.Hash + " [skip ci]"}, actions); err != nil {
			return MergeRequest{}, err
		}
	} else if err != nil {
		return MergeRequest{}, err
	} else if strings.TrimSpace(b.Commit.Message) != "JustCD setup "+p.Hash+" [skip ci]" {
		return MergeRequest{}, errors.New("proposal branch already exists with different changes")
	}
	var result MergeRequest
	err = g.request(ctx, "POST", prefix+"/merge_requests", map[string]any{"source_branch": branch, "target_branch": s.Revision, "title": "Set up JustCD deployment", "description": "Adds missing deployment/build files without replacing existing files.\n\n" + strings.Join(p.Notes, "\n\n"), "remove_source_branch": true}, &result)
	if err != nil {
		return result, err
	}
	return result, validateProposalURL(s, result)
}
func validateProposalURL(s RepositorySnapshot, m MergeRequest) error {
	if m.IID <= 0 || !strings.HasPrefix(m.WebURL, strings.TrimSuffix(s.Project.RepositoryURL, ".git")+"/-/merge_requests/") {
		return errors.New("GitLab returned an incomplete merge request")
	}
	return nil
}
