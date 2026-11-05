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

// GitLabDeployment describes one deployment to record in a GitLab environment.
type GitLabDeployment struct {
	Environment string
	Tier        string
	ExternalURL string
	SHA         string
	Ref         string
	Status      string // running, success, failed, or canceled
}

var gitlabDeploymentStatuses = map[string]bool{"running": true, "success": true, "failed": true, "canceled": true}

// gitlabJSON sends a form-encoded request, requires a 2xx answer and decodes a
// bounded JSON body into out when one is wanted.
func (c Client) gitlabJSON(ctx context.Context, apiURL, token, method, path string, form url.Values, out any) error {
	base, err := url.Parse(apiURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil {
		return errors.New("invalid provider API URL")
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(apiURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return fmt.Errorf("GitLab API returned HTTP %d", res.StatusCode)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
}

// RecordGitLabDeployment ensures the environment exists with the declared
// tier and URL, then records a deployment in it. It returns the deployment ID.
func (c Client) RecordGitLabDeployment(ctx context.Context, apiURL, repository, token string, d GitLabDeployment) (int64, error) {
	if len(d.SHA) != 40 {
		return 0, errors.New("invalid commit SHA")
	}
	if d.Environment == "" || !gitlabDeploymentStatuses[d.Status] {
		return 0, errors.New("invalid GitLab deployment")
	}
	project := "/projects/" + url.PathEscape(repository)
	if err := c.ensureGitLabEnvironment(ctx, apiURL, token, project, d); err != nil {
		return 0, fmt.Errorf("could not update GitLab environment: %w", err)
	}
	ref := d.Ref
	if ref == "" {
		ref = d.SHA
	}
	form := url.Values{"environment": {d.Environment}, "sha": {d.SHA}, "ref": {ref}, "tag": {"false"}, "status": {d.Status}}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := c.gitlabJSON(ctx, apiURL, token, http.MethodPost, project+"/deployments", form, &created); err != nil {
		return 0, fmt.Errorf("could not record GitLab deployment: %w", err)
	}
	return created.ID, nil
}

func (c Client) ensureGitLabEnvironment(ctx context.Context, apiURL, token, project string, d GitLabDeployment) error {
	var found []struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Tier        string `json:"tier"`
		ExternalURL string `json:"external_url"`
	}
	query := url.Values{"name": {d.Environment}}
	if err := c.gitlabJSON(ctx, apiURL, token, http.MethodGet, project+"/environments?"+query.Encode(), nil, &found); err != nil {
		return err
	}
	for _, e := range found {
		if e.Name != d.Environment {
			continue
		}
		update := url.Values{}
		if d.Tier != "" && d.Tier != e.Tier {
			update.Set("tier", d.Tier)
		}
		if d.ExternalURL != "" && d.ExternalURL != e.ExternalURL {
			update.Set("external_url", d.ExternalURL)
		}
		if len(update) == 0 {
			return nil
		}
		return c.gitlabJSON(ctx, apiURL, token, http.MethodPut, project+"/environments/"+strconv.FormatInt(e.ID, 10), update, nil)
	}
	create := url.Values{"name": {d.Environment}}
	if d.Tier != "" {
		create.Set("tier", d.Tier)
	}
	if d.ExternalURL != "" {
		create.Set("external_url", d.ExternalURL)
	}
	return c.gitlabJSON(ctx, apiURL, token, http.MethodPost, project+"/environments", create, nil)
}
