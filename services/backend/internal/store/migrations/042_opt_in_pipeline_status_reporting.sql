-- Commit statuses can attach external jobs to unrelated GitLab pipelines.
-- Existing connections must explicitly opt in again.
ALTER TABLE source_control_connections
    ADD COLUMN pipeline_status_reporting BOOLEAN NOT NULL DEFAULT FALSE;
