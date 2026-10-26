ALTER TABLE applications DROP CONSTRAINT applications_sync_policy_check;
ALTER TABLE applications ADD CONSTRAINT applications_sync_policy_check CHECK (sync_policy IN ('manual', 'auto-safe', 'auto'));
ALTER TABLE application_groups DROP CONSTRAINT application_groups_sync_policy_check;
ALTER TABLE application_groups ADD CONSTRAINT application_groups_sync_policy_check CHECK (sync_policy IN ('manual', 'auto-safe', 'auto'));
