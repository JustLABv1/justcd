ALTER TABLE applications
    ADD COLUMN health_condition_status TEXT NOT NULL DEFAULT 'Unknown'
        CHECK (health_condition_status IN ('Healthy','Progressing','Degraded','Suspended','Missing','Unknown','Partial')),
    ADD COLUMN health_condition_reason TEXT NOT NULL DEFAULT 'NoHealthEvidence',
    ADD COLUMN health_condition_message TEXT NOT NULL DEFAULT 'No supported Kubernetes workload health has been observed.',
    ADD COLUMN health_condition_last_transition_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN health_condition_observed_at TIMESTAMPTZ,
    ADD COLUMN health_condition_resources JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN health_condition_warnings JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE application_resource_observations
    ADD COLUMN health_summary JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE application_health_transitions (
    id BIGSERIAL PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('Healthy','Progressing','Degraded','Suspended','Missing','Unknown','Partial')),
    reason TEXT NOT NULL,
    message TEXT NOT NULL,
    resources JSONB NOT NULL DEFAULT '[]'::jsonb,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX application_health_transitions_app_idx
    ON application_health_transitions (application_id, changed_at DESC, id DESC);
