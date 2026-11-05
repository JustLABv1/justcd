package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/justlab/justcd/services/backend/internal/publishing"
)

const maxCustomTemplates = 50

// ErrTemplateExists reports an ID that is already used in the workspace.
var ErrTemplateExists = errors.New("a template with this ID already exists in the workspace")

// ErrTemplateInUse reports a template that profiles still reference.
var ErrTemplateInUse = errors.New("profiles still use this template; switch or delete them first")

const customTemplateColumns = `id,name,description,category,detail,runtime,entry_point,port,layout,base_template`

type rowScanner interface{ Scan(dest ...any) error }

func scanCustomTemplate(row rowScanner) (publishing.Template, error) {
	t := publishing.Template{Custom: true, Version: publishing.Version}
	var layout []byte
	if err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Category, &t.Detail, &t.Runtime, &t.EntryPoint, &t.Port, &layout, &t.BaseTemplate); err != nil {
		return t, err
	}
	return t, json.Unmarshal(layout, &t.Layout)
}

// CustomPublishingTemplates lists a workspace's own templates.
func (s *Store) CustomPublishingTemplates(ctx context.Context, workspace string) ([]publishing.Template, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+customTemplateColumns+` FROM publishing_templates WHERE workspace_id=$1 ORDER BY lower(name),id`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []publishing.Template{}
	for rows.Next() {
		t, err := scanCustomTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CustomPublishingTemplate loads one workspace template.
func (s *Store) CustomPublishingTemplate(ctx context.Context, workspace, id string) (publishing.Template, error) {
	return scanCustomTemplate(s.DB.QueryRowContext(ctx, `SELECT `+customTemplateColumns+` FROM publishing_templates WHERE workspace_id=$1 AND id=$2`, workspace, id))
}

// CreateCustomPublishingTemplate stores the template and its first version in
// one transaction, so a custom template always has files.
func (s *Store) CreateCustomPublishingTemplate(ctx context.Context, workspace string, t publishing.Template, label, createdBy string, files map[string]string) (int, error) {
	layout, err := json.Marshal(append([]publishing.LayoutRule{}, t.Layout...))
	if err != nil {
		return 0, err
	}
	payload, err := json.Marshal(files)
	if err != nil {
		return 0, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "templates:"+workspace); err != nil {
		return 0, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM publishing_templates WHERE workspace_id=$1`, workspace).Scan(&count); err != nil {
		return 0, err
	}
	if count >= maxCustomTemplates {
		return 0, errors.New("the workspace already has the maximum number of custom templates")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO publishing_templates(workspace_id,id,name,description,category,detail,runtime,entry_point,port,layout,base_template,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		workspace, t.ID, t.Name, t.Description, t.Category, t.Detail, t.Runtime, t.EntryPoint, t.Port, layout, t.BaseTemplate, createdBy); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return 0, ErrTemplateExists
		}
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO publishing_template_versions(workspace_id,template_id,version,label,files,created_by) VALUES($1,$2,1,$3,$4,$5)`, workspace, t.ID, label, payload, createdBy); err != nil {
		return 0, err
	}
	return 1, tx.Commit()
}

// UpdateCustomPublishingTemplate changes the definition (not the ID or kind).
func (s *Store) UpdateCustomPublishingTemplate(ctx context.Context, workspace string, t publishing.Template) error {
	layout, err := json.Marshal(append([]publishing.LayoutRule{}, t.Layout...))
	if err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE publishing_templates SET name=$3,description=$4,category=$5,detail=$6,entry_point=$7,port=$8,layout=$9,updated_at=now() WHERE workspace_id=$1 AND id=$2`,
		workspace, t.ID, t.Name, t.Description, t.Category, t.Detail, t.EntryPoint, t.Port, layout)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("template not found")
	}
	return nil
}

// DeleteCustomPublishingTemplate removes a template and its versions unless
// a profile or any recorded profile revision references it.
func (s *Store) DeleteCustomPublishingTemplate(ctx context.Context, workspace, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var used bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM publishing_profiles WHERE workspace_id=$1 AND settings->>'templateId'=$2)
 OR EXISTS(SELECT 1 FROM publishing_profile_versions v JOIN publishing_profiles p ON p.id=v.profile_id WHERE p.workspace_id=$1 AND v.settings->>'templateId'=$2)`, workspace, id).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrTemplateInUse
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM publishing_template_versions WHERE workspace_id=$1 AND template_id=$2`, workspace, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM publishing_templates WHERE workspace_id=$1 AND id=$2`, workspace, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("template not found")
	}
	return tx.Commit()
}
