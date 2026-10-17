# JustCD cluster agent

Installation, connection modes, local scope, and recovery behavior are documented
in [Private cluster connections](../../services/docs/content/docs/operations/cluster-agents.md).

For the opt-in `profiles[].deploymentCredentials` mode, minimal issuer RBAC,
prepared-namespace bootstrap, automatic provisioning/deletion, migration and validation, see
[Short-lived namespace deployment credentials](../../services/docs/content/docs/operations/managed-deployment-credentials.md).

Migrating an existing stored cluster-wide token connection:
[production migration and rollback](../../services/docs/content/docs/operations/migrate-to-managed-credentials.md).

Use an existing enrollment Secret; never commit enrollment tokens in Helm values.
The chart requires an HTTPS server URL and explicit local workspace IDs. Identity
storage is persistent, namespace RBAC is the default, and cluster-wide RBAC requires
explicit opt-in. The image is built with `services/backend/Dockerfile.agent` and
published as `ghcr.io/justlabv1/justcd:agent` by the release workflow.
