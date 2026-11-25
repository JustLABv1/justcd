ALTER TABLE credentials DROP CONSTRAINT credentials_kind_check;
ALTER TABLE credentials ADD CONSTRAINT credentials_kind_check CHECK (kind IN ('git-ssh','git-https','kubernetes-token','kubeconfig','publishing','publishing-database','helm-registry'));
-- Registry location a helm-registry credential applies to, such as
-- oci://registry.example.com/charts or https://charts.example.com/stable.
ALTER TABLE credentials ADD COLUMN endpoint TEXT NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN helm_chart JSONB;
ALTER TABLE application_groups ADD COLUMN helm_chart JSONB;
