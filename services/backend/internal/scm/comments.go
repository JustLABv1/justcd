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
