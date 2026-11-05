ALTER TABLE published_applications
 ADD COLUMN completion_token_cipher bytea,
 ADD COLUMN completion_pipeline_id bigint NOT NULL DEFAULT 0,
 ADD COLUMN completion_status text NOT NULL DEFAULT '',
 ADD COLUMN completion_error text NOT NULL DEFAULT '',
 ADD COLUMN completion_next_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX published_completion_due ON published_applications(completion_next_at)
 WHERE config->>'managedCompletion' = 'true';
