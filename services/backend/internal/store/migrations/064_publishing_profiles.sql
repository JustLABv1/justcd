ALTER TABLE credentials DROP CONSTRAINT credentials_kind_check;
ALTER TABLE credentials ADD CONSTRAINT credentials_kind_check CHECK (kind IN ('git-ssh','git-https','kubernetes-token','kubeconfig','publishing'));
CREATE TABLE publishing_profiles (
 id text PRIMARY KEY,
 workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 name text NOT NULL,
 description text NOT NULL DEFAULT '',
 enabled boolean NOT NULL DEFAULT true,
 settings jsonb NOT NULL,
 credential_id text NOT NULL REFERENCES credentials(id) ON DELETE RESTRICT,
 git_read_credential_id text NOT NULL REFERENCES credentials(id) ON DELETE RESTRICT,
 version bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,name)
);
ALTER TABLE published_applications ADD COLUMN profile_id text REFERENCES publishing_profiles(id) ON DELETE RESTRICT;
