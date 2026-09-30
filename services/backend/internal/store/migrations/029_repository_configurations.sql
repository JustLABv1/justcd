CREATE TABLE repository_configurations (
 id TEXT PRIMARY KEY,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL REFERENCES git_sources(id),
 revision TEXT NOT NULL,
 enabled BOOLEAN NOT NULL DEFAULT TRUE,
 last_checked_at TIMESTAMPTZ,
 last_commit TEXT NOT NULL DEFAULT '',
 last_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(workspace_id, source_id, revision)
);
ALTER TABLE applications ADD COLUMN repository_configuration_id TEXT REFERENCES repository_configurations(id);
ALTER TABLE applications ADD COLUMN configuration_path TEXT NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN configuration_commit TEXT NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN configuration_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN configuration_missing BOOLEAN NOT NULL DEFAULT FALSE;
CREATE UNIQUE INDEX applications_repository_identity ON applications(repository_configuration_id, name) WHERE repository_configuration_id IS NOT NULL;
