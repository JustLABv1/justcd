---
title: Application controls and policies
---

## Find the right control

Work within the selected workspace. The available actions depend on your workspace
role, the application's state, and the selected Kubernetes credential's permissions.

| Where | What to do |
| --- | --- |
| Workspace connections | Add and test Git and cluster connections; bind deployment namespaces and credentials. |
| Applications | Create an application, or inspect applications discovered from Git. |
| Application overview | Build a plan, inspect drift and health, and review reconciliation status. |
| Plan review | Read the commit, destination, resource diff, ownership conflicts, and policy findings before approval or apply. |
| Approvals | Review requests for the exact plan; expired or changed plans need fresh review. |
| Topology | Inspect managed resources and observed descendants; use the resource actions described below. |
| Application Pull requests | Enable provider reporting, review PR plans, and manage preview settings. |
| Test branch | Review a selected revision in the existing environment or create an isolated preview. |
| Application history and activity | Follow deployment progress and inspect failures before retrying. |
| Workspace settings | Manage access, deployment schedules, approval rules, and deployment guardrails. |
| Audit | Inspect or export the actor, commit, plan, approval, and outcome evidence. |

For the complete sequence, follow [the delivery workflow](/guides/workflow).
[PR previews](/guides/pull-requests) and [branch tests](/guides/test-branch) have different
cleanup and return-to-source behavior; read their guides before deploying a test revision.
Deployment windows and freezes are described in
[deployment schedules](/guides/repository-applications#deployment-schedules).

## Pause and resource actions

Workspace owners can pause reconciliation from an application's detail page.
Pausing preserves its sync policy and rejects queued or running operations; wait
for the current operation to finish before making a manual hotfix. Automatic
sync stays paused until an owner selects **Resume reconciliation**. Explicit,
reviewed manual syncs are still available while paused.

The **Topology** tab offers actions for namespaced managed resources: rescale
Deployments and StatefulSets (including zero replicas), redeploy Deployments,
StatefulSets and DaemonSets by restarting their current pod template, and delete
live resources. Every action pauses the whole application before changing
Kubernetes, checks ownership and resource identity, and records its outcome in
activity and the audit trail. Sync stays paused after success or failure.
Resuming can replace manual changes with Git's desired configuration.

## Deployment policy guardrails

Workspace owners configure deployment guardrails in workspace settings or with
`GET`/`PUT /api/v1/workspaces/{workspaceID}/guardrail-policy`:

```json
{
  "mode": "block",
  "forbiddenNamespaces": ["kube-system"],
  "disabledRules": []
}
```

All five built-in checks are enabled by default: `privileged-containers`
(privileged containers and host network/PID/IPC access), `mutable-image-tags`
(images must use a SHA-256 digest), `missing-resource-limits` (positive CPU and
memory limits on regular and init containers), `public-services` (LoadBalancer,
NodePort, or externalIPs), and `forbidden-namespaces` (exact namespace names).
Pod, Deployment, StatefulSet, DaemonSet, ReplicaSet, ReplicationController, Job,
and CronJob manifests are supported. Ephemeral containers are checked for
privilege and image pinning; Kubernetes does not allow resource limits on them.
Use `disabledRules` to opt out of a specific check.

The default mode is `audit`, which records findings without preventing deployment.
`warn` also highlights findings in review. `block` prevents approval and apply
while any enabled rule has findings, including for already-approved plans.
Configuration changes invalidate current plans in that workspace; queued and
running operations recheck the policy before starting each resource step.
A resource step already started can finish, and completed changes remain applied.
Deletion-only plans evaluate no desired resources so remediation by deletion
remains possible.

Plan API responses expose `plan.policyEvaluation` with the configuration digest,
mode, and findings (resource, rule, evidence, remediation). These fields are bound
to the plan digest and included in plan creation audit events. Evidence uses
field paths instead of manifest values. The pure `core.PolicyEngine` interface
provides an extension boundary for future local CEL or OPA/Rego adapters; no
custom policy runtime is enabled. Engines receive desired manifests and policy
configuration without access to stored credentials, database handles, or cluster
clients, and must remain deterministic and unable to perform network or mutation
operations.

See [notification integrations](/guides/notifications) for event routing, providers and delivery troubleshooting.
See [security hardening](/operations/security-hardening) for production settings, sessions, MFA, rate limits and encryption-key rotation.

Audit exports, deployment evidence, schema versioning, and workspace retention are documented in [Audit compliance](/operations/audit-compliance).
