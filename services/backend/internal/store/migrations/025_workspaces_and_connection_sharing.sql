ALTER TABLE projects RENAME TO workspaces;
ALTER TABLE project_memberships RENAME TO workspace_memberships;
ALTER TABLE project_cluster_credentials RENAME TO workspace_cluster_credentials;

DO $$
DECLARE
    target RECORD;
BEGIN
    FOR target IN
        SELECT table_name
        FROM information_schema.columns
        WHERE table_schema = current_schema() AND column_name = 'project_id'
    LOOP
        EXECUTE format('ALTER TABLE %I RENAME COLUMN project_id TO workspace_id', target.table_name);
    END LOOP;
END $$;

ALTER TABLE clusters
    ADD COLUMN workspace_id TEXT REFERENCES workspaces(id) ON DELETE CASCADE;
CREATE INDEX clusters_workspace_idx ON clusters(workspace_id);

CREATE TABLE workspace_cluster_shares (
    id TEXT PRIMARY KEY,
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    owner_workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    target_workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'declined', 'revoked')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    accepted_at TIMESTAMPTZ,
    CHECK (owner_workspace_id <> target_workspace_id)
);
CREATE INDEX workspace_cluster_shares_target_idx ON workspace_cluster_shares(target_workspace_id, status);
CREATE UNIQUE INDEX workspace_cluster_shares_active_unique_idx ON workspace_cluster_shares(cluster_id, target_workspace_id) WHERE status IN ('pending', 'accepted');

CREATE TABLE workspace_git_source_shares (
    id TEXT PRIMARY KEY,
    git_source_id TEXT NOT NULL REFERENCES git_sources(id) ON DELETE CASCADE,
    owner_workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    target_workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    credential_id TEXT REFERENCES credentials(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'declined', 'revoked')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    accepted_at TIMESTAMPTZ,
    CHECK (owner_workspace_id <> target_workspace_id)
);
CREATE INDEX workspace_git_source_shares_target_idx ON workspace_git_source_shares(target_workspace_id, status);
CREATE UNIQUE INDEX workspace_git_source_shares_active_unique_idx ON workspace_git_source_shares(git_source_id, target_workspace_id) WHERE status IN ('pending', 'accepted');
