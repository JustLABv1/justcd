# JustCD cluster agent

Stable `vX.Y.Z` releases publish this chart to
`oci://ghcr.io/justlabv1/charts/justcd-agent` and attach its package to the
GitHub release, after the images have been published. Packaged chart versions
use `X.Y.Z` and default to `ghcr.io/justlabv1/justcd:agent-vX.Y.Z`.

```sh
helm upgrade --install justcd-agent oci://ghcr.io/justlabv1/charts/justcd-agent \
  --version X.Y.Z --namespace justcd-agent --create-namespace -f agent-values.yaml
```

Replace `X.Y.Z` with a published version. Set `serverUrl`, `enrollmentSecret`,
and the explicit workspace/namespace profiles in `agent-values.yaml` as
described below. Omit image overrides to use the packaged release image.
Private GHCR packages require `helm registry login ghcr.io` with package read
access. See [chart publishing](../justcd/README.md#published-releases) for
workflow and package visibility configuration.

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
