package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
)

type AgentActivity struct {
	ID            string     `json:"id"`
	Profile       string     `json:"profile"`
	Namespace     string     `json:"namespace"`
	Method        string     `json:"method"`
	Resource      string     `json:"resource"`
	OperationID   string     `json:"operationId,omitempty"`
	ApplicationID string     `json:"applicationId,omitempty"`
	State         string     `json:"state"`
	HTTPStatus    int        `json:"httpStatus,omitempty"`
	ErrorCode     string     `json:"errorCode,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	FinishedAt    *time.Time `json:"finishedAt,omitempty"`
	Deadline      time.Time  `json:"deadline"`
}

type AgentActivitySummary struct {
	Queued        int        `json:"queued"`
	Running       int        `json:"running"`
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
	LastFailureAt *time.Time `json:"lastFailureAt,omitempty"`
}

// Resource labels exclude names, query parameters, bodies, and credentials.
func agentResource(uri string) string {
	path := strings.SplitN(uri, "?", 2)[0]
	parts := strings.Split(strings.Trim(path, "/"), "/")
	offset := 0
	if len(parts) >= 2 && parts[0] == "api" {
		offset = 2
	} else if len(parts) >= 3 && parts[0] == "apis" {
		offset = 3
	} else {
		return "discovery"
	}
	parts = parts[offset:]
	if len(parts) >= 3 && parts[0] == "namespaces" {
		parts = parts[2:]
	}
	if len(parts) == 0 {
		return "discovery"
	}
	return parts[0]
}

func agentResultClassification(result agentprotocol.Result) (string, string) {
	if result.Error != "" {
		switch result.ErrorCode {
		case "cluster.agent_scope_denied":
			return "failed", result.ErrorCode
		case "cluster.agent_unknown_outcome":
			return "unknown", result.ErrorCode
		default:
			return "failed", "cluster.agent_request_failed"
		}
	}
	if result.Status >= 400 {
		if result.Status == 401 || result.Status == 403 {
			return "failed", "kubernetes.access_denied"
		}
		return "failed", "kubernetes.request_failed"
	}
	return "succeeded", ""
}

func (s *Store) ListAgentActivity(ctx context.Context, clusterID, workspaceID string) ([]AgentActivity, AgentActivitySummary, error) {
	summary := AgentActivitySummary{}
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE state='queued' AND deadline>NOW()),COUNT(*) FILTER (WHERE state='running' AND deadline>NOW()),MAX(finished_at) FILTER (WHERE state='succeeded'),MAX(COALESCE(finished_at,deadline)) FILTER (WHERE state IN ('failed','unknown','expired') OR (state IN ('queued','running') AND deadline<=NOW())) FROM cluster_agent_activity WHERE cluster_id=$1 AND workspace_id=$2 AND created_at>NOW()-INTERVAL '7 days'`, clusterID, workspaceID).Scan(&summary.Queued, &summary.Running, &summary.LastSuccessAt, &summary.LastFailureAt)
	if err != nil {
		return nil, summary, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT h.id,h.profile,h.namespace,h.method,h.resource,h.operation_id,COALESCE(a.id,''),CASE WHEN h.state='queued' AND h.deadline<=NOW() THEN 'expired' WHEN h.state='running' AND h.deadline<=NOW() THEN 'unknown' ELSE h.state END,h.http_status,CASE WHEN h.state='queued' AND h.deadline<=NOW() THEN 'cluster.agent_task_expired' WHEN h.state='running' AND h.deadline<=NOW() THEN 'cluster.agent_unknown_outcome' ELSE h.error_code END,h.created_at,h.started_at,CASE WHEN h.state IN ('queued','running') AND h.deadline<=NOW() THEN h.deadline ELSE h.finished_at END,h.deadline FROM cluster_agent_activity h LEFT JOIN operations o ON o.id=h.operation_id LEFT JOIN applications a ON a.id=o.application_id AND a.workspace_id=h.workspace_id WHERE h.cluster_id=$1 AND h.workspace_id=$2 AND h.created_at>NOW()-INTERVAL '7 days' ORDER BY h.created_at DESC,h.id LIMIT 100`, clusterID, workspaceID)
	if err != nil {
		return nil, summary, err
	}
	defer rows.Close()
	items := make([]AgentActivity, 0)
	for rows.Next() {
		var item AgentActivity
		if err := rows.Scan(&item.ID, &item.Profile, &item.Namespace, &item.Method, &item.Resource, &item.OperationID, &item.ApplicationID, &item.State, &item.HTTPStatus, &item.ErrorCode, &item.CreatedAt, &item.StartedAt, &item.FinishedAt, &item.Deadline); err != nil {
			return nil, summary, err
		}
		items = append(items, item)
	}
	return items, summary, rows.Err()
}

func recordAgentTask(ctx context.Context, tx *sql.Tx, id, clusterID string, deadline time.Time, request agentprotocol.Request) error {
	operationID := ""
	if request.Execution != nil {
		operationID = request.Execution.OperationID
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO cluster_agent_activity(id,cluster_id,workspace_id,profile,namespace,method,resource,operation_id,state,deadline) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'queued',$9)`, id, clusterID, request.WorkspaceID, request.Profile, request.Namespace, request.Method, agentResource(request.URI), operationID, deadline)
	return err
}
