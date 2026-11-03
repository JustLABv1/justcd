package publishing

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sigs.k8s.io/yaml"
	"strings"
	"testing"
)

func repoSnapshot(paths ...string) RepositorySnapshot {
	s := RepositorySnapshot{Project: Project{ID: 1, RepositoryURL: "https://gitlab.example.com/analytics/report.git"}, Revision: "release/reports", SHA: strings.Repeat("a", 40)}
	for _, p := range paths {
		s.Entries = append(s.Entries, RepositoryEntry{Path: p, Type: "blob", Mode: "100644"})
	}
	return s
}
func TestRepositoryPlansReuseExistingBuilds(t *testing.T) {
	for _, runtime := range []string{"shiny", "streamlit", "python-static"} {
		t.Run(runtime, func(t *testing.T) {
			c := config()
			c.Runtime = runtime
			c.EntryPoint = "."
			source := []string{"apps/report/app.R", "apps/report/packages.txt"}
			if runtime == "streamlit" {
				c.EntryPoint = "app.py"
				source = []string{"apps/report/app.py", "apps/report/requirements.txt"}
			}
			if runtime == "python-static" {
				c.EntryPoint = "build_html.py"
				c.BuildCommand = "python build_html.py"
				c.OutputDir = "dist"
				c.WebServerImage = "registry.invalid/nginx:1"
				source = []string{"apps/report/build_html.py"}
			}
			for _, existing := range []string{"none", "docker", "pipeline"} {
				t.Run(existing, func(t *testing.T) {
					s := repoSnapshot(source...)
					if existing != "none" {
						s.Entries = append(s.Entries, RepositoryEntry{Path: "apps/report/Dockerfile", Type: "blob", Mode: "100644"})
					}
					if existing == "pipeline" {
						s.Entries = append(s.Entries, RepositoryEntry{Path: ".gitlab-ci.yml", Type: "blob", Mode: "100644"})
					}
					image := ""
					if existing == "pipeline" {
						image = "registry.example.com/apps/report@sha256:" + strings.Repeat("b", 64)
					}
					p, err := PlanRepository(c, s, "apps/report", image)
					if err != nil {
						t.Fatal(err)
					}
					seen := map[string]string{}
					for _, f := range p.Files {
						seen[f.Path] = f.Content
						for _, e := range s.Entries {
							if f.Path == e.Path {
								t.Fatal("existing file overwritten", f.Path)
							}
						}
					}
					if existing == "none" && !strings.Contains(seen["apps/report/Dockerfile"], "COPY . /app/") {
						t.Fatal("wrong build context")
					}
					if existing == "pipeline" {
						if _, ok := seen[".gitlab-ci.yml"]; ok {
							t.Fatal("pipeline replaced")
						}
						if !strings.Contains(seen["deploy/statistics/application.yaml"], image) {
							t.Fatal("image digest missing")
						}
					} else {
						ci := seen[".gitlab-ci.yml"]
						var parsed map[string]any
						if err := yaml.Unmarshal([]byte(ci), &parsed); err != nil {
							t.Fatal("invalid generated CI", err)
						}
						if !strings.Contains(ci, "--local context=apps/report --local dockerfile=apps/report") || !strings.Contains(ci, "release/reports") || !strings.Contains(ci, "mkdir -p deploy/statistics") || !strings.Contains(ci, ".justcd/statistics/deployment.yaml") {
							t.Fatal("incorrect pipeline", ci)
						}
					}
				})
			}
		})
	}
}
func TestRepositoryPlanRefusesConflictsAndAmbiguousPaths(t *testing.T) {
	for _, conflict := range []RepositoryEntry{{Path: ".justcd/statistics/README.md", Type: "blob"}, {Path: ".justcd", Type: "blob"}, {Path: "deploy/statistics/application.yaml", Type: "blob"}, {Path: "apps", Type: "commit"}, {Path: "apps/report/Dockerfile", Type: "blob", Mode: "120000"}} {
		s := repoSnapshot(".gitlab-ci.yml")
		s.Entries = append(s.Entries, conflict)
		// A pipeline supplies a valid image, so source/dependency checks are irrelevant.
		_, err := PlanRepository(config(), s, "apps/report", "registry.example.com/apps/report@sha256:"+strings.Repeat("b", 64))
		if conflict.Path == "apps/report/Dockerfile" { // Existing symlink Dockerfile isn't used by an existing pipeline.
			continue
		}
		if err == nil {
			t.Fatal("unsafe path accepted", conflict)
		}
	}
	for _, dir := range []string{"../app", "/app", "app;echo", "app with spaces"} {
		if _, err := PlanRepository(config(), repoSnapshot(), dir, ""); err == nil {
			t.Fatal("unsafe context accepted", dir)
		}
	}
	for _, branch := range []string{"../main", "main;echo", "-main", "a//b", "a.lock", "a/"} {
		if ValidBranch(branch) {
			t.Fatal("unsafe branch accepted", branch)
		}
	}
}
func TestRepositoryPlanOnlyRequiresUsedBuildInputs(t *testing.T) {
	c := config()
	c.BaseImage = ""
	c.BuilderImage = ""
	c.RegistryImage = ""
	p, err := PlanRepository(c, repoSnapshot(".gitlab-ci.yml"), ".", "")
	if err != nil || !p.NeedsImage {
		t.Fatal("existing pipeline requires unrelated build inputs", err)
	}
	_, err = PlanRepository(c, repoSnapshot(".gitlab-ci.yml"), ".", "registry.example.com/report:latest")
	if err == nil {
		t.Fatal("mutable image accepted")
	}
}
func TestRepositoryProposalUsesCreateOnlyBranchAndMR(t *testing.T) {
	s := repoSnapshot("app.R", "packages.txt")
	p, err := PlanRepository(config(), s, ".", "")
	if err != nil {
		t.Fatal(err)
	}
	requests := []string{}
	g := GitLab{URL: "https://gitlab.example.com", Token: "secret-token", HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/merge_requests"):
			return response(200, `[]`), nil
		case r.Method == "GET":
			return response(404, `{}`), nil
		case strings.HasSuffix(r.URL.Path, "/repository/commits"):
			var commit struct {
				Branch  string `json:"branch"`
				Start   string `json:"start_sha"`
				Actions []struct {
					Action string `json:"action"`
				} `json:"actions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&commit); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(commit.Branch, "justcd/setup-") || commit.Start != s.SHA {
				t.Fatal("target branch mutated", commit)
			}
			for _, a := range commit.Actions {
				if a.Action != "create" {
					t.Fatal("non-additive action", a)
				}
			}
			return response(201, `{}`), nil
		case strings.HasSuffix(r.URL.Path, "/merge_requests"):
			data, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(data), `"target_branch":"release/reports"`) {
				t.Fatal("wrong target", string(data))
			}
			return response(201, `{"web_url":"https://gitlab.example.com/analytics/report/-/merge_requests/1","iid":1}`), nil
		default:
			t.Fatal("unexpected mutation", r.Method, r.URL)
			return nil, nil
		}
	})}}
	mr, err := g.ProposeRepository(context.Background(), s, p)
	if err != nil || mr.IID != 1 {
		t.Fatal(err, mr)
	}
	if len(requests) != 4 {
		t.Fatal(requests)
	}
}
func TestRepositoryInspectionPinsTreeToCommit(t *testing.T) {
	sha := strings.Repeat("a", 40)
	g := GitLab{URL: "https://gitlab.example.com", Token: "secret-token", HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("inspection mutated repository")
		}
		if strings.HasSuffix(r.URL.Path, "/repository/tree") {
			if r.URL.Query().Get("ref") != sha {
				t.Fatal("unpinned tree")
			}
			return response(200, `[{"path":"app.R","type":"blob","mode":"100644"}]`), nil
		}
		if strings.Contains(r.URL.Path, "/repository/branches/") {
			return response(200, `{"commit":{"id":"`+sha+`"}}`), nil
		}
		return response(200, `{"id":1,"http_url_to_repo":"https://gitlab.example.com/analytics/report.git"}`), nil
	})}}
	s, err := g.InspectRepository(context.Background(), "analytics/report", "main")
	if err != nil || s.SHA != sha || len(s.Entries) != 1 {
		t.Fatal(s, err)
	}
}

func TestRepositoryPreservesCustomCIConfiguration(t *testing.T) {
	s := repoSnapshot()
	s.Project.CIConfigPath = ".gitlab/custom.yml"
	p, err := PlanRepository(config(), s, ".", "registry.example.com/report@sha256:"+strings.Repeat("b", 64))
	if err != nil || !p.HasPipeline {
		t.Fatal("custom CI not detected", err)
	}
	for _, f := range p.Files {
		if f.Path == ".gitlab-ci.yml" || f.Path == "Dockerfile" {
			t.Fatal("existing custom CI replaced", f.Path)
		}
	}
}
