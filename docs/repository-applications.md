# Applications managed by Git

Connect a Git source to a workspace once, then manage application configuration
with `justcd.yaml` or `justcd.yml` files in the repository. These are JustCD
configuration files, not Kubernetes resources or CRDs; no controller is installed
in the target cluster.

## Connect a repository

1. Open the workspace's **Git sources** page and connect a repository if necessary.
2. Under **Applications managed by Git**, choose **Manage applications from Git**.
3. Select the Git source and tracked branch (for example `main`), then choose
   **Connect and discover**.

Workspace owners can configure discovery. Viewers can inspect its status.
A workspace can connect multiple sources and revisions. Shared Git sources use
that workspace's existing access and credential rules.

JustCD recursively discovers definitions immediately and checks them every
minute. **Discover now** requests an immediate refresh. **Pause discovery** stops
configuration discovery; it does not pause existing application deployments.
Use an application's **Pause reconciliation** action to pause deployment.

## Kustomize

Place this file beside an overlay's `kustomization.yaml`:

```yaml
apiVersion: justcd.io/v1alpha1
kind: Application
metadata:
  name: shop-production
spec:
  source:
    renderer: kustomize
    path: .
    kustomizeNamespaceOverride: true
    # kustomizeHelmEnabled: true
  destination:
    cluster: production
    namespace: shop
    createNamespaces: true
  syncPolicy: auto-safe
  pollSeconds: 300
```

`cluster` references an existing cluster name or ID accessible to this workspace.
Use the ID if names are ambiguous. The namespace must already be bound to the
workspace. Credentials, cluster connections, namespace bindings, and approval
policies remain managed in JustCD and cannot be created by repository files.

## Application labels

Define shared application labels in `metadata.labels`:

```yaml
metadata:
  name: shop-production
  labels:
    team: commerce
    environment: production
    example.com/service: storefront
```

Labels organize applications inside JustCD and are not injected into Kubernetes
objects. Up to 32 labels are supported. Keys and values follow Kubernetes label
syntax, including optional DNS prefixes on keys. Values may be empty; quote
numeric or boolean-looking values in YAML.

Discovery creates, updates, and removes labels to match the tracked definition.
For Git-managed applications, edit labels in `justcd.yaml`. For other
applications, workspace owners can choose **Edit labels** from the application's
actions menu on the Applications page.

On **Applications**, choose **Group: Label**, then a label key. Each value gets a
group; applications without that key appear in a separate **No … label** group.
Labels are also searchable and visible in cards and the list.

Open the views menu beside **New** in the Applications header and choose
**Save as new view…** to name and save your grouping, filters, sort order,
favorites filter, and card/list layout. The selected saved view reopens when you
refresh or return, including in another browser signed in to the same account.
Changes remain drafts until you choose **Update current view** from the menu.
**Discard view changes** restores the saved configuration. Selecting **Default
view** restores the standard view. Each user can keep up to 20 personal views;
views can be deleted without changing applications.

Use the star on an application card or list row to add or remove a favorite.
The **Favorites** filter shows your starred applications. Favorites and saved
views belong to the signed-in user and do not affect other workspace members.
Application access still follows workspace membership.

## Helm

Place this file beside `Chart.yaml`:

```yaml
apiVersion: justcd.io/v1alpha1
kind: Application
metadata:
  name: shop-production
spec:
  source:
    renderer: helm
    path: .
    releaseName: shop
    valuesFiles:
      - values-production.yaml
    # valuesYaml: |
    #   replicaCount: 2
  destination:
    cluster: production
    namespace: shop
  syncPolicy: manual
```

Values files are resolved relative to `justcd.yaml`, independently of the chart
path. Inline `valuesYaml` is optional. The default release name is `justcd`, as
for manually configured applications.

## Plain Kubernetes YAML

Use `renderer: yaml` and point `path` to a manifest directory or a manifest file.
When rendering a directory, JustCD excludes `justcd.yaml` and `justcd.yml` from
Kubernetes manifests.

## Ignored resources

Use `spec.ignoreResources` to exclude resources managed by another controller or
outside your namespace service account's permissions. For example, add these
entries to your existing application definition for `ntfy-dev`:

