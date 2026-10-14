package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/justlab/justcd/services/backend/internal/core"
)

func (s *Store) RenderArtifact(ctx context.Context, applicationID, inputDigest string) ([]core.Resource, bool, error) {
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT resources FROM application_render_artifacts WHERE application_id=$1 AND input_digest=$2`, applicationID, inputDigest).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var resources []core.Resource
	err = json.Unmarshal(raw, &resources)
	return resources, err == nil, err
}

// Concurrent planners must all use the first persisted render, including the
// planner that loses the insert race. Never overwrite a reviewed artifact.
func (s *Store) SaveRenderArtifact(ctx context.Context, applicationID, inputDigest string, resources []core.Resource) ([]core.Resource, error) {
	raw, err := json.Marshal(resources)
	if err != nil {
		return nil, err
	}
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO application_render_artifacts(application_id,input_digest,resources) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, applicationID, inputDigest, raw); err != nil {
		return nil, err
	}
	stored, found, err := s.RenderArtifact(ctx, applicationID, inputDigest)
	if err == nil && !found {
		return nil, sql.ErrNoRows
	}
	return stored, err
}
