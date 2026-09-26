CREATE TABLE source_control_connections (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('github', 'gitlab')),
    api_url TEXT NOT NULL,
    repository TEXT NOT NULL,
    webhook_secret_cipher BYTEA NOT NULL,
    status_token_cipher BYTEA NOT NULL,
    preview_profile JSONB NOT NULL DEFAULT '{"enabled":false}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (application_id)
);

CREATE TABLE pull_request_reviews (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL REFERENCES source_control_connections(id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK (number > 0),
    head_sha TEXT NOT NULL,
    source_url TEXT NOT NULL,
    fork BOOLEAN NOT NULL DEFAULT FALSE,
    closed BOOLEAN NOT NULL DEFAULT FALSE,
    phase TEXT NOT NULL DEFAULT 'pending',
    plan JSONB,
    error TEXT NOT NULL DEFAULT '',
    report_error TEXT NOT NULL DEFAULT '',
    preview_application_id TEXT REFERENCES applications(id) ON DELETE SET NULL,
    expires_at TIMESTAMPTZ,
    processed_sha TEXT NOT NULL DEFAULT '',
    reported_phase TEXT NOT NULL DEFAULT '',
    event_at TIMESTAMPTZ NOT NULL,
    lease_until TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (connection_id, number)
);
CREATE INDEX pull_request_reviews_due_idx ON pull_request_reviews (phase, updated_at);

CREATE TABLE source_control_deliveries (
    connection_id TEXT NOT NULL REFERENCES source_control_connections(id) ON DELETE CASCADE,
    delivery_id TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (connection_id, delivery_id)
);

CREATE TABLE preview_slots (
    connection_id TEXT NOT NULL REFERENCES source_control_connections(id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK (number > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (connection_id, number)
);
