package publishing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuccessfulBuildIdentity(t *testing.T) {
	sha := strings.Repeat("a", 40)
	digest := "sha256:" + strings.Repeat("b", 64)
	for _, mode := range []string{"valid", "pending", "wrong-job-sha", "invalid-digest", "redirect", "completed"} {
		t.Run(mode, func(t *testing.T) {
			artifacts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("PRIVATE-TOKEN") != "test-token" {
					t.Error("missing auth")
				}
				switch {
				case strings.Contains(r.URL.Path, "/artifacts/"):
					artifacts++
					if mode == "redirect" {
						http.Redirect(w, r, "https://example.com/secret", 302)
						return
					}
					if mode == "invalid-digest" {
						fmt.Fprint(w, "bad")
						return
					}
					fmt.Fprint(w, digest)
				case strings.HasSuffix(r.URL.Path, "/jobs"):
					jobsha := sha
					if mode == "wrong-job-sha" {
						jobsha = strings.Repeat("c", 40)
					}
					fmt.Fprintf(w, `[{"id":8,"name":"publish","status":"success","ref":"main","commit":{"id":%q},"pipeline":{"id":5}}]`, jobsha)
				case strings.HasSuffix(r.URL.Path, "/pipelines/5"):
					status := "success"
					if mode == "pending" {
						status = "running"
					}
					json.NewEncoder(w).Encode(BuildResult{ID: 5, SHA: sha, Ref: "main", Status: status})
				default:
					fmt.Fprintf(w, `[{"id":6,"status":"skipped"},{"id":5,"status":"success"}]`)
				}
			}))
			defer srv.Close()
			g := GitLab{URL: srv.URL, Token: "test-token", HTTP: srv.Client()}
			completed := int64(0)
			if mode == "completed" {
				completed = 5
			}
			p, d, err := g.SuccessfulBuild(context.Background(), 1, completed)
			if mode == "valid" {
				if err != nil || d != digest || p.ID != 5 {
					t.Fatalf("unexpected result %v %s", err, d)
				}
			} else if mode == "pending" || mode == "completed" {
				if err != nil || d != "" || artifacts != 0 {
					t.Fatal("pending/completed build downloaded artifacts")
				}
			} else if err == nil {
				t.Fatal("invalid build accepted")
			}
		})
	}
}

func TestManagedCompletionPipeline(t *testing.T) {
	c := config()
	c.BuildEngine = "podman"
	c.ManagedCompletion = true
	ci := pipeline(c)
	for _, forbidden := range []string{"update-deployment:", "git push", "git fetch", "jq -"} {
		if strings.Contains(ci, forbidden) {
			t.Fatalf("unexpected managed CI command %s", forbidden)
		}
	}
	if !strings.Contains(ci, "artifacts:") || !strings.Contains(ci, ".justcd-build/image-digest") {
		t.Fatal("missing digest handoff")
	}
}

func TestCommitDeploymentFastForwardAndRecovery(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git failed: %s %v", out, err)
		}
		return strings.TrimSpace(string(out))
	}
	run(root, "init", "--bare", remote)
	run(root, "init", "-b", "main", work)
	os.MkdirAll(filepath.Join(work, ".justcd"), 0700)
	os.MkdirAll(filepath.Join(work, "deploy"), 0700)
	os.WriteFile(filepath.Join(work, ".justcd/deployment.yaml"), []byte("image: JUSTCD_IMAGE_DIGEST\n"), 0600)
	os.WriteFile(filepath.Join(work, "app.html"), []byte("original source"), 0600)
	run(work, "add", ".")
	run(work, "commit", "-m", "source")
	run(work, "remote", "add", "origin", remote)
	run(work, "push", "origin", "main")
	sha := run(work, "rev-parse", "HEAD")
	digest := "sha256:" + strings.Repeat("a", 64)
	complete := func() error {
		return commitDeployment(context.Background(), remote, "not-a-real-token", sha, digest, "registry.example.com/apps/test", 42, true)
	}
	if err := complete(); err != nil {
		t.Fatal(err)
	}
	head := run(root, "--git-dir="+remote, "rev-parse", "main")
	if run(root, "--git-dir="+remote, "rev-parse", "main^") != sha {
		t.Fatal("wrong source parent")
	}
	if got := run(root, "--git-dir="+remote, "show", "main:app.html"); got != "original source" {
		t.Fatal("source overwritten")
	}
	if got := run(root, "--git-dir="+remote, "show", "main:deploy/application.yaml"); !strings.Contains(got, digest) {
		t.Fatal("wrong manifest")
	}
	if err := complete(); err != nil {
		t.Fatal("crash recovery failed", err)
	}
	if run(root, "--git-dir="+remote, "rev-parse", "main") != head {
		t.Fatal("duplicate commit")
	}
	run(work, "pull", "--ff-only", "origin", "main")
	os.WriteFile(filepath.Join(work, "app.html"), []byte("new source"), 0600)
	run(work, "add", ".")
	run(work, "commit", "-m", "new source")
	run(work, "push", "origin", "main")
	latest := run(work, "rev-parse", "HEAD")
	if err := complete(); err == nil {
		t.Fatal("stale build accepted")
	}
	if run(root, "--git-dir="+remote, "rev-parse", "main") != latest {
		t.Fatal("new source overwritten")
	}
}

