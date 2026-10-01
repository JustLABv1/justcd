package scm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRepositoryPRTargetAndMergeState(t *testing.T) {
	for _, provider := range []string{"github", "gitlab"} {
		body := `{"number":42,"head":{"sha":"` + strings.Repeat("a", 40) + `","repo":{"full_name":"org/app"}},"base":{"ref":"main","repo":{"full_name":"org/app"}},"state":"closed","merged_at":"2026-10-01T12:00:00Z"}`
		if provider == "gitlab" {
			body = `{"iid":42,"sha":"` + strings.Repeat("a", 40) + `","target_branch":"main","source_branch":"feature","source_project_id":1,"target_project_id":1,"state":"merged"}`
		}
		client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		})}}
		event, err := client.Current(context.Background(), provider, "https://example.com", "org/app", "token", 42)
		if err != nil || event.TargetBranch != "main" || !event.Closed || !event.Merged || event.Fork {
			t.Fatalf("merge handoff state unavailable: %+v %v", event, err)
		}
	}
}
