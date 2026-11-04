package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/justlab/justcd/services/backend/internal/publishing"
)

type PublishingProfile struct {
	PublishRevision     bool                       `json:"-"`
	CurrentRevision     int64                      `json:"currentRevision"`
	ID                  string                     `json:"id"`
	WorkspaceID         string                     `json:"workspaceId"`
	Name                string                     `json:"name"`
	Description         string                     `json:"description"`
	Enabled             bool                       `json:"enabled"`
	Settings            publishing.ProfileSettings `json:"settings"`
	CredentialID        string                     `json:"-"`
	GitReadCredentialID string                     `json:"-"`
	Version             int64                      `json:"version"`
	Applications        int                        `json:"applications"`
}

const profileColumns = `p.id,p.workspace_id,p.name,p.description,p.enabled,p.settings,p.credential_id,p.git_read_credential_id,p.version,p.current_revision,(SELECT count(*) FROM published_applications a WHERE a.profile_id=p.id)`

func scanPublishingProfile(row interface{ Scan(...any) error }) (PublishingProfile, error) {
	var p PublishingProfile
	var settings []byte
	err := row.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Description, &p.Enabled, &settings, &p.CredentialID, &p.GitReadCredentialID, &p.Version, &p.CurrentRevision, &p.Applications)
	if err == nil {
		err = json.Unmarshal(settings, &p.Settings)
	}
	return p, err
}
func (s *Store) PublishingProfile(ctx context.Context, id string) (PublishingProfile, error) {
	return scanPublishingProfile(s.DB.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM publishing_profiles p WHERE p.id=$1`, id))
}
func (s *Store) PublishingProfiles(ctx context.Context, workspace string) ([]PublishingProfile, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+profileColumns+` FROM publishing_profiles p WHERE p.workspace_id=$1 ORDER BY p.name`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PublishingProfile{}
	for rows.Next() {
		p, e := scanPublishingProfile(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

var ErrProfileChanged = errors.New("publishing profile changed; reload before saving")

// Repository/destination settings become immutable after the first publication;
// approved build image references remain editable. The row lock
// also serializes registration against edits so a destination cannot change mid-registration.
func (s *Store) SavePublishingProfile(ctx context.Context, p PublishingProfile, credentials []Credential, create bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	settings, _ := json.Marshal(p.Settings)
	publishRevision := create || p.PublishRevision
	if !create {
		var old []byte
		var version int64
		if err = tx.QueryRowContext(ctx, `SELECT settings,version FROM publishing_profiles WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, p.ID, p.WorkspaceID).Scan(&old, &version); err != nil {
			return err
		}
		if version != p.Version {
			return ErrProfileChanged
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM published_applications WHERE profile_id=$1`, p.ID).Scan(&count); err != nil {
			return err
		}
		var previous publishing.ProfileSettings
		if err = json.Unmarshal(old, &previous); err != nil {
			return err
		}
		publishRevision = p.PublishRevision || previous != p.Settings
		// Release build/runtime settings while retaining repository, destination,
		// namespace policy and secret-provider identity.
		previous.BaseImage = p.Settings.BaseImage
		previous.BuilderImage = p.Settings.BuilderImage
		previous.WebServerImage = p.Settings.WebServerImage
		previous.RunnerTag = p.Settings.RunnerTag
		previous.Exposure = p.Settings.Exposure
		previous.ExposureLabels = p.Settings.ExposureLabels
		previous.ExposureAnnotations = p.Settings.ExposureAnnotations
		previous.IngressClass = p.Settings.IngressClass
		previous.IngressTLS = p.Settings.IngressTLS
		previous.TemplateVersion = p.Settings.TemplateVersion
		previous.TemplateParameters = p.Settings.TemplateParameters
		previous.RunnerServiceAccount = p.Settings.RunnerServiceAccount
		previous.RunnerSCC = p.Settings.RunnerSCC
		previous.BuilderSetup = p.Settings.BuilderSetup
		previous.CAVariable = p.Settings.CAVariable
		previous.BuildEngine = p.Settings.BuildEngine
		previous.GitImage = p.Settings.GitImage
		previous.PullSecret = p.Settings.PullSecret
		if count > 0 && previous != p.Settings {
			return errors.New("profile is in use; only build engines, build/deployment job images, runner tags, runner service account/SCC/setup script, route/ingress type and labels/annotations, template version/parameters, custom CA variable names, and image-pull secrets may change; create a new profile to change other infrastructure settings")
		}
	}
	for _, c := range credentials {
		if create {
			_, err = tx.ExecContext(ctx, `INSERT INTO credentials(id,workspace_id,name,kind,secret_cipher) VALUES($1,$2,$3,$4,$5)`, c.ID, c.WorkspaceID, c.Name, c.Kind, c.Cipher)
		} else {
			var result sql.Result
			result, err = tx.ExecContext(ctx, `UPDATE credentials SET secret_cipher=$3,name=$4,updated_at=now() WHERE id=$1 AND workspace_id=$2`, c.ID, p.WorkspaceID, c.Cipher, c.Name)
			if err == nil {
				n, _ := result.RowsAffected()
				if n != 1 {
					err = errors.New("profile credential not found")
				}
			}
		}
		if err != nil {
			return err
		}
	}
	if create {
		_, err = tx.ExecContext(ctx, `INSERT INTO publishing_profiles(id,workspace_id,name,description,enabled,settings,credential_id,git_read_credential_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, p.ID, p.WorkspaceID, p.Name, p.Description, p.Enabled, settings, p.CredentialID, p.GitReadCredentialID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE publishing_profiles SET name=$3,description=$4,enabled=$5,settings=$6,version=version+1 WHERE id=$1 AND workspace_id=$2`, p.ID, p.WorkspaceID, p.Name, p.Description, p.Enabled, settings)
	}
	if err != nil {
		return err
	}
	if publishRevision {
		if !create {
			if _, err = tx.ExecContext(ctx, `UPDATE publishing_profiles SET current_revision=current_revision+1 WHERE id=$1`, p.ID); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO publishing_profile_versions(profile_id,revision,settings,template_version) SELECT id,current_revision,settings,$2 FROM publishing_profiles WHERE id=$1`, p.ID, publishing.Version); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) DeletePublishingProfile(ctx context.Context, p PublishingProfile) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM publishing_profiles WHERE id=$1 AND workspace_id=$2 AND version=$3`, p.ID, p.WorkspaceID, p.Version)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrProfileChanged
	}
	for _, id := range []string{p.CredentialID, p.GitReadCredentialID} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM credentials WHERE id=$1 AND workspace_id=$2`, id, p.WorkspaceID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
