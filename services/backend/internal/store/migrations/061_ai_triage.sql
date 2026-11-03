ALTER TABLE workspace_ai_settings ADD COLUMN auto_triage boolean NOT NULL DEFAULT FALSE;
CREATE TABLE ai_triage_results (
 workspace_id text PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
 inputs_hash text NOT NULL,
 trigger text NOT NULL,
 model text NOT NULL DEFAULT '',
 result jsonb NOT NULL,
 generated_at timestamptz NOT NULL DEFAULT now()
);
