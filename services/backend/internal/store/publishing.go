package store

import (
	"context"
	"encoding/json"
	"errors"
)

type PublishedApplication struct {
	ProfileRevision int64           `json:"profileRevision,omitempty"`
	ProfileVersion  int64           `json:"-"`
	ProfileID       *string         `json:"profileId,omitempty"`
	ApplicationID   string          `json:"applicationId"`
	Config          json.RawMessage `json:"config"`
	GitLabURL       string          `json:"gitlabUrl"`
	Group           string          `json:"group"`
	Fingerprint     string          `json:"-"`
	RepositoryURL   string          `json:"repositoryUrl"`
	ProjectURL      string          `json:"projectUrl"`
	ProjectID       int             `json:"-"`
	PipelineURL     string          `json:"pipelineUrl"`
}

func (s *Store) PublishedApplication(ctx context.Context, workspace, url string) (PublishedApplication, error) {
	var p PublishedApplication
	err := s.DB.QueryRowContext(ctx, `SELECT application_id,config,gitlab_url,group_path,fingerprint,repository_url,project_url,gitlab_project_id,pipeline_url,profile_id,COALESCE(profile_revision,0) FROM published_applications WHERE workspace_id=$1 AND repository_url=$2`, workspace, url).Scan(&p.ApplicationID, &p.Config, &p.GitLabURL, &p.Group, &p.Fingerprint, &p.RepositoryURL, &p.ProjectURL, &p.ProjectID, &p.PipelineURL, &p.ProfileID, &p.ProfileRevision)
	return p, err
}

// RegisterPublishedApplication atomically stores the encrypted Git read credential,
// source, application and publication metadata. Failed registrations leave no local partial objects.
func (s *Store) RegisterPublishedApplication(ctx context.Context, c Credential, source GitSource, a Application, p PublishedApplication, extraCredentials ...Credential) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if p.ProfileID != nil {
		var enabled bool
		var version int64
		if err = tx.QueryRowContext(ctx, `SELECT enabled,version FROM publishing_profiles WHERE id=$1 AND workspace_id=$2 FOR SHARE`, *p.ProfileID, a.WorkspaceID).Scan(&enabled, &version); err != nil {
			return err
		}
		if !enabled || (p.ProfileVersion != 0 && p.ProfileVersion != version) {
			return errors.New("publishing profile is disabled")
		}
	}
	if c.ID != "" {
		_, err = tx.ExecContext(ctx, `INSERT INTO credentials(id,workspace_id,name,kind,secret_cipher) VALUES($1,$2,$3,$4,$5)`, c.ID, c.WorkspaceID, c.Name, c.Kind, c.Cipher)
		if err != nil {
			return err
		}
	}
	for _, credential := range extraCredentials {
		if credential.ID == "" {
			continue
		}
		if credential.Kind != "publishing-database" || credential.WorkspaceID == nil || *credential.WorkspaceID != a.WorkspaceID {
			return errors.New("invalid runtime credential scope")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO credentials(id,workspace_id,name,kind,secret_cipher) VALUES($1,$2,$3,$4,$5)`, credential.ID, credential.WorkspaceID, credential.Name, credential.Kind, credential.Cipher); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO git_sources(id,workspace_id,name,repository_url,credential_id) VALUES($1,$2,$3,$4,$5)`, source.ID, source.WorkspaceID, source.Name, source.RepositoryURL, source.CredentialID)
	if err != nil {
		return err
	}
	if len(a.Namespaces) != 1 {
		return errors.New("publishing requires one namespace")
	}
	binding, err := publishingNamespaceBinding(ctx, tx, a.WorkspaceID, a.ClusterID, a.Namespaces[0].Namespace, a.CreateNamespaces)
	if err != nil {
		return err
	}
	a.Namespaces = []NamespaceBinding{binding}
	if err = createApplication(ctx, tx, a); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO published_applications(application_id,workspace_id,fingerprint,repository_url,project_url,gitlab_project_id,config,gitlab_url,group_path,profile_id,profile_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, a.ID, a.WorkspaceID, p.Fingerprint, p.RepositoryURL, p.ProjectURL, p.ProjectID, p.Config, p.GitLabURL, p.Group, p.ProfileID, nullableProfileRevision(p.ProfileID, p.ProfileRevision))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) SetPublishingPipeline(ctx context.Context, appID, url string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE published_applications SET pipeline_url=$2,
 completion_status=CASE WHEN config->>'managedCompletion'='true' AND pipeline_url IS DISTINCT FROM $2 THEN 'build_pending' ELSE completion_status END,
 completion_error=CASE WHEN config->>'managedCompletion'='true' AND pipeline_url IS DISTINCT FROM $2 THEN '' ELSE completion_error END,
 completion_next_at=CASE WHEN config->>'managedCompletion'='true' AND pipeline_url IS DISTINCT FROM $2 THEN now() ELSE completion_next_at END
 WHERE application_id=$1`, appID, url)
	return err
}

func (s *Store) UpdatePublishedSource(ctx context.Context, p PublishedApplication) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE published_applications SET fingerprint=$2,config=$3,pipeline_url='' WHERE application_id=$1`, p.ApplicationID, p.Fingerprint, p.Config)
	return err
}
