-- Helm templates may generate passwords, certificates and checksum annotations.
-- Keep one immutable render for each exact source revision and render settings.
CREATE TABLE application_render_artifacts (
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    input_digest TEXT NOT NULL,
    resources JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (application_id, input_digest)
);
