ALTER TABLE credentials DROP CONSTRAINT credentials_kind_check;
ALTER TABLE credentials ADD CONSTRAINT credentials_kind_check CHECK (kind IN ('git-ssh','git-https','kubernetes-token','kubeconfig','publishing','publishing-database'));