```yaml
spec:
  # Keep your existing source and destination settings here.
  ignoreResources:
    - apiVersion: secrets.hashicorp.com/v1beta1
      kind: VaultStaticSecret
      reason: Managed by the Vault operator
    - apiVersion: v1
      kind: Secret
      name: ntfy-env
      reason: Secret populated by Vault
    - labelKey: justcd.io/exclude
      labelValue: "true"
      reason: Managed outside JustCD
```

An entry with `apiVersion` and `kind` excludes every matching resource in the
application. Adding `name` restricts it to one resource; `namespace` defaults to
`spec.destination.namespace` and must match that destination. To exclude an exact
cluster-scoped resource, set `clusterScoped: true` and omit `namespace`.

A label entry matches an exact key/value pair across kinds. Quote values such as
`"true"` to keep them strings. Omitting `labelValue` matches an empty label value,
not every resource with that key. Label entries cannot also specify identity
fields. Each entry requires a reason of 5–500 characters; at most 100 entries are
accepted. These entries exclude whole resources; JSON-pointer field ignores
remain available through the normal UI.

Exclusions apply before Kubernetes discovery and live reads. Normal plans do not
read, create, update, or prune matching resources, including previously managed
resources. They do not grant the service account additional permissions.

Git-owned exclusions appear as **Managed by Git** in application settings. Edit
or remove them in the file; discovery updates them atomically with application
configuration and invalidates existing plans. Removing an exclusion allows the
next plan to manage matching resources again, so review it before applying.
Manual exclusions remain intact. An identical manual rule causes discovery to
fail: remove that manual rule before moving its definition into Git. Rules from
an old, manually created application are not copied to a newly created Git
application. Operational rollback pins defer Git rule updates until resumed.

## File format and defaults

Each file contains exactly one `justcd.io/v1alpha1` `Application`. Multiple
applications use separate files in separate directories. Names must be unique
within each workspace and destination cluster, and remain stable when moving files.
The same application name can be used on different clusters in one workspace. Renaming an
application creates a new application and marks the old definition missing.
Existing manual applications are never implicitly adopted or overwritten.

| Field | Behavior |
| --- | --- |
| `metadata.name` | Required lowercase Kubernetes-style name, at most 63 characters |
| `spec.source.renderer` | Required: `yaml`, `kustomize`, or `helm` |
| `spec.source.path` | Defaults to `.`, relative to the definition |
| `spec.source.releaseName` | Helm only; defaults to `justcd`, at most 53 characters |
| `spec.source.valuesFiles` | Helm only; list of paths relative to the definition |
| `spec.source.valuesYaml` | Helm only; inline values mapping |
| `spec.source.kustomizeHelmEnabled` | Kustomize only; defaults to `false` |
| `spec.source.kustomizeNamespaceOverride` | Kustomize only; defaults to `false` |
| `spec.destination.cluster` | Required existing cluster name or ID |
| `spec.destination.namespace` | Required namespace already bound to the workspace |
| `spec.ignoreResources` | Optional list of whole-resource exclusions; defaults to empty |
| `spec.syncPolicy` | `manual` (default) or `auto-safe` |
| `spec.pollSeconds` | Application drift-check interval, 30–86400; defaults to 300 |

The Git source and revision are inherited from the repository connection. This
initial format targets one cluster and namespace per definition. Unknown fields,
duplicate keys or names for the same cluster, multiple YAML documents, and invalid paths are rejected.
Paths may reference shared content elsewhere within the same repository, but
cannot escape it, including through symlinks. Configuration files themselves
cannot be symlinks. At most 500 definitions are accepted per repository snapshot,
and each definition is limited to 256 KiB.

## Updates, errors, and removals

The complete snapshot is validated before application configuration is changed.
Application updates commit together: a validation error, name conflict, active
sync, or protected target change leaves the previous configuration intact.
The Git sources page shows the last successfully processed commit and current
error. Application details show **Managed by Git**, the definition path, and its
last processed commit. Unchanged definitions preserve application identity,
health, retries, and operational pause state.

Git controls these application settings. Editing the application's configuration
or render options through the UI/API is blocked. Sync, approvals, resource
actions, and operational pause remain available under their existing permissions.
A rollback pin is preserved until explicitly resumed; repository settings changed
while pinned are picked up on a subsequent discovery pass. Application retry
settings use JustCD's defaults. Existing approval rules still apply to deployment;
`auto-safe` never automatically prunes or applies cluster-scoped changes.

