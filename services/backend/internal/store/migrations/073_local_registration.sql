-- Optional self-service registration with a password. Off by default.
-- A self-registered account starts unverified: its owner has not proven control of the email address, so it
-- can sign in but cannot create workspaces or be added to one until an administrator approves it, or the
-- real owner of the address signs in through single sign-on (which reclaims the account).
ALTER TABLE users ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE instance_settings ADD COLUMN local_registration_enabled BOOLEAN NOT NULL DEFAULT FALSE;
