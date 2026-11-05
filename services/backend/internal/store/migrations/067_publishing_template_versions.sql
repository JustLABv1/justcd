-- Immutable, workspace-owned versions of application template files. A profile
-- pins a version through its settings; version 0 means the built-in template.
CREATE TABLE publishing_template_versions (
 workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 template_id text NOT NULL,
 version integer NOT NULL,
 label text NOT NULL DEFAULT '',
 files jsonb NOT NULL,
 created_by text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,template_id,version),
 CHECK(version > 0)
);