Changing a cluster or namespace while the application manages resources is
rejected. Decommission the old target through the normal reviewed workflow
before moving it. Repository configuration does not bypass those rules.

Removing a definition marks its existing application as removed from Git,
invalidates outstanding sync plans, and prepares a deletion plan from the live
managed resources. This also works when the entire application directory is
removed. A workspace owner must review, approve, and apply the plan; discovery
never queues deletion automatically, including with `auto-safe`. Repeated
discovery reuses an unchanged, unexpired plan. Cluster access failures are
reported and retried, and an owner can generate a fresh deletion plan manually.

Only application-owned resources are deletion targets. Explicitly managed
Namespace manifests follow the normal deletion rules; namespaces provisioned
through `createNamespaces` with static credentials remain shared infrastructure and are preserved. Managed agent environments can opt into ownership-checked deletion; see [managed namespace credentials](managed-deployment-credentials.md). For an
explicitly managed Namespace, enable **Keep namespace** in the deletion plan to
retain it while deleting the application's other managed resources. Changing
this option creates a new immutable plan and requires fresh approval. The choice
survives plan refresh and repeated discovery. After successful decommissioning,
retained namespaces are released from application management without a Kubernetes
delete.
After applying the plan, finish removing the decommissioned application in
JustCD. If a sync is active, discovery retries the removal after it finishes.
Restoring the same definition cancels its pending deletion plan, restores the
same application, and preserves any explicit operational pause. Removing all
definitions has the same behavior for every application in the repository.

## API

All writes require a session, CSRF token, and workspace owner role.

- `GET /api/v1/repository-configurations?workspaceId=…` lists discovery connections.
- `POST /api/v1/repository-configurations` with
  `{"workspaceId":"…","sourceId":"…","revision":"main"}` connects and discovers.
  The connection is persisted even if discovery fails; inspect `lastError`.
- `POST /api/v1/repository-configurations/{id}/reconcile` requests discovery.
- `PUT /api/v1/repository-configurations/{id}` with `{"enabled":false}` pauses
  discovery; `true` resumes it.

## Moving from a deleted manual application

Deleting a JustCD application while keeping its workloads can leave the old
`justcd.io/application-id` label on Kubernetes objects. A new Git-managed
application reports these as ownership conflicts. If the old application no
longer exists, the objects are eligible for a reviewed takeover: select the
resources (or **Select all eligible**), choose **Take over selected**, and confirm
that the previous controller has stopped reconciling.

JustCD rechecks the old application ID, each object's UID, and the reviewed live
configuration before claiming it. Status updates and server-maintained metadata
do not invalidate that review. Each label claim still uses the latest resource
version as an atomic precondition, with up to three attempts if status updates
race with the claim. Configuration or ownership changes require a fresh review.
The claim only updates ownership labels and inventory, then
pauses automatic reconciliation. Refresh and review a new plan before syncing.
Objects owned by an existing application, including another workspace's
application, or carrying Kubernetes owner references remain blocked.

The ownership review is bound to the resolved Git commit and renderer settings,
including Helm values and namespace targets. Randomly generated Helm secrets or
checksums do not invalidate an otherwise unchanged ownership review. Takeover
does not apply these generated values or overwrite existing credentials. After
upgrading JustCD, refresh any ownership review opened before the upgrade.

## Creating target namespaces

Set `spec.destination.createNamespaces: true` to let JustCD create a missing
target namespace before applying workloads. This works with Kustomize, Helm,
and plain YAML; a separate Namespace manifest is not required. The option is
also available when creating or editing a manual application in JustCD and is
`false` by default.

