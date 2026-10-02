# Private cluster connections

JustCD supports direct Kubernetes API connections and outbound cluster agents.
An agent runs inside each private target cluster and connects to JustCD over HTTPS.
The target Kubernetes API needs no public endpoint or inbound tunnel. The target
must be able to reach JustCD; its outbound proxy, DNS, and HTTPS trust must allow it.

Git discovery, rendering, plans, deployment policy, approval, and operation queues
remain in central JustCD. Kubernetes discovery, permission checks, live reads,
dry-run, apply, health, resource actions, and reviewed cleanup use the same client
interfaces through the agent. Git credentials remain central; Kubernetes service
account tokens remain local. Branch tests and PR deployments use this connection
without a separate approval mechanism.

## Install

1. In **Connections → Clusters → Connect a cluster**, choose **Outbound cluster
   agent**. Save a name, then open **Agent** to generate an enrollment token.
   Existing clusters can use **Set up agent**. Only the cluster owner or an
   administrator managing a legacy cluster can enable, re-enroll, or revoke it.
2. Copy the one-use token to a local file `enrollment-token`. It expires in ten
   minutes. Keep it out of committed values and shell history.
3. Create the agent namespace and enrollment Secret in the target cluster:

   ```sh
   kubectl create namespace justcd-agent
   kubectl -n justcd-agent create secret generic justcd-agent-enrollment \
     --from-file=enrollmentToken=./enrollment-token
   ```

4. Create `agent-values.yaml` (replace the workspace ID with the ID displayed in
   the setup dialog):

   ```yaml
   serverUrl: https://justcd.example.com
   enrollmentSecret: justcd-agent-enrollment
   profiles:
     - name: default
       workspaceIds: [YOUR_JUSTCD_WORKSPACE_ID]
       namespaces: [dev]
       clusterScope: false
   ```

   The target namespace `dev` must exist for the chart's namespace RoleBindings.
   Kubernetes 1.30 or newer is required. Before deploying workloads, enable Pod
   Security Admission in each target namespace:

   ```sh
   kubectl label namespace dev \
     pod-security.kubernetes.io/enforce=restricted \
     pod-security.kubernetes.io/enforce-version=latest --overwrite
   ```

   The chart's admission policy rejects Pods in managed namespaces without these
   labels. Ensure Pod Security Admission is enabled and has no exemption for your
   workload users/namespaces. Existing Pods are not retroactively remediated.
   The policy also affects controller-created Pods and other clients in these
   namespaces, so review existing workloads before upgrading.
   The chart creates namespace-scoped workload permissions, API discovery and
   access-review permissions, and permission to read the `kube-system` namespace
   UID to identify the physical cluster unless `kubernetes.clusterId` is configured. Customize `rbac.rules` for your workload
   kinds. With `rbac.create: false`, provide equivalent RBAC yourself.

5. Install the chart:

   ```sh
   helm upgrade --install private-cluster ./charts/justcd-agent \
     --namespace justcd-agent --values agent-values.yaml
   ```

   The release workflow publishes `ghcr.io/justlabv1/justcd:agent` and
   `agent-vX.Y.Z`. For an unreleased build, build and push the image to your own
   registry and set `image.repository` and `image.tag` in your values:

   ```sh
   docker build -f services/backend/Dockerfile.agent \
     -t YOUR_REGISTRY/justcd-agent:dev services/backend
   docker push YOUR_REGISTRY/justcd-agent:dev
   ```

6. Wait for **Agent online**, bind the deployment namespace in JustCD, and run
   the normal connection verification. Refresh plans created before the switch.

For an internal HTTPS CA, create a Secret containing `ca.crt` and set
`serverCASecret` to its name. TLS verification cannot be disabled for the agent's
connection to JustCD. The chart creates a persistent identity volume by default;
keep it during upgrades. `persistence.existingClaim` can select an existing PVC.
The agent uses a single replica with a Recreate strategy. Set `imagePullSecrets` for a private image registry, or `extraEnv` for outbound proxy settings. Include the local Kubernetes API and service addresses in `NO_PROXY` when using a proxy.

