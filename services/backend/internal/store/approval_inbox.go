package store

import "context"

// ApprovalCandidate keeps the inbox query small; the API evaluates the snapshotted
// plan policy and currently eligible approvals before exposing each row.
type ApprovalCandidate struct {
	PlanID          string
	ApplicationID   string
	ApplicationName string
	ProjectID       string
	ProjectName     string
	GroupID         *string
}

func (s *Store) ListApprovalCandidates(ctx context.Context, user User, projectID, applicationID string) ([]ApprovalCandidate, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id,a.id,a.name,pr.id,pr.name,a.application_group_id
		FROM plans p JOIN applications a ON a.id=p.application_id JOIN projects pr ON pr.id=a.project_id
		WHERE p.status='current' AND p.expires_at>NOW()
		AND ($3='' OR pr.id=$3) AND ($4='' OR a.id=$4)
		AND ($2 OR EXISTS (SELECT 1 FROM project_memberships m WHERE m.project_id=pr.id AND m.user_id=$1)
		    OR EXISTS (SELECT 1 FROM oidc_membership_grants g WHERE g.project_id=pr.id AND g.user_id=$1))
		ORDER BY p.created_at DESC,p.id DESC`, user.ID, user.IsAdmin, projectID, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ApprovalCandidate, 0)
	for rows.Next() {
		var item ApprovalCandidate
		if err := rows.Scan(&item.PlanID, &item.ApplicationID, &item.ApplicationName, &item.ProjectID, &item.ProjectName, &item.GroupID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