With this option, repository discovery also registers a missing workspace
namespace binding automatically, using the workspace's default Kubernetes
credential for that cluster (or the cluster default). Configure that default
once in cluster settings; no manual binding is required for each application.
Existing bindings and their credential overrides are preserved. Bindings inherit
the default, so later credential changes apply to them too. Without the option,
an existing binding is required. Discovery registers bindings atomically with
the complete validated repository snapshot and does not create Kubernetes objects.
JustCD uses
the configured cluster-scope credential, or otherwise the resolved credential
for that namespace (including a cluster-wide ServiceAccount token). The token
needs `get` and `create` on `namespaces` plus the workload permissions. Planning
performs a dry-run creation to validate RBAC and admission without creating the
namespace. This check runs before workload reads; workloads in a confirmed
missing namespace are planned as new resources without reading or listing them.
Access denials for existing namespaces or namespace provisioning still fail the
plan. A missing namespace appears as a cluster-scoped Create change and
requires the existing owner approval, including with `auto-safe` sync.

Existing namespaces are left untouched. Automatically created namespaces are
shared infrastructure, not application-owned resources, and are never pruned or
deleted during app decommissioning. Explicit Namespace manifests remain under
the normal GitOps ownership and deletion rules. Namespace creation is not atomic
with deployment: if a workload fails, the new namespace remains for a retry.
Certificate and connectivity failures are not bypassed by this option.

## Pull request handling

Use `spec.pullRequests` to manage PR reporting and deployments alongside the
application. The repository comes from the application's Git source. Reference
an existing JustCD **Git HTTPS credential by ID**; never commit API tokens.
That credential needs provider API access for reading PRs and comments and
writing review comments. Commit statuses are disabled by default. Set
`pipelineStatusReporting: true` under `spec.pullRequests` only when JustCD should
affect commit checks. On GitLab, statuses add external jobs to pipelines for the
commit; a JustCD failure can fail that pipeline and block merging. Existing
connections also default to comments only after upgrading. Git fetches continue
to use the Git source credential. Reporting uses polling; existing webhook configuration is
preserved.

JustCD maintains one updated summary per workspace/repository PR or MR, combining
application results and discovery errors. `commentMode` defaults to `summary`;
`action-required` creates a summary only for failures, blocked reviews, approvals,
or cleanup decisions, and updates an existing summary when the issue resolves.
`off` disables comments independently of commit statuses. `commentOnDrafts`
defaults to `false`: previews still follow the configured deployment policy, but
comments wait until the PR is ready for review. These settings are also available
in application reporting and repository PR discovery settings, and can be changed
while previews are active. Application connections sharing a summary must use the
same provider account. Existing historical comments are left in place.

Review-only example (add this block under `spec`):

```yaml
pullRequests:
  enabled: true
  pipelineStatusReporting: false # Opt in only to make JustCD affect commit checks.
  commentMode: summary # summary, action-required, or off
  commentOnDrafts: false
  credentialId: existing-https-credential-id
  # Required for a custom GitLab host; omitted for github.com or gitlab.com:
  provider: gitlab
  apiUrl: https://gitlab.example.com/api/v4
  previewProfile:
    enabled: false
    approvalActors:
      "123456": justcd-user-id
```

For isolated deployments, including draft PRs:

```yaml
pullRequests:
  enabled: true
  pipelineStatusReporting: false # Opt in only to make JustCD affect commit checks.
  commentMode: summary # summary, action-required, or off
  commentOnDrafts: false
  credentialId: existing-https-credential-id
  previewProfile:
    enabled: true
    deploymentMode: isolated
    manifestPath: ../preview
    namespacePrefix: preview
    hostSuffix: preview.example.com
    databaseStrategy: ephemeral
    maxActive: 5
    maxLifetimeHours: 72
    quotaCpu: "2"
    quotaMemory: 2Gi
    allowedSecrets: []
    allowForks: false
    approvalActors:
      "123456": justcd-user-id
```

`manifestPath` and `helmValuesFiles` are relative to the `justcd.yaml` file,
just like the application's source paths. Helm previews can supply
`helmValuesFiles` and/or `helmValuesYaml` instead of a separate manifest path.
Preview overlays must configure safe ingress hosts and data services; JustCD
does not create databases or copy production secrets.

To use the application's existing environment, use this profile instead:

```yaml
previewProfile:
  enabled: true
  deploymentMode: existing
  confirmShared: true
  allowForks: false
```

This permits one PR at a time and pauses reconciliation. Closing or merging
restores the tracked source with a return plan to review; migrations and data
changes are not undone. Deployment and approval policies still apply in both
modes. A mapped provider account can approve the current plan using
`/justcd approve <plan-id> <digest>`; current JustCD permissions are checked.

