CREATE TABLE ai_settings (
 singleton boolean PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 enabled boolean NOT NULL DEFAULT FALSE,
 base_url text NOT NULL DEFAULT '',
 model text NOT NULL DEFAULT '',
 api_key_cipher bytea,
 max_tokens integer NOT NULL DEFAULT 2048 CHECK(max_tokens BETWEEN 256 AND 8192)
);
INSERT INTO ai_settings(singleton) VALUES(TRUE);
CREATE TABLE workspace_ai_settings (
 workspace_id text PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
 enabled boolean NOT NULL DEFAULT FALSE
);
