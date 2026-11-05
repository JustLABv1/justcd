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

// gitlabTarget is everything needed to talk to the application's GitLab project.
type gitlabTarget struct {
	apiURL, repository, token string
}

// gitlabEnvironmentTarget resolves the reporting target. It returns false when
// the application is not eligible or the repository has not opted in.
func (s *Service) gitlabEnvironmentTarget(ctx context.Context, app store.Application) (gitlabTarget, bool, error) {
	// Branch tests deploy unmerged code; they must not appear as the real environment.
	if app.RepositoryConfigurationID == "" || app.BranchTest != nil || len(app.Namespaces) == 0 {
		return gitlabTarget{}, false, nil
	}
	settings, err := s.Store.EnvironmentSettings(ctx, app.RepositoryConfigurationID)
	if err != nil || !settings.Enabled {
		return gitlabTarget{}, false, err
	}
	repo, err := s.Store.RepositoryConfigurationByID(ctx, app.RepositoryConfigurationID)
	if err != nil {
		return gitlabTarget{}, false, err
	}
	source, err := s.Store.GitSourceByID(ctx, repo.SourceID)
	if err != nil {
		return gitlabTarget{}, false, err
	}
	details, err := scm.Details(source.RepositoryURL, "gitlab", settings.APIURL)
	if err != nil {
		return gitlabTarget{}, true, err
	}
	token, err := gitops.APIToken(ctx, s.Store, s.EncryptionKey, settings.CredentialID, app.WorkspaceID)
	if err != nil {
		return gitlabTarget{}, true, err
	}
	return gitlabTarget{apiURL: details.APIURL, repository: details.Repository, token: string(token)}, true, nil
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
	if !commitSHA.MatchString(app.Revision) {
		d.Ref = app.Revision
	}
	return d, nil
}

// reportEnvironmentDeployment records a deployment in the application's GitLab
// environment and returns its ID (0 when nothing was recorded). It is opt-in
// per repository, never changes the operation's outcome, and keeps the last
// result, including any error, for display in the UI.
func (s *Service) reportEnvironmentDeployment(app store.Application, operationID string, plan store.PlanRecord, revision, status string) int64 {
	if plan.Plan.Decommission {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	target, enabled, err := s.gitlabEnvironmentTarget(ctx, app)
	if !enabled && err == nil {
		return 0
	}
	record := store.EnvironmentDeployment{OperationID: operationID, ApplicationID: app.ID, Status: status, SHA: revision}
	var id int64
	if err == nil {
		var deployment scm.GitLabDeployment
		deployment, err = s.gitlabDeployment(ctx, app, revision, status)
		record.Environment = deployment.Environment
		if err == nil {
			id, err = scm.Client{}.RecordGitLabDeployment(ctx, target.apiURL, target.repository, target.token, deployment)
			record.GitLabDeploymentID = id
		}
	}
	if err != nil {
		record.Error = err.Error()
		slog.Warn("could not report GitLab environment deployment", "applicationId", app.ID, "operationId", operationID, "error", err)
	}
	if err := s.Store.RecordEnvironmentDeployment(ctx, record); err != nil {
		slog.Warn("could not save GitLab environment deployment", "applicationId", app.ID, "error", err)
	}
	return id
}

// finishEnvironmentDeployment closes a deployment opened as "running". When no
// deployment was opened (reporting was down or off then), it records the final
// status directly so GitLab still sees the outcome.
func (s *Service) finishEnvironmentDeployment(app store.Application, operationID string, deploymentID int64, revision, status string) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	target, enabled, err := s.gitlabEnvironmentTarget(ctx, app)
	if !enabled && err == nil {
		return
	}
	if err == nil && deploymentID == 0 {
		s.reportEnvironmentDeployment(app, operationID, store.PlanRecord{}, revision, status)
		return
	}
	record := store.EnvironmentDeployment{OperationID: operationID, ApplicationID: app.ID, Status: status, SHA: revision, GitLabDeploymentID: deploymentID}
	if err == nil {
		err = scm.Client{}.UpdateGitLabDeployment(ctx, target.apiURL, target.repository, target.token, deploymentID, status)
	}
	if err != nil {
		record.Error = err.Error()
		slog.Warn("could not finish GitLab environment deployment", "applicationId", app.ID, "operationId", operationID, "error", err)
	}
	if existing, getErr := s.Store.LatestEnvironmentDeployment(ctx, app.ID); getErr == nil && existing != nil && existing.OperationID == operationID {
		record.Environment = existing.Environment
	}
	if err := s.Store.RecordEnvironmentDeployment(ctx, record); err != nil {
		slog.Warn("could not save GitLab environment deployment", "applicationId", app.ID, "error", err)
	}
}
