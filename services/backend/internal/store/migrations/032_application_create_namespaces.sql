ALTER TABLE applications ADD COLUMN create_namespaces BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE plans ADD COLUMN namespace_creations JSONB NOT NULL DEFAULT '[]'::jsonb;
