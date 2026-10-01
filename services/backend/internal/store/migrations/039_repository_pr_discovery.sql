ALTER TABLE repository_configurations ADD COLUMN pr_settings jsonb NOT NULL DEFAULT '{"enabled":false}'::jsonb;
ALTER TABLE repository_configurations ADD COLUMN pr_error text NOT NULL DEFAULT '';
CREATE TABLE repository_pr_applications (
 id text PRIMARY KEY,
 repository_id text NOT NULL REFERENCES repository_configurations(id) ON DELETE CASCADE,
 number integer NOT NULL CHECK(number>0),
 definition_name text NOT NULL,
 configuration_path text NOT NULL,
 head_sha text NOT NULL,
 mode text NOT NULL CHECK(mode IN ('review-only','isolated','existing')),
 application_id text REFERENCES applications(id) ON DELETE SET NULL,
 connection_id text REFERENCES source_control_connections(id) ON DELETE SET NULL,
 definition_removed boolean NOT NULL DEFAULT false,
 merged boolean NOT NULL DEFAULT false,
 phase text NOT NULL DEFAULT 'pending',
 error text NOT NULL DEFAULT '',
 plan jsonb,
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(repository_id,number,definition_name)
);
ALTER TABLE source_control_connections ADD COLUMN repository_pr_id text REFERENCES repository_pr_applications(id) ON DELETE RESTRICT;
CREATE INDEX repository_pr_application_idx ON repository_pr_applications(application_id) WHERE application_id IS NOT NULL;
