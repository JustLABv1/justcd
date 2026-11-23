-- Digest of the inputs a plan was computed from, used to recheck an
-- unchanged plan without rendering and dry-running it again. Empty disables
-- the fast recheck for that plan.
ALTER TABLE plans ADD COLUMN IF NOT EXISTS recheck_basis TEXT NOT NULL DEFAULT '';
