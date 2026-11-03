CREATE TABLE ai_plan_reviews (
 plan_id text PRIMARY KEY REFERENCES plans(id) ON DELETE CASCADE,
 plan_digest text NOT NULL,
 model text NOT NULL DEFAULT '',
 result jsonb NOT NULL,
 generated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE ai_digests (
 workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 period text NOT NULL CHECK (period IN ('day','week')),
 model text NOT NULL DEFAULT '',
 result jsonb NOT NULL,
 generated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (workspace_id, period)
);