## Local authority and cluster scope

Profiles are configured by the target-cluster operator. Central application
configuration and feature branches cannot expand them. Each profile requires
explicit workspace IDs. When sharing a cluster, add the receiving workspace ID
locally as well as accepting the cluster share in JustCD.

The connection's namespace profile defaults to `default`. Its namespace list
accepts exact names, trailing patterns such as `preview-*`, and `*` for all
non-protected namespaces. The local profile
and Kubernetes RBAC must both permit a request. Namespaced profiles cannot create
or delete namespaces, read resources across all namespaces, or access pod exec,
logs, attach, port forwarding, API proxy endpoints, or watches.

For reviewed namespace creation, isolated previews, or cluster-scoped resources,
configure another local profile with `clusterScope: true`, set its name as the
**Cluster-scope profile** in JustCD, and explicitly enable appropriate cluster-wide
RBAC. `rbac.clusterWide: true` grants the configured `rbac.rules` cluster-wide;
review and narrow those rules before enabling it. Namespace RoleBindings cannot
authorize future namespaces. A cluster-scope profile is deliberately broader and
allows cluster resources and namespaced preview setup/cleanup for its configured
workspace IDs. A namespace profile alone does not grant these operations.

Advanced installations can set a profile's `tokenFile` to a locally mounted token
for another service account. Use `extraVolumes` and `extraVolumeMounts` to mount these local Secrets. Such files are
never downloaded from JustCD. The chart's default profiles use its service account.

Updating local profiles rolls the Deployment. Re-enroll to update
reported profile metadata. Local configuration remains authoritative even if the
central reported metadata is stale. Keep the persistent identity file private.

## Identity, tasks, and disconnects

Enrollment is bound to one JustCD cluster connection and either the immutable
`kube-system` UID or an operator-assigned `kubernetes.clusterId`. Re-enrollment
cannot change that identity binding. The enrollment token is consumed atomically; central storage keeps only
identity token hashes. Agent identities expire in 30 days and renew automatically
with seven days remaining. A five-minute previous-token grace window allows a
lost renewal response to recover. Update the enrollment Secret with a fresh token to re-enroll an installation with an existing identity PVC; the agent exchanges it when its old identity is rejected. Revocation blocks new work immediately;
already executing Kubernetes requests may finish.

Requests contain a fixed Kubernetes API path, method, workspace, local profile,
namespace scope, deadline, and body. Deployment requests carry operation ID, plan
ID, and plan digest for correlation. They contain no arbitrary URL, shell command,
or Kubernetes authentication headers. Request and result bodies are encrypted at
rest using JustCD's configured encryption key, consumed results are removed, and
expired tasks are pruned after five minutes. Request and response bodies are
limited to 8 MiB each; requests expire after at most 30 seconds.

An agent claims each task once using a lease. It retries delivery of the result,
not Kubernetes execution. A lost write result fails the central operation with an
unknown-outcome error. Existing operation recovery and policy must produce a fresh
plan and re-read live state before another deployment; claimed tasks are never
replayed after a restart or network interruption. After 60 seconds without a
heartbeat the connection reports offline and no new task is queued. Agent mode
never automatically falls back to direct credentials. An owner can explicitly
switch back to direct API access and refresh plans.

This first implementation uses bounded HTTPS polling and one sequential execution
stream per cluster agent. It does not support streaming Kubernetes APIs, agent HA,
or local Git rendering. Central JustCD and its deployment authorization are the
trusted task issuer; the agent enforces local cluster, workspace, namespace, API,
and RBAC bounds rather than independently issuing deployment approvals.

## Existing Kubernetes token Secret

The enrollment Secret authenticates the installation to JustCD. A Kubernetes
service-account token is separate and stays inside the target cluster. Mount an
existing Secret with a `token` key and select its file in a local profile:

