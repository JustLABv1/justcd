-- Cold restore only: old backend MUST be stopped/fenced before this transaction.
-- Preserve completed history and partial progress; prevent ALL automatic replay.
BEGIN;
-- Save only prior switches/IDs (not credentials or manifest-bearing settings).
-- Use this inventory for deliberate operator resumption, never blanket replay.
INSERT INTO audit_events(action,resource_type,resource_id,details)
SELECT 'disaster_recovery.quarantined','system','',jsonb_build_object(
  'reason','cold restore; old backend fenced; fresh plans required',
  'priorAutomation',jsonb_build_object(
    'applications',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',id,'autoSyncPaused',auto_sync_paused) ORDER BY id) FROM applications),'[]'::jsonb),
    'sourceControlConnections',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',id,'enabled',enabled,'previewEnabled',preview_profile->'enabled') ORDER BY id) FROM source_control_connections),'[]'::jsonb),
    'repositories',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',id,'enabled',enabled,'prEnabled',pr_settings->'enabled','previewEnabled',pr_settings->'profile'->'enabled') ORDER BY id) FROM repository_configurations),'[]'::jsonb)
  )
);
-- DueReviews and enabled-connection polling check this switch. App pause alone
-- does not prevent PR workers creating/applying previews or cleaning them up.
UPDATE source_control_connections SET enabled=FALSE,
  preview_profile=CASE WHEN jsonb_typeof(preview_profile)='object' THEN jsonb_set(preview_profile,'{enabled}','false'::jsonb) ELSE '{"enabled":false}'::jsonb END,updated_at=NOW();
-- Discovery can create applications and Git-managed connections: fence it too.
UPDATE repository_configurations SET enabled=FALSE,
  pr_settings=CASE WHEN jsonb_typeof(pr_settings)='object' THEN jsonb_set(pr_settings,'{enabled}','false'::jsonb) ELSE '{"enabled":false}'::jsonb END;
UPDATE repository_configurations SET pr_settings=jsonb_set(pr_settings,'{profile,enabled}','false'::jsonb)
  WHERE jsonb_typeof(pr_settings->'profile')='object';
UPDATE applications SET auto_sync_paused=TRUE, retry_next_at=NULL;
UPDATE operations SET status='cancelled', message='Cancelled queued work during disaster recovery; build a fresh plan',
  finished_at=NOW(), next_retry_at=NULL, terminal_reason='disaster recovery review required'
  WHERE status='queued';
UPDATE operations SET status='failed', message='Interrupted operation restored; inspect live state and build a fresh plan',
  finished_at=NOW(), next_retry_at=NULL, terminal_reason='disaster recovery review required'
  WHERE status='running';
DELETE FROM operation_leases;
UPDATE plans SET status='stale' WHERE status='current';
COMMIT;