While the block exists, PR settings are marked as Git-managed and cannot be
edited, disabled, or deleted through the UI/API. Set `enabled: false` in Git to
disable reporting. Removing the block disables reporting and returns settings
to manual management, retaining history and webhook configuration. Removing an
application definition also disables its Git-managed reporting. These changes,
and profile changes, are rejected while previews are active; the complete
repository snapshot remains unchanged until the conflict is resolved. An active
Test branch freezes the tracked definition until the tracked source is resumed.
Omitting the block from an application that has manually configured PR reporting
leaves those settings unchanged.

## New applications on feature branches

A workspace owner can enable **Git sources → Applications managed by Git →
PR discovery** on a repository connection. This discovers `justcd.yaml` and
`justcd.yml` definitions introduced by open PRs/MRs targeting the tracked branch,
including drafts. The application does not need to exist on that branch yet.
Repository discovery must be enabled. Scans run every two minutes; **Discover
now** also scans PRs. A complete changed-file list without a JustCD definition
skips new-application discovery. Incomplete file lists fall back to inspecting
the pinned Git snapshot.

Select an existing HTTPS Git credential for provider API access and explicitly
allow the destination cluster/namespace bindings that feature-branch definitions
may reference. Git fetching keeps using the repository's Git source credential.
For a custom GitLab host, configure the provider and its HTTPS `/api/v4` URL.
These settings belong to the trusted repository connection; a branch's
`spec.pullRequests`, credentials, sync policy, or `createNamespaces` flag cannot
grant itself deployment access. Fork PRs are excluded.

Allowed destinations can use an **Exact namespace** or a **Namespace regex**
scoped to one cluster. For example, `^dev-.*$` allows `dev-shop` and
`dev-payments` without adding a repository rule for each namespace. Patterns use
Go/RE2 syntax, match the entire namespace name, and are limited to 256 bytes.
An exact namespace and a regex cannot be combined in one rule. The cluster must
already be accessible to the workspace. By default, each concrete destination
must have an existing workspace namespace binding before PR discovery.
The destination in `justcd.yaml` selects where the application goes; these
owner-managed rules define where an unmerged branch is permitted to send it.
Definitions outside these destinations are ignored by new-application PR
discovery, without creating applications or namespaces or failing reconciliation.
For example, `.*-dev` permits `mimir-dev` while skipping `mimir-prd`, even when
both definitions appear in the same PR. Existing applications with PR reporting
configured can still receive plan comments through their own connections.
Matching definitions must still pass namespace access and deployment checks.

Choose one of three modes:

- **Review plans only:** creates a temporary, paused application so manifests,
  resources, and the PR plan can be inspected. Applying its plans is rejected.
  Live planning requires an existing workspace namespace binding and Kubernetes
  access. This mode does not register bindings or create namespaces, even when
  the branch requests `createNamespaces: true`.
- **Deploy in isolated namespaces:** creates a dedicated namespace per PR
  application. Kustomize applies a namespace transform. Existing preview
  restrictions, host validation, quotas, and lifetime limits apply. The new
  application's own source path supplies the manifests; configure them for safe
  preview hosts, workloads, and dependencies. Helm templates must honor the
  release namespace. Cluster-scoped resources are rejected.
- **Deploy to approved dev namespaces:** uses the definition's approved binding,
  after explicit shared-environment confirmation. Resources must stay inside
  that namespace. A second PR cannot claim an application with the same name.
  Existing manual applications are never adopted implicitly.

For **Deploy to approved dev namespaces**, enable **Allow creation of matching
dev namespaces** in the Policy tab to let a new application request automatic
namespace setup:

```yaml
spec:
  destination:
    cluster: development
    namespace: dev-shop
    createNamespaces: true
```

The option is off by default and requires the shared-environment confirmation.
Both the owner-controlled option and `createNamespaces: true` are required.
Exact destination rules may then name a namespace that does not exist yet, and
regex rules can cover all matching new dev namespaces. Configure a default
Kubernetes credential for the workspace and cluster (or a cluster agent) first.
Discovery registers the namespace binding and application together in one
database transaction, then bootstraps the authorized namespace before live
workload planning. Existing namespace bindings and credential overrides are
preserved; new bindings inherit the default. The PR worker retries setup if a
previous attempt failed. This owner-enabled option authorizes environment setup
during discovery; workload deployment still requires its normal plan and approvals.