```yaml
profiles:
  - name: default
    workspaceIds: [YOUR_JUSTCD_WORKSPACE_ID]
    namespaces: [dev]
    clusterScope: false
  - name: cluster-admin
    workspaceIds: [YOUR_JUSTCD_WORKSPACE_ID]
    namespaces: []
    clusterScope: true
    tokenFile: /etc/justcd-agent-kubernetes/token
extraVolumes:
  - name: kubernetes-token
    secret:
      secretName: justcd-agent-kubernetes-token
extraVolumeMounts:
  - name: kubernetes-token
    mountPath: /etc/justcd-agent-kubernetes
    readOnly: true
```

Create the Secret in the Helm release namespace from a local token file:

```sh
kubectl -n justcd-agent create secret generic justcd-agent-kubernetes-token \
  --from-file=token=./kubernetes-token
```

Set **Cluster-scope profile** to `cluster-admin` in JustCD. The token's service
account must have the intended Kubernetes RBAC permissions. The chart's
`rbac.clusterWide` configures its own service account; it does not change the
permissions of a token supplied by Secret. The first configured profile is used for enrollment cluster identification; its
token must also be allowed to get the `kube-system` namespace unless
`kubernetes.clusterId` is configured. Profiles without
`tokenFile` use the Pod service account unless `kubernetes.tokenSecret` is set.
Secret volumes update in place; do not mount the token using `subPath` if it needs
to rotate. A projected service-account token is also supported via `extraVolumes`
when the profile uses the Pod's service account.

## Custom Kubernetes CA and token for all application namespaces

Set the Kubernetes CA and token independently of `serverCASecret`, which trusts
only the JustCD HTTPS server. Both existing Secrets must be in the Helm release
namespace: the CA Secret needs a `ca.crt` key and the token Secret a `token` key.

```yaml
serverUrl: https://justcd.example.com
enrollmentSecret: justcd-agent-enrollment
kubernetes:
  clusterId: production-cluster-01
  serverUrl: https://kube-api.example.com:6443
  caSecret: my-kubernetes-ca
  tokenSecret: my-kubernetes-token
rbac:
  create: false
profiles:
  - name: default
    workspaceIds: [YOUR_JUSTCD_WORKSPACE_ID]
    namespaces: ["*"]
    clusterScope: true
```

`kubernetes.serverUrl` overrides the endpoint discovered from the Pod environment.
Omit it to use the in-cluster endpoint. The configured endpoint must use HTTPS
and its certificate must match the endpoint hostname.

With `kubernetes.clusterId` set, startup and enrollment make no Kubernetes
identity lookup and need no access to `kube-system`. Choose a unique, stable ID
for each cluster (at most 128 bytes) and retain it across upgrades. This ID is
operator-assigned: it cannot detect a different physical cluster if you reuse the
same ID. Without this setting, the agent still reads the `kube-system` namespace UID.

An existing enrolled installation must use its previously stored cluster UID as
`clusterId` to preserve its identity binding. A different ID requires a new JustCD
cluster connection and a new agent identity volume; re-enrollment alone does not
change the binding.

This uses the supplied token for all requests in the default profile. An explicit profile `tokenFile` overrides the shared token.
Set **Cluster-scope profile** to `default` in JustCD. The existing token's RBAC must
allow namespace creation and management of the intended resources. The chart does not grant this token additional
permissions. Kubernetes CA trust is applied to every profile, with TLS verification
remaining enabled. Secret mounts support file rotation without `subPath`.

The chart still protects its own namespace and Kubernetes system namespaces.
Its workload admission policy still requires restricted/latest Pod Security labels
in application namespaces; see the policy configuration above.

## Security contexts

`podSecurityContext` and `securityContext` are configurable Helm values. Their
defaults preserve non-root UID/GID 10001, RuntimeDefault seccomp, a read-only root
filesystem, disabled privilege escalation, and dropped capabilities. Override
UID/GID or `fsGroup` to match your cluster and persistent-volume requirements.
Set individual fields to `null` to omit fields imposed by cluster policy.

