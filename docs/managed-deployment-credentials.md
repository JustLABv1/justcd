# Managed namespace deployment credentials

This opt-in agent mode uses Kubernetes TokenRequest credentials for the dedicated
`justcd-deployer` ServiceAccount in each deployment namespace. Tokens stay inside
the agent. Workload discovery, reads, access reviews, dry-run, apply, pruning and
rollback never use the provisioning identity or fall back to stored credentials.
Existing direct-token/kubeconfig connections and namespace overrides keep their
current behavior. Automatic managed provisioning is agent-only.

## Automatic namespace provisioning

A cluster operator authorizes the agent and an approved deployment ClusterRole
once. Application configuration can request namespace creation, but cannot choose
an account, role, audience, token lifetime, or expand the local namespace policy.

For example, this repository definition requests a missing namespace:

```yaml
apiVersion: justcd.io/v1alpha1
kind: Application
metadata:
  name: shop
spec:
  source:
    renderer: yaml
    path: manifests
  destination:
    cluster: production
    namespace: preview-shop
    createNamespace: true
  syncPolicy: manual
```

`spec.destination.createNamespaces: true` remains supported. The singular spelling
is an alias; both normalize to the same definition hash. Conflicting flags are
rejected. The destination still needs a workspace binding, or the existing
owner-authorized repository discovery policy that permits registering it.

Before workload planning, JustCD registers an environment and asks the agent to:

1. Check its local workspace, namespace and provisioning policy.
2. Create the missing Namespace with restricted/latest Pod Security labels.
3. Persist the namespace UID and workspace/application ownership in its private
   durable ledger, then create `justcd-deployer` with automount disabled.
4. Create a namespace-local RoleBinding named `justcd-deployer`, referencing only
   the administrator-approved deployment ClusterRole.
5. Report readiness. Minting and workload operations begin only after readiness.

Provisioning is separate from workload deployment: explicitly enabling local
provisioning and requesting `createNamespace` authorizes infrastructure setup
before planning. Workload apply/delete approvals, review gates, rate limits,
workspace authorization and operation leases still apply. A failed workload plan
can leave a provisioned environment for inspection and later cleanup.

Existing namespaces are never adopted or modified. To use a pre-existing namespace,
list its **exact name** in the local profile and prepare its deployment account/RBAC
as an administrator. A prefix match alone does not authorize adopting an existing
namespace. Existing account/binding conflicts are reported; the helper never edits
platform-owned RBAC or repairs an account whose recorded UID changed.

## Bootstrap and local policy

Use the normal [agent installation](cluster-agents.md), HTTPS enrollment and
persistent identity volume. Create the approved deployment ClusterRole as an
administrator. For example, a role for a ConfigMap-only application is:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: justcd-approved-deployer
rules:
  - apiGroups: [""]
    resources: [configmaps]
    verbs: [get, list, create, update, patch, delete]
```

Add explicit reviewed workload kinds/verbs as needed, including `resourcequotas`
for isolated preview quota setup. Avoid wildcard permissions. Do not grant Secrets
unless required, RBAC management, ServiceAccount mutation/token issuance,
impersonation, provisioning, `bind`, or `escalate` to deployment accounts.
TokenRequest does not attenuate the account's actual RBAC.

Install or upgrade the agent with:

```yaml
serverUrl: https://justcd.example.com
enrollmentSecret: justcd-agent-enrollment
profiles:
  - name: default
    workspaceIds: [YOUR_JUSTCD_WORKSPACE_ID]
    namespaces: [dev, "preview-*"]
    clusterScope: false
    deploymentCredentials:
      lifetimeSeconds: 600
      audience: "" # Kubernetes API default; otherwise use an accepted API audience.
      provisioning:
        platform: kubernetes
        deploymentClusterRole: justcd-approved-deployer
        deleteNamespaces: false
rbac:
  create: true
  clusterWide: false
namespaceProvisioningPolicy:
  enabled: true