On OpenShift, JustCD uses the `project.openshift.io/v1` **ProjectRequest** API
with the application's namespace credential. OpenShift's configured project
template supplies the requester's project-admin binding and any platform defaults,
just as for `oc new-project`. The account must be permitted to request projects.
A denied project request is reported without falling back to bare Namespace
creation. Existing projects are never modified or assigned new permissions.
On other Kubernetes clusters, JustCD creates a missing Namespace through the
configured provisioning credential; Kubernetes RBAC remains cluster-managed.
The opt-in [short-lived deployment credential mode](managed-deployment-credentials.md)
supports administrator-prepared namespaces and explicitly authorized automatic
provisioning. `createNamespace: true` (alias of `createNamespaces`) provisions the
namespace/account/approved role binding before workload planning. Optional deletion
uses persisted ownership and UID checks; shared/pre-existing namespaces stay intact.

Static-credential bootstrap is create-only. It does not update namespace/project labels, annotations,
requester ownership, or RBAC, even if an existing environment has another creator.
If OpenShift reports an inaccessible project already exists, setup leaves it
alone and normal workload planning checks the account's permissions. Bootstrap
does not manage, adopt, synchronize, or delete RoleBindings generated by the
project template or admission controllers such as Kyverno.

New project creation still runs the cluster's project template and admission
policies. Compatibility with arbitrary policies is not guaranteed: a project
template and a policy can generate the same RoleBinding name with different
subjects, or a policy can depend on the user identity seen during Namespace
creation. This is the same platform provisioning path used by `oc new-project`;
JustCD does not bypass or override those policies.

Namespace setup is not atomic with the database transaction or workload
deployment: the registered app can remain after a setup failure, and a created
namespace/project remains after a plan failure. Other cluster-scoped
manifests remain prohibited. Closing a PR does not remove these shared dev
namespaces or their bindings. Review-only and isolated modes do not use this
option. A branch's `createNamespaces` flag alone never grants this permission.

Every new commit updates the same temporary application at an immutable SHA and
invalidates previous plans. Normal reconciliation stays paused. Deployment uses
normal workspace approval policies; plans that require no approval can deploy
through the PR worker. Configure approval identity mappings to authorize
`/justcd approve <plan-id> <digest>` comments. The mapped user's current workspace
role and the plan's current digest, expiry, head commit, and target branch are
checked before execution.

When a dev-environment PR merges, tracked-branch discovery takes over the same
application ID and managed resource ownership. Its merged definition must retain
its destination. Normal reconciliation remains paused until you review a fresh
tracked-source plan and explicitly resume it. For isolated previews, tracked
branch discovery creates the normal application separately; the preview follows
reviewed cleanup. Closing an unmerged PR or removing its definition also prepares
a cleanup plan for managed resources. Cleanup does not delete existing dev
namespaces or untracked objects in those namespaces. Isolated preview namespace
cleanup uses the existing namespace ownership checks. An undeployed temporary application can be
removed immediately. Repository records retain the outcome after cleanup.

Close and clean up active PR applications before changing this policy. Errors
appear in repository PR settings and the application's Pull requests view.
Failures while discovering a PR are also posted as a PR/MR comment, even if
discovery stopped before an application or review could be registered. The same
commit and error are reported once, so repeated discovery does not repeat the
comment. If provider comment access fails, that reporting error appears alongside
the discovery error in repository settings.


## Deployment schedules

Use `spec.deploymentSchedule` to configure application maintenance windows and
freezes in Git. Workspace restrictions (called project restrictions in older
versions) also apply and cannot be bypassed by an application definition.
Owners configure workspace restrictions in **Workspace settings → Deployment
schedule**. Applications created manually use **Overview → Deployment schedule**.
Git-managed application schedules are read-only in the UI.

```yaml
spec:
  deploymentSchedule:
    windows:
      - timeZone: Europe/Berlin
        days: [1, 2, 3, 4, 5] # Sunday=0; Saturday=6
        start: "09:00"
        end: "17:00"
    freezes:
      - reason: Holiday release freeze
        start: "2026-12-24T00:00:00+01:00"
        end: "2027-01-02T00:00:00+01:00"
      - reason: Weekly database maintenance
        schedule:
          timeZone: Europe/Berlin
          days: [0]
          start: "01:00"
          end: "04:00"
      # A freeze with only a reason lasts until removed:
      # - reason: Incident response
```

