package api

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/store"
)

type sourceControlSource struct {
	RepositoryURL string `json:"repositoryUrl"`
	Provider      string `json:"provider"`
	APIURL        string `json:"apiUrl"`
	Repository    string `json:"repository"`
	SelfHosted    bool   `json:"selfHosted"`
}

func gitRepositoryParts(raw string) (string, string, error) {
	var host, repository string
	if strings.HasPrefix(raw, "git@") {
		var ok bool
		host, repository, ok = strings.Cut(strings.TrimPrefix(raw, "git@"), ":")
		if !ok {
			return "", "", errors.New("Git source URL has no repository path")
		}
	} else {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "ssh") {
			return "", "", errors.New("Git source URL is not supported for pull requests")
		}
		host, repository = u.Hostname(), strings.Trim(u.Path, "/")
	}
	repository = strings.TrimSuffix(repository, ".git")
	if host == "" || repository == "" || strings.ContainsAny(repository, "?#@ \\ \t\r\n") || strings.Contains(repository, "..") {
		return "", "", errors.New("Git source repository path is invalid")
	}
	return strings.ToLower(host), repository, nil
}

func sourceControlDetails(source store.GitSource, inputProvider, inputAPIURL string) (sourceControlSource, error) {
	host, repository, err := gitRepositoryParts(source.RepositoryURL)
	if err != nil {
		return sourceControlSource{}, err
	}
	provider, apiURL := "", ""
	switch host {
	case "github.com":
		provider, apiURL = "github", "https://api.github.com"
	case "gitlab.com":
		provider, apiURL = "gitlab", "https://gitlab.com/api/v4"
	default:
		provider, apiURL = "gitlab", "https://"+host+"/api/v4"
		if inputProvider != "gitlab" {
			return sourceControlSource{}, errors.New("unknown Git host; confirm it is self-hosted GitLab")
		}
		if inputAPIURL != "" {
			apiURL = inputAPIURL
		}
	}
	if inputProvider != "" && inputProvider != provider {
		return sourceControlSource{}, errors.New("provider does not match the application's Git source")
	}
	if inputAPIURL != "" && inputAPIURL != apiURL {
		return sourceControlSource{}, errors.New("API URL is derived from the application's Git source")
	}
	u, err := url.Parse(apiURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return sourceControlSource{}, errors.New("apiUrl must be an HTTPS URL without credentials or query")
	}
	if name := strings.ToLower(u.Hostname()); name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return sourceControlSource{}, errors.New("apiUrl cannot use a loopback host")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) {
		return sourceControlSource{}, errors.New("apiUrl cannot use a loopback or link-local address")
	}
	if provider == "gitlab" && !strings.HasSuffix(u.Path, "/api/v4") {
		return sourceControlSource{}, errors.New("GitLab apiUrl must end in /api/v4")
	}
	if provider == "gitlab" && host != strings.ToLower(u.Hostname()) {
		return sourceControlSource{}, errors.New("GitLab API host must match the Git source host")
	}
	if provider == "gitlab" {
		prefix := strings.Trim(strings.TrimSuffix(u.Path, "/api/v4"), "/")
		if prefix != "" {
			if !strings.HasPrefix(repository, prefix+"/") {
				return sourceControlSource{}, errors.New("GitLab API path does not match the Git source path")
			}
			repository = strings.TrimPrefix(repository, prefix+"/")
		}
	}
	if len(repository) > 250 || provider == "github" && strings.Count(repository, "/") != 1 {
		return sourceControlSource{}, errors.New("Git source repository path is invalid")
	}
	return sourceControlSource{RepositoryURL: source.RepositoryURL, Provider: provider, APIURL: apiURL, Repository: repository, SelfHosted: host != "github.com" && host != "gitlab.com"}, nil
}

// A Git source on an unknown host is offered as a self-hosted GitLab candidate.
// Confirmation and an optional custom API base are required on save.
func sourceControlSuggestion(source store.GitSource) (sourceControlSource, error) {
	host, _, err := gitRepositoryParts(source.RepositoryURL)
	if err != nil {
		return sourceControlSource{}, err
	}
	provider := ""
	if host != "github.com" && host != "gitlab.com" {
		provider = "gitlab"
	}
	return sourceControlDetails(source, provider, "")
}
