ALTER TABLE applications ADD COLUMN status_issues JSONB NOT NULL DEFAULT '[]'::jsonb;
