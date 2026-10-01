-- Keep only diagnostic metadata after encrypted task payloads are consumed.
CREATE TABLE cluster_agent_activity (
 id TEXT PRIMARY KEY,
 cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 profile TEXT NOT NULL,
 namespace TEXT NOT NULL DEFAULT '',
 method TEXT NOT NULL,
 resource TEXT NOT NULL DEFAULT '',
 operation_id TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK (state IN ('queued','running','succeeded','failed','expired','unknown')),
 http_status INTEGER NOT NULL DEFAULT 0,
 error_code TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 started_at TIMESTAMPTZ,
 finished_at TIMESTAMPTZ,
 deadline TIMESTAMPTZ NOT NULL
);
CREATE INDEX cluster_agent_activity_workspace ON cluster_agent_activity(cluster_id,workspace_id,created_at DESC);
