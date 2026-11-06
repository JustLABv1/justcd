package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/scm"
)

// Environment names follow GitLab's rules: letters, digits and a small set
// of separators, starting and ending with an alphanumeric character.
var environmentNamePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9 _./-]{0,253}[A-Za-z0-9])?$`)
var environmentNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9 _./-]+`)

var environmentTiers = map[string]bool{"production": true, "staging": true, "testing": true, "development": true, "other": true}

// ApplicationEnvironment holds the optional justcd.yaml override. Empty
// fields fall back to the cluster/namespace-derived defaults.
type ApplicationEnvironment struct {
	Name string `json:"name,omitempty"`
	Tier string `json:"tier,omitempty"`
	URL  string `json:"url,omitempty"`
}

func (e ApplicationEnvironment) IsZero() bool { return e == ApplicationEnvironment{} }

func ValidateApplicationEnvironment(e ApplicationEnvironment) error {
	if e.Name != "" && !environmentNamePattern.MatchString(e.Name) {
		return errors.New("environment name may contain letters, digits, spaces and _ . / - (at most 255 characters) and must start and end with a letter or digit")
	}
	if e.Tier != "" && !environmentTiers[e.Tier] {
		return errors.New("environment tier must be production, staging, testing, development, or other")
	}
	if e.URL != "" {
		u, err := url.Parse(e.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(e.URL) > 255 {
			return errors.New("environment url must be an https URL without credentials, at most 255 characters")
		}
	}
	return nil
}

// DefaultEnvironmentName derives "cluster/namespace" for applications that do
// not name their environment explicitly.
func DefaultEnvironmentName(cluster, namespace string) string {
	clean := func(v string) string {
		return strings.Trim(environmentNameUnsafe.ReplaceAllString(strings.TrimSpace(v), "-"), " -./_")
	}
	name := clean(cluster) + "/" + clean(namespace)
	name = strings.Trim(name, "/")
	if len(name) > 255 {
		name = strings.Trim(name[:255], " -./_")
	}
	return name
}

// ResolveEnvironment merges an override with the derived defaults.
func ResolveEnvironment(override ApplicationEnvironment, cluster, namespace string) ApplicationEnvironment {
	resolved := override
	if resolved.Name == "" {
		resolved.Name = DefaultEnvironmentName(cluster, namespace)
	}
	if resolved.Tier == "" {
		resolved.Tier = "other"
	}
	return resolved
}

type EnvironmentSettings struct {
	Enabled      bool   `json:"enabled"`
	CredentialID string `json:"credentialId"`
	APIURL       string `json:"apiUrl,omitempty"`
}

func (s *Store) EnvironmentSettings(ctx context.Context, repositoryID string) (EnvironmentSettings, error) {
	var v EnvironmentSettings
	err := s.DB.QueryRowContext(ctx, `SELECT enabled,credential_id,api_url FROM repository_environment_settings WHERE repository_id=$1`, repositoryID).Scan(&v.Enabled, &v.CredentialID, &v.APIURL)
	if errors.Is(err, sql.ErrNoRows) {
		return EnvironmentSettings{}, nil
	}
	return v, err
}

func (s *Store) SaveEnvironmentSettings(ctx context.Context, repositoryID string, v EnvironmentSettings) error {
	repo, err := s.RepositoryConfigurationByID(ctx, repositoryID)
	if err != nil {
		return err
	}
	if v.Enabled {
		source, err := s.GitSourceByID(ctx, repo.SourceID)
		if err != nil {
			return err
		}
		details, err := scm.Details(source.RepositoryURL, "gitlab", v.APIURL)
		if err != nil {
			return err
		}
		if details.Provider != "gitlab" {
			return errors.New("GitLab environments are only available for GitLab repositories")
		}
		credential, err := s.CredentialByID(ctx, v.CredentialID)
		if err != nil || credential.Kind != "git-https" || (credential.WorkspaceID != nil && *credential.WorkspaceID != repo.WorkspaceID) ||
			(credential.ExpiresAt != nil && !credential.ExpiresAt.After(time.Now())) {
			return errors.New("select an active HTTPS Git credential available to this workspace")
		}
	} else {
		v.CredentialID, v.APIURL = "", ""
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO repository_environment_settings(repository_id,enabled,credential_id,api_url) VALUES($1,$2,$3,$4)
		ON CONFLICT(repository_id) DO UPDATE SET enabled=EXCLUDED.enabled,credential_id=EXCLUDED.credential_id,api_url=EXCLUDED.api_url,updated_at=NOW()`, repositoryID, v.Enabled, v.CredentialID, v.APIURL)
	return err
}

func (s *Store) ApplicationEnvironmentOverride(ctx context.Context, applicationID string) (ApplicationEnvironment, error) {
	var e ApplicationEnvironment
	err := s.DB.QueryRowContext(ctx, `SELECT name,tier,external_url FROM application_environments WHERE application_id=$1`, applicationID).Scan(&e.Name, &e.Tier, &e.URL)
	if errors.Is(err, sql.ErrNoRows) {
		return ApplicationEnvironment{}, nil
	}
	return e, err
}

func writeApplicationEnvironment(ctx context.Context, tx *sql.Tx, applicationID string, e ApplicationEnvironment) error {
	if err := ValidateApplicationEnvironment(e); err != nil {
		return err
	}
	if e.IsZero() {
		_, err := tx.ExecContext(ctx, `DELETE FROM application_environments WHERE application_id=$1`, applicationID)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO application_environments(application_id,name,tier,external_url) VALUES($1,$2,$3,$4)
		ON CONFLICT(application_id) DO UPDATE SET name=EXCLUDED.name,tier=EXCLUDED.tier,external_url=EXCLUDED.external_url`, applicationID, e.Name, e.Tier, e.URL)
	return err
}

type EnvironmentDeployment struct {
	OperationID        string    `json:"operationId"`
	ApplicationID      string    `json:"applicationId"`
	Environment        string    `json:"environment"`
	GitLabDeploymentID int64     `json:"gitlabDeploymentId,omitempty"`
	Status             string    `json:"status"`
	SHA                string    `json:"sha,omitempty"`
	Error              string    `json:"error,omitempty"`
	ReportedAt         time.Time `json:"reportedAt"`
}

func (s *Store) RecordEnvironmentDeployment(ctx context.Context, d EnvironmentDeployment) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO environment_deployments(operation_id,application_id,environment,gitlab_deployment_id,status,sha,error) VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT(operation_id) DO UPDATE SET gitlab_deployment_id=EXCLUDED.gitlab_deployment_id,status=EXCLUDED.status,sha=EXCLUDED.sha,error=EXCLUDED.error,reported_at=NOW()`,
		d.OperationID, d.ApplicationID, d.Environment, d.GitLabDeploymentID, d.Status, d.SHA, d.Error)
	return err
}

func (s *Store) LatestEnvironmentDeployment(ctx context.Context, applicationID string) (*EnvironmentDeployment, error) {
	var d EnvironmentDeployment
	err := s.DB.QueryRowContext(ctx, `SELECT operation_id,application_id,environment,gitlab_deployment_id,status,sha,error,reported_at FROM environment_deployments WHERE application_id=$1 ORDER BY reported_at DESC LIMIT 1`, applicationID).
		Scan(&d.OperationID, &d.ApplicationID, &d.Environment, &d.GitLabDeploymentID, &d.Status, &d.SHA, &d.Error, &d.ReportedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &d, err
}
