package syncer

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"time"

	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/store"
)

var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// reportEnvironmentDeployment records a finished sync as a GitLab deployment.
// It is opt-in per repository, never changes the operation's outcome, and
// keeps the last result (including any error) for display in the UI.
func (s *Service) reportEnvironmentDeployment(app store.Application, operationID string, plan store.PlanRecord, revision, status string) {
	// Branch tests deploy unmerged code; they must not appear as the real environment.
	if app.RepositoryConfigurationID == "" || app.BranchTest != nil || plan.Plan.Decommission || len(app.Namespaces) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	settings, err := s.Store.EnvironmentSettings(ctx, app.RepositoryConfigurationID)
	if err != nil || !settings.Enabled {
		if err != nil {
			slog.Warn("could not load GitLab environment settings", "applicationId", app.ID, "error", err)
		}
		return
	}
	record := store.EnvironmentDeployment{OperationID: operationID, ApplicationID: app.ID, Status: status, SHA: revision}
	deployment, err := s.gitlabDeployment(ctx, app, revision, status)
	record.Environment = deployment.Environment
	if err == nil {
		record.GitLabDeploymentID, err = s.postDeployment(ctx, app, settings, deployment)
	}
	if err != nil {
		record.Error = err.Error()
		slog.Warn("could not report GitLab environment deployment", "applicationId", app.ID, "operationId", operationID, "error", err)
	}
	if err := s.Store.RecordEnvironmentDeployment(ctx, record); err != nil {
		slog.Warn("could not save GitLab environment deployment", "applicationId", app.ID, "error", err)
	}
}

func (s *Service) gitlabDeployment(ctx context.Context, app store.Application, revision, status string) (scm.GitLabDeployment, error) {
	override, err := s.Store.ApplicationEnvironmentOverride(ctx, app.ID)
	if err != nil {
		return scm.GitLabDeployment{}, err
	}
	env := store.ResolveEnvironment(override, app.ClusterName, app.Namespaces[0].Namespace)
	d := scm.GitLabDeployment{Environment: env.Name, Tier: env.Tier, ExternalURL: env.URL, SHA: revision, Status: status}
	if !commitSHA.MatchString(revision) {
		return d, errors.New("deployment has no commit to report")
	}
	if commitSHA.MatchString(app.Revision) {
		d.Ref = ""
	} else {
		d.Ref = app.Revision
	}
	return d, nil
}

func (s *Service) postDeployment(ctx context.Context, app store.Application, settings store.EnvironmentSettings, d scm.GitLabDeployment) (int64, error) {
	repo, err := s.Store.RepositoryConfigurationByID(ctx, app.RepositoryConfigurationID)
	if err != nil {
		return 0, err
	}
	source, err := s.Store.GitSourceByID(ctx, repo.SourceID)
	if err != nil {
		return 0, err
	}
	details, err := scm.Details(source.RepositoryURL, "gitlab", settings.APIURL)
	if err != nil {
		return 0, err
	}
	token, err := gitops.APIToken(ctx, s.Store, s.EncryptionKey, settings.CredentialID, app.WorkspaceID)
	if err != nil {
		return 0, err
	}
	return scm.Client{}.RecordGitLabDeployment(ctx, details.APIURL, details.Repository, string(token), d)
}
