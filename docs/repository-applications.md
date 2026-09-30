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
  syncPolicy: auto-safe
  pollSeconds: 300
```

`cluster` references an existing cluster name or ID accessible to this workspace.
Use the ID if names are ambiguous. The namespace must already be bound to the
workspace. Credentials, cluster connections, namespace bindings, and approval
policies remain managed in JustCD and cannot be created by repository files.

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
within the workspace and remain stable when moving files. Renaming an
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
duplicate keys or names, multiple YAML documents, and invalid paths are rejected.
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

Removing a definition marks its existing application as missing, invalidates
outstanding plans, and excludes it from automatic reconciliation. Neither the
application nor its workloads are automatically deleted. Normal sync is blocked
until the definition is restored; reviewed manual decommissioning remains
available. If a sync is active, discovery retries the removal after it finishes.
Restoring the same name restores the same application and preserves any explicit
operational pause. Removing the entire repository's definitions has the same
behavior for all of its applications.

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

JustCD rechecks the old application ID and each object's UID/resource version
before claiming it. The claim only updates ownership labels and inventory, then
pauses automatic reconciliation. Refresh and review a new plan before syncing.
Objects owned by an existing application, including another workspace's
application, or carrying Kubernetes owner references remain blocked.