## Agent overview and activity

**Connections → Clusters** shows clusters and their agent connections
together in one searchable list. Each agent-connected cluster shows last heartbeat,
version, local profiles, workspace-scoped queued/running tasks, and its last request
failure. **Agent activity** shows
the latest 100 tasks from the last seven days, including Kubernetes resource type,
namespace, profile, HTTP status, safe error code, and links to deployment activity.
All counts and activity are scoped to the current workspace, including shared
clusters. Profile metadata is filtered to that workspace.

Only metadata is retained: Kubernetes request/response bodies, resource names,
query parameters, tokens, and arbitrary error text are excluded. Consumed encrypted
payloads are still deleted. Expired unclaimed tasks are shown as expired; claimed
tasks with no confirmed result are shown as unknown outcome. Heartbeat online
status indicates connectivity to JustCD, not successful Kubernetes access.
The overview lists one identity per connection; multiple replicas and HA remain
unsupported.

## Security defaults and upgrades

Chart 0.2 replaces wildcard RBAC with explicit common workload resources. It does
not grant RBAC management, ServiceAccount mutation, node access, admission-policy
management or arbitrary custom resources. Add only the resources your reviewed
workloads require to `rbac.rules`; namespace creation for previews now needs an
explicit namespace rule as well as `clusterWide: true` and a cluster-scope profile.
A token mounted through `tokenFile` retains its external Kubernetes permissions:
operator-managed credentials must follow the same least-privilege requirements.

Install the agent in a dedicated namespace. The generated local profiles protect
that namespace and Kubernetes system namespaces through `deniedNamespaces`, even
for cluster-scope requests. Cross-namespace collections of common namespaced
resources (including Secrets) are also blocked to prevent reading protected
namespace contents through a cluster-wide list. Other locally supplied profiles can also configure
this denylist. A cluster-wide chart policy applies to non-system namespaces outside
its own installation namespace; namespace-scoped policy uses configured exact
names and `preview-*` prefixes.

`workloadPolicy.enabled` defaults to true. Its ValidatingAdmissionPolicy requires
restricted/latest Pod Security labels, permits only `default` as a workload
ServiceAccount, and denies Secret references unless listed in
`workloadPolicy.allowedSecrets`. This includes regular/init container environment
references and direct/projected Secret volumes. Extend
`workloadPolicy.allowedServiceAccounts` deliberately, ensuring each account has
minimal permissions. Review the default ServiceAccount's RBAC too. Keep the agent's
enrollment, credential Secrets and identity PVC outside workload namespaces.
PVC and ServiceAccount access remain Kubernetes trust boundaries; admission rules
cannot make an intentionally privileged credential safe.

Example explicit application permissions:

```yaml
workloadPolicy:
  enabled: true
  allowedServiceAccounts: [default, application-reader]
  allowedSecrets: [application-database]
```

A cluster administrator may disable the bundled policy only when equivalent
admission enforcement is provided separately. The chart never grants permission
to remove its policy to the agent. Pod Security enforcement and Secret/ServiceAccount
allowlists intentionally reject some previously accepted workloads on upgrade.
See [Kubernetes ValidatingAdmissionPolicy](https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/)
and [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/).

Agent token rotation preserves the original five-minute old-token grace deadline.
Task claims recheck token validity and revocation under a database row lock; tasks
already claimed before revocation may still finish. Each backend process permits
one active poll per cluster and 120 poll starts/minute, with one-second database
poll intervals. Enrollment is limited to ten attempts/minute per direct peer IP.
Rate/concurrency limits are process-local; deployments with multiple replicas
should additionally enforce shared ingress limits. Password-login throttling uses
normalized account names rather than the frontend proxy's shared address; it still
limits targeted attempts against one account to eight per fifteen minutes.