```

Automatic provisioning requires a dedicated single-profile chart installation.
Managed profiles cannot overlap another profile or coexist with cluster-scope
workload profiles. There is one explicit workspace; namespace patterns can be exact
names or prefixes ending in `-*`. Protected namespaces, including `default`, the
agent installation namespace and Kubernetes system namespaces, are rejected.

Keep `justcd-deployer` out of `workloadPolicy.allowedServiceAccounts`. The existing
workload policy still constrains Pod service accounts, Secret references and Pod
Security. No privileged Kubernetes token needs to be copied into JustCD: the helper
uses its local projected/rotated Pod credentials. A locally supplied `tokenFile`
selects the issuer only, never the workload identity.

The **runtime provisioner** ClusterRole grants only:

| API resources | Verbs | Name restriction |
| --- | --- | --- |
| `namespaces` | `get`, `create`; optional `delete` | Namespace mutations restricted by local/admission policy |
| `serviceaccounts` | `create` | Fixed name enforced locally and at admission |
| `serviceaccounts` | `get` | `justcd-deployer` |
| `serviceaccounts/token` | `create` | `justcd-deployer` |
| `rolebindings` | `create` | Fixed object and role reference enforced locally/admission |
| `rolebindings` | `get` | `justcd-deployer` |
| `clusterroles` | `bind` | Only `provisioning.deploymentClusterRole` |

There are no runtime Role/ClusterRole writes, unrestricted `bind`/`escalate`, Secret
reads, list/watch, patches or updates. Deleting a managed namespace lets Kubernetes
remove its account/binding; retained namespaces keep their infrastructure.
The chart's existing identity role permits discovery/access reviews and reading
the `kube-system` UID unless `kubernetes.clusterId` is set.

The administrator installing the approved role/chart needs the corresponding
bootstrap authority, including authority to grant the approved role. Kubernetes
escalation protection still applies; creating RoleBindings alone is insufficient.
The runtime helper receives restricted `bind`, not administrator privileges.

Ordinary RBAC cannot limit top-level namespace creation to a prefix. With
`namespaceProvisioningPolicy.enabled`, the chart installs a fail-closed admission
policy for its helper ServiceAccount, restricting namespace targets, account names,
requested token lifetime, and the deployment RoleBinding's role/subject. The
helper remains a trusted provisioning identity: it can obtain every privilege of
every account for which Kubernetes permits token issuance. If using an external
issuer identity/token file, provide equivalent admission rules matching that
identity. Local checks alone do not constrain a compromised helper's Kubernetes
credentials. Requested lifetime ceilings cannot constrain a server-adjusted grant.

## Prepared namespaces without automatic provisioning

Omit `deploymentCredentials.provisioning` to retain issuance-only mode. Namespaces
must then be exact names, and must already exist. With `rbac.create: true`, Helm
creates the deployment account and namespace-local deployment/issuer RBAC once;
Helm never creates the namespace. `managedCredentials.rules` configures the
namespace Role, or `managedCredentials.deploymentClusterRole` selects an existing
approved ClusterRole through a namespace-local binding.

The minimal runtime issuer rules in each prepared namespace are:

```yaml
rules:
  - apiGroups: [""]
    resources: [serviceaccounts/token]
    resourceNames: [justcd-deployer]
    verbs: [create]
  - apiGroups: [""]
    resources: [serviceaccounts]
    resourceNames: [justcd-deployer]
    verbs: [get]
