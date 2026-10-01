package api

import (
	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/store"
)

type sourceControlSource = scm.SourceDetails

func gitRepositoryParts(raw string) (string, string, error) { return scm.GitRepositoryParts(raw) }
func sourceControlDetails(source store.GitSource, provider, apiURL string) (sourceControlSource, error) {
	return scm.Details(source.RepositoryURL, provider, apiURL)
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
