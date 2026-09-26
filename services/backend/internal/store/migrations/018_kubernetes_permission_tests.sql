CREATE TABLE kubernetes_permission_tests (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    namespace TEXT NOT NULL,
    report JSONB NOT NULL,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, cluster_id, namespace)
);
