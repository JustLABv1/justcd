ALTER TABLE instance_settings ADD COLUMN justscan_url TEXT;
ALTER TABLE justscan_connections ADD COLUMN server_url TEXT NOT NULL DEFAULT '';
