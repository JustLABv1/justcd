package scm

import (
	"bytes"
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

type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      struct {
		ID int64 `json:"id"`
	} `json:"user"`
	Author struct {
		ID int64 `json:"id"`
	} `json:"author"`
	System bool `json:"system"`
}

func commentEndpoint(provider, apiURL, repository string, number int) (string, error) {
	if provider == "github" {
		return strings.TrimRight(apiURL, "/") + "/repos/" + repository + "/issues/" + strconv.Itoa(number) + "/comments", nil
	}
	if provider == "gitlab" {
		return strings.TrimRight(apiURL, "/") + "/projects/" + url.PathEscape(repository) + "/merge_requests/" + strconv.Itoa(number) + "/notes", nil
	}
	return "", errors.New("unsupported provider")
}

func (c Client) commentRequest(ctx context.Context, provider, token, method, endpoint string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if provider == "github" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
	} else {
		req.Header.Set("PRIVATE-TOKEN", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return client.Do(req)
}

func (c Client) Comments(ctx context.Context, provider, apiURL, repository, token string, number int) ([]Comment, error) {
	endpoint, err := commentEndpoint(provider, apiURL, repository, number)
	if err != nil {
		return nil, err
	}
	items := []Comment{}
	for page := 1; page <= 100; page++ {
		res, err := c.commentRequest(ctx, provider, token, http.MethodGet, endpoint+"?per_page=100&page="+strconv.Itoa(page), nil)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return nil, fmt.Errorf("provider comments API returned HTTP %d", res.StatusCode)
		}
		var batch []Comment
		err = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&batch)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		items = append(items, batch...)
		if len(batch) < 100 {
			return items, nil
		}
	}
	return nil, errors.New("too many PR comments to verify approvals")
}

func (c Client) PostComment(ctx context.Context, provider, apiURL, repository, token string, number int, body string) error {
	endpoint, err := commentEndpoint(provider, apiURL, repository, number)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"body": body})
	res, err := c.commentRequest(ctx, provider, token, http.MethodPost, endpoint, payload)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		return fmt.Errorf("provider comment API returned HTTP %d", res.StatusCode)
	}
	return nil
}

// UpsertComment only edits a summary authored by the authenticated account.
// Never trust a marker alone: PR authors can copy it into their own comments.
func (c Client) UpsertComment(ctx context.Context, provider, apiURL, repository, token string, number int, id int64, marker, body string) (int64, error) {
	res, err := c.commentRequest(ctx, provider, token, http.MethodGet, strings.TrimRight(apiURL, "/")+"/user", nil)
	if err != nil {
		return 0, err
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return 0, fmt.Errorf("provider current user API returned HTTP %d", res.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&user)
	res.Body.Close()
	if err != nil {
		return 0, err
	}
	if user.ID <= 0 {
		return 0, errors.New("provider returned invalid current user")
	}
	owned := func(item Comment) bool {
		author := item.User.ID
		if provider == "gitlab" {
			author = item.Author.ID
		}
		return !item.System && author == user.ID && strings.Contains(item.Body, marker)
	}
	endpoint, err := commentEndpoint(provider, apiURL, repository, number)
	if err != nil {
		return 0, err
	}
	itemEndpoint := func(commentID int64) string {
		if provider == "github" {
			return strings.TrimRight(apiURL, "/") + "/repos/" + repository + "/issues/comments/" + strconv.FormatInt(commentID, 10)
		}
		return endpoint + "/" + strconv.FormatInt(commentID, 10)
	}
	var current *Comment
	if id > 0 {
		res, err = c.commentRequest(ctx, provider, token, http.MethodGet, itemEndpoint(id), nil)
		if err != nil {
			return 0, err
		}
		if res.StatusCode == http.StatusOK {
			var item Comment
			err = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&item)
			res.Body.Close()
			if err != nil {
				return 0, err
			}
			if !owned(item) {
				return 0, errors.New("stored PR summary is not owned by the authenticated account")
			}
			current = &item
		} else {
			res.Body.Close()
			if res.StatusCode != http.StatusNotFound {
				return 0, fmt.Errorf("provider comment API returned HTTP %d", res.StatusCode)
			}
		}
	}
	if current == nil {
		items, err := c.Comments(ctx, provider, apiURL, repository, token, number)
		if err != nil {
			return 0, err
		}
		for i := range items {
			if owned(items[i]) {
				current = &items[i]
				break
			}
		}
	}
	if current != nil && current.Body == body {
		return current.ID, nil
	}
	method, target := http.MethodPost, endpoint
	expected := http.StatusCreated
	if current != nil {
		target = itemEndpoint(current.ID)
		expected = http.StatusOK
		method = http.MethodPatch
		if provider == "gitlab" {
			method = http.MethodPut
		}
	}
	payload, _ := json.Marshal(map[string]string{"body": body})
	res, err = c.commentRequest(ctx, provider, token, method, target, payload)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != expected {
		return 0, fmt.Errorf("provider comment API returned HTTP %d", res.StatusCode)
	}
	var item Comment
	if err = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&item); err != nil {
		return 0, err
	}
	if item.ID <= 0 {
		return 0, errors.New("provider returned invalid comment ID")
	}
	return item.ID, nil
}
