CREATE TABLE deployment_policies (
    workspace_id TEXT PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    policy JSONB NOT NULL DEFAULT '{"windows":[],"freezes":[]}'
);
CREATE TABLE application_deployment_policies (
    application_id TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    policy JSONB NOT NULL DEFAULT '{"windows":[],"freezes":[]}'
);
ALTER TABLE operations ADD COLUMN schedule_check_at TIMESTAMPTZ;
ALTER TABLE operations ADD COLUMN schedule_override_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE operations ADD COLUMN waiting_reasons JSONB NOT NULL DEFAULT '[]';
ALTER TABLE operations ADD COLUMN next_allowed_at TIMESTAMPTZ;
CREATE INDEX operations_schedule_queue_idx ON operations(schedule_check_at,started_at) WHERE status='queued';
ALTER TABLE operations ADD COLUMN schedule_override_actor_id TEXT REFERENCES users(id);
ALTER TABLE operations ADD COLUMN schedule_deferred BOOLEAN NOT NULL DEFAULT FALSE;
