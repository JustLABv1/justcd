ALTER TABLE applications
    ADD COLUMN kustomize_helm_enabled BOOLEAN NOT NULL DEFAULT FALSE;
