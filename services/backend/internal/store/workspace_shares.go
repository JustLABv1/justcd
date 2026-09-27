package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type WorkspaceConnectionShare struct {
	ID                 string    `json:"id"`
	Kind               string    `json:"kind"`
	ResourceID         string    `json:"resourceId"`
	ResourceName       string    `json:"resourceName"`
	RepositoryURL      string    `json:"repositoryUrl,omitempty"`
	OwnerWorkspaceID   string    `json:"ownerWorkspaceId"`
	OwnerWorkspaceName string    `json:"ownerWorkspaceName"`
	TargetWorkspaceID  string    `json:"targetWorkspaceId"`
	TargetWorkspaceName string   `json:"targetWorkspaceName"`
	CredentialID       *string   `json:"credentialId,omitempty"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"createdAt"`
	AcceptedAt         *time.Time `json:"acceptedAt,omitempty"`
}

func (s *Store) ListWorkspaceConnectionShares(ctx context.Context, workspaceID string) ([]WorkspaceConnectionShare, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT sh.id,'cluster',c.id,c.name,'',sh.owner_workspace_id,owner.name,sh.target_workspace_id,target.name,NULL::text,sh.status,sh.created_at,sh.accepted_at
		FROM workspace_cluster_shares sh
		JOIN clusters c ON c.id=sh.cluster_id
		JOIN workspaces owner ON owner.id=sh.owner_workspace_id
		JOIN workspaces target ON target.id=sh.target_workspace_id
		WHERE sh.owner_workspace_id=$1 OR sh.target_workspace_id=$1
		UNION ALL
		SELECT sh.id,'git-source',g.id,g.name,g.repository_url,sh.owner_workspace_id,owner.name,sh.target_workspace_id,target.name,CASE WHEN sh.target_workspace_id=$1 THEN sh.credential_id ELSE NULL::text END,sh.status,sh.created_at,sh.accepted_at
		FROM workspace_git_source_shares sh
		JOIN git_sources g ON g.id=sh.git_source_id
		JOIN workspaces owner ON owner.id=sh.owner_workspace_id
		JOIN workspaces target ON target.id=sh.target_workspace_id
		WHERE sh.owner_workspace_id=$1 OR sh.target_workspace_id=$1
		ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]WorkspaceConnectionShare, 0)
	for rows.Next() {
		var item WorkspaceConnectionShare
		if err := rows.Scan(&item.ID, &item.Kind, &item.ResourceID, &item.ResourceName, &item.RepositoryURL, &item.OwnerWorkspaceID, &item.OwnerWorkspaceName, &item.TargetWorkspaceID, &item.TargetWorkspaceName, &item.CredentialID, &item.Status, &item.CreatedAt, &item.AcceptedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateWorkspaceClusterShare(ctx context.Context, share WorkspaceConnectionShare) error {
	result, err := s.DB.ExecContext(ctx, `INSERT INTO workspace_cluster_shares(id,cluster_id,owner_workspace_id,target_workspace_id)
		SELECT $1,c.id,$3,$4 FROM clusters c WHERE c.id=$2 AND c.workspace_id=$3`, share.ID, share.ResourceID, share.OwnerWorkspaceID, share.TargetWorkspaceID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) CreateWorkspaceGitSourceShare(ctx context.Context, share WorkspaceConnectionShare) error {
	result, err := s.DB.ExecContext(ctx, `INSERT INTO workspace_git_source_shares(id,git_source_id,owner_workspace_id,target_workspace_id,credential_id)
		SELECT $1,g.id,$3,$4,$5 FROM git_sources g WHERE g.id=$2 AND g.workspace_id=$3`, share.ID, share.ResourceID, share.OwnerWorkspaceID, share.TargetWorkspaceID, share.CredentialID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DecideWorkspaceConnectionShare(ctx context.Context, shareID, targetWorkspaceID string, accept bool) error {
	status := "declined"
	var acceptedAt any
	if accept {
		status = "accepted"
		acceptedAt = time.Now()
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE workspace_cluster_shares SET status=$3,accepted_at=$4,updated_at=NOW() WHERE id=$1 AND target_workspace_id=$2 AND status='pending'`, shareID, targetWorkspaceID, status, acceptedAt)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		result, err = s.DB.ExecContext(ctx, `UPDATE workspace_git_source_shares SET status=$3,accepted_at=$4,updated_at=NOW() WHERE id=$1 AND target_workspace_id=$2 AND status='pending'`, shareID, targetWorkspaceID, status, acceptedAt)
		if err != nil {
			return err
		}
		count, _ = result.RowsAffected()
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return s.invalidatePlans(ctx)
}

func (s *Store) RevokeWorkspaceConnectionShare(ctx context.Context, shareID, workspaceID string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE workspace_cluster_shares SET status='revoked',updated_at=NOW() WHERE id=$1 AND (owner_workspace_id=$2 OR target_workspace_id=$2) AND status IN ('pending','accepted')`, shareID, workspaceID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		result, err = s.DB.ExecContext(ctx, `UPDATE workspace_git_source_shares SET status='revoked',updated_at=NOW() WHERE id=$1 AND (owner_workspace_id=$2 OR target_workspace_id=$2) AND status IN ('pending','accepted')`, shareID, workspaceID)
		if err != nil {
			return err
		}
		count, _ = result.RowsAffected()
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return s.invalidatePlans(ctx)
}

func (s *Store) SetWorkspaceGitSourceShareCredential(ctx context.Context, shareID, targetWorkspaceID string, credentialID *string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE workspace_git_source_shares SET credential_id=$3,updated_at=NOW() WHERE id=$1 AND target_workspace_id=$2 AND status='accepted'`, shareID, targetWorkspaceID, credentialID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return s.invalidatePlans(ctx)
}

func (s *Store) invalidatePlans(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE plans SET status='stale' WHERE status='current'`)
	return err
}

func (s *Store) SharedGitSourceWorkspaceIDs(ctx context.Context, sourceID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT target_workspace_id FROM workspace_git_source_shares WHERE git_source_id=$1 AND status='accepted' ORDER BY target_workspace_id`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func IsConnectionNotShared(err error) bool { return errors.Is(err, ErrConnectionNotShared) }
