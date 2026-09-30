package store

import "context"

// ApplicationExists checks all workspaces: another workspace's owner must also block adoption.
func (s *Store) ApplicationExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM applications WHERE id=$1)`, id).Scan(&exists)
	return exists, err
}
