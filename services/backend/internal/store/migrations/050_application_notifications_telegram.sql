ALTER TABLE notification_destinations DROP CONSTRAINT notification_destinations_provider_check;
ALTER TABLE notification_destinations ADD CONSTRAINT notification_destinations_provider_check CHECK(provider IN ('slack','teams','email','webhook','telegram'));
CREATE TABLE application_notification_policies (
 application_id TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
 mode TEXT NOT NULL CHECK(mode IN ('inherit','custom','disabled')),
 destination_ids JSONB NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(destination_ids)='array'),
 events JSONB NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(events)='array'),
 minimum_severity TEXT NOT NULL DEFAULT 'info' CHECK(minimum_severity IN ('info','warning','error'))
);
-- One predicate governs both event routing and cancellation of queued deliveries.
CREATE FUNCTION notification_routes_to(destination TEXT, event TEXT) RETURNS BOOLEAN LANGUAGE SQL VOLATILE AS $$
 SELECT COALESCE((SELECT d.enabled AND d.workspace_id=e.workspace_id
 AND COALESCE(p.mode,'inherit')<>'disabled'
 AND CASE WHEN p.mode='custom' THEN p.destination_ids ? d.id AND p.events ? e.event_type
 ELSE d.events ? e.event_type END
 AND CASE e.severity WHEN 'error' THEN 3 WHEN 'warning' THEN 2 ELSE 1 END
 >= CASE CASE WHEN p.mode='custom' THEN p.minimum_severity ELSE d.minimum_severity END WHEN 'error' THEN 3 WHEN 'warning' THEN 2 ELSE 1 END
 FROM notification_destinations d CROSS JOIN notification_events e
 LEFT JOIN application_notification_policies p ON p.application_id=e.application_id
 WHERE d.id=destination AND e.id=event),FALSE);
$$;
CREATE OR REPLACE FUNCTION route_notification_event() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO notification_deliveries(id,destination_id,event_id)
 SELECT md5(NEW.id || ':' || d.id),d.id,NEW.id FROM notification_destinations d
 WHERE d.workspace_id=NEW.workspace_id AND notification_routes_to(d.id,NEW.id);
 RETURN NEW;
END $$;
