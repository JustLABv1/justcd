package api

import (
	"testing"

	"github.com/justlab/justcd/services/backend/internal/store"
)

func TestSourceControlDetailsDerivesApplicationRepository(t *testing.T) {
	tests := []struct {
		name, sourceURL, provider, apiURL, wantProvider, wantRepository string
	}{
		{"GitHub HTTPS", "https://github.com/team/app.git", "", "", "github", "team/app"},
		{"GitHub SSH", "git@github.com:team/app.git", "", "", "github", "team/app"},
		{"GitLab.com", "ssh://git@gitlab.com/team/sub/app.git", "", "", "gitlab", "team/sub/app"},
		{"self-hosted GitLab", "git@gitlab.example.com:team/app.git", "gitlab", "", "gitlab", "team/app"},
		{"GitLab subpath", "https://gitlab.example.com/gitlab/team/app.git", "gitlab", "https://gitlab.example.com/gitlab/api/v4", "gitlab", "team/app"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sourceControlDetails(store.GitSource{RepositoryURL: tc.sourceURL}, tc.provider, tc.apiURL)
			if err != nil {
				t.Fatal(err)
			}
			if got.Provider != tc.wantProvider || got.Repository != tc.wantRepository {
				t.Fatalf("got provider %q repository %q", got.Provider, got.Repository)
			}
		})
	}
}

func TestSourceControlDetailsRejectsMismatchedProviderAndHost(t *testing.T) {
	source := store.GitSource{RepositoryURL: "https://github.com/team/app.git"}
	if _, err := sourceControlDetails(source, "gitlab", ""); err == nil {
		t.Fatal("accepted a provider different from the Git source")
	}
	selfHosted := store.GitSource{RepositoryURL: "https://gitlab.example.com/team/app.git"}
	if _, err := sourceControlDetails(selfHosted, "", ""); err == nil {
		t.Fatal("accepted unknown Git host without GitLab confirmation")
	}
	if _, err := sourceControlDetails(selfHosted, "gitlab", "https://other.example.com/api/v4"); err == nil {
		t.Fatal("accepted an API host different from the Git source")
	}
	if _, err := sourceControlDetails(selfHosted, "gitlab", "https://gitlab.example.com/gitlab/api/v4"); err == nil {
		t.Fatal("accepted an API subpath outside the Git source path")
	}
}

func TestSourceControlInputUsesGitSourceInsteadOfClientRepository(t *testing.T) {
	input := sourceControlInput{Repository: "attacker/other", WebhookSecret: "a-long-webhook-secret", StatusToken: "a-long-status-token"}
	source := store.GitSource{RepositoryURL: "git@github.com:team/app.git"}
	if err := validateSourceControlInput(&input, source, store.Application{}, nil); err != nil {
		t.Fatal(err)
	}
	if input.Provider != "github" || input.APIURL != "https://api.github.com" || input.Repository != "team/app" {
		t.Fatalf("source control identity was not derived from Git source: %+v", input)
	}
}
