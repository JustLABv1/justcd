ALTER TABLE applications
    ADD COLUMN auto_sync_paused BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN rollback_resume_state JSONB,
    ADD COLUMN rollback_resume_requires_revision BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE plans
    ADD COLUMN rollback_target JSONB;

ALTER TABLE operations
    ADD COLUMN operation_type TEXT NOT NULL DEFAULT 'sync' CHECK (operation_type IN ('sync', 'rollback')),
    ADD COLUMN rollback_checkpoint_id TEXT;

CREATE TABLE rollback_snapshots (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    operation_id TEXT REFERENCES operations(id) ON DELETE SET NULL,
    kind TEXT NOT NULL CHECK (kind IN ('successful_sync', 'pre_operation')),
    revision TEXT NOT NULL DEFAULT '',
    resource_count INTEGER NOT NULL DEFAULT 0 CHECK (resource_count >= 0),
    payload_cipher BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX rollback_snapshots_app_idx ON rollback_snapshots (application_id, created_at DESC);

ALTER TABLE operations
    ADD CONSTRAINT operations_rollback_checkpoint_fk
    FOREIGN KEY (rollback_checkpoint_id) REFERENCES rollback_snapshots(id) ON DELETE SET NULL;

CREATE INDEX applications_poll_paused_idx ON applications (sync_policy, auto_sync_paused, last_checked_at);
