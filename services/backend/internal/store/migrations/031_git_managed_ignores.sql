ALTER TABLE application_ignore_rules ADD COLUMN managed_by_git BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE application_ignore_selectors ADD COLUMN managed_by_git BOOLEAN NOT NULL DEFAULT FALSE;
