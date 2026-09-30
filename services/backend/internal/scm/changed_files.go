package scm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
			return nil, fmt.Errorf("provider changed files API returned HTTP %d", res.StatusCode)
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
					return nil, errors.New("GitLab diff list is incomplete")
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
		return 0, fmt.Errorf("GitLab merge request API returned HTTP %d", res.StatusCode)
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
