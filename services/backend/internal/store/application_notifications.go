package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/justlab/justcd/services/backend/internal/notifications"
)

type ApplicationNotificationPolicy struct {
	Mode            string   `json:"mode"`
	DestinationIDs  []string `json:"destinationIds"`
	Events          []string `json:"events"`
	MinimumSeverity string   `json:"minimumSeverity"`
}

func (p ApplicationNotificationPolicy) Validate() error {
	if p.Mode != "inherit" && p.Mode != "custom" && p.Mode != "disabled" {
		return errors.New("mode must be inherit, custom or disabled")
	}
	if p.Mode == "custom" {
		if len(p.DestinationIDs) == 0 || len(p.DestinationIDs) > 100 || len(p.Events) == 0 || len(p.Events) > 6 || notifications.Severity(p.MinimumSeverity) == 0 {
			return errors.New("custom routing requires destinations, events and a minimum severity")
		}
		seen := map[string]bool{}
		for _, id := range p.DestinationIDs {
			if id == "" || seen[id] {
				return errors.New("destination IDs must be unique")
			}
			seen[id] = true
		}
		for _, e := range p.Events {
			valid := false
			for _, event := range notifications.EventTypes[:6] {
				if event == e {
					valid = true
				}
			}
			if !valid {
				return errors.New("unsupported notification event")
			}
		}
	}
	return nil
}
func (s *Store) ApplicationNotificationPolicy(ctx context.Context, app string) (ApplicationNotificationPolicy, error) {
	p := ApplicationNotificationPolicy{Mode: "inherit", DestinationIDs: []string{}, Events: []string{}, MinimumSeverity: "info"}
	var dest, events []byte
	err := s.DB.QueryRowContext(ctx, `SELECT mode,destination_ids,events,minimum_severity FROM application_notification_policies WHERE application_id=$1`, app).Scan(&p.Mode, &dest, &events, &p.MinimumSeverity)
	if err == sql.ErrNoRows {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(dest, &p.DestinationIDs); err != nil {
		return p, err
	}
	err = json.Unmarshal(events, &p.Events)
	return p, err
}
func (s *Store) SaveApplicationNotificationPolicy(ctx context.Context, app, workspace, actor string, p ApplicationNotificationPolicy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Mode != "custom" {
		p.DestinationIDs = []string{}
		p.Events = []string{}
		p.MinimumSeverity = "info"
	}
	dest, _ := json.Marshal(p.DestinationIDs)
	events, _ := json.Marshal(p.Events)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var scope string
	if err = tx.QueryRowContext(ctx, `SELECT workspace_id FROM applications WHERE id=$1 FOR UPDATE`, app).Scan(&scope); err != nil {
		return err
	}
	if scope != workspace {
		return errors.New("application workspace mismatch")
	}
	// Lock selected destinations so concurrent deletion cannot leave a new route dangling.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM notification_destinations WHERE workspace_id=$1 AND $2::jsonb ? id ORDER BY id FOR SHARE`, workspace, dest)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count != len(p.DestinationIDs) {
		return errors.New("select destinations belonging to this workspace")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO application_notification_policies(application_id,mode,destination_ids,events,minimum_severity) VALUES($1,$2,$3,$4,$5) ON CONFLICT(application_id) DO UPDATE SET mode=$2,destination_ids=$3,events=$4,minimum_severity=$5`, app, p.Mode, dest, events, p.MinimumSeverity); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE notification_deliveries q SET status='disabled',result_code='application_routing_changed' FROM notification_events e WHERE q.event_id=e.id AND e.application_id=$1 AND q.status='queued' AND e.event_type<>'test' AND NOT notification_routes_to(q.destination_id,e.id)`, app); err != nil {
		return err
	}
	raw, _ := json.Marshal(p)
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'notification.application_policy_saved','application',$2,$3)`, actor, app, raw); err != nil {
		return err
	}
	return tx.Commit()
}
