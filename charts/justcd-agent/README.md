# JustCD cluster agent

Installation, connection modes, local scope, and recovery behavior are documented
in [Private cluster connections](../../docs/cluster-agents.md).

Use an existing enrollment Secret; never commit enrollment tokens in Helm values.
The chart requires an HTTPS server URL and explicit local workspace IDs. Identity
storage is persistent, namespace RBAC is the default, and cluster-wide RBAC requires
explicit opt-in. The image is built with `services/backend/Dockerfile.agent` and
published as `ghcr.io/justlabv1/justcd:agent` by the release workflow.
