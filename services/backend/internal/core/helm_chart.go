package core

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// HelmChartSource selects a packaged chart from a Helm repository or OCI
// registry instead of a chart directory in Git. Values files still come from
// the application's Git source.
type HelmChartSource struct {
	// Repository is an oci:// registry path (without the chart name) or an
	// https:// chart repository URL.
	Repository string `json:"repository"`
	Chart      string `json:"chart"`
	// Version is an exact chart version. Ranges are rejected so renders stay
	// reproducible.
	Version string `json:"version"`
}

var (
	helmChartNamePattern    = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]{0,98}[A-Za-z0-9])?$`)
	helmChartVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
)

// Normalize trims the fields and validates their syntax. It does not resolve
// hostnames; renderers apply network policy when the chart is pulled.
func (c HelmChartSource) Normalize() (HelmChartSource, error) {
	c.Repository = strings.TrimSuffix(strings.TrimSpace(c.Repository), "/")
	c.Chart = strings.TrimSpace(c.Chart)
	c.Version = strings.TrimSpace(c.Version)
	if c.Repository == "" || len(c.Repository) > 512 {
		return c, errors.New("Helm chart repository is required and must be at most 512 characters")
	}
	parsed, err := url.Parse(c.Repository)
	if err != nil || (parsed.Scheme != "oci" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return c, errors.New("Helm chart repository must be an oci:// registry path or an https:// chart repository URL without credentials")
	}
	if strings.Contains(parsed.Path, "//") || strings.Contains(parsed.Path, "/../") || strings.HasSuffix(parsed.Path, "/..") {
		return c, errors.New("Helm chart repository path is invalid")
	}
	if !helmChartNamePattern.MatchString(c.Chart) {
		return c, errors.New("Helm chart name must contain only letters, digits, dots, dashes, and underscores")
	}
	if !helmChartVersionPattern.MatchString(c.Version) || len(c.Version) > 128 {
		return c, errors.New("Helm chart version must be an exact semantic version such as 1.2.3")
	}
	return c, nil
}

// IsOCI reports whether the chart is stored in an OCI registry.
func (c HelmChartSource) IsOCI() bool {
	return strings.HasPrefix(c.Repository, "oci://")
}

// Reference is the location that identifies the chart for credential
// matching: the full OCI artifact path, or the HTTPS repository URL.
func (c HelmChartSource) Reference() string {
	if c.IsOCI() {
		return c.Repository + "/" + c.Chart
	}
	return c.Repository
}
