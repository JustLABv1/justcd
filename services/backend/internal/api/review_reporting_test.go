package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/store"
)

type reviewStatusTransport func(*http.Request) (*http.Response, error)

func (f reviewStatusTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReviewCommitStatusesRequireOptIn(t *testing.T) {
	for _, provider := range []string{"gitlab", "github"} {
		t.Run(provider, func(t *testing.T) {
			calls := 0
			client := scm.Client{HTTP: &http.Client{Transport: reviewStatusTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/statuses/") {
					t.Fatalf("unexpected status request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
			})}}
			connection := store.SourceControlConnection{Provider: provider, APIURL: "https://provider.example/api", Repository: "group/app", ApplicationID: "app"}
			review := store.PullRequestReview{HeadSHA: strings.Repeat("a", 40), Phase: "failed"}
			for _, state := range []string{"pending", "running", "failed", "success"} {
				if err := reportReviewCommitStatus(context.Background(), client, connection, review, "token", state, "preview status", "https://justcd.example/review"); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 0 {
				t.Fatalf("default reporting made %d pipeline mutations", calls)
			}
			connection.PipelineStatusReporting = true
			if err := reportReviewCommitStatus(context.Background(), client, connection, review, "token", "failed", "preview failed", "https://justcd.example/review"); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("opt-in reporting made %d requests", calls)
			}
		})
	}
}

func TestReviewCommentsIncludeFailureWithoutPlan(t *testing.T) {
	review := store.PullRequestReview{HeadSHA: strings.Repeat("a", 40), Phase: "failed"}
	key, body, err := reviewComment(review, "JustCD could not prepare a healthy preview", "https://justcd.example/review")
	if err != nil || !strings.Contains(body, "healthy preview") || !strings.Contains(body, review.HeadSHA[:8]) || !strings.Contains(body, "https://justcd.example/review") {
		t.Fatalf("missing review feedback: %q, %v", body, err)
	}
	repeatKey, _, _ := reviewComment(review, "JustCD could not prepare a healthy preview", "https://justcd.example/review")
	if key != repeatKey {
		t.Fatal("same feedback must deduplicate")
	}
	review.Phase = "ready"
	nextKey, _, _ := reviewComment(review, "JustCD preview is healthy", "https://justcd.example/review")
	if nextKey == key {
		t.Fatal("phase changes must report new feedback")
	}
	review.Plan = json.RawMessage(`{"id":"plan-1","status":"review-only","plan":{"digest":"digest-1"}}`)
	_, body, err = reviewComment(review, "JustCD preview is healthy", "https://justcd.example/review")
	if err != nil || !strings.Contains(body, "plan-1") || !strings.Contains(body, "digest-1") || !strings.Contains(body, "Deployment plan details") {
		t.Fatalf("missing plan summary: %q, %v", body, err)
	}
}

func TestReviewCommentPolicies(t *testing.T) {
	for _, tt := range []struct {
		mode               string
		draft, optIn, want bool
	}{
		{"", false, false, true}, {"summary", true, false, false}, {"summary", true, true, true}, {"off", false, true, false}, {"action-required", false, false, true},
	} {
		if got := reviewCommentsEnabled(store.SourceControlConnection{CommentMode: tt.mode, CommentOnDrafts: tt.optIn}, store.PullRequestReview{Draft: tt.draft}); got != tt.want {
			t.Fatalf("%+v: %v", tt, got)
		}
	}
	for _, phase := range []string{"failed", "degraded", "blocked", "approval_required", "cleanup_pending", "adoption_available"} {
		if !reviewNeedsAction(phase) {
			t.Fatal(phase)
		}
	}
	for _, phase := range []string{"pending", "syncing", "ready", "planned", "removed", "closed"} {
		if reviewNeedsAction(phase) {
			t.Fatal(phase)
		}
	}
}

func TestReviewCommentErrorChangesAreReported(t *testing.T) {
	review := store.PullRequestReview{HeadSHA: strings.Repeat("a", 40), Phase: "failed", Error: "failed <resource>"}
	key, body, err := reviewComment(review, "Preview failed", "https://justcd.example/review")
	if err != nil || !strings.Contains(body, "&lt;resource&gt;") {
		t.Fatal(body, err)
	}
	review.Error = "another failure"
	next, _, _ := reviewComment(review, "Preview failed", "https://justcd.example/review")
	if key == next {
		t.Fatal("changed error must update summary")
	}
}
