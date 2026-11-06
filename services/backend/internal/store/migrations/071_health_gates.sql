-- Post-deploy health gate. A Git-declared policy (spec.healthGate) makes each
-- successful sync wait for the application to report Healthy before the
-- deployment is considered good. The check row is the durable state the worker
-- resolves, so a restart or a second replica never loses or repeats a verdict.
CREATE TABLE application_health_gates (
 application_id text PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
 timeout_seconds integer NOT NULL CHECK (timeout_seconds BETWEEN 30 AND 3600),
 on_failure text NOT NULL CHECK (on_failure IN ('mark-failed','prepare-rollback'))
);

CREATE TABLE health_gate_checks (
 operation_id text PRIMARY KEY,
 application_id text NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
 revision text NOT NULL DEFAULT '',
 status text NOT NULL CHECK (status IN ('pending','passed','failed','superseded')),
 started_at timestamptz NOT NULL DEFAULT now(),
 deadline timestamptz NOT NULL,
 finished_at timestamptz,
 message text NOT NULL DEFAULT '',
 on_failure text NOT NULL DEFAULT 'mark-failed',
 gitlab_deployment_id bigint NOT NULL DEFAULT 0,
 rollback_plan_id text NOT NULL DEFAULT ''
);
CREATE INDEX health_gate_checks_pending ON health_gate_checks(deadline) WHERE status='pending';
CREATE INDEX health_gate_checks_application ON health_gate_checks(application_id, started_at DESC);
