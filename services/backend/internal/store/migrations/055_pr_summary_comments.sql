ALTER TABLE source_control_connections ADD COLUMN comment_mode TEXT NOT NULL DEFAULT '' CHECK (comment_mode IN ('', 'summary', 'action-required', 'off'));
ALTER TABLE source_control_connections ADD COLUMN comment_on_drafts BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE pull_request_reviews ADD COLUMN draft BOOLEAN NOT NULL DEFAULT FALSE;
-- One summary per workspace/repository/PR, shared by every application and discovery.
CREATE TABLE pull_request_comments (
    key TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    comment_id BIGINT NOT NULL DEFAULT 0,
    sections JSONB NOT NULL DEFAULT '{}',
    reported_body TEXT NOT NULL DEFAULT ''
);
