---
title: "Migrate a production cluster from a stored token to managed credentials"
---

This runbook replaces a direct Kubernetes connection using a stored cluster-wide
token with an outbound agent and short-lived, namespace-specific deployment
credentials. It also enables automatic provisioning for new namespaces. Existing
direct connections are unchanged until you explicitly enable the agent.

The switch applies to the **entire JustCD cluster connection**, including all
applications and workspaces using it. It is not a per-application rollout. Running
workloads continue running, but schedule a deployment maintenance window: enabling
the agent immediately changes routing and invalidates plans, before the agent has
enrolled. There is no automatic fallback to the stored token.

For the permission model, ownership rules and OpenShift details, see
[managed deployment credentials](/operations/managed-deployment-credentials).

## 1. Inventory and prepare the change

Before changing the production connection:

- Record the existing connection's API endpoint, CA configuration, credential
  references, workspace shares and namespace bindings. Keep the old credential
  valid and securely available for the initial rollback window; do not copy it
  into the agent or commit it to Git.
- Back up the JustCD database and retain existing encryption-key material. Deploy
  the backend/frontend release containing this feature. Backend startup applies
  migration `057_managed_namespace_environments` automatically. Use the matching
  agent image and chart; an older agent does not implement this lifecycle.
- Inventory **all** destination namespaces and resource kinds, including rendered
  Helm resources, hooks, preview namespaces, Secrets, custom resources and workload
  ServiceAccounts. The deployment role must cover reads, dry-run, apply, health,
  pruning and rollback for the resources your applications actually use.
- Resolve cluster-scoped manifests separately. Managed profiles do not deploy
  Namespaces, CRDs, ClusterRoles or other cluster resources as ordinary workloads,
  and cannot coexist with a cluster-scope profile to bypass that restriction.
  Have an administrator or a separate, deliberately authorized connection manage
  these resources. Remove their management from these applications using the
  reviewed keep/orphan workflow where appropriate; do not accidentally prune them.
- This mode requires one workspace per managed profile. Automatic provisioning
  requires a dedicated, single-profile agent. A connection shared across workspaces
  needs a separate connection/agent design before migration; do not simply omit
  other workspaces from a production connection they still use.
- Verify Kubernetes 1.30+ and admission-policy support, outbound HTTPS from the
  agent to JustCD, DNS/proxy settings, persistent storage and image availability.
  Protected namespaces (`default`, Kubernetes system namespaces and the agent's
  namespace) cannot be deployment targets in this mode.

Test the same configuration on a separate test connection/cluster before the
production cutover. Do not have two connections reconcile the same production
resources concurrently.

## 2. Install an approved deployment role

A cluster administrator creates this role. The agent only binds it inside each
namespace; it cannot edit the role. Save the following as
`justcd-approved-deployer.yaml` and adjust it to your reviewed workload inventory:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: justcd-approved-deployer
rules:
  - apiGroups: [""]
    resources: [pods, services, configmaps, persistentvolumeclaims]
    verbs: [get, list, create, update, patch, delete]
  - apiGroups: [apps]
    resources: [deployments, statefulsets, daemonsets, replicasets]
    verbs: [get, list, create, update, patch, delete]
  - apiGroups: [batch]
    resources: [jobs, cronjobs]
    verbs: [get, list, create, update, patch, delete]
  - apiGroups: [networking.k8s.io]
    resources: [ingresses, networkpolicies]
    verbs: [get, list, create, update, patch, delete]
```

```sh
kubectl apply -f justcd-approved-deployer.yaml
```

This example excludes Secret access, ServiceAccount/RBAC management, token issuance,
cluster resources and custom resources. Add only required workload kinds and verbs.
Do not use `cluster-admin`, wildcards, `bind`, `escalate` or `impersonate`. Because
this is an existing administrator-supplied role, reviewing its contents is your
responsibility. A ClusterRole bound using a **RoleBinding** grants its namespaced
permissions only in that namespace; do not create a deployment ClusterRoleBinding.

## 3. Prepare every existing application namespace

The automatic provisioner does not adopt existing namespaces or create/repair
their deployment RBAC. Prepare each existing destination explicitly, even when its
application already specifies `createNamespace: true`.

For an existing namespace named `prod`, save this as `prod-justcd-deployer.yaml`:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: justcd-deployer
  namespace: prod
automountServiceAccountToken: false
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: justcd-deployer
  namespace: prod
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: justcd-approved-deployer
subjects:
  - kind: ServiceAccount
    name: justcd-deployer
    namespace: prod
```

Inspect any existing objects with these names first. Resolve conflicts deliberately;
do not overwrite another controller's objects or add JustCD ownership annotations.
Then apply the reviewed manifest and repeat for every existing destination:

```sh
kubectl apply -f prod-justcd-deployer.yaml
```

Review existing workloads before enabling namespace-wide admission restrictions.
Once they are compatible, configure Pod Security Admission:

