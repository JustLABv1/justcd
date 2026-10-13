CREATE TABLE notification_destinations (
 id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 name TEXT NOT NULL, provider TEXT NOT NULL CHECK(provider IN ('slack','teams','email','webhook')),
 enabled BOOLEAN NOT NULL DEFAULT TRUE, events JSONB NOT NULL, minimum_severity TEXT NOT NULL CHECK(minimum_severity IN ('info','warning','error')),
 config_cipher BYTEA NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE notification_events (
 id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 application_id TEXT NOT NULL DEFAULT '', resource_id TEXT NOT NULL DEFAULT '', event_type TEXT NOT NULL,
 severity TEXT NOT NULL, occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE notification_deliveries (
 id TEXT PRIMARY KEY, destination_id TEXT NOT NULL REFERENCES notification_destinations(id) ON DELETE CASCADE,
 event_id TEXT NOT NULL REFERENCES notification_events(id) ON DELETE CASCADE,
 status TEXT NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','delivered','failed','disabled')),
 attempts INTEGER NOT NULL DEFAULT 0, next_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 status_code INTEGER NOT NULL DEFAULT 0, result_code TEXT NOT NULL DEFAULT '',
 UNIQUE(destination_id,event_id)
);
CREATE INDEX notification_delivery_queue ON notification_deliveries(next_at) WHERE status='queued';
CREATE TABLE notification_attempts (
 id BIGSERIAL PRIMARY KEY, delivery_id TEXT NOT NULL REFERENCES notification_deliveries(id) ON DELETE CASCADE,
 attempt INTEGER NOT NULL, status_code INTEGER NOT NULL, result_code TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE FUNCTION route_notification_event() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO notification_deliveries(id,destination_id,event_id)
 SELECT md5(NEW.id || ':' || d.id),d.id,NEW.id FROM notification_destinations d
 WHERE d.workspace_id=NEW.workspace_id AND d.enabled AND d.events ? NEW.event_type
 AND CASE NEW.severity WHEN 'error' THEN 3 WHEN 'warning' THEN 2 ELSE 1 END >= CASE d.minimum_severity WHEN 'error' THEN 3 WHEN 'warning' THEN 2 ELSE 1 END;
 RETURN NEW;
END $$;
CREATE TRIGGER notification_routing AFTER INSERT ON notification_events FOR EACH ROW EXECUTE FUNCTION route_notification_event();
-- Payloads use an allowlist of identifiers and enums. Never copy manifests, diffs, errors or credentials.
CREATE FUNCTION capture_notification_event() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE workspace TEXT; app TEXT; kind TEXT; severity TEXT := 'info';
BEGIN
 IF TG_TABLE_NAME='plans' THEN
  app:=NEW.application_id;
  IF NEW.status='current' AND jsonb_array_length(CASE WHEN jsonb_typeof(NEW.changes)='array' THEN NEW.changes ELSE '[]'::jsonb END)>0 THEN
   INSERT INTO notification_events(id,workspace_id,application_id,resource_id,event_type,severity)
   SELECT 'drift:'||NEW.id,workspace_id,app,NEW.id,'drift','warning' FROM applications WHERE id=app;
  END IF;
  IF NEW.status='current' AND NEW.required_approvals>0 THEN kind:='approval_requested';severity:='warning'; END IF;
 ELSIF TG_TABLE_NAME='operations' THEN
  app:=NEW.application_id;
  IF NEW.status IS DISTINCT FROM OLD.status AND NEW.status IN ('succeeded','failed','cancelled') THEN
   kind:='operation_completed';
   IF NEW.status='failed' THEN
    severity:='error';
    INSERT INTO notification_events(id,workspace_id,application_id,resource_id,event_type,severity)
    SELECT 'sync_failed:'||NEW.id,workspace_id,app,NEW.id,'sync_failed','error' FROM applications WHERE id=app;
   END IF;
   IF NEW.operation_type='rollback' THEN
    INSERT INTO notification_events(id,workspace_id,application_id,resource_id,event_type,severity)
    SELECT 'rollback:'||NEW.id,workspace_id,app,NEW.id,'rollback',CASE WHEN NEW.status='failed' THEN 'error' ELSE 'warning' END FROM applications WHERE id=app;
   END IF;
  END IF;
 ELSIF TG_TABLE_NAME='applications' THEN
  app:=NEW.id;
  IF NEW.health_condition_status IS DISTINCT FROM OLD.health_condition_status AND NEW.health_condition_status IN ('Degraded','Missing') THEN kind:='unhealthy';severity:='error'; END IF;
 END IF;
 IF kind IS NOT NULL THEN
  SELECT workspace_id INTO workspace FROM applications WHERE id=app;
  INSERT INTO notification_events(id,workspace_id,application_id,resource_id,event_type,severity)
  VALUES(md5(random()::text||clock_timestamp()::text),workspace,app,NEW.id,kind,severity);
 END IF;
 RETURN NEW;
EXCEPTION WHEN OTHERS THEN
 -- Notification persistence is best effort; never fail a deployment transaction.
 RAISE WARNING 'JustCD notification capture failed (SQLSTATE %)', SQLSTATE;
 RETURN NEW;
END $$;
CREATE TRIGGER notification_plan AFTER INSERT ON plans FOR EACH ROW EXECUTE FUNCTION capture_notification_event();
CREATE TRIGGER notification_operation AFTER UPDATE OF status ON operations FOR EACH ROW EXECUTE FUNCTION capture_notification_event();
CREATE TRIGGER notification_health AFTER UPDATE OF health_condition_status ON applications FOR EACH ROW EXECUTE FUNCTION capture_notification_event();
