ALTER TABLE application_resource_observations
    ADD COLUMN topology_spec JSONB NOT NULL DEFAULT '{}'::jsonb;
