ALTER TABLE projects
    ADD COLUMN approval_policy JSONB NOT NULL DEFAULT '{"sync":{"requiredApprovals":0,"approverRoles":["owner"],"approverUserIds":[]},"deletion":{"requiredApprovals":1,"approverRoles":["owner"],"approverUserIds":[]}}'::jsonb;

ALTER TABLE applications
    ADD COLUMN approval_policy_override JSONB;

ALTER TABLE plans
    ADD COLUMN approval_kind TEXT NOT NULL DEFAULT '',
    ADD COLUMN required_approvals INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN approver_roles JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN approver_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE operations
    ADD COLUMN approval_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE operations
SET approval_ids = jsonb_build_array(approval_id)
WHERE approval_id IS NOT NULL;
