package store

import (
	"context"
	"database/sql"
	"errors"
)

// BootstrapJustScan imports the legacy environment setting once. An explicit
// empty database value disables the integration and survives restarts.
func (s *Store) BootstrapJustScan(ctx context.Context, endpoint string) error {
	_, err := s.DB.ExecContext(ctx, `
		WITH initialized AS (
			UPDATE instance_settings SET justscan_url=$1 WHERE singleton AND justscan_url IS NULL
			RETURNING justscan_url
		)
		UPDATE justscan_connections SET server_url=initialized.justscan_url FROM initialized
		WHERE server_url='' AND initialized.justscan_url<>''`, endpoint)
	return err
}

func (s *Store) JustScanURL(ctx context.Context) (string, error) {
	var endpoint string
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(justscan_url,'') FROM instance_settings WHERE singleton`).Scan(&endpoint)
	return endpoint, err
}

func (s *Store) SetJustScanURL(ctx context.Context, endpoint, actorID string) error {
	_, err := s.DB.ExecContext(ctx, `
		WITH changed AS (
			UPDATE instance_settings SET justscan_url=$1
			WHERE singleton AND justscan_url IS DISTINCT FROM $1 RETURNING justscan_url
		)
		INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details)
		SELECT $2,'instance.justscan_updated','instance','instance',jsonb_build_object('serverUrl',justscan_url) FROM changed`, endpoint, actorID)
	return err
}

var ErrJustScanServerChanged = errors.New("JustScan server changed; reload settings")

type JustScanConnection struct {
	ServerURL      string `json:"-"`
	OrganizationID string `json:"organizationId"`
	TokenCipher    []byte `json:"-"`
}

func (s *Store) JustScanConnection(ctx context.Context, workspaceID string) (JustScanConnection, error) {
	var result JustScanConnection
	err := s.DB.QueryRowContext(ctx, `SELECT organization_id, token_cipher, server_url FROM justscan_connections WHERE workspace_id=$1`, workspaceID).
		Scan(&result.OrganizationID, &result.TokenCipher, &result.ServerURL)
	return result, err
}

func (s *Store) SaveJustScanConnection(ctx context.Context, workspaceID string, connection JustScanConnection) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var endpoint string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(justscan_url,'') FROM instance_settings WHERE singleton FOR SHARE`).Scan(&endpoint); err != nil {
		return err
	}
	if endpoint == "" || endpoint != connection.ServerURL {
		return ErrJustScanServerChanged
	}
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, workspaceID).Scan(&id); err != nil {
		return err
	}
	// Changing the server or organization invalidates previous results and queued scans.
	if _, err := tx.ExecContext(ctx, `DELETE FROM justscan_connections WHERE workspace_id=$1 AND (organization_id<>$2 OR server_url<>$3)`, workspaceID, connection.OrganizationID, connection.ServerURL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO justscan_connections(workspace_id,organization_id,token_cipher,server_url) VALUES($1,$2,$3,$4)
		ON CONFLICT(workspace_id) DO UPDATE SET token_cipher=EXCLUDED.token_cipher`, workspaceID, connection.OrganizationID, connection.TokenCipher, connection.ServerURL); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteJustScanConnection(ctx context.Context, workspaceID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM justscan_connections WHERE workspace_id=$1`, workspaceID)
	return err
}

type JustScanPlanScan struct {
	Image         string `json:"image"`
	ScanID        string `json:"scanId,omitempty"`
	ReportURL     string `json:"reportUrl,omitempty"`
	Status        string `json:"status"`
	CriticalCount int    `json:"criticalCount"`
	HighCount     int    `json:"highCount"`
}

