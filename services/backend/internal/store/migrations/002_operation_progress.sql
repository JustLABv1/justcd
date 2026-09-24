ALTER TABLE operations ADD COLUMN progress JSONB NOT NULL DEFAULT '{}'::jsonb;
