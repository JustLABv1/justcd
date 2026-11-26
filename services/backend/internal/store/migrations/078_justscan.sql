CREATE TABLE justscan_connections (
    workspace_id TEXT PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    organization_id TEXT NOT NULL,
    token_cipher BYTEA NOT NULL
);

CREATE TABLE justscan_plan_scans (
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES justscan_connections(workspace_id) ON DELETE CASCADE,
    image TEXT NOT NULL,
    scan_id TEXT NOT NULL DEFAULT '',
    report_url TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'queued',
    critical_count INTEGER NOT NULL DEFAULT 0,
    high_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    next_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempt_id TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (plan_id, image)
);

CREATE INDEX justscan_plan_scans_due_idx ON justscan_plan_scans(next_at)
    WHERE status IN ('queued', 'submitting', 'pending');
