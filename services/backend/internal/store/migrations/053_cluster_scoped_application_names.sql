-- Preserve application IDs, history and resource ownership; only relax name scope.
-- Renaming project_id to workspace_id left the original constraint name intact.
ALTER TABLE applications DROP CONSTRAINT applications_project_id_name_key;
ALTER TABLE applications ADD CONSTRAINT applications_workspace_cluster_name_key
    UNIQUE (workspace_id, cluster_id, name);
DROP INDEX applications_repository_identity;
CREATE UNIQUE INDEX applications_repository_identity
    ON applications(repository_configuration_id, cluster_id, name)
    WHERE repository_configuration_id IS NOT NULL;

-- Keep PR definition identity separate for each destination cluster, including
-- existing PR deployments. Deleted historical applications have no destination.
ALTER TABLE repository_pr_applications ADD COLUMN cluster_id TEXT;
UPDATE repository_pr_applications p SET cluster_id=a.cluster_id
    FROM applications a WHERE a.id=p.application_id;
-- PostgreSQL shortens generated constraint names; locate it by its definition.
DO $$
DECLARE old_constraint TEXT;
BEGIN
    SELECT conname INTO STRICT old_constraint FROM pg_constraint
    WHERE conrelid='repository_pr_applications'::regclass AND contype='u'
      AND pg_get_constraintdef(oid)='UNIQUE (repository_id, number, definition_name)';
    EXECUTE format('ALTER TABLE repository_pr_applications DROP CONSTRAINT %I', old_constraint);
END $$;
ALTER TABLE repository_pr_applications ADD CONSTRAINT repository_pr_cluster_definition_key
    UNIQUE (repository_id, number, cluster_id, definition_name);
