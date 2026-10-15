package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

const AuditSchemaVersion = 1

type AuditFilter struct {
	EventID     int64
	WorkspaceID string
	Group       string
	Query       string
	ResourceID  string
	ActorID     string
	From        *time.Time
	Until       *time.Time
	Before      int64
	Through     int64
}

const auditSelect = `SELECT e.id,e.actor_id,COALESCE(NULLIF(u.display_name,''),u.email,''),e.action,e.resource_type,e.resource_id,e.details,e.created_at,e.workspace_id,e.resource_name,e.deployment_record
 FROM audit_events e LEFT JOIN users u ON u.id=e.actor_id
 WHERE ($1='' OR e.workspace_id=$1) AND ($2=0 OR e.id<$2) AND ($3=0 OR e.id<=$3)
 AND ($4='' OR e.resource_id=$4) AND ($5='' OR e.actor_id=$5)
 AND ($6::timestamptz IS NULL OR e.created_at >= $6) AND ($7::timestamptz IS NULL OR e.created_at < $7)
 AND ($8='' OR strpos(lower(concat_ws(' ',e.id::text,e.action,replace(replace(e.action,'_',' '),'.',' '),e.resource_type,e.resource_id,e.resource_name,u.display_name,u.email,e.actor_id)),lower($8))>0)
 AND ($9='all' OR CASE
 WHEN e.action ~ '^(sync|rollback|auto_sync|plan|deployment)\.' OR e.action ~ '(decommission|rollback|resource_adopted)' THEN 'deployments'
 WHEN e.action ~ '^(user|workspace_member|oidc|oidc_provider|credential|session|mfa)\.' OR e.action LIKE '%approval_policy%' THEN 'access'
 ELSE 'changes' END=$9)
 AND ($11=0 OR e.id=$11)
 ORDER BY e.id DESC LIMIT $10`

func (s *Store) QueryAuditEvents(ctx context.Context, filter AuditFilter, limit int) ([]AuditEvent, error) {
	if filter.Group == "" {
		filter.Group = "all"
	}
	rows, err := s.DB.QueryContext(ctx, auditSelect, filter.WorkspaceID, filter.Before, filter.Through, filter.ResourceID, filter.ActorID, filter.From, filter.Until, strings.TrimSpace(filter.Query), filter.Group, limit, filter.EventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuditEvent, 0)
	for rows.Next() {
		e, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanAuditEvent(rows *sql.Rows) (AuditEvent, error) {
	var e AuditEvent
	var deployment []byte
	err := rows.Scan(&e.ID, &e.ActorID, &e.ActorName, &e.Action, &e.ResourceType, &e.ResourceID, &e.Details, &e.CreatedAt, &e.WorkspaceID, &e.ResourceName, &deployment)
	e.SchemaVersion = AuditSchemaVersion
	e.Details = safeAuditDetails(e.Details)
	if len(deployment) > 0 {
		e.DeploymentRecord = json.RawMessage(deployment)
	}
	return e, err
}

func (s *Store) AuditEventByID(ctx context.Context, id int64) (AuditEvent, error) {
	items, err := s.QueryAuditEvents(ctx, AuditFilter{Before: id + 1}, 1)
	if err != nil {
		return AuditEvent{}, err
	}
	if len(items) == 0 || items[0].ID != id {
		return AuditEvent{}, sql.ErrNoRows
	}
	return items[0], nil
}

type AuditRetention struct {
	Days        int `json:"days"`
	DefaultDays int `json:"defaultDays"`
}

func (s *Store) AuditRetention(ctx context.Context, workspace string) (AuditRetention, error) {
	var value AuditRetention
	err := s.DB.QueryRowContext(ctx, `SELECT audit_retention_days FROM workspaces WHERE id=$1`, workspace).Scan(&value.Days)
	return value, err
}
func (s *Store) SetAuditRetention(ctx context.Context, workspace, actor string, days int) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE workspaces SET audit_retention_days=$2 WHERE id=$1`, workspace, days)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'workspace.audit_retention_updated','workspace',$2,jsonb_build_object('retentionDays',$3::int))`, actor, workspace, days)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// A bounded delete avoids long transactions when a workspace lowers retention.
func (s *Store) PruneAuditEvents(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM audit_events WHERE id IN (SELECT e.id FROM audit_events e JOIN workspaces w ON w.id=e.workspace_id WHERE w.audit_retention_days>0 AND e.created_at<NOW()-make_interval(days=>w.audit_retention_days) ORDER BY e.id LIMIT 5000)`)
	return err
}

// Portable evidence deliberately excludes free text, settings, manifests and
// arbitrary user-defined detail fields, even when their keys look innocuous.
func AuditExportEvent(e AuditEvent) AuditEvent {
	var details map[string]json.RawMessage
	_ = json.Unmarshal(e.Details, &details)
	safe := map[string]json.RawMessage{}
	for _, key := range []string{"operationId", "planId", "approvalId", "digest", "revision", "errorCode"} {
		var value string
		if raw, ok := details[key]; ok && json.Unmarshal(raw, &value) == nil {
			safe[key] = raw
		}
	}
	for _, key := range []string{"changes", "completed", "total", "attemptCount", "retentionDays"} {
		var value json.Number
		if raw, ok := details[key]; ok && len(raw) > 0 && raw[0] != '"' && json.Unmarshal(raw, &value) == nil {
			safe[key] = raw
		}
	}
	if raw, ok := details["anonymized"]; ok {
		var value bool
		if json.Unmarshal(raw, &value) == nil {
			safe["anonymized"] = raw
		}
	}
	if raw, ok := details["approvalIds"]; ok {
		var value []string
		if json.Unmarshal(raw, &value) == nil {
			safe["approvalIds"] = raw
		}
	}

	e.Details, _ = json.Marshal(safe)
	return e
}
