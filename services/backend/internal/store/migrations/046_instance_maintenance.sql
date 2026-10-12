CREATE TABLE instance_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    maintenance_enabled BOOLEAN NOT NULL DEFAULT FALSE
);
INSERT INTO instance_settings(singleton) VALUES (TRUE);
