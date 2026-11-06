-- GitLab Environments reporting. Repository settings opt a git-managed
-- repository into recording a GitLab deployment after each sync; the
-- optional per-application row overrides the derived environment name
-- (cluster/namespace), tier and URL declared in justcd.yaml.
CREATE TABLE repository_environment_settings (
 repository_id text PRIMARY KEY REFERENCES repository_configurations(id) ON DELETE CASCADE,
 enabled boolean NOT NULL DEFAULT false,
 credential_id text NOT NULL DEFAULT '',
 api_url text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE application_environments (
 application_id text PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
 name text NOT NULL DEFAULT '',
 tier text NOT NULL DEFAULT '',
 external_url text NOT NULL DEFAULT ''
);

-- Latest reported deployment per application, for status display and retries.
CREATE TABLE environment_deployments (
 operation_id text PRIMARY KEY,
 application_id text NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
 environment text NOT NULL,
 gitlab_deployment_id bigint NOT NULL DEFAULT 0,
 status text NOT NULL,
 sha text NOT NULL DEFAULT '',
 error text NOT NULL DEFAULT '',
 reported_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX environment_deployments_application ON environment_deployments(application_id, reported_at DESC);