```

Bind these using a namespace RoleBinding. Account reads detect UID changes. No
namespace/RBAC creation or deletion permission is needed. With `rbac.create: false`,
prepare all resources externally. Helm refuses to adopt objects lacking its
ownership metadata; do not force ownership onto platform-managed resources.

## OpenShift

Set `provisioning.platform: openshift` to create missing environments using
`project.openshift.io/v1/ProjectRequest`. The helper gets `create projectrequests`
in place of `create namespaces`, plus the same named-account/binding permissions.
Discovery must match the explicitly configured platform. A denied ProjectRequest
never falls back to creating a bare Namespace.

Project templates/admission retain control of project labels, requester annotations
and platform RoleBindings. Configure the template to supply the required Pod
Security labels. The helper creates only its dedicated account/binding and refuses
conflicts; it never overwrites template-generated bindings. The platform may grant
the requesting helper additional project-admin access according to its template;
review that separately. Deployment operations still use the minted deployment
identity, not the project requester.

The ledger records the UID returned by successful ProjectRequest creation. Cleanup
checks the same underlying namespace UID without adding/changing platform ownership
annotations. Existing projects still require an exact local namespace entry and
administrator-prepared deployment RBAC. Existing static connections retain their
original ProjectRequest bootstrap behavior. See the [OpenShift ProjectRequest API](https://docs.redhat.com/en/documentation/openshift_container_platform/4.18/html/project_apis/projectrequest-project-openshift-io-v1).

## Ownership, deletion and recovery

Namespace deletion requires **both** operator `deleteNamespaces: true` and an
application deletion using `resources=delete`. Its default is false. The helper
must have created the namespace for that application, have its persisted UID, and
still find matching ownership. Pre-existing, shared, unknown or ownership-changed
namespaces are preserved or fail closed. Choosing to keep namespaces in the
reviewed decommission plan also preserves automatically provisioned namespaces.
`resources=keep` never requests namespace deletion.

Application deletion first completes reviewed workload cleanup. JustCD then checks
for active operations/previews and other applications referencing the namespace,
marks the environment deleting, and prevents new plans, queued deployments and
applications joining it. Once shared, a namespace never becomes exclusively owned
again merely because another app was removed. The agent serializes lifecycle and
workload requests per namespace; queued requests cannot mint credentials or deploy
after the deleting phase begins.

Deletion uses UID **and resourceVersion** preconditions. Accepted deletion stays
`deleting` until the namespace disappears; finalizers are not removed. Retry
application deletion to finish cleanup, or let preview cleanup retry. The application
record stays present while cleanup is pending. A namespace recreated with the same
name and a different UID is never deleted. Environments owned at earlier application
destinations are also considered during final application cleanup.

Ownership is recorded centrally and in the agent identity volume at
`identity.json.environments.json`, written atomically with private permissions.
Keep and back up this ledger with the identity PVC. It contains ownership metadata,
not deployment tokens. Partial account/binding failures retain namespace UID and
created-resource metadata, and retries resume create-only setup after restart.
A corrupt/missing ledger, changed UID, or ambiguous namespace creation whose UID
was not durably recorded requires administrator verification/restoration. The
helper never guesses ownership, overwrites RBAC, or recreates a missing owned
namespace under an old identity. Starting namespace deletion cannot be undone by
simply requesting a new deployment.

**Agent connection → Namespace environments** reports phase, failed step, reason,
created resources and an application link. Refresh the application plan after
fixing provisioning policy/RBAC to retry setup. Namespace self-tests distinguish
credential availability from deployment permissions; heartbeat health alone says
nothing about them. Provisioning-profile self-tests require initialized environments.

## Lifetime, revocation and auditing

Requested lifetimes are 600–3600 seconds. The cache uses Kubernetes' **returned**
expiration; empty tokens or tokens with at most 30 seconds remaining are rejected.
Refresh starts 30 seconds before expiry to cover the 25-second workload HTTP
budget. Every subsequent request in a long sync/rollback resolves credentials again.
The bounded memory cache is isolated by cluster/agent, profile, workspace,
namespace, ServiceAccount UID and audience. Concurrent refreshes are deduplicated;
refresh failure, helper outage and changed account UID fail closed. A workload 401
evicts that token and returns without replaying the mutation.

Revoking enrollment blocks new tasks; already claimed requests may finish. Agent
shutdown prevents refreshes from repopulating the cache. Kubernetes tokens already
issued are not necessarily revoked by a JustCD disconnect: their granted expiry
still applies. Remove the ServiceAccount to revoke that account's tokens, remove
deployment bindings to revoke workload access, and remove issuer grants to prevent
future minting. Do not assume a requested lifetime bounds a server-extended grant.

Task activity and lifecycle audits record application/workspace/namespace,
request/operation IDs, creation/cleanup decisions and errors. Issuance logs record
account UID and requested/granted expiration. No token values, upstream credential
response bodies or decoder diagnostics are exposed. Kubernetes details are
redacted on provisioning failure. See [ServiceAccount administration](https://kubernetes.io/docs/reference/access-authn-authz/service-accounts-admin/)
and [RBAC escalation protection](https://kubernetes.io/docs/reference/access-authn-authz/rbac/).

## Installation, migration and validation

For an existing production connection using a stored cluster-wide token, follow
the [step-by-step migration and rollback runbook](migrate-to-managed-credentials.md).
It covers prepared namespaces, the connection-wide cutover and retiring the old token.

1. Apply the approved deployment ClusterRole and review its exact workload rules.
2. Upgrade the backend (migration 057 adds lifecycle metadata/guards) and agent image.
3. Explicitly enable local provisioning values, retaining the identity/ownership PVC.
4. Re-enroll to refresh reported profile policy. Local policy remains authoritative.
5. Request `createNamespace: true` on an authorized bound destination, refresh the
   application plan, inspect environment readiness, then review/deploy normally.

Run automated checks against a disposable PostgreSQL database:

```sh
cd services/backend
go test ./...
go test -race ./internal/clusteragent ./internal/kube ./internal/agentprotocol
JUSTCD_E2E_DATABASE_URL='postgres://USER@HOST/DISPOSABLE_DB?sslmode=disable' \
  go test -tags integration ./internal/syncer -run TestIntegrationManagedNamespace -count=1
```

Those integration tests exercise actual encrypted agent tasks, missing-namespace
provisioning before planning, workload credential isolation, ownership persistence,
deletion retries and database concurrency guards against local Kubernetes fixtures.
Chart tests also evaluate admission expressions and named-account/restricted-bind
RBAC. Run them from `charts/justcd-agent/tests` with `go test -count=1 ./...`.

A separate opt-in real-cluster test actually mints helper/deployment tokens in two
temporary namespaces and checks allowed ConfigMap operations and forbidden
cross-namespace/Secret/RBAC/cluster/token operations:

```sh
JUSTCD_MANAGED_TEST_KUBECONFIG=/absolute/path/to/disposable-test-admin.kubeconfig \
  go test ./internal/clusteragent -run '^TestManagedCredentialsRealCluster$' -count=1 -v
```

It never uses the default kubeconfig or displays tokens, and requests fixture cleanup
using namespace UID preconditions. Validate the installed provisioning/admission
policy and any OpenShift templates on the target cluster before rollout. Actual
expiry timing and platform admission behavior require real-cluster validation;
unit tests exercise expiry, refresh, outages, concurrent requests, namespace UID
replacement, restart recovery, protected targets and safe finalizer waits.
