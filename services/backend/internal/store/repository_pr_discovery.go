package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/justlab/justcd/services/backend/internal/scm"
)

type PRDestination struct {
	ClusterID string `json:"clusterId"`
	Namespace string `json:"namespace"`
}
type RepositoryPRSettings struct {
	PipelineStatusReporting bool            `json:"pipelineStatusReporting"`
	Enabled                 bool            `json:"enabled"`
	Provider                string          `json:"provider,omitempty"`
	APIURL                  string          `json:"apiUrl,omitempty"`
	CredentialID            string          `json:"credentialId"`
	Mode                    string          `json:"mode"`
	Destinations            []PRDestination `json:"destinations"`
	Profile                 PreviewProfile  `json:"profile"`
}
type RepositoryPRApplication struct {
	ID                string          `json:"id"`
	RepositoryID      string          `json:"repositoryId"`
	Number            int             `json:"number"`
	DefinitionName    string          `json:"definitionName"`
	ConfigurationPath string          `json:"configurationPath"`
	HeadSHA           string          `json:"headSha"`
	Mode              string          `json:"mode"`
	ApplicationID     *string         `json:"applicationId,omitempty"`
	ConnectionID      *string         `json:"connectionId,omitempty"`
	DefinitionRemoved bool            `json:"definitionRemoved"`
	Merged            bool            `json:"merged"`
	Phase             string          `json:"phase"`
	Error             string          `json:"error"`
	Plan              json.RawMessage `json:"plan,omitempty"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

const repositoryPRColumns = `id,repository_id,number,definition_name,configuration_path,head_sha,mode,application_id,connection_id,definition_removed,merged,phase,error,plan,updated_at`

func scanRepositoryPR(row interface{ Scan(...any) error }) (RepositoryPRApplication, error) {
	var v RepositoryPRApplication
	var plan []byte
	err := row.Scan(&v.ID, &v.RepositoryID, &v.Number, &v.DefinitionName, &v.ConfigurationPath, &v.HeadSHA, &v.Mode, &v.ApplicationID, &v.ConnectionID, &v.DefinitionRemoved, &v.Merged, &v.Phase, &v.Error, &plan, &v.UpdatedAt)
	v.Plan = plan
	return v, err
}
func (s *Store) RepositoryPRByID(ctx context.Context, id string) (RepositoryPRApplication, error) {
	return scanRepositoryPR(s.DB.QueryRowContext(ctx, `SELECT `+repositoryPRColumns+` FROM repository_pr_applications WHERE id=$1`, id))
}
func (s *Store) ListRepositoryPRApplications(ctx context.Context, id string) ([]RepositoryPRApplication, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+repositoryPRColumns+` FROM repository_pr_applications WHERE repository_id=$1 ORDER BY updated_at DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RepositoryPRApplication{}
	for rows.Next() {
		v, e := scanRepositoryPR(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (s *Store) EnabledPRRepositories(ctx context.Context) ([]RepositoryConfiguration, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+repositoryColumns+` FROM repository_configurations WHERE enabled AND pr_settings->>'enabled'='true' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RepositoryConfiguration{}
	for rows.Next() {
		v, e := scanRepository(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (p RepositoryPRSettings) Allows(cluster, namespace string) bool {
	for _, d := range p.Destinations {
		if d.ClusterID == cluster && d.Namespace == namespace {
			return true
		}
	}
	return false
}
func ValidateRepositoryPRSettings(p RepositoryPRSettings) error {
	if !p.Enabled {
		return nil
	}
	if p.Mode != "review-only" && p.Mode != "isolated" && p.Mode != "existing" {
		return errors.New("mode must be review-only, isolated, or existing")
	}
	if len(p.Destinations) == 0 || len(p.Destinations) > 32 {
		return errors.New("select 1-32 allowed destinations")
	}
	if p.Profile.MaxActive < 0 || p.Profile.MaxActive > 100 {
		return errors.New("maxActive must be 1-100, or zero for the default limit of 10")
	}
	if p.Profile.AllowForks {
		return errors.New("repository application discovery does not allow forks")
	}
	profile := p.Profile
	profile.Enabled = p.Mode != "review-only"
	profile.DeploymentMode = p.Mode
	if p.Mode == "review-only" {
		profile.DeploymentMode = ""
	} else if p.Mode == "isolated" { // The new application's own manifests are its preview overlay.
		profile.ManifestPath = "preview"
		profile.HelmValuesFiles = nil
		profile.HelmValuesYAML = ""
	}
	return ValidatePreviewProfile(&profile, Application{Renderer: "kustomize", ManifestPath: "application"})
}
func (s *Store) SaveRepositoryPRSettings(ctx context.Context, id string, p RepositoryPRSettings) error {
	if err := ValidateRepositoryPRSettings(p); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	repo, err := scanRepository(tx.QueryRowContext(ctx, `SELECT `+repositoryColumns+` FROM repository_configurations WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	oldPolicy := repo.PRSettings
	oldPolicy.PipelineStatusReporting = p.PipelineStatusReporting
	old, _ := json.Marshal(oldPolicy)
	raw, _ := json.Marshal(p)
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM repository_pr_applications WHERE repository_id=$1 AND application_id IS NOT NULL AND phase<>'promoted')`, id).Scan(&active); err != nil {
		return err
	}
	if active && string(old) != string(raw) {
		return errors.New("close and clean up repository PR applications before changing their policy")
	}
	if p.Enabled {
		var sourceURL, kind string
		var expiry sql.NullTime
		if err = tx.QueryRowContext(ctx, `SELECT repository_url FROM git_sources WHERE id=$1`, repo.SourceID).Scan(&sourceURL); err != nil {
			return err
		}
		if _, err = scm.Details(sourceURL, p.Provider, p.APIURL); err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, `SELECT kind,expires_at FROM credentials WHERE id=$1 AND (workspace_id IS NULL OR workspace_id=$2) FOR SHARE`, p.CredentialID, repo.WorkspaceID).Scan(&kind, &expiry); err != nil || kind != "git-https" || (expiry.Valid && !expiry.Time.After(time.Now())) {
			return errors.New("select an active HTTPS Git credential available to this workspace")
		}
		for _, d := range p.Destinations {
			var exists bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM namespace_bindings WHERE workspace_id=$1 AND cluster_id=$2 AND namespace=$3)`, repo.WorkspaceID, d.ClusterID, d.Namespace).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return errors.New("allowed destinations require an existing workspace namespace binding")
			}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE repository_configurations SET pr_settings=$2,pr_error='' WHERE id=$1`, id, raw)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE source_control_connections c SET pipeline_status_reporting=$2,updated_at=NOW() FROM repository_pr_applications p WHERE c.repository_pr_id=p.id AND p.repository_id=$1`, id, p.PipelineStatusReporting)
	if err != nil {
		return err
	}
	if repo.PRSettings.PipelineStatusReporting != p.PipelineStatusReporting {
		if _, err := tx.ExecContext(ctx, `UPDATE pull_request_reviews r SET reported_phase='' FROM source_control_connections c JOIN repository_pr_applications p ON c.repository_pr_id=p.id WHERE r.connection_id=c.id AND p.repository_id=$1`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpsertRepositoryPRApplication registers the app and its restricted connection atomically.
func (s *Store) UpsertRepositoryPRApplication(ctx context.Context, repo RepositoryConfiguration, v RepositoryPRApplication, app Application, c SourceControlConnection) (RepositoryPRApplication, error) {
	if app.WorkspaceID != repo.WorkspaceID || app.SourceID != repo.SourceID || len(app.Namespaces) != 1 {
		return v, errors.New("repository PR application source or destination is invalid")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	locked, err := scanRepository(tx.QueryRowContext(ctx, `SELECT `+repositoryColumns+` FROM repository_configurations WHERE id=$1 FOR UPDATE`, repo.ID))
	if err != nil {
		return v, err
	}
	a, _ := json.Marshal(locked.PRSettings)
	b, _ := json.Marshal(repo.PRSettings)
	if !locked.Enabled || !locked.PRSettings.Enabled || string(a) != string(b) {
		return v, errors.New("repository PR policy changed during discovery")
	}
	previous, err := scanRepositoryPR(tx.QueryRowContext(ctx, `SELECT `+repositoryPRColumns+` FROM repository_pr_applications WHERE repository_id=$1 AND number=$2 AND definition_name=$3 FOR UPDATE`, repo.ID, v.Number, v.DefinitionName))
	if err == nil {
		v = previous
		if v.ApplicationID == nil || v.Phase == "promoted" {
			return v, nil
		}
		app.ID = *v.ApplicationID
		c.ID = *v.ConnectionID
		old, err := scanApplication(tx.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1 FOR UPDATE`, app.ID))
		if err != nil {
			return v, err
		}
		if old.ClusterID != app.ClusterID || len(old.Namespaces) != 1 || old.Namespaces[0].Namespace != app.Namespaces[0].Namespace {
			return v, errors.New("PR destination cannot change; close this PR and clean up first")
		}
		if old.ConfigurationHash != app.ConfigurationHash || old.Revision != app.Revision {
			if err = updateApplication(ctx, tx, app); err != nil {
				return v, err
			}
		} else {
			return v, nil
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM repository_pr_applications WHERE repository_id=$1 AND application_id IS NOT NULL AND phase<>'promoted'`, repo.ID).Scan(&count); err != nil {
			return v, err
		}
		max := repo.PRSettings.Profile.MaxActive
		if max == 0 {
			max = 10
		}
		if count >= max {
			return v, errors.New("repository PR application limit reached")
		}
		v.ID = NewID()
		app.ID = NewID()
		c.ApplicationID = app.ID
		v.ApplicationID = &app.ID
		v.ConnectionID = &c.ID
		if err = createApplication(ctx, tx, app); err != nil {
			return v, fmt.Errorf("new PR application: %w", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO repository_pr_applications(id,repository_id,number,definition_name,configuration_path,head_sha,mode,application_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, v.ID, repo.ID, v.Number, v.DefinitionName, v.ConfigurationPath, app.Revision, v.Mode, app.ID)
		if err != nil {
			return v, err
		}
		profile, _ := json.Marshal(c.PreviewProfile)
		_, err = tx.ExecContext(ctx, `INSERT INTO source_control_connections(id,workspace_id,application_id,provider,api_url,repository,status_token_cipher,status_credential_id,preview_profile,managed_by_git,repository_pr_id,pipeline_status_reporting) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,TRUE,$10,$11)`, c.ID, app.WorkspaceID, app.ID, c.Provider, c.APIURL, c.Repository, []byte{}, c.StatusCredentialID, profile, v.ID, c.PipelineStatusReporting)
		if err != nil {
			return v, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO pull_request_reviews(id,connection_id,number,head_sha,source_url,event_at,preview_application_id,head_branch) VALUES($1,$2,$3,$4,'','1970-01-01'::timestamptz,$5,'')`, NewID(), c.ID, v.Number, app.Revision, app.ID)
		if err != nil {
			return v, err
		}
	} else {
		return v, err
	}
	if err = replaceRepositoryIgnores(ctx, tx, app); err != nil {
		return v, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=TRUE,configuration_path=$2,configuration_hash=$3,configuration_commit=$4 WHERE id=$1`, app.ID, app.ConfigurationPath, app.ConfigurationHash, app.Revision); err != nil {
		return v, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE repository_pr_applications SET connection_id=$2,head_sha=$3,configuration_path=$4,updated_at=NOW() WHERE id=$1`, v.ID, c.ID, app.Revision, app.ConfigurationPath); err != nil {
		return v, err
	}
	v.HeadSHA = app.Revision
	return v, tx.Commit()
}
func (s *Store) UpdateRepositoryPRResult(ctx context.Context, id string, r PullRequestReview) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE repository_pr_applications SET phase=$2,error=$3,plan=$4,updated_at=NOW() WHERE id=$1`, id, r.Phase, r.Error, nullableJSON(r.Plan))
	return err
}

// promoteRepositoryPR preserves application IDs and managed resource ownership.
// Only an explicit, merged repository PR record may adopt an existing app.
func promoteRepositoryPR(ctx context.Context, tx *sql.Tx, repo RepositoryConfiguration, app Application) (string, error) {
	v, err := scanRepositoryPR(tx.QueryRowContext(ctx, `SELECT `+repositoryPRColumns+` FROM repository_pr_applications WHERE repository_id=$1 AND definition_name=$2 AND mode='existing' AND application_id IS NOT NULL AND phase<>'promoted' FOR UPDATE`, repo.ID, app.Name))
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !v.Merged {
		return "", errors.New("waiting for provider to confirm the application PR was merged")
	}
	app.ID = *v.ApplicationID
	old, err := scanApplication(tx.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1 FOR UPDATE`, app.ID))
	if err != nil {
		return "", err
	}
	if old.SourceID != app.SourceID || old.ClusterID != app.ClusterID || len(old.Namespaces) != 1 || len(app.Namespaces) != 1 || old.Namespaces[0].Namespace != app.Namespaces[0].Namespace {
		return "", errors.New("merged application destination differs from the reviewed PR deployment")
	}
	if err = updateApplication(ctx, tx, app); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pull_request_reviews SET preview_application_id=NULL,phase='closed',closed=TRUE WHERE connection_id=$1`, v.ConnectionID); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE source_control_connections SET repository_pr_id=NULL,managed_by_git=FALSE,enabled=FALSE WHERE id=$1`, v.ConnectionID); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE repository_pr_applications SET phase='promoted',updated_at=NOW() WHERE id=$1`, v.ID); err != nil {
		return "", err
	}
	// A fresh tracked-source plan is required before normal auto reconciliation.
	if _, err = tx.ExecContext(ctx, `UPDATE applications SET auto_sync_paused=TRUE WHERE id=$1`, app.ID); err != nil {
		return "", err
	}
	return app.ID, nil
}
