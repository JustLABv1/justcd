-- Cold restore only: old backend MUST be stopped/fenced before this transaction.
-- Preserve completed history and partial progress; prevent replay of old work.
BEGIN;
UPDATE applications SET auto_sync_paused=TRUE, retry_next_at=NULL;
UPDATE operations SET status='cancelled', message='Cancelled queued work during disaster recovery; build a fresh plan',
  finished_at=NOW(), next_retry_at=NULL, terminal_reason='disaster recovery review required'
  WHERE status='queued';
UPDATE operations SET status='failed', message='Interrupted operation restored; inspect live state and build a fresh plan',
  finished_at=NOW(), next_retry_at=NULL, terminal_reason='disaster recovery review required'
  WHERE status='running';
DELETE FROM operation_leases;
UPDATE plans SET status='stale' WHERE status='current';
INSERT INTO audit_events(action,resource_type,resource_id,details)
  VALUES('disaster_recovery.quarantined','system','',
  '{"reason":"cold restore; old backend fenced; fresh plans required"}');
COMMIT;
