ALTER TABLE sessions ADD COLUMN id TEXT NOT NULL DEFAULT md5(random()::text || clock_timestamp()::text);
CREATE UNIQUE INDEX sessions_public_id ON sessions(id);
CREATE TABLE security_rate_limits (bucket TEXT PRIMARY KEY, window_at TIMESTAMPTZ NOT NULL, attempts INTEGER NOT NULL);
CREATE TABLE user_mfa (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 secret_cipher BYTEA NOT NULL, enabled BOOLEAN NOT NULL DEFAULT FALSE,
 enrollment_expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW()+INTERVAL '10 minutes',
 last_step BIGINT NOT NULL DEFAULT -1
);
CREATE TABLE mfa_recovery_codes (
 user_id TEXT NOT NULL REFERENCES user_mfa(user_id) ON DELETE CASCADE, code_hash BYTEA NOT NULL,
 PRIMARY KEY(user_id,code_hash)
);
