ALTER TABLE application_groups
    ADD COLUMN renderer TEXT NOT NULL DEFAULT 'helm' CHECK (renderer IN ('helm', 'kustomize')),
    ADD COLUMN kustomize_helm_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN kustomize_namespace_override BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE applications
    ADD COLUMN target_manifest_path TEXT NOT NULL DEFAULT '',
    ADD COLUMN namespace_manifest_paths JSONB NOT NULL DEFAULT '{}'::jsonb;
