package scm

import (
	"context"
	"errors"
	"fmt"
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

func TestChangedFilesDiagnostic(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500} {
		detail := ChangedFilesDiagnostic(&ChangedFilesAPIError{Status: status})
		if !strings.Contains(detail, fmt.Sprintf("HTTP %d", status)) {
			t.Fatalf("status hidden: %s", detail)
		}
	}
	detail := ChangedFilesDiagnostic(errors.New("GitLab diff list is incomplete: expected 12 files, received 3"))
	if !strings.Contains(detail, "expected 12 files, received 3") {
		t.Fatal("diff completeness details hidden")
	}
	secret := "provider response with secret-token"
	if strings.Contains(ChangedFilesDiagnostic(errors.New(secret)), secret) {
		t.Fatal("untrusted provider detail exposed")
	}
}

func TestCurrentPRComparisonBase(t *testing.T) {
	head := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	for _, provider := range []string{"github", "gitlab"} {
		t.Run(provider, func(t *testing.T) {
			body := fmt.Sprintf(`{"head":{"sha":%q,"repo":{"full_name":"org/app"}},"base":{"sha":%q,"repo":{"full_name":"org/app"}},"state":"open"}`, head, base)
			if provider == "gitlab" {
				body = fmt.Sprintf(`{"sha":%q,"diff_refs":{"head_sha":%q,"base_sha":%q},"source_project_id":1,"target_project_id":1,"state":"opened"}`, head, head, base)
			}
			client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
			})}}
			current, err := client.Current(context.Background(), provider, "https://example.com", "org/app", "token", 1)
			if err != nil || current.BaseSHA != base || current.BaseIsMergeBase != (provider == "gitlab") {
				t.Fatalf("incorrect comparison base: %+v %v", current, err)
			}
			if provider == "gitlab" {
				body = strings.Replace(body, `"head_sha":"`+head, `"head_sha":"`+base, 1)
				current, err = client.Current(context.Background(), provider, "https://example.com", "org/app", "token", 1)
				if err != nil || current.BaseSHA != "" {
					t.Fatal("outdated GitLab diff base accepted")
				}
			}
		})
	}
}
