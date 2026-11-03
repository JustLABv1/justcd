package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/justlab/justcd/services/backend/internal/publishing"
)

type PublishingProfile struct {
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

const profileColumns = `p.id,p.workspace_id,p.name,p.description,p.enabled,p.settings,p.credential_id,p.git_read_credential_id,p.version,(SELECT count(*) FROM published_applications a WHERE a.profile_id=p.id)`

func scanPublishingProfile(row interface{ Scan(...any) error }) (PublishingProfile, error) {
	var p PublishingProfile
	var settings []byte
	err := row.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Description, &p.Enabled, &settings, &p.CredentialID, &p.GitReadCredentialID, &p.Version, &p.Applications)
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

// Profile settings become immutable after the first publication. The row lock
// also serializes registration against edits so a destination cannot change mid-registration.
func (s *Store) SavePublishingProfile(ctx context.Context, p PublishingProfile, credentials []Credential, create bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	settings, _ := json.Marshal(p.Settings)
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
		if count > 0 && previous != p.Settings {
			return errors.New("profile is in use; create a new profile to change infrastructure settings")
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
