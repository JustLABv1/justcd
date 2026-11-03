CREATE TABLE published_applications (
 application_id text PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
 workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 fingerprint text NOT NULL,
 config jsonb NOT NULL,
 gitlab_url text NOT NULL,
 group_path text NOT NULL,
 repository_url text NOT NULL,
 project_url text NOT NULL,
 gitlab_project_id integer NOT NULL,
 pipeline_url text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id, repository_url)
);
