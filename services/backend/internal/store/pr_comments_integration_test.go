//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justlab/justcd/services/backend/internal/scm"
)

func testPRSummaryComments(t *testing.T, ctx context.Context, s *Store) {
	for _, provider := range []string{"github", "gitlab"} {
		var mu sync.Mutex
		var item scm.Comment
		requests, posts, updates := 0, 0, 0
		denied := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			requests++
			if denied {
				w.WriteHeader(403)
				return
			}
			if r.URL.Path == "/user" {
				_, _ = w.Write([]byte(`{"id":42}`))
				return
			}
			if r.Method == http.MethodGet {
				if strings.HasSuffix(r.URL.Path, "/comments") || strings.HasSuffix(r.URL.Path, "/notes") {
					items := []scm.Comment{}
					if item.ID > 0 {
						items = append(items, item)
					}
					_ = json.NewEncoder(w).Encode(items)
				} else {
					_ = json.NewEncoder(w).Encode(item)
				}
				return
			}
			var payload struct{ Body string }
			_ = json.NewDecoder(r.Body).Decode(&payload)
			item.Body = payload.Body
			item.ID = 77
			item.User.ID = 42
			item.Author.ID = 42
			if r.Method == http.MethodPost {
				posts++
				w.WriteHeader(201)
			} else {
				updates++
			}
			_ = json.NewEncoder(w).Encode(item)
		}))
		c := SourceControlConnection{WorkspaceID: "integration-workspace", Provider: provider, APIURL: server.URL, Repository: "org/app"}
		write := func(section, body string, create bool) error {
			return s.UpdatePRComment(ctx, scm.Client{}, c, 42, "token", section, body, create)
		}
		if err := write("app-a", "healthy", false); err != nil {
			t.Fatal(err)
		}
		if requests != 0 {
			t.Fatal("action-only mode reported a healthy app")
		}
		if err := write("app-a", "approval required", true); err != nil {
			t.Fatal(err)
		}
		before := requests
		if err := write("app-a", "approval required", true); err != nil {
			t.Fatal(err)
		}
		if requests != before {
			t.Fatal("unchanged body made provider requests")
		}
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, entry := range []struct{ key, body string }{{"app-b", "deploying b"}, {"discovery:repo", "discovery blocked"}} {
			wg.Add(1)
			go func(key, body string) { defer wg.Done(); errs <- write(key, body, true) }(entry.key, entry.body)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if posts != 1 || !strings.Contains(item.Body, "approval required") || !strings.Contains(item.Body, "deploying b") || !strings.Contains(item.Body, "discovery blocked") {
			t.Fatal("lost section or duplicate comment", posts, item.Body)
		}
		if err := write("discovery:repo", "", false); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(item.Body, "discovery blocked") {
			t.Fatal("resolved discovery error remained")
		}
		if err := write("app-a", "healthy", false); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(item.Body, "healthy") || strings.Contains(item.Body, "approval required") {
			t.Fatal("action-only recovery missing")
		}
		denied = true
		before = updates
		if err := write("app-a", "failed", true); err == nil {
			t.Fatal("permission error swallowed")
		}
		denied = false
		if err := write("app-a", "failed", true); err != nil {
			t.Fatal(err)
		}
		if updates != before+1 || posts != 1 {
			t.Fatal("failed report was not retried cleanly")
		}
		server.Close()
	}
	// Reporting preferences round-trip independently from preview policy.
	c, err := s.SourceControlConnectionByApplication(ctx, "integration-app")
	if err != nil {
		t.Fatal(err)
	}
	c.CommentMode = "action-required"
	c.CommentOnDrafts = true
	if err = s.SaveSourceControlConnection(ctx, c); err != nil {
		t.Fatal(err)
	}
	saved, err := s.SourceControlConnectionByApplication(ctx, "integration-app")
	if err != nil || saved.CommentMode != "action-required" || !saved.CommentOnDrafts {
		t.Fatal(saved, err)
	}
	now := time.Now().UTC()
	r := PullRequestReview{ID: NewID(), Number: 2, HeadSHA: strings.Repeat("c", 40), Draft: true, EventAt: now, HeadBranch: "feature"}
	if _, err = s.RecordReviewEvent(ctx, c.ID, "draft-start", r); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE pull_request_reviews SET phase='planned',plan='{"id":"preserved"}',error='preserved error',reported_phase='planned' WHERE id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	r.Draft = false
	r.EventAt = now.Add(time.Second)
	if _, err = s.RecordReviewEvent(ctx, c.ID, "draft-ready", r); err != nil {
		t.Fatal(err)
	}
	reviews, err := s.ListReviews(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].Draft || reviews[0].Phase != "planned" || reviews[0].Error != "preserved error" || !strings.Contains(string(reviews[0].Plan), "preserved") || reviews[0].ReportedPhase != "" {
		t.Fatal("draft transition rebuilt plan or missed reporting", reviews)
	}
	// Older events cannot undo readiness.
	r.Draft = true
	r.EventAt = now
	if _, err = s.RecordReviewEvent(ctx, c.ID, "draft-late", r); err != nil {
		t.Fatal(err)
	}
	reviews, err = s.ListReviews(ctx, c.ID)
	if err != nil || reviews[0].Draft {
		t.Fatal("stale draft event accepted", err)
	}
	if _, err = s.DB.ExecContext(ctx, `DELETE FROM pull_request_reviews WHERE id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	c.CommentMode = ""
	c.CommentOnDrafts = false
	if err = s.SaveSourceControlConnection(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err = ValidateCommentMode("always-post"); err == nil {
		t.Fatal(errors.New("invalid mode accepted"))
	}
}
