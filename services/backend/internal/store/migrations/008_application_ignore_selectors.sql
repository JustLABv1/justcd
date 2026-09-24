CREATE TABLE application_ignore_selectors (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    api_version TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT '',
    label_key TEXT NOT NULL DEFAULT '',
    label_value TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL,
    created_by TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((api_version <> '' AND kind <> '') OR (label_key <> '' AND api_version = '' AND kind = '')),
    UNIQUE (application_id, api_version, kind, label_key, label_value)
);
CREATE INDEX application_ignore_selectors_app_idx ON application_ignore_selectors (application_id, created_at DESC);
