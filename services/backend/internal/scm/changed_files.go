package scm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ChangedPaths returns both old and new paths so renames and removals are scoped
// correctly. An incomplete provider response is an error, never a negative match.
func (c Client) ChangedPaths(ctx context.Context, provider, apiURL, repository, token string, number int) ([]string, error) {
	if provider != "github" && provider != "gitlab" {
		return nil, errors.New("unsupported provider")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	base := strings.TrimRight(apiURL, "/")
	if provider == "github" {
		base += "/repos/" + repository + "/pulls/" + strconv.Itoa(number) + "/files"
	} else {
		base += "/projects/" + url.PathEscape(repository) + "/merge_requests/" + strconv.Itoa(number) + "/diffs"
	}
	paths := []string{}
	count := 0
	for page := 1; page <= 100; page++ {
		endpoint := base + "?per_page=100&page=" + strconv.Itoa(page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		if provider == "github" {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Accept", "application/vnd.github+json")
		} else {
			req.Header.Set("PRIVATE-TOKEN", token)
		}
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return nil, &ChangedFilesAPIError{Status: res.StatusCode}
		}
		var raw []struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
			OldPath          string `json:"old_path"`
			NewPath          string `json:"new_path"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&raw)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, file := range raw {
			if provider == "github" {
				if file.Filename == "" {
					return nil, errors.New("provider returned a file without a path")
				}
				paths = append(paths, file.Filename)
				if file.PreviousFilename != "" {
					paths = append(paths, file.PreviousFilename)
				}
			} else {
				if file.OldPath == "" || file.NewPath == "" {
					return nil, errors.New("provider returned a diff without paths")
				}
				paths = append(paths, file.OldPath, file.NewPath)
			}
		}
		count += len(raw)
		if provider == "github" && count >= 3000 {
			return nil, errors.New("GitHub changed-file list may be truncated at 3000 files")
		}
		if len(raw) < 100 {
			if provider == "gitlab" {
				// GitLab can limit stored diffs independently of pagination.
				total, err := c.gitLabChangeCount(ctx, client, apiURL, repository, token, number)
				if err != nil {
					return nil, err
				}
				if total != count {
					return nil, fmt.Errorf("GitLab diff list is incomplete: expected %d files, received %d", total, count)
				}
			}
			return paths, nil
		}
	}
	return nil, errors.New("provider changed-file list is too large")
}

func (c Client) gitLabChangeCount(ctx context.Context, client *http.Client, apiURL, repository, token string, number int) (int, error) {
	endpoint := strings.TrimRight(apiURL, "/") + "/projects/" + url.PathEscape(repository) + "/merge_requests/" + strconv.Itoa(number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, &ChangedFilesAPIError{Status: res.StatusCode}
	}
	var raw struct {
		ChangesCount string `json:"changes_count"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&raw); err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(raw.ChangesCount)
	if err != nil {
		return 0, errors.New("GitLab did not provide a complete change count")
	}
	return count, nil
}

// Error details are generated locally; never show provider response bodies or tokens.
type ChangedFilesAPIError struct{ Status int }

func (e *ChangedFilesAPIError) Error() string {
	return fmt.Sprintf("Provider changed-file API returned HTTP %d", e.Status)
}

func ChangedFilesDiagnostic(err error) string {
	var apiErr *ChangedFilesAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 401:
			return "Provider API returned HTTP 401. The PR API credential is invalid or expired; replace it in Credentials."
		case 403:
			return "Provider API returned HTTP 403. Check the PR API credential’s scopes and access to this repository."
		case 404:
			return "Provider API returned HTTP 404. Check the provider API URL and repository access; private repositories may return 404 when access is denied."
		case 429:
			return "Provider API returned HTTP 429 (rate limit). Wait for the provider limit to reset."
		default:
			return fmt.Sprintf("Provider API returned HTTP %d. Check the Git provider’s availability and API URL.", apiErr.Status)
		}
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "Provider API request timed out. Check connectivity between JustCD and the Git provider."
		}
		return "Provider API connection failed. Check DNS, connectivity and the provider’s TLS certificate."
	}
	message := err.Error()
	switch {
	case strings.HasPrefix(message, "GitLab diff list is incomplete:"):
		return message + ". GitLab may have truncated its stored diff; check this MR’s diff limits."
	case message == "GitLab did not provide a complete change count":
		return "GitLab returned no exact changed-file count (it may be pending or capped). Retry after GitLab finishes processing the MR; large MRs can exceed diff limits."
	case strings.Contains(message, "truncated") || message == "provider changed-file list is too large":
		return "The changed-file list exceeds the provider or JustCD limit. Split the PR to enable reliable path filtering."
	default:
		return "Provider changed-file data could not be decoded or verified. Check the Git provider’s API response and version."
	}
}
