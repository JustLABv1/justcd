ALTER TABLE applications
    ADD COLUMN namespace_helm_values JSONB NOT NULL DEFAULT '{}'::jsonb;
