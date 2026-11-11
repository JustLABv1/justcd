ALTER TABLE mcp_credentials ALTER COLUMN expires_at DROP NOT NULL;
CREATE TABLE mcp_policy (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
 allowed_lifetimes JSONB NOT NULL DEFAULT '[7,30,90,365,0]'
);
INSERT INTO mcp_policy(singleton) VALUES(TRUE);
