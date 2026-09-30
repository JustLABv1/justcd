package scm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestChangedPathsIncludesRenames(t *testing.T) {
	for _, provider := range []string{"github", "gitlab"} {
		t.Run(provider, func(t *testing.T) {
			client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := `[{"filename":"apps/new/manifest.yaml","previous_filename":"apps/old/manifest.yaml"}]`
				if provider == "gitlab" {
					body = `[{"old_path":"apps/old/manifest.yaml","new_path":"apps/new/manifest.yaml"}]`
					if !strings.HasSuffix(r.URL.Path, "/diffs") {
						body = `{"changes_count":"1"}`
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
			})}}
			paths, err := client.ChangedPaths(context.Background(), provider, "https://example.com/api", "org/repo", "token", 7)
			if err != nil || len(paths) != 2 || paths[0] != "apps/new/manifest.yaml" && paths[0] != "apps/old/manifest.yaml" {
				t.Fatalf("unexpected paths: %v, %v", paths, err)
			}
		})
	}
}