Every recurring schedule requires an explicit IANA time zone. Each scope allows
up to 16 windows and 16 freezes. Fixed freeze timestamps use RFC 3339 with `Z` or
an explicit UTC offset; the UI edits these in UTC. A missing start means
immediately; a missing end means indefinitely. Recurring freezes use `schedule`
and cannot also specify fixed timestamps. Every freeze requires a reason.

Windows within one scope form a union; workspace and application policies form
an intersection. Any active freeze wins over all windows. Empty or omitted
windows allow all hours, subject to freezes and the workspace policy. Omitting
the entire application schedule removes its Git-managed restrictions on the next
successful discovery. This never removes workspace restrictions.

Start times are inclusive and end times exclusive. An end earlier than the start
continues into the following day; `days` identifies its start day. Equal start
and end times mean the entire selected local calendar day. On spring DST
transitions, nonexistent wall-clock minutes do not occur. On autumn transitions,
both occurrences of a repeated minute belong to the schedule. The next allowed
start is searched over the next eight days, jumping over active finite freezes;
if none is found, the UI reports that no start exists in the next weekly cycle.

Planning, drift observation, and approval remain available outside windows.
Submitting apply queues the exact reviewed snapshot. Auto-safe applies use the
same queue gate. Entries and waiting reasons persist across backend restarts;
workers reevaluate restrictions about every 30 seconds (subject to queue load) and again before the
first cluster write. Policy edits wake waiting entries. A queued operation waits
when its window closes; an operation admitted to apply finishes even if a window
closes or a freeze begins while it is running. Running work is never resumed
automatically after a worker crash.

Plan and approval expiry are checked when apply is accepted. An accepted request
that waits for a schedule retains that authorization, including consumed approvals,
while waiting. Before executing it, JustCD still checks current deploy/owner and
approver permissions, current policy, the exact plan digest, Git revision, live
state, and resource preconditions. Drift polling continues for waiting entries.
Changed plans are cancelled and require a fresh plan and any required review.
Changes to a Git definition cancel its waiting entry before discovery updates the
application. Owners and deployers may also cancel a queued apply in the UI.

A workspace owner can select **Emergency override** on a waiting operation and
must provide a reason (at most 1,000 characters). The override applies only to
that operation's windows and freezes. It does not bypass plan, approval,
permission, destination, or cluster concurrency checks. The owner is checked
again at execution time. The audit trail records the owner, reason, operation,
and restrictions being overridden. Instances retain their existing administrator
access to workspace owner actions. Git manifests cannot grant overrides.

The authenticated API exposes `GET` and owner-only `PUT` at
`/api/v1/workspaces/{id}/deployment-schedule` and
`/api/v1/applications/{id}/deployment-schedule`. Responses contain `policy`,
`managedByGit`, and `decision` (`allowed`, `reasons`, optional `nextAllowedAt`).
Use owner-only `POST /api/v1/operations/{id}/schedule-override` with
`{"reason":"Incident response"}` or deployer-level
`POST /api/v1/operations/{id}/cancel` for a queued operation. All mutations
require the normal CSRF token. Operations expose `waitingReasons`,
`nextAllowedAt`, `scheduleDeferred`, `scheduleOverrideReason`, and
`scheduleOverrideActorId`.

## Notification routing

Declare `spec.notifications` to manage application routing alongside deployments:

```yaml
spec:
  notifications:
    mode: custom
    destinations: [Production Telegram]
    events: [sync_failed, unhealthy]
    minimumSeverity: error
```

Destinations reference existing workspace IDs or unique names; their credentials
remain configured in JustCD. Use `mode: disabled` for quiet development apps or
`mode: inherit` for workspace defaults. Declared policies are marked **Managed by
Git** and cannot be overwritten in the UI/API. Removing the declaration restores
inheritance and allows UI editing; omission preserves policies that were configured
outside Git. See [notification routing](notifications.md#routing-in-justcdyaml) for
validation, defaults, and reconciliation behavior.
