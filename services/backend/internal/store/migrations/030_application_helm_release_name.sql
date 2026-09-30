-- Some development instances applied repository discovery before release names
-- were added. Keep the follow-up compatible with either version of that schema.
ALTER TABLE applications ADD COLUMN IF NOT EXISTS helm_release_name TEXT NOT NULL DEFAULT '';