```sh
kubectl label namespace prod \
  pod-security.kubernetes.io/enforce=restricted \
  pod-security.kubernetes.io/enforce-version=latest --overwrite
```

The chart's workload admission policy also limits Pod ServiceAccounts and Secret
references. Inventory controller-created Pods as well as standalone Pods: existing
Pods can keep running while replacement Pods are rejected. Explicitly allow required
application ServiceAccounts and Secrets in the values below, and prepare workload
ServiceAccounts separately. Never allow Pods to use `justcd-deployer`. These checks
affect other clients deploying into matching namespaces too.

These existing namespaces remain administrator-prepared/shared environments and
are preserved on application deletion, even if namespace deletion is later enabled.

## 4. Prepare and render the agent configuration

Create a dedicated installation namespace, here `justcd-agent`. Save the following
as `agent-values.yaml`, replacing the URL, image tag and workspace ID. Include every
existing destination as an exact name; prefixes authorize **new** namespaces and
do not authorize adopting existing ones.

```yaml
serverUrl: https://justcd.example.com
enrollmentSecret: justcd-agent-enrollment
image:
  repository: ghcr.io/justlabv1/justcd
  tag: agent-vX.Y.Z # Replace with a release containing this feature.
profiles:
  - name: default
    workspaceIds: [YOUR_JUSTCD_WORKSPACE_ID]
    namespaces: [prod, "preview-*"]
    clusterScope: false
    deploymentCredentials:
      lifetimeSeconds: 600
      audience: ""
      provisioning:
        platform: kubernetes
        deploymentClusterRole: justcd-approved-deployer
        deleteNamespaces: false
rbac:
  create: true
  clusterWide: false
namespaceProvisioningPolicy:
  enabled: true
workloadPolicy:
  enabled: true
  allowedServiceAccounts: [default]
  allowedSecrets: []
```

