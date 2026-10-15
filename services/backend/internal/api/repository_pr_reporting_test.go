package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestRepositoryDiscoveryFailureSummary(t *testing.T) {
	event := scm.Event{Number: 147, HeadSHA: strings.Repeat("a", 40)}
	failure := repositoryPRBindingError("review-only", "envs/demo/dev/mimir/justcd.yaml", "mimir-dev")
	body := discoveryFailureComment(event, failure)
	for _, value := range []string{"mimir-dev", "review-only", "aaaaaaaa"} {
		if !strings.Contains(body, value) {
			t.Fatalf("missing %s: %s", value, body)
		}
	}
	body = discoveryFailureComment(event, errors.New("invalid <definition>"))
	if !strings.Contains(body, "&lt;definition&gt;") {
		t.Fatal("unsafe error markup")
	}
}

func TestReviewOnlyBindingErrorDoesNotSuggestDeployment(t *testing.T) {
	message := repositoryPRBindingError("review-only", "justcd.yaml", "mimir-dev").Error()
	if !strings.Contains(message, "existing workspace binding") || strings.Contains(message, "Allow creation") || strings.Contains(message, "createNamespaces: true") {
		t.Fatalf("misleading review-only guidance: %s", message)
	}
}

func TestDraftDiscoveryCommentsAreSuppressed(t *testing.T) {
	// A nil store/client is intentional: suppressed reporting must do no I/O.
	s := &Server{}
	for _, c := range []store.SourceControlConnection{{CommentMode: "off"}, {}} {
		if err := s.reportRepositoryPRDiscoveryFailure(context.Background(), scm.Client{}, c, store.RepositoryConfiguration{}, scm.Event{Draft: true}, "token", errors.New("blocked")); err != nil {
			t.Fatal(err)
		}
	}
}