func TestRootlessRunnerSettings(t *testing.T) {
	c := config()
	c.BuildEngine = "podman"
	c.RunnerServiceAccount = "gitlab-runner-app-sa"
	c.RunnerSCC = "gitlab-runner-scc"
	c.BuilderSetup = "/home/user/entrypoint.sh"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	ci := pipeline(c)
	for _, want := range []string{
		`KUBERNETES_SERVICE_ACCOUNT_OVERWRITE: "gitlab-runner-app-sa"`,
		`KUBERNETES_POD_ANNOTATIONS_1: "openshift.io/required-scc=gitlab-runner-scc"`,
		"  before_script:\n    - \"/home/user/entrypoint.sh\"\n  script:",
	} {
		if !strings.Contains(ci, want) {
			t.Fatalf("missing %q in pipeline", want)
		}
	}
	c.BuilderSetup = "entrypoint.sh"
	if c.Validate() == nil {
		t.Fatal("relative builder setup accepted")
	}
	c.BuilderSetup = "/run.sh; curl evil"
	if c.Validate() == nil {
		t.Fatal("builder setup with shell syntax accepted")
	}
	c.BuilderSetup, c.RunnerSCC = "", "Bad SCC"
	if c.Validate() == nil {
		t.Fatal("invalid SCC accepted")
	}
}

func TestExposureResource(t *testing.T) {
	c := config()
	c.Host = "app.apps.example.com"
	plain := manifest(c, 8080)
	if !strings.Contains(plain, "kind: Route") || strings.Contains(plain, "labels:\n    \"") {
		t.Fatal("default exposure must stay an unlabeled Route")
	}
	c.ExposureLabels = "router=external\nexample.com/team = data"
	c.ExposureAnnotations = "haproxy.router.openshift.io/timeout=5m"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	route := manifest(c, 8080)
	for _, want := range []string{"kind: Route", `"router": "external"`, `"example.com/team": "data"`, `"haproxy.router.openshift.io/timeout": "5m"`} {
		if !strings.Contains(route, want) {
			t.Fatalf("route missing %q:\n%s", want, route)
		}
	}
	c.Exposure, c.IngressClass = "ingress", "openshift-default"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	ing := manifest(c, 8080)
	for _, want := range []string{"kind: Ingress", "networking.k8s.io/v1", `ingressClassName: "openshift-default"`, `"router": "external"`, "host: app.apps.example.com"} {
		if !strings.Contains(ing, want) {
			t.Fatalf("ingress missing %q:\n%s", want, ing)
		}
	}
	c.IngressTLS = "none"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if none := manifest(c, 8080); !strings.Contains(none, "  tls: []\n") || strings.Contains(none, "hosts:") {
		t.Fatalf("expected tls: [] without hosts:\n%s", none)
	}
	c.IngressTLS = ""
	if strings.Contains(ing, "kind: Route") {
		t.Fatal("ingress exposure must not create a Route")
	}
	for _, bad := range []string{"no-equals", "bad key=x", "a=b\na=c", "k=has space"} {
		c.ExposureLabels = bad
		if c.Validate() == nil {
			t.Fatalf("label %q accepted", bad)
		}
	}
	c.ExposureLabels, c.Exposure = "", "route"
	if c.Validate() == nil {
		t.Fatal("ingress class accepted without ingress exposure")
	}
}
