ALTER TABLE pull_request_reviews ADD COLUMN scope_note TEXT NOT NULL DEFAULT '';
-- Earlier scope discovery fell back for helmCharts. Reevaluate existing plans.
UPDATE pull_request_reviews r SET phase='pending',updated_at=NOW()
FROM source_control_connections c, applications a
WHERE r.connection_id=c.id AND c.application_id=a.id
 AND a.renderer='kustomize' AND r.phase='planned'
 AND r.preview_application_id IS NULL AND NOT r.closed;
