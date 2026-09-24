ALTER TABLE plans
    ADD COLUMN ignored_changes JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN selection JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN ignore_rules_digest TEXT NOT NULL DEFAULT '';

ALTER TABLE operations
    ADD COLUMN approval_id TEXT REFERENCES deletion_approvals(id) ON DELETE SET NULL;

CREATE TABLE application_ignore_rules (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    api_version TEXT NOT NULL,
    kind TEXT NOT NULL,
    namespace TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    path TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL,
    created_by TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (application_id, cluster_id, api_version, kind, namespace, name, path)
);
CREATE INDEX application_ignore_rules_app_idx ON application_ignore_rules (application_id, created_at DESC);
