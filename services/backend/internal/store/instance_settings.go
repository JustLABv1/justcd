package store

import "context"

type InstanceSettings struct {
	MaintenanceEnabled bool `json:"maintenanceEnabled"`
}

func (s *Store) InstanceSettings(ctx context.Context) (InstanceSettings, error) {
	var settings InstanceSettings
	err := s.DB.QueryRowContext(ctx, `SELECT maintenance_enabled FROM instance_settings WHERE singleton=TRUE`).Scan(&settings.MaintenanceEnabled)
	return settings, err
}

func (s *Store) SetMaintenance(ctx context.Context, enabled bool, actorID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous bool
	if err := tx.QueryRowContext(ctx, `SELECT maintenance_enabled FROM instance_settings WHERE singleton=TRUE FOR UPDATE`).Scan(&previous); err != nil {
		return err
	}
	if previous == enabled {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE instance_settings SET maintenance_enabled=$1 WHERE singleton=TRUE`, enabled); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,'instance.maintenance_updated','instance','instance',jsonb_build_object('maintenanceEnabled',$2::boolean))`, actorID, enabled); err != nil {
		return err
	}
	return tx.Commit()
}
