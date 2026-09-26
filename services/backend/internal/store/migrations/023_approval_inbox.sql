ALTER TABLE deletion_approvals ADD COLUMN comment TEXT NOT NULL DEFAULT '';
CREATE INDEX plans_pending_approval_idx ON plans (created_at DESC, id) WHERE status = 'current';
