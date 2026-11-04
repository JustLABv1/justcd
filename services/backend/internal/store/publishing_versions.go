package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/justlab/justcd/services/backend/internal/publishing"
	"time"
)

type PublishingProfileVersion struct {
	ProfileID       string                     `json:"profileId"`
	Revision        int64                      `json:"revision"`
	Settings        publishing.ProfileSettings `json:"settings"`
	TemplateVersion string                     `json:"templateVersion"`
	CreatedAt       time.Time                  `json:"createdAt"`
}

func nullableProfileRevision(profile *string, revision int64) any {
	if profile == nil {
		return nil
	}
	return revision
}
func (s *Store) PublishingProfileVersion(ctx context.Context, id string, revision int64) (PublishingProfileVersion, error) {
	var v PublishingProfileVersion
	var settings []byte
	err := s.DB.QueryRowContext(ctx, `SELECT profile_id,revision,settings,template_version,created_at FROM publishing_profile_versions WHERE profile_id=$1 AND revision=$2`, id, revision).Scan(&v.ProfileID, &v.Revision, &settings, &v.TemplateVersion, &v.CreatedAt)
	if err == nil {
		err = json.Unmarshal(settings, &v.Settings)
	}
	return v, err
}
func (s *Store) PublishingProfileVersions(ctx context.Context, id string) ([]PublishingProfileVersion, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT profile_id,revision,settings,template_version,created_at FROM publishing_profile_versions WHERE profile_id=$1 ORDER BY revision DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PublishingProfileVersion{}
	for rows.Next() {
		var v PublishingProfileVersion
		var settings []byte
		if err := rows.Scan(&v.ProfileID, &v.Revision, &settings, &v.TemplateVersion, &v.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(settings, &v.Settings); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// Pin only after the reviewed recipes have been committed. The current profile
// remains the credential/enablement authority, not the application's defaults.
func (s *Store) UpgradePublishingRevision(ctx context.Context, expected Application, old PublishedApplication, config publishing.Config, revision int64, fingerprint string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if old.ProfileID == nil {
		return errors.New("application has no publishing profile")
	}
	var enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT enabled FROM publishing_profiles WHERE id=$1 FOR SHARE`, *old.ProfileID).Scan(&enabled); err != nil {
		return err
	}
	if !enabled {
		return errors.New("publishing profile is disabled")
	}
	app, err := scanApplication(tx.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1 FOR UPDATE`, old.ApplicationID))
	if err != nil {
		return err
	}
	if app.Decommissioning || app.SourceID != expected.SourceID || app.Revision != expected.Revision || app.ManifestPath != expected.ManifestPath || app.ClusterID != expected.ClusterID || app.Name != expected.Name || len(app.Namespaces) != 1 || app.Namespaces[0].Namespace != config.Namespace {
		return errors.New("application destination changed; review a fresh upgrade")
	}
	data, _ := json.Marshal(config)
	result, err := tx.ExecContext(ctx, `UPDATE published_applications SET profile_revision=$3,config=$4,fingerprint=$5,pipeline_url='' WHERE application_id=$1 AND profile_id=$2 AND profile_revision=$6 AND config=$7::jsonb`, old.ApplicationID, *old.ProfileID, revision, data, fingerprint, old.ProfileRevision, old.Config)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("application changed; review a fresh profile upgrade")
	}
	return tx.Commit()
}

type PublishingProfileStatus struct {
	NeedsBuild      bool   `json:"needsBuild"`
	ProfileID       string `json:"profileId"`
	Name            string `json:"name"`
	Revision        int64  `json:"revision"`
	LatestRevision  int64  `json:"latestRevision"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Enabled         bool   `json:"enabled"`
}

func (s *Store) PopulatePublishingProfileStatus(ctx context.Context, apps []Application) error {
	if len(apps) == 0 {
		return nil
	}
	ids := make([]string, len(apps))
	for i := range apps {
		ids[i] = apps[i].ID
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT a.application_id,p.id,p.name,a.profile_revision,p.current_revision,p.enabled,(a.pipeline_url='' AND a.fingerprint LIKE 'upgrade:%') FROM published_applications a JOIN publishing_profiles p ON p.id=a.profile_id WHERE a.application_id=ANY($1)`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	indexes := map[string]int{}
	for i := range apps {
		indexes[apps[i].ID] = i
	}
	for rows.Next() {
		var id string
		var status PublishingProfileStatus
		if err := rows.Scan(&id, &status.ProfileID, &status.Name, &status.Revision, &status.LatestRevision, &status.Enabled, &status.NeedsBuild); err != nil {
			return err
		}
		status.UpdateAvailable = status.LatestRevision > status.Revision
		apps[indexes[id]].PublishingProfile = &status
	}
	return rows.Err()
}
