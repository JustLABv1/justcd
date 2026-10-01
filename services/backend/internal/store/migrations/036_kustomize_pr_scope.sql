-- Recheck existing review-only Kustomize plans under dependency-aware filtering.
-- Active previews retain their lifecycle and are never detached by scope checks.
UPDATE pull_request_reviews r SET phase='pending', updated_at=NOW()
FROM source_control_connections c, applications a
WHERE r.connection_id=c.id AND c.application_id=a.id
  AND a.renderer='kustomize' AND r.phase='planned'
  AND r.preview_application_id IS NULL AND NOT r.closed;
