ALTER TABLE applications
    ADD COLUMN retry_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN retry_max_attempts INTEGER NOT NULL DEFAULT 5 CHECK (retry_max_attempts BETWEEN 1 AND 20),
    ADD COLUMN retry_initial_delay_seconds INTEGER NOT NULL DEFAULT 5 CHECK (retry_initial_delay_seconds BETWEEN 1 AND 3600),
    ADD COLUMN retry_max_delay_seconds INTEGER NOT NULL DEFAULT 300 CHECK (retry_max_delay_seconds BETWEEN 1 AND 86400),
    ADD COLUMN retry_jitter_percent INTEGER NOT NULL DEFAULT 20 CHECK (retry_jitter_percent BETWEEN 0 AND 50),
    ADD COLUMN retry_attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_attempt_count >= 0),
    ADD COLUMN retry_next_at TIMESTAMPTZ,
    ADD COLUMN retry_terminal_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN retry_last_error_code TEXT NOT NULL DEFAULT '';

ALTER TABLE operations
    ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 1 CHECK (attempt_count >= 1),
    ADD COLUMN error_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN next_retry_at TIMESTAMPTZ,
    ADD COLUMN terminal_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE clusters
    ADD COLUMN max_concurrent_operations INTEGER NOT NULL DEFAULT 2 CHECK (max_concurrent_operations BETWEEN 1 AND 20),
    ADD COLUMN operations_per_minute INTEGER NOT NULL DEFAULT 30 CHECK (operations_per_minute BETWEEN 1 AND 1000);

CREATE TABLE cluster_operation_gates (
    cluster_id TEXT PRIMARY KEY REFERENCES clusters(id) ON DELETE CASCADE,
    next_operation_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO cluster_operation_gates(cluster_id)
SELECT id FROM clusters
ON CONFLICT (cluster_id) DO NOTHING;

CREATE FUNCTION create_cluster_operation_gate() RETURNS trigger AS $$
BEGIN
    INSERT INTO cluster_operation_gates(cluster_id) VALUES(NEW.id) ON CONFLICT (cluster_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER clusters_create_operation_gate
AFTER INSERT ON clusters
FOR EACH ROW EXECUTE FUNCTION create_cluster_operation_gate();