Do not set `kubernetes.tokenSecret` or a profile `tokenFile`: this installation uses
the agent Pod's projected ServiceAccount credential for provisioning and issuance.
It requests deployment tokens from Kubernetes and keeps them in memory. For an
unreleased build, build/push an image from this change and set its repository/tag
as described in [agent installation](/operations/cluster-agents#install).

For OpenShift, set `platform: openshift` and have an administrator prepare the
ProjectRequest template, including required Pod Security labels. A rejected
ProjectRequest does not fall back to bare Namespace creation. Keep platform-owned
RBAC and annotations intact.

Render and inspect before switching production:

```sh
helm lint ./charts/justcd-agent --values agent-values.yaml
helm template private-cluster ./charts/justcd-agent \
  --namespace justcd-agent --values agent-values.yaml > agent-rendered.yaml
```

Review the helper's restricted `bind` permission, named-account token issuance,
namespace allowlist and admission policies. In automatic mode the chart creates
the agent's provisioner/issuer RBAC, but **does not** prepare the deployment account
or binding in existing namespaces; that was step 3.

If upgrading an already installed agent, preserve its release name, namespace,
identity PVC and cluster identity. Use complete reviewed values rather than
`--reuse-values`, which can retain an old privileged token or broad profile.
Preserve the existing `kubernetes.clusterId` if configured; introducing a different
ID breaks the enrollment identity binding. Do not delete the identity volume:
it also contains the namespace ownership ledger.

## 5. Switch the connection during the deployment window

1. Pause automatic sync/preview creation and wait for queued/running operations
   across this cluster to finish. Enabling or re-enrolling an agent is rejected
   while those operations remain active.
2. In **Connections → Clusters**, open the existing production connection's
   **Set up agent connection** dialog. Use `default` as **Namespace access profile**
   and leave **Cluster-scope profile** empty.
3. Choose **Enable agent connection** and create an enrollment token. **This is the
   cutover point**: new Kubernetes requests use the agent immediately. The old
   stored credential is retained but ignored, including namespace overrides.
4. Save the one-use token to a private local file named `enrollment-token`, without
   putting it in shell history or committed values. It expires after ten minutes.
   Create/update its Secret and install the agent:

   ```sh
   kubectl create namespace justcd-agent --dry-run=client -o yaml | kubectl apply -f -
   kubectl -n justcd-agent create secret generic justcd-agent-enrollment \
     --from-file=enrollmentToken=./enrollment-token --dry-run=client -o yaml \
     | kubectl apply -f -
   helm upgrade --install private-cluster ./charts/justcd-agent \
     --namespace justcd-agent --values agent-values.yaml
   kubectl -n justcd-agent rollout status deployment/private-cluster-justcd-agent
   rm ./enrollment-token
   ```

5. Wait for **Agent online**. Check **Reported local profiles** shows short-lived
   credentials, the approved role and namespace deletion disabled. Existing
   namespace bindings/workspace authorization must still be present in JustCD.
6. Refresh plans before deploying. For an existing agent, create a fresh enrollment
   token after profile changes so the backend receives the current policy metadata;
   replace the enrollment Secret and restart the Deployment if needed. Re-enrollment
   revokes the previous JustCD agent identity, so do it in the same maintenance window.

## 6. Validate before resuming automation

Run namespace connection verification in JustCD for **every** existing destination.
An online heartbeat proves connectivity to JustCD, not Kubernetes deployment access.
Inspect credential-issuance availability and deployment permission results separately.
Refresh a representative application's plan, review its changes, deploy normally
and confirm health. Existing applications and workloads do not need recreation.

As an administrator with impersonation permission, check the example RBAC:

```sh
# Expected: yes in prod; no in an unrelated namespace without a binding.
kubectl auth can-i create configmaps -n prod \
  --as=system:serviceaccount:prod:justcd-deployer
kubectl auth can-i create configmaps -n default \
  --as=system:serviceaccount:prod:justcd-deployer
# Expected: no for the example deployment role.
kubectl auth can-i create rolebindings.rbac.authorization.k8s.io -n prod \
  --as=system:serviceaccount:prod:justcd-deployer
kubectl auth can-i get secrets -n prod \
  --as=system:serviceaccount:prod:justcd-deployer
# Expected: yes for justcd-deployer, no for default.
kubectl auth can-i create serviceaccounts/justcd-deployer --subresource=token -n prod \
  --as=system:serviceaccount:justcd-agent:private-cluster-justcd-agent
kubectl auth can-i create serviceaccounts/default --subresource=token -n prod \
  --as=system:serviceaccount:justcd-agent:private-cluster-justcd-agent
```

These commands check RBAC only, not local agent policy, admission or actual token
execution. For an actual minted-token test on a disposable cluster, use the
[opt-in real-cluster test](/operations/managed-deployment-credentials#installation-migration-and-validation).
Do not print or paste deployment tokens for diagnosis.

Finally, use a disposable application with a workspace-authorized namespace binding
and an unused name such as `preview-migration-check`. Set its destination to:

```yaml
spec:
  destination:
    cluster: production # Use your existing JustCD connection name.
    namespace: preview-migration-check
    createNamespace: true
```

Refresh the plan. Before planning workloads, the agent should create the namespace,
its `justcd-deployer` ServiceAccount and RoleBinding. Inspect **Namespace environments**
for readiness and recorded resources, then deploy a harmless test workload. A failed
plan can leave the provisioned namespace in place for inspection.

Keep `deleteNamespaces: false` during migration. Application resource deletion
preserves the test namespace in this configuration. An administrator can remove
the disposable namespace after checking its contents/UID. If you later enable
automatic namespace deletion, update Helm and re-enroll, then validate a **new**
exclusive test environment end to end. Only namespaces JustCD created exclusively
for the environment qualify; shared/pre-existing namespaces stay intact. Finalizers
can leave deletion pending and must not be forcibly removed by JustCD.

## 7. Retire the old cluster-wide token

Resume automation only after all destination checks and representative deployments
pass. Observe at least a token refresh interval and another deployment before
closing the rollback window. Then remove unneeded stored credential references and
retire the old token at its Kubernetes source using your credential-management
process. Deleting a JustCD credential record alone does not revoke a Kubernetes
token. Check whether other tools share that identity before removing its RBAC or
ServiceAccount. Keep the API endpoint/CA settings if they are still useful.

The three credentials have different purposes: the one-use enrollment token connects
the installation to JustCD; the agent's projected Kubernetes credential authorizes
provisioning/issuance; short-lived `justcd-deployer` tokens authorize workload
operations in one namespace. The old production token is no longer required.

## Roll back the connection

Before retiring the old token, rollback is an explicit connection-mode change:

1. Pause new sync/preview work and let queued/running operations settle. Resolve
   any namespace environment already in `deleting` with the managed agent before
   switching; changing connection mode does not cancel Kubernetes deletion.
2. Open **Agent connection → Use direct connection** and confirm. This revokes the
   agent identity and restores direct use of the saved endpoint and credentials.
   **Revoke agent** alone leaves agent mode enabled and is not a rollback.
3. Verify namespace/cluster access, refresh plans and resume deployment automation.
   After retiring the old token, supply a valid direct credential first.

The mode switch does not uninstall Helm, remove admission policies, revert Pod
Security labels, remove prepared accounts/RBAC or delete provisioned namespaces.
Review those changes separately if they caused the problem. Preserve the agent
PVC/ownership ledger and keep the approved deployment role while it is still bound.
Do not uninstall the chart as the first recovery step. Reverting the connection mode
does not require reversing database migration 057 or downgrading JustCD.

Revoking the JustCD agent identity stops new tasks; already claimed requests may
finish, and issued Kubernetes tokens can remain valid until their granted expiry.
For immediate Kubernetes access removal, remove the deployment RoleBinding or
ServiceAccount deliberately; revoking enrollment alone is insufficient.
