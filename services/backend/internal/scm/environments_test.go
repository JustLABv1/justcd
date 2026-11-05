package scm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func TestRecordGitLabDeploymentCreatesEnvironmentThenDeployment(t *testing.T) {
	var calls []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "secret-token" {
			t.Errorf("missing token on %s %s", r.Method, r.URL)
		}
		_ = r.ParseForm()
		calls = append(calls, r.Method+" "+r.URL.EscapedPath()+" "+r.Form.Encode())
		switch {
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/deployments"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":42}`))
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":7}`))
		}
	}))
	defer server.Close()
	id, err := (Client{HTTP: server.Client()}).RecordGitLabDeployment(context.Background(), server.URL, "infra/setup-infra", "secret-token", GitLabDeployment{
		Environment: "prod/shop", Tier: "production", ExternalURL: "https://shop.example.com", SHA: testSHA, Ref: "main", Status: "success",
	})
	if err != nil || id != 42 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	if len(calls) != 3 || !strings.HasPrefix(calls[0], "GET /projects/infra%2Fsetup-infra/environments") ||
		!strings.HasPrefix(calls[1], "POST /projects/infra%2Fsetup-infra/environments ") || !strings.Contains(calls[1], "tier=production") ||
		!strings.HasPrefix(calls[2], "POST /projects/infra%2Fsetup-infra/deployments ") {
		t.Fatalf("unexpected calls: %q", calls)
	}
	form, _ := url.ParseQuery(strings.SplitN(calls[2], " ", 3)[2])
	if form.Get("environment") != "prod/shop" || form.Get("sha") != testSHA || form.Get("ref") != "main" || form.Get("status") != "success" || form.Get("tag") != "false" {
		t.Fatalf("unexpected deployment form: %v", form)
	}
}

func TestRecordGitLabDeploymentUpdatesChangedEnvironment(t *testing.T) {
	var calls []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		calls = append(calls, r.Method+" "+r.URL.Path+" "+r.Form.Encode())
		switch {
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[{"id":9,"name":"prod/shop-old","tier":"other"},{"id":5,"name":"prod/shop","tier":"other","external_url":"https://shop.example.com"}]`))
		case strings.HasSuffix(r.URL.Path, "/deployments"):
			_, _ = w.Write([]byte(`{"id":1}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	_, err := (Client{HTTP: server.Client()}).RecordGitLabDeployment(context.Background(), server.URL, "g/p", "t", GitLabDeployment{
		Environment: "prod/shop", Tier: "production", ExternalURL: "https://shop.example.com", SHA: testSHA, Status: "failed",
	})
	if err != nil || len(calls) != 3 || !strings.HasPrefix(calls[1], "PUT /projects/g/p/environments/5 tier=production") {
		t.Fatalf("err=%v calls=%q", err, calls)
	}
	if !strings.Contains(calls[2], "ref="+testSHA) {
		t.Fatalf("ref should fall back to the SHA: %q", calls[2])
	}
}

func TestRecordGitLabDeploymentRejectsBadInputAndHTTPFailures(t *testing.T) {
	c := Client{}
	if _, err := c.RecordGitLabDeployment(context.Background(), "https://gitlab.example/api/v4", "g/p", "t", GitLabDeployment{Environment: "e", SHA: "short", Status: "success"}); err == nil {
		t.Fatal("expected SHA validation error")
	}
	if _, err := c.RecordGitLabDeployment(context.Background(), "https://gitlab.example/api/v4", "g/p", "t", GitLabDeployment{Environment: "e", SHA: testSHA, Status: "bogus"}); err == nil {
		t.Fatal("expected status validation error")
	}
	if _, err := c.RecordGitLabDeployment(context.Background(), "http://gitlab.example/api/v4", "g/p", "t", GitLabDeployment{Environment: "e", SHA: testSHA, Status: "success"}); err == nil {
		t.Fatal("expected plain HTTP to be rejected")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	if _, err := (Client{HTTP: server.Client()}).RecordGitLabDeployment(context.Background(), server.URL, "g/p", "t", GitLabDeployment{Environment: "e", SHA: testSHA, Status: "success"}); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected HTTP 403 error, got %v", err)
	}
}
