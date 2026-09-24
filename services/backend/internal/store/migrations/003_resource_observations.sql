CREATE TABLE application_resource_observations (
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    api_version TEXT NOT NULL,
    kind TEXT NOT NULL,
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    uid TEXT NOT NULL,
    resource_version TEXT NOT NULL DEFAULT '',
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    owner_uids JSONB NOT NULL DEFAULT '[]'::jsonb,
    phase TEXT NOT NULL DEFAULT '',
    readiness TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'kubernetes' CHECK (source IN ('kubernetes','sample')),
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (application_id, cluster_id, api_version, kind, namespace, name)
);
CREATE INDEX application_resource_observations_app_idx ON application_resource_observations (application_id, observed_at DESC);
