ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS guardrail_policy JSONB NOT NULL DEFAULT '{"mode":"audit"}'::jsonb;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS policy_evaluation JSONB NOT NULL DEFAULT 'null'::jsonb;
