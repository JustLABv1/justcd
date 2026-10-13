package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestRepositoryDiscoveryFailuresReachPRWithoutApplication(t *testing.T) {
	for _, provider := range []string{"gitlab", "github"} {
		t.Run(provider, func(t *testing.T) {
			var comments []scm.Comment
			posts := 0
			client := scm.Client{HTTP: &http.Client{Transport: reviewStatusTransport(func(r *http.Request) (*http.Response, error) {
				if !strings.HasSuffix(r.URL.Path, "/notes") && !strings.HasSuffix(r.URL.Path, "/comments") {
					t.Fatalf("unexpected request, including any pipeline mutation: %s", r.URL)
				}
				status := http.StatusOK
				if r.Method == http.MethodPost {
					var payload struct{ Body string }
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					comments = append(comments, scm.Comment{Body: payload.Body})
					posts++
					status = http.StatusCreated
				}
				body, _ := json.Marshal(comments)
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(body))), Header: http.Header{}}, nil
			})}}
			connection := store.SourceControlConnection{Provider: provider, APIURL: "https://provider.example/api", Repository: "org/app"}
			repo := store.RepositoryConfiguration{ID: "repo"}
			event := scm.Event{Number: 147, HeadSHA: strings.Repeat("a", 40)}
			failure := repositoryPRBindingError("review-only", "envs/demo/dev/mimir/justcd.yaml", "mimir-dev")
			for i := 0; i < 2; i++ {
				if err := reportRepositoryPRDiscoveryFailure(context.Background(), client, connection, repo, event, "token", failure); err != nil {
					t.Fatal(err)
				}
			}
			if posts != 1 || !strings.Contains(comments[0].Body, "mimir-dev") || !strings.Contains(comments[0].Body, "review-only") || !strings.Contains(comments[0].Body, event.HeadSHA) {
				t.Fatalf("missing or duplicate review feedback: %+v", comments)
			}
			event.HeadSHA = strings.Repeat("b", 40)
			if err := reportRepositoryPRDiscoveryFailure(context.Background(), client, connection, repo, event, "token", errors.New("invalid <definition>")); err != nil {
				t.Fatal(err)
			}
			if posts != 2 || !strings.Contains(comments[1].Body, "&lt;definition&gt;") {
				t.Fatal("new commit feedback missing or unsafe markup in comment")
			}
		})
	}
}

func TestReviewOnlyBindingErrorDoesNotSuggestDeployment(t *testing.T) {
	message := repositoryPRBindingError("review-only", "justcd.yaml", "mimir-dev").Error()
	if !strings.Contains(message, "existing workspace binding") || strings.Contains(message, "Allow creation") || strings.Contains(message, "createNamespaces: true") {
		t.Fatalf("misleading review-only guidance: %s", message)
	}
}

func TestDiscoveryCommentPermissionErrorsAreSurfaced(t *testing.T) {
	for _, deniedMethod := range []string{http.MethodGet, http.MethodPost} {
		client := scm.Client{HTTP: &http.Client{Transport: reviewStatusTransport(func(r *http.Request) (*http.Response, error) {
			status, body := http.StatusOK, "[]"
			if r.Method == deniedMethod {
				status, body = http.StatusForbidden, `{}`
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		})}}
		connection := store.SourceControlConnection{Provider: "gitlab", APIURL: "https://provider.example/api/v4", Repository: "org/app"}
		err := reportRepositoryPRDiscoveryFailure(context.Background(), client, connection, store.RepositoryConfiguration{ID: "repo"}, scm.Event{Number: 147, HeadSHA: "commit"}, "token", errors.New("no namespace access"))
		if err == nil || !strings.Contains(err.Error(), "403") {
			t.Fatalf("%s comment permission failure swallowed: %v", deniedMethod, err)
		}
	}
}
