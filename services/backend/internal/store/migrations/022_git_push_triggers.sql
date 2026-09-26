CREATE TABLE git_push_webhooks (
    source_id TEXT PRIMARY KEY REFERENCES git_sources(id) ON DELETE CASCADE,
    secret_cipher BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE git_push_deliveries (
    source_key TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (source_key, delivery_id)
);

CREATE INDEX git_push_deliveries_received_idx ON git_push_deliveries (received_at);

CREATE TABLE git_push_triggers (
    application_id TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    source_key TEXT NOT NULL,
    provider TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    ref TEXT NOT NULL,
    reported_sha TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX git_push_triggers_received_idx ON git_push_triggers (received_at);

ALTER TABLE plans ADD COLUMN trigger_info JSONB;
