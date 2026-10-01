-- Some installations applied migration 032 before the plan column was added
-- to that migration. A separate version repairs their schema without changing
-- existing plans and remains safe for installations with the complete 032.
ALTER TABLE plans ADD COLUMN IF NOT EXISTS namespace_creations JSONB NOT NULL DEFAULT '[]'::jsonb;
