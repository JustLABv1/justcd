// Package justscan connects to the JustScan pipeline scan API.
package justscan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPError struct {
	Method string
	Status int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("JustScan %s returned HTTP %d", e.Method, e.Status)
}

type Client struct {
	scansURL string
	token    string
	http     *http.Client
}

type Target struct {
	Image       string `json:"image"`
	Platform    string `json:"platform,omitempty"`
	RegistryID  string `json:"registry_id,omitempty"`
	ExternalRef string `json:"external_ref,omitempty"`
}

type Scan struct {
	ID            string `json:"scan_id"`
	URL           string `json:"scan_url,omitempty"`
	ImageTag      string `json:"image_tag,omitempty"`
	Status        string `json:"status"`
	Verdict       string `json:"verdict,omitempty"`
	CriticalCount int    `json:"critical_count"`
	HighCount     int    `json:"high_count"`
}

func ParseServer(server string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(server))
	if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Hostname() == "" ||
		base.User != nil || base.RawQuery != "" || base.Opaque != "" || base.ForceQuery || base.Fragment != "" {
		return nil, errors.New("invalid JustScan server URL")
	}
	return base, nil
}

func NewClient(server, organizationID, token string) (*Client, error) {
	base, err := ParseServer(server)
	if err != nil {
		return nil, err
	}
	if !validID(organizationID) || strings.TrimSpace(token) == "" {
		return nil, errors.New("JustScan requires an organization ID and token")
	}
	base.Path = strings.TrimRight(base.Path, "/")
	if !strings.HasSuffix(base.Path, "/api/v1") {
		base = base.JoinPath("api", "v1")
	}
	return &Client{
		scansURL: base.JoinPath("orgs", organizationID, "pipeline-scans").String(),
		token:    token,
		http: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *Client) Submit(ctx context.Context, target Target) (Scan, error) {
	if strings.TrimSpace(target.Image) == "" {
		return Scan{}, errors.New("JustScan requires an image reference")
	}
	body, err := json.Marshal(struct {
		Target
		Source string `json:"source"`
	}{Target: target, Source: "generic"})
	if err != nil {
		return Scan{}, err
	}
	var response struct {
		Scan
		ScanStatus string `json:"scan_status"`
	}
	if err := c.request(ctx, http.MethodPost, c.scansURL, bytes.NewReader(body), http.StatusAccepted, &response); err != nil {
		return Scan{}, err
	}
	if !validID(response.ID) {
		return Scan{}, errors.New("JustScan returned an invalid scan ID")
	}
	response.Scan.Status = response.ScanStatus
	return response.Scan, nil
}

func (c *Client) Status(ctx context.Context, id string) (Scan, error) {
	if !validID(id) {
		return Scan{}, errors.New("invalid JustScan scan ID")
	}
	var scan Scan
	if err := c.request(ctx, http.MethodGet, c.scansURL+"/"+id, nil, http.StatusOK, &scan); err != nil {
		return Scan{}, err
	}
	if !strings.EqualFold(scan.ID, id) || scan.Verdict == "" {
		return Scan{}, errors.New("JustScan returned an incomplete or mismatched scan result")
	}
	return scan, nil
}

func (c *Client) request(ctx context.Context, method, endpoint string, body io.Reader, expectedStatus int, output any) error {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("build JustScan request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("send JustScan request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		return &HTTPError{Method: method, Status: response.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output); err != nil {
		return fmt.Errorf("decode JustScan response: %w", err)
	}
	return nil
}

func validID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, char := range id {
		switch i {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
				return false
			}
		}
	}
	return true
}
