-- Provisioning metadata contains no credentials. Keep ownership after app removal.
CREATE TABLE managed_namespace_environments (
 id TEXT PRIMARY KEY,
 cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 namespace TEXT NOT NULL,
 owner_application_id TEXT NOT NULL,
 namespace_uid TEXT NOT NULL DEFAULT '',
 phase TEXT NOT NULL DEFAULT 'pending',
 shared BOOLEAN NOT NULL DEFAULT FALSE,
 status JSONB NOT NULL DEFAULT '{}',
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(cluster_id,namespace)
);

-- Serialize destination changes with cleanup. A namespace can never become
-- exclusive again after a second app has referenced it.
CREATE FUNCTION guard_managed_namespace_destination() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE binding JSONB; environment managed_namespace_environments%ROWTYPE;
BEGIN
 FOR binding IN SELECT * FROM jsonb_array_elements(CASE WHEN jsonb_typeof(NEW.namespaces)='array' THEN NEW.namespaces ELSE '[]'::jsonb END) LOOP
  PERFORM pg_advisory_xact_lock(hashtextextended(jsonb_build_array(NEW.cluster_id,binding->>'namespace')::text,0));
  SELECT * INTO environment FROM managed_namespace_environments
    WHERE cluster_id=NEW.cluster_id AND namespace=binding->>'namespace' FOR UPDATE;
  IF FOUND THEN
   IF environment.phase='deleting' THEN RAISE EXCEPTION 'namespace environment is undergoing deletion'; END IF;
   IF environment.workspace_id<>NEW.workspace_id THEN RAISE EXCEPTION 'namespace environment belongs to another workspace'; END IF;
   IF environment.owner_application_id<>NEW.id AND environment.phase<>'deleted' THEN
    UPDATE managed_namespace_environments SET shared=TRUE WHERE id=environment.id;
   END IF;
  END IF;
 END LOOP;
 RETURN NEW;
END $$;
CREATE TRIGGER guard_managed_namespace_destination BEFORE INSERT OR UPDATE OF namespaces,cluster_id,workspace_id ON applications
 FOR EACH ROW EXECUTE FUNCTION guard_managed_namespace_destination();

CREATE FUNCTION guard_managed_namespace_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status IN ('queued','running') AND EXISTS(
  SELECT 1 FROM applications a JOIN managed_namespace_environments e ON e.cluster_id=a.cluster_id
   AND a.namespaces @> jsonb_build_array(jsonb_build_object('namespace',e.namespace))
  WHERE a.id=NEW.application_id AND e.phase='deleting'
 ) OR (NEW.status IN ('queued','running') AND EXISTS(SELECT 1 FROM managed_namespace_environments WHERE owner_application_id=NEW.application_id AND phase IN ('deleting','deleted'))) THEN RAISE EXCEPTION 'namespace environment is undergoing deletion'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER guard_managed_namespace_operation BEFORE INSERT ON operations
 FOR EACH ROW EXECUTE FUNCTION guard_managed_namespace_operation();
