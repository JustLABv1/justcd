ALTER TABLE source_control_connections ADD COLUMN status_credential_id TEXT REFERENCES credentials(id) ON DELETE RESTRICT;
ALTER TABLE pull_request_reviews ADD COLUMN head_branch TEXT NOT NULL DEFAULT '';
ALTER TABLE pull_request_reviews ADD COLUMN adopted_branch_preview BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE pull_request_reviews ADD COLUMN shared_environment BOOLEAN NOT NULL DEFAULT FALSE;
CREATE UNIQUE INDEX pull_request_active_application ON pull_request_reviews(preview_application_id) WHERE preview_application_id IS NOT NULL;
ALTER TABLE pull_request_reviews ADD COLUMN branch_preview_declined BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE pull_request_reviews ADD COLUMN reported_plan_id TEXT NOT NULL DEFAULT '';
