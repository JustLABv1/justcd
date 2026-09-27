CREATE TABLE onboarding_acknowledgements (
    step_id TEXT PRIMARY KEY,
    completed_by TEXT NOT NULL REFERENCES users(id),
    completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
