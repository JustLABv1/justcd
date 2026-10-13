package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/justlab/justcd/services/backend/internal/notifications"
	"time"
)

type NotificationDestination struct {
	ID              string   `json:"id"`
	WorkspaceID     string   `json:"workspaceId"`
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	Enabled         bool     `json:"enabled"`
	Events          []string `json:"events"`
	MinimumSeverity string   `json:"minimumSeverity"`
	Cipher          []byte   `json:"-"`
}

func (s *Store) NotificationDestinations(ctx context.Context, workspace string) ([]NotificationDestination, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,workspace_id,name,provider,enabled,events,minimum_severity FROM notification_destinations WHERE workspace_id=$1 ORDER BY created_at`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationDestination{}
	for rows.Next() {
		var d NotificationDestination
		var raw []byte
		if err = rows.Scan(&d.ID, &d.WorkspaceID, &d.Name, &d.Provider, &d.Enabled, &raw, &d.MinimumSeverity); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &d.Events); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) SaveNotificationDestination(ctx context.Context, d NotificationDestination, actor string) error {
	raw, err := json.Marshal(d.Events)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO notification_destinations(id,workspace_id,name,provider,enabled,events,minimum_severity,config_cipher) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET name=$3,provider=$4,enabled=$5,events=$6,minimum_severity=$7,config_cipher=CASE WHEN octet_length($8)>0 THEN $8 ELSE notification_destinations.config_cipher END WHERE notification_destinations.workspace_id=$2 AND (notification_destinations.provider=$4 OR octet_length($8)>0)`, d.ID, d.WorkspaceID, d.Name, d.Provider, d.Enabled, raw, d.MinimumSeverity, d.Cipher)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	if !d.Enabled {
		if _, err = tx.ExecContext(ctx, `UPDATE notification_deliveries SET status='disabled',result_code='destination_disabled' WHERE destination_id=$1 AND status='queued'`, d.ID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'notification.destination_saved','workspace',$2,$3)`, actor, d.WorkspaceID, fmt.Sprintf(`{"destinationId":%q,"enabled":%t}`, d.ID, d.Enabled)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) DeleteNotificationDestination(ctx context.Context, workspace, id, actor string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM notification_destinations WHERE id=$1 AND workspace_id=$2`, id, workspace)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'notification.destination_deleted','workspace',$2,$3)`, actor, workspace, fmt.Sprintf(`{"destinationId":%q}`, id)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) TestNotificationDestination(ctx context.Context, workspace, id, actor string) (string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var enabled bool
	if err = tx.QueryRowContext(ctx, `SELECT enabled FROM notification_destinations WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, id, workspace).Scan(&enabled); err != nil {
		return "", err
	}
	if !enabled {
		return "", fmt.Errorf("enable the destination before testing")
	}
	eventID := NewID()
	if _, err = tx.ExecContext(ctx, `INSERT INTO notification_events(id,workspace_id,event_type,severity) VALUES($1,$2,'test','info')`, eventID, workspace); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO notification_deliveries(id,destination_id,event_id) VALUES($1,$2,$3) ON CONFLICT(destination_id,event_id) DO NOTHING`, NewID(), id, eventID); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'notification.test_requested','workspace',$2,$3)`, actor, workspace, fmt.Sprintf(`{"destinationId":%q}`, id)); err != nil {
		return "", err
	}
	return eventID, tx.Commit()
}

type NotificationActivity struct {
	ID            string          `json:"id"`
	DestinationID string          `json:"destinationId"`
	EventType     string          `json:"eventType"`
	EventID       string          `json:"eventId"`
	Status        string          `json:"status"`
	Attempts      int             `json:"attempts"`
	StatusCode    int             `json:"statusCode"`
	ResultCode    string          `json:"resultCode"`
	NextAt        time.Time       `json:"nextAt"`
	OccurredAt    time.Time       `json:"occurredAt"`
	History       json.RawMessage `json:"history"`
}

func (s *Store) NotificationActivity(ctx context.Context, workspace string) ([]NotificationActivity, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT d.id,d.destination_id,e.event_type,e.id,d.status,d.attempts,d.status_code,d.result_code,d.next_at,e.occurred_at, COALESCE((SELECT jsonb_agg(jsonb_build_object('attempt',a.attempt,'statusCode',a.status_code,'resultCode',a.result_code,'createdAt',a.created_at) ORDER BY a.id) FROM notification_attempts a WHERE a.delivery_id=d.id),'[]'::jsonb) FROM notification_deliveries d JOIN notification_events e ON e.id=d.event_id WHERE e.workspace_id=$1 ORDER BY e.occurred_at DESC LIMIT 100`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationActivity{}
	for rows.Next() {
		var a NotificationActivity
		if err = rows.Scan(&a.ID, &a.DestinationID, &a.EventType, &a.EventID, &a.Status, &a.Attempts, &a.StatusCode, &a.ResultCode, &a.NextAt, &a.OccurredAt, &a.History); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Row locks serialize replicas. Crash recovery may resend with the same idempotency key.
func (s *Store) DeliverNotification(ctx context.Context, send func(NotificationDestination, notifications.Event, string) notifications.Result) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var d NotificationDestination
	var e notifications.Event
	var id string
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT q.id,q.attempts,d.id,d.provider,d.config_cipher,(d.enabled AND (e.event_type='test' OR notification_routes_to(d.id,e.id))),e.id,e.workspace_id,e.application_id,e.resource_id,e.event_type,e.severity,e.occurred_at FROM notification_deliveries q JOIN notification_destinations d ON d.id=q.destination_id JOIN notification_events e ON e.id=q.event_id WHERE q.status='queued' AND q.next_at<=NOW() ORDER BY q.next_at LIMIT 1 FOR UPDATE OF q FOR SHARE OF d SKIP LOCKED`).Scan(&id, &attempts, &d.ID, &d.Provider, &d.Cipher, &d.Enabled, &e.ID, &e.WorkspaceID, &e.ApplicationID, &e.ResourceID, &e.Type, &e.Severity, &e.OccurredAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	e.SchemaVersion = 1
	e.OccurredAt = e.OccurredAt.UTC()
	result := notifications.Result{Code: "routing_disabled"}
	status := "disabled"
	if d.Enabled {
		result = send(d, e, id)
		attempts++
		status = "failed"
		if result.Code == "delivered" {
			status = "delivered"
		} else if result.Retry && attempts < 5 {
			status = "queued"
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO notification_attempts(delivery_id,attempt,status_code,result_code) VALUES($1,$2,$3,$4)`, id, attempts, result.StatusCode, result.Code); err != nil {
			return false, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE notification_deliveries SET status=$2,attempts=$3,status_code=$4,result_code=$5,next_at=$6 WHERE id=$1`, id, status, attempts, result.StatusCode, result.Code, time.Now().Add(notifications.Backoff(attempts))); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(action,resource_type,resource_id,details) VALUES('notification.delivery_attempt','workspace',$1,$2)`, e.WorkspaceID, fmt.Sprintf(`{"deliveryId":%q,"attempt":%d,"status":%q,"resultCode":%q}`, id, attempts, status, result.Code)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
