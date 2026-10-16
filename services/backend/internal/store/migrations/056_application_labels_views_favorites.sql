ALTER TABLE applications ADD COLUMN labels JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(labels)='object');

CREATE TABLE application_favorites (
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
 PRIMARY KEY(user_id,application_id)
);
CREATE TABLE application_saved_views (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 configuration JSONB NOT NULL,
 is_default BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(user_id,name)
);
CREATE UNIQUE INDEX application_saved_views_default ON application_saved_views(user_id) WHERE is_default;
