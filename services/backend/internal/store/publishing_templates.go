package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// PublishingTemplateVersion is an immutable, workspace-owned set of template files.
type PublishingTemplateVersion struct {
	WorkspaceID string            `json:"workspaceId"`
	TemplateID  string            `json:"templateId"`
	Version     int               `json:"version"`
	Label       string            `json:"label"`
	Files       map[string]string `json:"files,omitempty"`
	CreatedBy   string            `json:"createdBy"`
	CreatedAt   time.Time         `json:"createdAt"`
}

const maxTemplateVersions = 200

// CreatePublishingTemplateVersion appends the next version. Versions are never
// updated or deleted, so applications and profiles can pin them safely.
func (s *Store) CreatePublishingTemplateVersion(ctx context.Context, workspace, templateID, label, createdBy string, files map[string]string) (PublishingTemplateVersion, error) {
	payload, err := json.Marshal(files)
	if err != nil {
		return PublishingTemplateVersion{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return PublishingTemplateVersion{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "template:"+workspace+":"+templateID); err != nil {
		return PublishingTemplateVersion{}, err
	}
	v := PublishingTemplateVersion{WorkspaceID: workspace, TemplateID: templateID, Label: label, CreatedBy: createdBy, Files: files}
	var latest int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(version),0) FROM publishing_template_versions WHERE workspace_id=$1 AND template_id=$2`, workspace, templateID).Scan(&latest); err != nil {
		return v, err
	}
	if latest >= maxTemplateVersions {
		return v, errors.New("this template already has the maximum number of versions")
	}
	v.Version = latest + 1
	if err = tx.QueryRowContext(ctx, `INSERT INTO publishing_template_versions(workspace_id,template_id,version,label,files,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING created_at`, workspace, templateID, v.Version, label, payload, createdBy).Scan(&v.CreatedAt); err != nil {
		return v, err
	}
	return v, tx.Commit()
}

func (s *Store) PublishingTemplateVersion(ctx context.Context, workspace, templateID string, version int) (PublishingTemplateVersion, error) {
	v := PublishingTemplateVersion{WorkspaceID: workspace, TemplateID: templateID, Version: version}
	var files []byte
	err := s.DB.QueryRowContext(ctx, `SELECT label,files,created_by,created_at FROM publishing_template_versions WHERE workspace_id=$1 AND template_id=$2 AND version=$3`, workspace, templateID, version).Scan(&v.Label, &files, &v.CreatedBy, &v.CreatedAt)
	if err == nil {
		err = json.Unmarshal(files, &v.Files)
	}
	return v, err
}

// PublishingTemplateVersions lists version metadata (without file contents),
// newest first. An empty templateID lists every template of the workspace.
func (s *Store) PublishingTemplateVersions(ctx context.Context, workspace, templateID string) ([]PublishingTemplateVersion, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT template_id,version,label,created_by,created_at FROM publishing_template_versions WHERE workspace_id=$1 AND ($2='' OR template_id=$2) ORDER BY template_id,version DESC`, workspace, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PublishingTemplateVersion{}
	for rows.Next() {
		v := PublishingTemplateVersion{WorkspaceID: workspace}
		if err := rows.Scan(&v.TemplateID, &v.Version, &v.Label, &v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
