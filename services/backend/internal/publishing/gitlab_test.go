package publishing

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func TestGitLabDoesNotAdoptExistingRepository(t *testing.T) {
	g := GitLab{URL: "https://gitlab.example.com", Token: "secret-token", HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("mutated unrelated repository")
		}
		return response(200, `{"id":1,"description":"somebody else's project"}`), nil
	})}}
	if _, e := g.EnsureProject(context.Background(), "analytics", "stats", "managed-marker"); e == nil {
		t.Fatal("adopted unrelated repository")
	}
}
func TestGitLabNeverReturnsProviderSecretBodies(t *testing.T) {
	g := GitLab{URL: "https://gitlab.example.com", Token: "secret-token", HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		return response(403, `{"error":"secret-token registry-password"}`), nil
	})}}
	_, e := g.StartPipeline(context.Background(), 1)
	if e == nil || strings.Contains(e.Error(), "secret-token") || strings.Contains(e.Error(), "registry-password") {
		t.Fatal("unsafe provider error")
	}
}
func TestReplaceSourceDeletesRemovedFilesButPreservesDeployment(t *testing.T) {
	calls := 0
	g := GitLab{URL: "https://gitlab.example.com", Token: "secret-token", HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return response(200, `[{"path":"source/old.R","type":"blob"},{"path":"source/app.R","type":"blob"},{"path":"deploy/application.yaml","type":"blob"}]`), nil
		case 2:
			return response(200, `{"commit":{"id":"abc"}}`), nil
		default:
			var body struct {
				Actions []struct {
					Action string `json:"action"`
					Path   string `json:"file_path"`
				}
			}
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Fatal(e)
			}
			deleted := false
			updated := false
			for _, a := range body.Actions {
				if a.Path == "deploy/application.yaml" {
					t.Fatal("changed running deployment before successful build")
				}
				if a.Path == "source/old.R" && a.Action == "delete" {
					deleted = true
				}
				if a.Path == "source/app.R" && a.Action == "update" {
					updated = true
				}
			}
			if !deleted || !updated {
				t.Fatal("source snapshot was not replaced")
			}
			return response(201, `{}`), nil
		}
	})}}
	if e := g.ReplaceSource(context.Background(), Project{ID: 1}, []File{{Path: "source/app.R", Content: "new", Encoding: "text"}}, "fingerprint"); e != nil {
		t.Fatal(e)
	}
}

func TestDiskCommitStreamsBinaryContentAndCleansTemporaryPayload(t *testing.T) {
	dir := t.TempDir()
	source := dir + "/source"
	binary := []byte{0, 1, 2, 255, '"', '\\', '\n'}
	if err := os.WriteFile(source, binary, 0600); err != nil {
		t.Fatal(err)
	}
	var payloadName string
	g := GitLab{URL: "https://gitlab.example.com", Token: "secret-token", TempDir: dir, HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		file, ok := r.Body.(*os.File)
		if !ok {
			t.Fatal("commit request was buffered instead of streamed from disk")
		}
		payloadName = file.Name()
		if r.ContentLength <= 0 {
			t.Fatal("missing request length")
		}
		var body struct {
			Actions []struct {
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		got, err := base64.StdEncoding.DecodeString(body.Actions[0].Content)
		if err != nil || !bytes.Equal(got, binary) {
			t.Fatal("binary source changed")
		}
		return response(403, `{}`), nil
	})}}
	err := g.commit(context.Background(), "/projects/1", map[string]string{"branch": "main"}, []commitAction{{Action: "create", File: File{Path: "source/data.bin", DiskPath: source, Encoding: "base64"}}})
	if err == nil {
		t.Fatal("provider error lost")
	}
	if _, err = os.Stat(payloadName); !os.IsNotExist(err) {
		t.Fatal("temporary commit payload retained after failure")
	}
}
