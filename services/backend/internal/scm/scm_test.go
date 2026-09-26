package scm

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGitHubWebhookVerification(t *testing.T) {
	secret := "example-webhook-secret-123"
	body := []byte(`{"action":"synchronize","number":42,"repository":{"full_name":"team/app"},"pull_request":{"html_url":"https://github.com/team/app/pull/42","updated_at":"2026-09-26T07:00:00Z","head":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","repo":{"full_name":"team/app"}}}}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	header := http.Header{"X-Hub-Signature-256": {"sha256=" + hex.EncodeToString(mac.Sum(nil))}, "X-Github-Event": {"pull_request"}, "X-Github-Delivery": {"delivery-1"}}
	event, err := VerifyAndParse("github", secret, header, body, time.Date(2026, 9, 26, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if event.Number != 42 || event.HeadSHA != strings.Repeat("a", 40) || event.Fork || event.Closed {
		t.Fatalf("unexpected event: %+v", event)
	}
	header.Set("X-Hub-Signature-256", "sha256="+strings.Repeat("0", 64))
	if _, err := VerifyAndParse("github", secret, header, body, time.Now()); err == nil {
		t.Fatal("tampered signature accepted")
	}
}

func TestGitLabLegacyAndSignedWebhooks(t *testing.T) {
	secret := "example-gitlab-secret-123"
	body := []byte(`{"object_kind":"merge_request","project":{"path_with_namespace":"group/app"},"object_attributes":{"action":"update","state":"opened","iid":7,"url":"https://gitlab.example.com/group/app/-/merge_requests/7","updated_at":"2026-09-26T07:00:00Z","source_project_id":2,"target_project_id":1,"last_commit":{"id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}}`)
	header := http.Header{"X-Gitlab-Token": {secret}, "X-Gitlab-Event": {"Merge Request Hook"}, "X-Gitlab-Event-Uuid": {"delivery-2"}}
	event, err := VerifyAndParse("gitlab", secret, header, body, time.Date(2026, 9, 26, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !event.Fork || event.Number != 7 || event.Closed {
		t.Fatalf("unexpected event: %+v", event)
	}
	header.Del("X-Gitlab-Token")
	header.Set("webhook-id", "delivery-2")
	header.Set("webhook-timestamp", "1790406000")
	when := time.Unix(1790406000, 0)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("delivery-2.1790406000."))
	mac.Write(body)
	header.Set("webhook-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	if _, err := VerifyAndParse("gitlab", secret, header, body, when); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyAndParse("gitlab", secret, header, body, when.Add(10*time.Minute)); err == nil {
		t.Fatal("old signed delivery accepted")
	}
}

func TestProviderStatusAndCurrentGitLabSelfHosted(t *testing.T) {
	sha := strings.Repeat("c", 40)
	var statusBody []byte
	client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("PRIVATE-TOKEN") != "private-token" {
			t.Errorf("missing token")
		}
		switch r.Method {
		case http.MethodGet:
			if r.URL.EscapedPath() != "/gitlab/api/v4/projects/group%2Fapp/merge_requests/9" {
				t.Errorf("unexpected GET path: %s", r.URL.EscapedPath())
			}
			body, _ := json.Marshal(map[string]any{"state": "opened", "sha": sha, "web_url": "https://gitlab.example.com/group/app/-/merge_requests/9", "updated_at": "2026-09-26T07:00:00Z", "source_project_id": 1, "target_project_id": 1})
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}, nil
		case http.MethodPost:
			if r.URL.EscapedPath() != "/gitlab/api/v4/projects/group%2Fapp/statuses/"+sha {
				t.Errorf("unexpected POST path: %s", r.URL.EscapedPath())
			}
			statusBody, _ = io.ReadAll(r.Body)
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		}
		return &http.Response{StatusCode: http.StatusMethodNotAllowed, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	})}}
	apiURL := "https://gitlab.example.com/gitlab/api/v4"
	event, err := client.Current(context.Background(), "gitlab", apiURL, "group/app", "private-token", 9)
	if err != nil {
		t.Fatal(err)
	}
	if event.HeadSHA != sha || event.Fork || event.Closed {
		t.Fatalf("unexpected current MR: %+v", event)
	}
	err = client.Report(context.Background(), "gitlab", apiURL, "group/app", "private-token", sha, "running", "preview deploying", "https://justcd.example/review", 9)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(statusBody, []byte("name=JustCD%2Fpreview%2F9")) || !bytes.Contains(statusBody, []byte("state=running")) {
		t.Fatalf("unexpected status form: %s", statusBody)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
