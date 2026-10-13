package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"github.com/justlab/justcd/services/backend/internal/security"
	"time"
)

type ActiveSession struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Current   bool      `json:"current"`
}

func (s *Store) ActiveSessions(ctx context.Context, user string, current []byte) ([]ActiveSession, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,created_at,expires_at,token_hash=$2 FROM sessions WHERE user_id=$1 AND expires_at>NOW() ORDER BY created_at DESC`, user, current)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActiveSession{}
	for rows.Next() {
		var a ActiveSession
		if err = rows.Scan(&a.ID, &a.CreatedAt, &a.ExpiresAt, &a.Current); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) RevokeSession(ctx context.Context, user, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1 AND id=$2`, user, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id) VALUES($1,'session.revoked','session',$2)`, user, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) AllowSecurityAttempt(ctx context.Context, bucket string, limit int, window time.Duration) (bool, error) {
	// Only a digest is stored, including for nonexistent accounts. UPSERT serializes replicas.
	digest := hex.EncodeToString(security.HashToken(bucket))
	var attempts int
	err := s.DB.QueryRowContext(ctx, `INSERT INTO security_rate_limits(bucket,window_at,attempts) VALUES($1,NOW(),1) ON CONFLICT(bucket) DO UPDATE SET attempts=CASE WHEN security_rate_limits.window_at<NOW()-($2*INTERVAL '1 second') THEN 1 ELSE LEAST(security_rate_limits.attempts+1,1000000) END,window_at=CASE WHEN security_rate_limits.window_at<NOW()-($2*INTERVAL '1 second') THEN NOW() ELSE security_rate_limits.window_at END RETURNING attempts`, digest, int(window.Seconds())).Scan(&attempts)
	return attempts <= limit, err
}
func (s *Store) PruneSecurityLimits(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM security_rate_limits WHERE window_at<NOW()-INTERVAL '1 day'`)
	return err
}

type MFAStatus struct {
	Enabled                bool `json:"enabled"`
	LocalAccount           bool `json:"localAccount"`
	RecoveryCodesRemaining int  `json:"recoveryCodesRemaining"`
}

func (s *Store) MFAStatus(ctx context.Context, user string) (MFAStatus, error) {
	var m MFAStatus
	err := s.DB.QueryRowContext(ctx, `SELECT password_hash IS NOT NULL,COALESCE((SELECT enabled FROM user_mfa WHERE user_id=u.id),FALSE),(SELECT count(*) FROM mfa_recovery_codes WHERE user_id=u.id) FROM users u WHERE id=$1 AND NOT disabled AND deleted_at IS NULL`, user).Scan(&m.LocalAccount, &m.Enabled, &m.RecoveryCodesRemaining)
	return m, err
}
func (s *Store) BeginMFA(ctx context.Context, user string, cipher []byte) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO user_mfa(user_id,secret_cipher) SELECT id,$2 FROM users WHERE id=$1 AND password_hash IS NOT NULL AND NOT disabled ON CONFLICT(user_id) DO UPDATE SET secret_cipher=$2,enrollment_expires_at=NOW()+INTERVAL '10 minutes',last_step=-1 WHERE NOT user_mfa.enabled`, user, cipher)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return fmt.Errorf("MFA already enabled or account is not local")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id) VALUES($1,'mfa.enrollment_started','user',$1)`, user); err != nil {
		return err
	}
	return tx.Commit()
}

// MFA verification locks the user row and consumes a TOTP step or recovery code atomically.
// mode is challenge, enroll or remove. Recovery codes are only valid for an enabled factor.
func (s *Store) VerifyMFA(ctx context.Context, user, code, mode string, key []byte, hashes [][]byte) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var cipher []byte
	var enabled bool
	var expires time.Time
	var last int64
	err = tx.QueryRowContext(ctx, `SELECT secret_cipher,enabled,enrollment_expires_at,last_step FROM user_mfa WHERE user_id=$1 FOR UPDATE`, user).Scan(&cipher, &enabled, &expires, &last)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	valid := false
	recovery := false
	var step int64
	if (mode == "enroll" && !enabled && time.Now().Before(expires)) || (mode != "enroll" && enabled) {
		plain, err := security.Decrypt(key, cipher, "mfa:"+user)
		if err != nil {
			return false, err
		}
		step, valid = security.VerifyTOTP(string(plain), code, time.Now(), last)
		if !valid && enabled {
			result, err := tx.ExecContext(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id=$1 AND code_hash=$2`, user, security.HashToken(security.NormalizeRecovery(code)))
			if err != nil {
				return false, err
			}
			n, _ := result.RowsAffected()
			valid = n == 1
			recovery = valid
		}
	}
	action := "mfa.challenge_failed"
	if valid {
		action = "mfa.challenge_succeeded"
		if recovery {
			action = "mfa.recovery_used"
		} else {
			if _, err = tx.ExecContext(ctx, `UPDATE user_mfa SET last_step=$2 WHERE user_id=$1`, user, step); err != nil {
				return false, err
			}
		}
		if mode == "enroll" {
			action = "mfa.enrolled"
			if _, err = tx.ExecContext(ctx, `UPDATE user_mfa SET enabled=TRUE WHERE user_id=$1`, user); err != nil {
				return false, err
			}
			for _, hash := range hashes {
				if _, err = tx.ExecContext(ctx, `INSERT INTO mfa_recovery_codes(user_id,code_hash) VALUES($1,$2)`, user, hash); err != nil {
					return false, err
				}
			}
		}
		if mode == "remove" {
			action = "mfa.removed"
			if _, err = tx.ExecContext(ctx, `DELETE FROM user_mfa WHERE user_id=$1`, user); err != nil {
				return false, err
			}
		}
		if mode == "enroll" || mode == "remove" {
			if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1`, user); err != nil {
				return false, err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,details) VALUES($1,$2,'user',$1,$3)`, user, action, fmt.Sprintf(`{"mode":%q,"recovery":%t}`, mode, recovery)); err != nil {
		return false, err
	}
	return valid, tx.Commit()
}
