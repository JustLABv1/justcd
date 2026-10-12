CREATE TABLE background_tasks (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    actor_id TEXT NOT NULL REFERENCES users(id),
    kind TEXT NOT NULL CHECK (kind IN ('plan', 'approval')),
    target TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL,
    input JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    result JSONB,
    http_status INTEGER,
    lease_token TEXT,
    lease_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    UNIQUE (actor_id, request_id)
);
CREATE UNIQUE INDEX background_tasks_active ON background_tasks(application_id, actor_id, kind, target)
    WHERE status IN ('queued', 'running');
CREATE INDEX background_tasks_queue ON background_tasks(created_at) WHERE status = 'queued';
CREATE INDEX background_tasks_actor ON background_tasks(actor_id, created_at DESC);

-- A second submission can share active work and must still resolve to that task
-- if its own response is lost and retried after the work has completed.
CREATE TABLE background_task_requests (
    actor_id TEXT NOT NULL REFERENCES users(id),
    request_id TEXT NOT NULL,
    task_id TEXT NOT NULL REFERENCES background_tasks(id) ON DELETE CASCADE,
    input JSONB NOT NULL,
    PRIMARY KEY (actor_id, request_id)
);
