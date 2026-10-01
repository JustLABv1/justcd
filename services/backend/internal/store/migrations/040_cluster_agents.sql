CREATE TABLE cluster_agents (
 cluster_id TEXT PRIMARY KEY REFERENCES clusters(id) ON DELETE CASCADE,
 default_profile TEXT NOT NULL DEFAULT 'default', cluster_profile TEXT NOT NULL DEFAULT '',
 enrollment_hash BYTEA, enrollment_expires_at TIMESTAMPTZ,
 token_hash BYTEA, token_expires_at TIMESTAMPTZ,
 cluster_uid TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '',
 profiles JSONB NOT NULL DEFAULT '[]', last_seen_at TIMESTAMPTZ,
 revoked BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE TABLE cluster_agent_tasks (
 id TEXT PRIMARY KEY, cluster_id TEXT NOT NULL REFERENCES cluster_agents(cluster_id) ON DELETE CASCADE,
 request_cipher BYTEA NOT NULL, response_cipher BYTEA,
 state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','claimed','completed')),
 lease TEXT, deadline TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX cluster_agent_tasks_poll ON cluster_agent_tasks(cluster_id,state,created_at);
ALTER TABLE cluster_agents ADD COLUMN previous_token_hash BYTEA, ADD COLUMN previous_token_expires_at TIMESTAMPTZ;

CREATE UNIQUE INDEX cluster_agent_enrollment_identity ON cluster_agents(enrollment_hash) WHERE enrollment_hash IS NOT NULL;
CREATE UNIQUE INDEX cluster_agent_current_identity ON cluster_agents(token_hash) WHERE token_hash IS NOT NULL;
CREATE UNIQUE INDEX cluster_agent_previous_identity ON cluster_agents(previous_token_hash) WHERE previous_token_hash IS NOT NULL;
