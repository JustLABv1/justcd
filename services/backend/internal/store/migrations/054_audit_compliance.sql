-- Zero keeps existing evidence indefinitely. Scope survives resource deletion.
ALTER TABLE workspaces ADD COLUMN audit_retention_days INTEGER NOT NULL DEFAULT 0 CHECK (audit_retention_days BETWEEN 0 AND 36500);
ALTER TABLE audit_events ADD COLUMN workspace_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN resource_name TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN deployment_record JSONB;
CREATE INDEX audit_events_workspace_cursor_idx ON audit_events(workspace_id,id DESC);
CREATE INDEX audit_events_resource_cursor_idx ON audit_events(resource_type,resource_id,id DESC);

CREATE FUNCTION capture_audit_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE plan TEXT; operation TEXT;
BEGIN
 IF NEW.workspace_id='' THEN
  CASE NEW.resource_type
   WHEN 'workspace' THEN NEW.workspace_id:=NEW.resource_id;
   WHEN 'application' THEN SELECT workspace_id,name INTO NEW.workspace_id,NEW.resource_name FROM applications WHERE id=NEW.resource_id;
   WHEN 'application_group' THEN SELECT workspace_id,name INTO NEW.workspace_id,NEW.resource_name FROM application_groups WHERE id=NEW.resource_id;
   WHEN 'git_source' THEN SELECT workspace_id,name INTO NEW.workspace_id,NEW.resource_name FROM git_sources WHERE id=NEW.resource_id;
   WHEN 'cluster' THEN SELECT workspace_id,name INTO NEW.workspace_id,NEW.resource_name FROM clusters WHERE id=NEW.resource_id;
   WHEN 'credential' THEN SELECT workspace_id,name INTO NEW.workspace_id,NEW.resource_name FROM credentials WHERE id=NEW.resource_id;
   WHEN 'repository_configuration' THEN SELECT workspace_id INTO NEW.workspace_id FROM repository_configurations WHERE id=NEW.resource_id;
   WHEN 'connection_share' THEN NEW.workspace_id:=NEW.details->>'workspaceId';
   ELSE NULL;
  END CASE;
 END IF;
 IF NEW.resource_type='cluster' AND (NEW.action LIKE 'namespace_binding.%' OR NEW.action LIKE 'workspace_cluster_credential.%') THEN
  NEW.workspace_id:=COALESCE(NEW.details->>'workspaceId',NEW.workspace_id);
 END IF;
 -- Deletion actions are often written after their target row is removed.
 IF COALESCE(NEW.workspace_id,'')='' AND NEW.action LIKE '%.deleted' THEN
  SELECT e.workspace_id,e.resource_name INTO NEW.workspace_id,NEW.resource_name FROM audit_events e
   WHERE e.resource_type=NEW.resource_type AND e.resource_id=NEW.resource_id AND e.workspace_id<>'' AND e.id<>NEW.id AND e.action NOT LIKE 'namespace_binding.%' AND e.action NOT LIKE 'workspace_cluster_credential.%' ORDER BY e.id DESC LIMIT 1;
 END IF;
 NEW.workspace_id:=COALESCE(NEW.workspace_id,'');
 NEW.resource_name:=COALESCE(NEW.resource_name,'');
 IF NEW.deployment_record IS NULL AND NEW.resource_type='application' THEN
  operation:=NEW.details->>'operationId';
  plan:=NEW.details->>'planId';
  IF operation IS NOT NULL THEN SELECT COALESCE(plan,plan_id) INTO plan FROM operations WHERE id=operation AND application_id=NEW.resource_id; END IF;
  SELECT jsonb_build_object(
   'schemaVersion',1,'applicationId',p.application_id,'workspaceId',NEW.workspace_id,
   'planId',p.id,'commit',p.revision,'planDigest',p.digest,'planCreatedAt',p.created_at,
   'targets',COALESCE((SELECT jsonb_agg(jsonb_build_object('clusterId',b->>'clusterId','namespace',b->>'namespace','clusterScope',b->'clusterScope')) FROM jsonb_array_elements(CASE WHEN jsonb_typeof(p.bindings)='array' THEN p.bindings ELSE '[]'::jsonb END) b),'[]'::jsonb),
   'policy',jsonb_build_object('mode',p.policy_evaluation->'mode','configDigest',p.policy_evaluation->'configDigest','results',COALESCE((SELECT jsonb_agg(jsonb_build_object('rule',v->'rule','resource',jsonb_build_object('clusterId',v->'resource'->>'clusterId','apiVersion',v->'resource'->>'apiVersion','kind',v->'resource'->>'kind','namespace',v->'resource'->>'namespace','name',v->'resource'->>'name'))) FROM jsonb_array_elements(CASE WHEN jsonb_typeof(p.policy_evaluation->'results')='array' THEN p.policy_evaluation->'results' ELSE '[]'::jsonb END) v),'[]'::jsonb)),
   'requiredApprovals',p.required_approvals,
   'approvals',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',a.id,'actorId',a.actor_id,'planDigest',a.plan_digest,'createdAt',a.created_at,'usedAt',a.used_at)) FROM deletion_approvals a WHERE a.plan_id=p.id AND a.plan_digest=p.digest AND (o.id IS NULL OR a.id IN (SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(o.approval_ids)='array' THEN o.approval_ids ELSE '[]'::jsonb END)))),'[]'::jsonb),
   'operationId',o.id,'operationType',o.operation_type,'result',COALESCE(o.status,CASE WHEN NEW.action LIKE '%.succeeded' THEN 'succeeded' WHEN NEW.action LIKE '%.failed' THEN 'failed' END),'startedAt',o.started_at,'finishedAt',o.finished_at,'errorCode',o.error_code
  ) INTO NEW.deployment_record FROM plans p LEFT JOIN operations o ON o.id=operation AND o.application_id=p.application_id WHERE p.id=plan AND p.application_id=NEW.resource_id;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER audit_evidence BEFORE INSERT OR UPDATE ON audit_events FOR EACH ROW EXECUTE FUNCTION capture_audit_evidence();
-- Backfill evidence still available in the source tables.
UPDATE audit_events SET workspace_id=workspace_id;
