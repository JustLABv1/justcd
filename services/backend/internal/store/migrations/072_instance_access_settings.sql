-- Instance-wide sign-in and registration controls. Defaults keep the behaviour of earlier releases:
-- password sign-in on, accounts created on first OIDC sign-in, any signed-in user may create workspaces.
ALTER TABLE instance_settings
    ADD COLUMN local_login_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN oidc_auto_provision BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN oidc_allowed_domains TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN workspace_creation TEXT NOT NULL DEFAULT 'anyone' CHECK (workspace_creation IN ('anyone', 'admins'));
