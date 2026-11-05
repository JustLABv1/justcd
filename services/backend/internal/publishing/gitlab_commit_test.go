package publishing

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func decodeCommit(t *testing.T, actions []commitAction) []map[string]string {
	t.Helper()
	var buf bytes.Buffer
	if err := writeCommit(context.Background(), &buf, map[string]string{"branch": "main", "commit_message": "m"}, actions); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Actions []map[string]string `json:"actions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("commit payload is not JSON: %v\n%s", err, buf.String())
	}
	return payload.Actions
}

func TestPackageScriptIsCommittedEncoded(t *testing.T) {
	script := "chmod 700 /dev/shm/x && unset -f f\n"
	out := decodeCommit(t, []commitAction{
		{Action: "create", File: File{Path: FilePackageSources, Content: script, Encoding: "text"}},
		{Action: "create", File: File{Path: "Dockerfile", Content: "FROM x\n", Encoding: "text"}},
	})
	if out[0]["encoding"] != "base64" || strings.Contains(out[0]["content"], "chmod") {
		t.Fatalf("the script must travel encoded: %v", out[0])
	}
	if decoded, _ := base64.StdEncoding.DecodeString(out[0]["content"]); string(decoded) != script {
		t.Fatalf("the script changed in transit: %q", decoded)
	}
	if out[1]["encoding"] != "text" || out[1]["content"] != "FROM x\n" {
		t.Fatalf("other files keep their encoding: %v", out[1])
	}
}

func TestRecipeUpgradeCommitsAreEncoded(t *testing.T) {
	var sent []map[string]string
	client := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		body := `{}`
		switch {
		case r.Method == "GET":
			body = `{"commit":{"id":"` + strings.Repeat("a", 40) + `"}}`
		case r.Method == "POST":
			var payload struct {
				Actions []map[string]string `json:"actions"`
			}
			data, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatal(err)
			}
			sent = payload.Actions
			body = `{"parent_ids":["` + strings.Repeat("a", 40) + `"]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"application/json"}}}, nil
	})}
	g := GitLab{URL: "https://gitlab.example.com", Token: "token-value-123", HTTP: client, TempDir: t.TempDir()}
	snapshot := RepositorySnapshot{Revision: "main", SHA: strings.Repeat("a", 40), Project: Project{ID: 7}}
	err := g.CommitRecipeUpgrade(context.Background(), snapshot, []RecipeChange{{Path: ".gitlab-ci.yml", After: "script: [echo $(id)]\n", Exists: true, LastCommitID: "abc"}}, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0]["encoding"] != "base64" {
		t.Fatalf("recipe files must be sent encoded: %v", sent)
	}
	if decoded, _ := base64.StdEncoding.DecodeString(sent[0]["content"]); string(decoded) != "script: [echo $(id)]\n" {
		t.Fatalf("content changed in transit: %q", decoded)
	}
}

func TestInvalidResponseShowsWhatAnswered(t *testing.T) {
	page := `<html><head><title>Request Rejected</title><style>p{color:red}</style></head><body><script>var a=1;</script><p>The requested URL was rejected.</p> <p>Your support ID is: 4711</p></body></html>`
	client := &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(page)), Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}}, nil
	})}
	g := GitLab{URL: "https://gitlab.example.com", Token: "token-value-123", HTTP: client}
	var out struct{ ParentIDs []string }
	err := g.request(context.Background(), "POST", "/projects/7/repository/commits", nil, &out)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"POST /projects/7/repository/commits", "HTTP 200", "text/html", "Request Rejected", "support ID is: 4711"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q: %s", want, err)
		}
	}
	if strings.Contains(err.Error(), "<") || strings.Contains(err.Error(), "var a=1") {
		t.Fatalf("markup and scripts must be stripped: %s", err)
	}
}
