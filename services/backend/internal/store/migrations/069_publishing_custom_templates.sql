-- Workspace-owned application templates. Built-in templates stay in code; a
-- custom template carries its own kind, port and upload rules, and its files
-- live in publishing_template_versions (version 0, the built-in files, does
-- not exist for these).
CREATE TABLE publishing_templates (
 workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 id text NOT NULL,
 name text NOT NULL,
 description text NOT NULL DEFAULT '',
 category text NOT NULL DEFAULT 'Custom',
 detail text NOT NULL DEFAULT '',
 runtime text NOT NULL,
 entry_point text NOT NULL,
 port integer NOT NULL DEFAULT 0,
 layout jsonb NOT NULL DEFAULT '[]',
 base_template text NOT NULL DEFAULT '',
 created_by text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,id),
 CHECK(runtime IN ('shiny','streamlit','static','python-static','server')),
 CHECK(port BETWEEN 0 AND 65535)
);
