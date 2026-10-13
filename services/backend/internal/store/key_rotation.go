package store

import (
	"context"
	"fmt"
	"github.com/justlab/justcd/services/backend/internal/security"
)

// RotateEncryptionKey is an offline, all-or-nothing operation. Stop every writer first.
func (s *Store) RotateEncryptionKey(ctx context.Context, oldKey, newKey []byte) (int, error) {
	if len(oldKey) != 32 || len(newKey) != 32 {
		return 0, fmt.Errorf("both keys must contain 32 bytes")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(57432896)`); err != nil {
		return 0, err
	}
	targets := []struct{ table, column, id, purpose string }{
		{"credentials", "secret_cipher", "id", "'credential:'||id"},
		{"oidc_providers", "client_secret_cipher", "id", "'oidc-provider:'||id"},
		{"source_control_connections", "webhook_secret_cipher", "id", "'source-control-webhook:'||id"},
		{"source_control_connections", "status_token_cipher", "id", "'source-control-status:'||id"},
		{"git_push_webhooks", "secret_cipher", "source_id", "'git-push-webhook:'||source_id"},
		{"rollback_snapshots", "payload_cipher", "id", "'rollback-snapshot:'||application_id||':'||id"},
		{"cluster_agent_tasks", "request_cipher", "id", "'agent-task:'||id||':request'"},
		{"cluster_agent_tasks", "response_cipher", "id", "'agent-task:'||id||':response'"},
		{"user_mfa", "secret_cipher", "user_id", "'mfa:'||user_id"},
		{"notification_destinations", "config_cipher", "id", "'notification:'||id"},
	}
	count := 0
	for _, target := range targets {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name=$2)`, target.table, target.column).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			continue
		}
		if _, err = tx.ExecContext(ctx, `LOCK TABLE `+target.table+` IN ACCESS EXCLUSIVE MODE`); err != nil {
			return 0, err
		}
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT %s,%s,%s FROM %s WHERE %s IS NOT NULL AND octet_length(%s)>0`, target.id, target.column, target.purpose, target.table, target.column, target.column))
		if err != nil {
			return 0, err
		}
		type value struct {
			id     string
			cipher []byte
		}
		values := []value{}
		for rows.Next() {
			var id, purpose string
			var cipher []byte
			if err = rows.Scan(&id, &cipher, &purpose); err != nil {
				rows.Close()
				return 0, err
			}
			plain, err := security.Decrypt(oldKey, cipher, purpose)
			if err != nil {
				rows.Close()
				return 0, fmt.Errorf("rotation aborted: cannot decrypt %s.%s", target.table, target.column)
			}
			rotated, err := security.Encrypt(newKey, plain, purpose)
			if err != nil {
				rows.Close()
				return 0, err
			}
			values = append(values, value{id, rotated})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
		for _, v := range values {
			if _, err = tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET %s=$2 WHERE %s=$1`, target.table, target.column, target.id), v.id, v.cipher); err != nil {
				return 0, err
			}
			count++
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(action,resource_type,resource_id,details) VALUES('security.encryption_key_rotated','instance','instance',$1)`, fmt.Sprintf(`{"valuesRotated":%d}`, count)); err != nil {
		return 0, err
	}
	return count, tx.Commit()
}
