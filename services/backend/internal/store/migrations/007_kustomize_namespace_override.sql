ALTER TABLE applications
    ADD COLUMN kustomize_namespace_override BOOLEAN NOT NULL DEFAULT FALSE;
