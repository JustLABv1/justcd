ALTER TABLE plans ADD COLUMN IF NOT EXISTS approval_reasons JSONB NOT NULL DEFAULT 'null'::jsonb;
