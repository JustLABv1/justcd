package store

import (
	"context"
	"encoding/json"
	"slices"
	"time"
)

type MCPCredential struct {
	ID          string     `json:"id"`
	UserID      string     `json:"-"`
	WorkspaceID string     `json:"workspaceId"`
	Name        string     `json:"name"`
	AllowPlan   bool       `json:"allowPlan"`
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   *time.Time `json:"expiresAt"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty"`
}

func (s *Store) CreateMCPCredential(ctx context.Context, c MCPCredential, hash []byte, lifetime int) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT allowed_lifetimes FROM mcp_policy WHERE singleton=TRUE FOR SHARE`).Scan(&raw); err != nil {
		return err
	}
	var allowed []int
	if err := json.Unmarshal(raw, &allowed); err != nil {
		return err
	}
	if !slices.Contains(allowed, lifetime) {
		return &AccessError{"This MCP key lifetime is not allowed by the instance policy."}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_credentials(id,user_id,workspace_id,name,token_hash,allow_plan,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, c.ID, c.UserID, c.WorkspaceID, c.Name, hash, c.AllowPlan, c.ExpiresAt); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) MCPCredentialByHash(ctx context.Context, hash []byte) (MCPCredential, error) {
	var c MCPCredential
	err := s.DB.QueryRowContext(ctx, `SELECT id,user_id,workspace_id,name,allow_plan,created_at,expires_at FROM mcp_credentials WHERE token_hash=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>NOW())`, hash).Scan(&c.ID, &c.UserID, &c.WorkspaceID, &c.Name, &c.AllowPlan, &c.CreatedAt, &c.ExpiresAt)
	return c, err
}
func (s *Store) ListMCPCredentials(ctx context.Context, userID string) ([]MCPCredential, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,workspace_id,name,allow_plan,created_at,expires_at,revoked_at FROM mcp_credentials WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MCPCredential{}
	for rows.Next() {
		var c MCPCredential
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Name, &c.AllowPlan, &c.CreatedAt, &c.ExpiresAt, &c.RevokedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
func (s *Store) RevokeMCPCredential(ctx context.Context, id, userID string) (bool, error) {
	result, err := s.DB.ExecContext(ctx, `UPDATE mcp_credentials SET revoked_at=NOW() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

// MCPPolicy controls new credentials only; existing credentials retain their expiry.
type MCPPolicy struct {
	AllowedLifetimes []int `json:"allowedLifetimes"`
}

func ValidateMCPLifetimes(values []int) error {
	if len(values) == 0 {
		return &AccessError{"Choose at least one MCP key lifetime."}
	}
	seen := map[int]bool{}
	for _, value := range values {
		if !slices.Contains([]int{7, 30, 90, 365, 0}, value) || seen[value] {
			return &AccessError{"MCP key lifetimes must be unique values from 7, 30, 90, 365 and 0 (no expiry)."}
		}
		seen[value] = true
	}
	return nil
}
func (s *Store) MCPPolicy(ctx context.Context) (MCPPolicy, error) {
	var p MCPPolicy
	var raw []byte
	if err := s.DB.QueryRowContext(ctx, `SELECT allowed_lifetimes FROM mcp_policy WHERE singleton=TRUE`).Scan(&raw); err != nil {
		return p, err
	}
	err := json.Unmarshal(raw, &p.AllowedLifetimes)
	return p, err
}
func (s *Store) UpdateMCPPolicy(ctx context.Context, p MCPPolicy, actorID string) error {
	if err := ValidateMCPLifetimes(p.AllowedLifetimes); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE mcp_policy SET allowed_lifetimes=$1::jsonb WHERE singleton=TRUE`, mustJSON(p.AllowedLifetimes)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'instance.mcp_policy_updated','instance','instance',$2::jsonb)`, actorID, mustJSON(p)); err != nil {
		return err
	}
	return tx.Commit()
}
