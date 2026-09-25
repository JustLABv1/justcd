ALTER TABLE applications
    ADD COLUMN helm_values_files JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN helm_values_yaml TEXT NOT NULL DEFAULT '';
