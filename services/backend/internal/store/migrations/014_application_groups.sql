CREATE TABLE application_groups (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    source_id TEXT NOT NULL REFERENCES git_sources(id) ON DELETE RESTRICT,
    revision TEXT NOT NULL,
    manifest_path TEXT NOT NULL,
    helm_values_files JSONB NOT NULL DEFAULT '[]'::jsonb,
    helm_values_yaml TEXT NOT NULL DEFAULT '',
    sync_policy TEXT NOT NULL DEFAULT 'manual' CHECK (sync_policy IN ('manual', 'auto-safe')),
    poll_seconds INTEGER NOT NULL DEFAULT 300 CHECK (poll_seconds BETWEEN 30 AND 86400),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name)
);

ALTER TABLE applications
    ADD COLUMN application_group_id TEXT REFERENCES application_groups(id) ON DELETE SET NULL,
    ADD COLUMN target_helm_values_files JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN target_helm_values_yaml TEXT NOT NULL DEFAULT '';

CREATE INDEX applications_group_idx ON applications (application_group_id, name);
