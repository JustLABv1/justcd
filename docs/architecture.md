# JustCD architecture

JustCD is a CRD-free, operator-free GitOps service. It reads desired Kubernetes
resources from Git, compares them with live resources, and applies reviewed
changes from outside the target cluster. It does not install a controller or
JustCD-specific CRDs into managed clusters. Existing CRDs in an application's
manifests are a separate, explicit capability.

## Core workflow

1. Register a cluster connection and a namespace-scoped credential reference.
2. Register an application: repository, immutable revision or tracked branch,
   manifest path, renderer, cluster, and one or more permitted namespaces.
3. Fetch and render in an isolated workspace. Planning must never mutate the
   cluster, including when rendering Helm charts that contain CRDs.
4. Discover live resources using the selected credential. Compute creates,
   updates, and deletion candidates. Only resources already owned by this
   application can become deletion candidates.
5. Show the complete plan, including the source commit, target, resource
   identities, diffs, and the credential identity used for each namespace.
6. Apply creates and updates with server-side apply. Deletions require a
   separate, short-lived approval for the exact plan and exact resource set.
   Recompute the plan immediately before execution; changed plans require a
   new approval. Recheck resource UID and ownership before each deletion.
7. Record an audit event for the actor, commit, target, plan hash, approval,
   and result. A background poller may plan and apply safe changes, but it
   must stop when deletions need approval. No headless auto-prune path exists.

## Credentials and isolation

JustCD runs as a hosted Go API/worker and a Next.js website outside the managed
cluster. Users can sign in with local accounts or configured OIDC providers.
The website lets them configure Git sources and inspect and manage deployed
resources. The backend owns Kubernetes credentials; browser code never receives
tokens. A connection has a stable cluster ID, API endpoint, TLS trust
configuration, and a credential reference. Each namespace binding selects its
own credential reference. Store credential material in an encrypted secret
store; the application database stores only references and metadata. Never log
tokens or serialize them in API responses.

Grant each credential only the Kubernetes verbs and namespace it needs. A plan
must prove that it can discover and completely read/list the application's
resource kinds using the selected credential; apply separately proves the
required write verbs. A manifest cannot target a namespace outside its
application's bindings. Cluster-scoped resources need a separate, explicit
cluster-scope credential and approval policy. This avoids treating a
namespaced token as authority over an entire cluster.

Credentials are encrypted with AES-GCM before being stored in PostgreSQL.
Kubeconfig exec plugins, auth-provider plugins, token files, SSH agent use, and
unpinned SSH host keys are not supported. This keeps authentication material
static, auditable, and independent from the host user's environment.

User login to JustCD is distinct from Kubernetes access. Local passwords use
a strong password hash, and OIDC uses authorization code with PKCE, issuer and
audience checks. JustCD authorizes users against application and namespace
roles. The namespace credentials are service credentials used by the backend.

## Data model

- **Cluster:** API endpoint and trust material.
- **Credential:** opaque secret reference, display name, expiration metadata.
- **Namespace binding:** cluster, namespace, credential, allowed applications.
- **Application:** Git source, revision policy, path, renderer, namespace bindings,
  sync policy, and ownership ID.
- **Application group:** shared renderer source, revision, and reconciliation
  settings, with Helm values or Kustomize options. Each configured cluster is
  stored as an ordinary child application with one or more bound namespaces,
  its own plans, health, and approvals.
- **Plan:** immutable source commit, target bindings, resource changes, digest,
  creation time, and expiry.
- **Deletion approval:** actor, plan digest, exact resource identities and UIDs,
  expiration, and single-use status.
- **Operation:** apply state and audit events.

## Upstream relationship

[`gitops-lite`](https://github.com/adrghph/gitops-lite) is a useful behavioral
reference for Git fetch, YAML/Kustomize rendering, ordering, `kubectl diff`,
server-side apply, and external polling. It is implemented in Python 3.13,
while JustCD's backend is Go, so direct Go library reuse is unavailable.
Port the ideas into testable Go interfaces and retain attribution if source
code is copied. Verify the upstream license before copying code: the package
metadata says MIT, but the inspected repository has no LICENSE file.

The upstream plan path calls rendering with automatic Helm CRD application,
which may write to a cluster before the diff. Its prune path applies first,
then lists a fixed set of resource kinds and deletes apparent orphans. JustCD
must build a complete, read-only deletion plan before any apply. Discovery must
cover the application's actual resource kinds and fail closed when listing is
incomplete or unauthorized.

## v1 implementation status

The first four milestones are implemented: Go planning and exact approval
checks; encrypted credential storage and scoped Kubernetes clients; isolated
Git fetch/rendering; persisted plans, operations, audit, and a guarded apply
executor; and a Next.js UI for workspaces, connections, applications, diffs,
approvals, inventory, and operation history.

Background polling checks every application's drift at its configured
interval. `auto-safe` may apply only non-destructive, namespace-scoped creates
and updates. Deletions and cluster-scoped changes stop for a workspace owner.
Webhooks, retries/backoff controls, fine-grained status conditions, and
permission self-tests during binding creation remain follow-up work.

CI is separate from this CD core: image builds and tests can remain in the
existing CI provider. JustCD consumes the resulting Git commit as desired
state. Native build pipelines can be added later if that is a product goal.

Multi-target Helm groups layer values in this order: chart defaults, shared Git
values files, shared JustCD values, cluster Git values files, cluster JustCD
values, namespace Git values files, and namespace JustCD values. Helm renders
once for each bound namespace. Identical cluster-scoped resources are coalesced;
if namespace renders disagree about a cluster-scoped resource, planning fails
instead of allowing one child application to manage different versions.

Kustomize groups use a shared repository path, with optional cluster and
namespace path overrides. JustCD selects the namespace path first, then the
cluster path, then the shared path. Each selected path is a complete Kustomize
entry point: include the shared base and any needed cluster overlay through
`resources` or components.
When namespace transformation is enabled, JustCD builds the selected entry
point once per bound namespace and applies that namespace through a temporary
Kustomize wrapper. Identical cluster-scoped resources across namespace builds
are coalesced; conflicting output stops planning. Helm charts in Kustomize are
available as an explicit render option. A shared group edit invalidates each
child's existing plans; each child still requires its own review and sync.
