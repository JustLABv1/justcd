package scm

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
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

type Event struct {
	TargetBranch    string
	Merged          bool
	BaseSHA         string
	BaseIsMergeBase bool
	HeadBranch      string
	Kind            string
	DeliveryID      string
	Repository      string
	Ref             string
	Number          int
	HeadSHA         string
	URL             string
	Fork            bool
	Closed          bool
	EventAt         time.Time
}

func VerifyAndParse(provider, secret string, header http.Header, body []byte, now time.Time) (Event, error) {
	if len(secret) < 16 {
		return Event{}, errors.New("webhook secret is too short")
	}
	switch provider {
	case "github":
		got := strings.TrimPrefix(header.Get("X-Hub-Signature-256"), "sha256=")
		decoded, err := hex.DecodeString(got)
		if err != nil || len(decoded) != sha256.Size {
			return Event{}, errors.New("invalid GitHub signature")
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		if !hmac.Equal(decoded, mac.Sum(nil)) {
			return Event{}, errors.New("invalid GitHub signature")
		}
		if header.Get("X-GitHub-Event") == "push" {
			var p struct {
				Ref        string `json:"ref"`
				After      string `json:"after"`
				Deleted    bool   `json:"deleted"`
				Repository struct {
					FullName string `json:"full_name"`
				} `json:"repository"`
			}
			if err := json.Unmarshal(body, &p); err != nil {
				return Event{}, err
			}
			if p.Deleted {
				return Event{}, errors.New("event ignored")
			}
			return validatePushEvent(Event{Kind: "push", DeliveryID: header.Get("X-GitHub-Delivery"), Repository: p.Repository.FullName, Ref: p.Ref, HeadSHA: p.After})
		}
		if header.Get("X-GitHub-Event") != "pull_request" {
			return Event{}, errors.New("event ignored")
		}
		var p struct {
			Action     string `json:"action"`
			Number     int    `json:"number"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			PullRequest struct {
				HTMLURL   string    `json:"html_url"`
				UpdatedAt time.Time `json:"updated_at"`
				Head      struct {
					SHA  string `json:"sha"`
					Ref  string `json:"ref"`
					Repo struct {
						FullName string `json:"full_name"`
					} `json:"repo"`
				} `json:"head"`
			} `json:"pull_request"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return Event{}, err
		}
		if p.Action != "opened" && p.Action != "reopened" && p.Action != "synchronize" && p.Action != "closed" {
			return Event{}, errors.New("event ignored")
		}
		e := Event{DeliveryID: header.Get("X-GitHub-Delivery"), Repository: p.Repository.FullName, Number: p.Number, HeadSHA: p.PullRequest.Head.SHA, URL: p.PullRequest.HTMLURL, Fork: p.PullRequest.Head.Repo.FullName != p.Repository.FullName, Closed: p.Action == "closed", EventAt: p.PullRequest.UpdatedAt}
		return validateEvent(e)
	case "gitlab":
		if sig := header.Get("webhook-signature"); sig != "" {
			id, timestamp := header.Get("webhook-id"), header.Get("webhook-timestamp")
			unix, err := strconv.ParseInt(timestamp, 10, 64)
			if err != nil || id == "" || strings.Contains(id, ".") || now.Sub(time.Unix(unix, 0)) > 5*time.Minute || time.Unix(unix, 0).Sub(now) > 5*time.Minute {
				return Event{}, errors.New("invalid GitLab webhook timestamp")
			}
			key := []byte(strings.TrimPrefix(secret, "whsec_"))
			if strings.HasPrefix(secret, "whsec_") {
				key, err = base64.StdEncoding.DecodeString(string(key))
				if err != nil {
					return Event{}, errors.New("invalid signing key")
				}
			}
			mac := hmac.New(sha256.New, key)
			mac.Write([]byte(id + "." + timestamp + "."))
			mac.Write(body)
			expected := mac.Sum(nil)
			valid := false
			for _, part := range strings.Fields(sig) {
				if !strings.HasPrefix(part, "v1,") {
					continue
				}
				actual, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(part, "v1,"))
				if err == nil && hmac.Equal(actual, expected) {
					valid = true
				}
			}
			if !valid {
				return Event{}, errors.New("invalid GitLab signature")
			}
		} else if subtle.ConstantTimeCompare([]byte(header.Get("X-Gitlab-Token")), []byte(secret)) != 1 {
			return Event{}, errors.New("invalid GitLab token")
		}
		if header.Get("X-Gitlab-Event") == "Push Hook" {
			var p struct {
				ObjectKind string `json:"object_kind"`
				Ref        string `json:"ref"`
				After      string `json:"after"`
				Project    struct {
					PathWithNamespace string `json:"path_with_namespace"`
				} `json:"project"`
			}
			if err := json.Unmarshal(body, &p); err != nil {
				return Event{}, err
			}
			if p.ObjectKind != "push" || p.After == strings.Repeat("0", 40) {
				return Event{}, errors.New("event ignored")
			}
			id := header.Get("webhook-id")
			if id == "" {
				id = header.Get("X-Gitlab-Event-UUID")
			}
			if id == "" {
				id = header.Get("X-Gitlab-Webhook-UUID")
			}
			return validatePushEvent(Event{Kind: "push", DeliveryID: id, Repository: p.Project.PathWithNamespace, Ref: p.Ref, HeadSHA: p.After})
		}
		if header.Get("X-Gitlab-Event") != "Merge Request Hook" {
			return Event{}, errors.New("event ignored")
		}
		var p struct {
			ObjectKind string `json:"object_kind"`
			Project    struct {
				PathWithNamespace string `json:"path_with_namespace"`
			} `json:"project"`
			ObjectAttributes struct {
				Action          string    `json:"action"`
				State           string    `json:"state"`
				IID             int       `json:"iid"`
				URL             string    `json:"url"`
				UpdatedAt       time.Time `json:"updated_at"`
				SourceProjectID int       `json:"source_project_id"`
				TargetProjectID int       `json:"target_project_id"`
				LastCommit      struct {
					ID string `json:"id"`
				} `json:"last_commit"`
			} `json:"object_attributes"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return Event{}, err
		}
		if p.ObjectKind != "merge_request" {
			return Event{}, errors.New("event ignored")
		}
		if p.ObjectAttributes.Action != "open" && p.ObjectAttributes.Action != "reopen" && p.ObjectAttributes.Action != "update" && p.ObjectAttributes.Action != "close" && p.ObjectAttributes.Action != "merge" {
			return Event{}, errors.New("event ignored")
		}
		e := Event{DeliveryID: header.Get("webhook-id"), Repository: p.Project.PathWithNamespace, Number: p.ObjectAttributes.IID, HeadSHA: p.ObjectAttributes.LastCommit.ID, URL: p.ObjectAttributes.URL, Fork: p.ObjectAttributes.SourceProjectID == 0 || p.ObjectAttributes.TargetProjectID == 0 || p.ObjectAttributes.SourceProjectID != p.ObjectAttributes.TargetProjectID, Closed: p.ObjectAttributes.State == "closed" || p.ObjectAttributes.State == "merged", EventAt: p.ObjectAttributes.UpdatedAt}
		if e.DeliveryID == "" {
			e.DeliveryID = header.Get("X-Gitlab-Event-UUID")
		}
		if e.DeliveryID == "" {
			e.DeliveryID = header.Get("X-Gitlab-Webhook-UUID")
		}
		if e.DeliveryID == "" {
			sum := sha256.Sum256(body)
			e.DeliveryID = hex.EncodeToString(sum[:])
		}
		return validateEvent(e)
	default:
		return Event{}, errors.New("unsupported source control provider")
	}
}

func validateEvent(e Event) (Event, error) {
	e.Kind = "review"
	if e.DeliveryID == "" || len(e.DeliveryID) > 200 || e.Repository == "" || e.Number <= 0 || len(e.HeadSHA) != 40 || e.EventAt.IsZero() {
		return Event{}, errors.New("incomplete pull request event")
	}
	if _, err := hex.DecodeString(e.HeadSHA); err != nil {
		return Event{}, errors.New("invalid head commit")
	}
	u, err := url.Parse(e.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return Event{}, errors.New("invalid pull request URL")
	}
	return e, nil
}

func validatePushEvent(e Event) (Event, error) {
	if len(e.DeliveryID) > 200 || !strings.HasPrefix(e.Ref, "refs/heads/") || len(e.Ref) <= len("refs/heads/") || len(e.HeadSHA) != 40 {
		return Event{}, errors.New("incomplete push event")
	}
	if _, err := hex.DecodeString(e.HeadSHA); err != nil {
		return Event{}, errors.New("invalid push commit")
	}
	return e, nil
}

// VerifyGenericPush accepts a minimal signed push envelope from CI systems.
// The endpoint is bound to a Git source, so the signed body needs only a ref and SHA.
func VerifyGenericPush(secret string, header http.Header, body []byte) (Event, error) {
	if len(secret) < 16 {
		return Event{}, errors.New("webhook secret is too short")
	}
	got := strings.TrimPrefix(header.Get("X-JustCD-Signature-256"), "sha256=")
	decoded, err := hex.DecodeString(got)
	if err != nil || len(decoded) != sha256.Size {
		return Event{}, errors.New("invalid webhook signature")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(decoded, mac.Sum(nil)) {
		return Event{}, errors.New("invalid webhook signature")
	}
	var p struct {
		Ref   string `json:"ref"`
		After string `json:"after"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return Event{}, err
	}
	return validatePushEvent(Event{Kind: "push", DeliveryID: header.Get("X-JustCD-Delivery"), Ref: p.Ref, HeadSHA: p.After})
}

type Client struct{ HTTP *http.Client }

// ListOpen returns every open PR/MR. A truncated list is an error because callers
// use it to detect closed reviews.
func (c Client) ListOpen(ctx context.Context, provider, apiURL, repository, token string) ([]Event, error) {
	if provider != "github" && provider != "gitlab" {
		return nil, errors.New("unsupported provider")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	items := []Event{}
	for page := 1; page <= 100; page++ {
		var endpoint string
		if provider == "github" {
			endpoint = strings.TrimRight(apiURL, "/") + "/repos/" + repository + "/pulls?state=open&per_page=100&page=" + strconv.Itoa(page)
		} else {
			endpoint = strings.TrimRight(apiURL, "/") + "/projects/" + url.PathEscape(repository) + "/merge_requests?state=opened&scope=all&per_page=100&page=" + strconv.Itoa(page)
		}
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
			return nil, fmt.Errorf("provider pull request list API returned HTTP %d", res.StatusCode)
		}
		var raw []struct {
			Number    int       `json:"number"`
			IID       int       `json:"iid"`
			HTMLURL   string    `json:"html_url"`
			WebURL    string    `json:"web_url"`
			SHA       string    `json:"sha"`
			UpdatedAt time.Time `json:"updated_at"`
			Head      struct {
				SHA  string `json:"sha"`
				Ref  string `json:"ref"`
				Repo struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"head"`
			Base struct {
				Ref  string `json:"ref"`
				Repo struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"base"`
			TargetBranch    string `json:"target_branch"`
			SourceBranch    string `json:"source_branch"`
			SourceProjectID int    `json:"source_project_id"`
			TargetProjectID int    `json:"target_project_id"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&raw)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, v := range raw {
			e := Event{Repository: repository, EventAt: v.UpdatedAt}
			if provider == "github" {
				e.HeadBranch = v.Head.Ref
				e.TargetBranch = v.Base.Ref
				e.Number, e.HeadSHA, e.URL, e.Fork = v.Number, v.Head.SHA, v.HTMLURL, v.Head.Repo.FullName != v.Base.Repo.FullName
			} else {
				e.HeadBranch = v.SourceBranch
				e.TargetBranch = v.TargetBranch
				e.Number, e.HeadSHA, e.URL, e.Fork = v.IID, v.SHA, v.WebURL, v.SourceProjectID == 0 || v.TargetProjectID == 0 || v.SourceProjectID != v.TargetProjectID
			}
			if e.Number <= 0 || len(e.HeadSHA) != 40 || e.EventAt.IsZero() {
				return nil, errors.New("provider returned incomplete pull request")
			}
			items = append(items, e)
		}
		if len(raw) < 100 {
			return items, nil
		}
	}
	return nil, errors.New("provider has more than 10000 open pull requests")
}

// Current reads the provider's present PR/MR state before acting on a webhook.
// This prevents delayed webhook delivery from deploying an old commit.
func (c Client) Current(ctx context.Context, provider, apiURL, repository, token string, number int) (Event, error) {
	var endpoint string
	if provider == "github" {
		endpoint = strings.TrimRight(apiURL, "/") + "/repos/" + repository + "/pulls/" + strconv.Itoa(number)
	} else if provider == "gitlab" {
		endpoint = strings.TrimRight(apiURL, "/") + "/projects/" + url.PathEscape(repository) + "/merge_requests/" + strconv.Itoa(number)
	} else {
		return Event{}, errors.New("unsupported provider")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Event{}, err
	}
	if provider == "github" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
	} else {
		req.Header.Set("PRIVATE-TOKEN", token)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, err := client.Do(req)
	if err != nil {
		return Event{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Event{}, fmt.Errorf("provider pull request API returned HTTP %d", res.StatusCode)
	}
	var raw struct {
		State     string     `json:"state"`
		MergedAt  *time.Time `json:"merged_at"`
		HTMLURL   string     `json:"html_url"`
		SHA       string     `json:"sha"`
		WebURL    string     `json:"web_url"`
		UpdatedAt time.Time  `json:"updated_at"`
		Head      struct {
			SHA  string `json:"sha"`
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Ref  string `json:"ref"`
			SHA  string `json:"sha"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
		DiffRefs struct {
			BaseSHA string `json:"base_sha"`
			HeadSHA string `json:"head_sha"`
		} `json:"diff_refs"`
		TargetBranch    string `json:"target_branch"`
		SourceBranch    string `json:"source_branch"`
		SourceProjectID int    `json:"source_project_id"`
		TargetProjectID int    `json:"target_project_id"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&raw); err != nil {
		return Event{}, err
	}
	e := Event{Number: number, Repository: repository, EventAt: raw.UpdatedAt}
	if provider == "github" {
		e.HeadBranch = raw.Head.Ref
		e.TargetBranch = raw.Base.Ref
		e.Merged = raw.MergedAt != nil
		e.HeadSHA = raw.Head.SHA
		e.BaseSHA = raw.Base.SHA
		e.URL = raw.HTMLURL
		e.Fork = raw.Head.Repo.FullName != raw.Base.Repo.FullName
		e.Closed = raw.State == "closed"
	}
	if provider == "gitlab" {
		e.HeadBranch = raw.SourceBranch
		e.TargetBranch = raw.TargetBranch
		e.Merged = raw.State == "merged"
		e.HeadSHA = raw.SHA
		if raw.DiffRefs.HeadSHA == raw.SHA {
			e.BaseSHA = raw.DiffRefs.BaseSHA
			e.BaseIsMergeBase = true
		}
		e.URL = raw.WebURL
		e.Fork = raw.SourceProjectID == 0 || raw.TargetProjectID == 0 || raw.SourceProjectID != raw.TargetProjectID
		e.Closed = raw.State == "closed" || raw.State == "merged"
	}
	if len(e.HeadSHA) != 40 {
		return Event{}, errors.New("provider returned invalid head SHA")
	}
	return e, nil
}

func (c Client) Report(ctx context.Context, provider, apiURL, repository, token, sha, state, description, targetURL, applicationID string) error {
	if len(sha) != 40 {
		return errors.New("invalid commit SHA")
	}
	base, err := url.Parse(apiURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil {
		return errors.New("invalid provider API URL")
	}
	var endpoint string
	var payload []byte
	if provider == "github" {
		parts := strings.Split(repository, "/")
		if len(parts) != 2 {
			return errors.New("invalid GitHub repository")
		}
		endpoint = strings.TrimRight(apiURL, "/") + "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/statuses/" + sha
		githubState := map[string]string{"failed": "failure", "canceled": "error", "running": "pending"}[state]
		if githubState == "" {
			githubState = state
		}
		payload, _ = json.Marshal(map[string]string{"state": githubState, "description": description, "context": "JustCD/" + applicationID, "target_url": targetURL})
	} else if provider == "gitlab" {
		endpoint = strings.TrimRight(apiURL, "/") + "/projects/" + url.PathEscape(repository) + "/statuses/" + sha
		values := url.Values{"state": {state}, "description": {description}, "name": {"JustCD/" + applicationID}, "target_url": {targetURL}}
		payload = []byte(values.Encode())
	} else {
		return errors.New("unsupported source control provider")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	if provider == "github" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Content-Type", "application/json")
	} else {
		req.Header.Set("PRIVATE-TOKEN", token)
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
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("provider status API returned HTTP %d", res.StatusCode)
	}
	return nil
}