func (s *Store) JustScanPlanScans(ctx context.Context, planID string) ([]JustScanPlanScan, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT image,scan_id,report_url,status,critical_count,high_count
		FROM justscan_plan_scans WHERE plan_id=$1 ORDER BY image`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []JustScanPlanScan{}
	for rows.Next() {
		var scan JustScanPlanScan
		if err := rows.Scan(&scan.Image, &scan.ScanID, &scan.ReportURL, &scan.Status, &scan.CriticalCount, &scan.HighCount); err != nil {
			return nil, err
		}
		result = append(result, scan)
	}
	return result, rows.Err()
}

func (s *Store) QueueJustScanPlanScans(ctx context.Context, workspaceID, planID string, images []string) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO justscan_plan_scans(plan_id,workspace_id,image)
		SELECT p.id,c.workspace_id,image FROM plans p
		JOIN applications a ON a.id=p.application_id
		JOIN justscan_connections c ON c.workspace_id=a.workspace_id
		JOIN instance_settings i ON i.singleton AND c.server_url=i.justscan_url AND c.server_url<>''
		CROSS JOIN unnest($3::text[]) AS targets(image)
		WHERE p.id=$1 AND c.workspace_id=$2 AND p.status='current' AND p.expires_at>NOW()
		ON CONFLICT(plan_id,image) DO UPDATE SET status='queued', scan_id='', report_url='',
			critical_count=0, high_count=0, created_at=NOW(), next_at=NOW(), attempt_id=''
		WHERE justscan_plan_scans.status='error'`, planID, workspaceID, images)
	return err
}

type JustScanWork struct {
	JustScanPlanScan
	JustScanConnection
	PlanID      string
	WorkspaceID string
	AttemptID   string
}

func (s *Store) ClaimJustScan(ctx context.Context) (JustScanWork, bool, error) {
	var work JustScanWork
	// An expired submission might already have reached JustScan.
	if _, err := s.DB.ExecContext(ctx, `UPDATE justscan_plan_scans SET status='error', attempt_id=''
		WHERE status IN ('submitting','pending') AND next_at<=NOW()
		AND (status='submitting' OR created_at<NOW()-INTERVAL '2 hours')`); err != nil {
		return work, false, err
	}
	work.AttemptID = NewID()
	err := s.DB.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT j.plan_id,j.image FROM justscan_plan_scans j
			JOIN justscan_connections active ON active.workspace_id=j.workspace_id
			JOIN instance_settings i ON i.singleton AND active.server_url=i.justscan_url AND active.server_url<>''
			WHERE j.status IN ('queued','pending') AND j.next_at<=NOW()
			ORDER BY j.next_at LIMIT 1 FOR UPDATE OF j SKIP LOCKED
		)
		UPDATE justscan_plan_scans j SET
			status=CASE WHEN j.status='queued' THEN 'submitting' ELSE j.status END,
			next_at=NOW()+INTERVAL '1 minute', attempt_id=$1
		FROM candidate, justscan_connections c
		WHERE j.plan_id=candidate.plan_id AND j.image=candidate.image AND c.workspace_id=j.workspace_id
		RETURNING j.plan_id,j.workspace_id,j.image,j.scan_id,j.report_url,j.status,c.organization_id,c.token_cipher,c.server_url`, work.AttemptID).
		Scan(&work.PlanID, &work.WorkspaceID, &work.Image, &work.ScanID, &work.ReportURL, &work.Status, &work.OrganizationID, &work.TokenCipher, &work.ServerURL)
	if errors.Is(err, sql.ErrNoRows) {
		return work, false, nil
	}
	return work, err == nil, err
}

func (s *Store) FinishJustScan(ctx context.Context, work JustScanWork) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE justscan_plan_scans SET
		scan_id=$4,report_url=$5,status=$6,critical_count=$7,high_count=$8,next_at=NOW()+INTERVAL '15 seconds',attempt_id=''
		WHERE plan_id=$1 AND image=$2 AND attempt_id=$3`,
		work.PlanID, work.Image, work.AttemptID, work.ScanID, work.ReportURL, work.Status, work.CriticalCount, work.HighCount)
	return err
}
