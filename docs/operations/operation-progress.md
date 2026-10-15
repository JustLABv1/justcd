# Operation progress and interruption

Sync and rollback operations persist per-resource results in the operation's
`progress.resources` JSON. Each result identifies the reviewed resource and
action, stage, attempt count, start/finish timestamps, and one of `queued`,
`running`, `retrying`, `succeeded`, `failed`, or `skipped`. Namespace prerequisites
are included in the total. `retrying` means a resource is executing on a later
attempt using a fresh plan, rather than replaying an old mutation.
The existing `completed` array remains available to older clients.

The application page polls durable operation history. Navigation, browser
restarts, and temporary network failures do not cancel execution. Returning to
the application reloads the results. Operation elapsed time includes queue wait;
resource elapsed time starts when its step begins. Automatic retry timing is
reported on the failed operation: the retry builds a fresh plan and creates a
new operation, retaining the previous attempt's results.

## Resource execution order

Sync and rollback apply namespace prerequisites first, followed by creates and
updates in dependency order. The durable resource timeline uses that same order.
For example, a Deployment referencing a ServiceAccount is applied after that
account and any RoleBindings or ClusterRoleBindings granting it permissions.
Bindings follow their referenced Role or ClusterRole and ServiceAccount subjects.

Pod, Deployment, StatefulSet, DaemonSet, ReplicaSet, ReplicationController, Job,
and CronJob references are recognized. Prerequisites include ServiceAccounts,
image pull Secrets, container environment Secrets and ConfigMaps, mounted and
projected Secrets and ConfigMaps, CSI volume Secrets, and persistent volume
claims. StatefulSets also follow their governing Service. ServiceAccount image
pull Secrets, service account token Secret annotations, PVC storage class and
volume references, namespaces, and Kubernetes owner references are considered.
References are scoped to the resource's cluster and namespace as appropriate.

Only resources changing in the plan participate in the graph. External or
unchanged prerequisites remain Kubernetes' responsibility. Unrelated resources
use a deterministic kind priority and identity order; namespace, infrastructure,
account, configuration, and RBAC kinds generally precede workloads. Arbitrary
custom-resource dependency fields and sync-wave annotations are not interpreted.
This ordering does not provision missing CRDs or wait for CRD discovery.

All creates and updates finish before deletions begin. Deletions reverse the
dependency order using the previous manifests, removing dependents before their
prerequisites. Cyclic dependencies reject the operation before resource mutations.
Execution ordering leaves the reviewed plan and its approval digest unchanged.
An older queued operation's resource rows are reordered when its worker starts;
historical completed operations retain their recorded order.

A resource succeeds when its Kubernetes mutation and managed-resource record
complete. Ordering does not wait for pod readiness, PVC binding, Job completion,
or controller health; runtime health is reported separately.

## Cancellation boundaries

`POST /api/v1/operations/{id}/cancel` requires workspace deploy permission.
Queued cancellation is immediate and invalidates the queued plan. For a running
sync or rollback, the endpoint persists a stop request and pauses automatic
reconciliation. Its response distinguishes `cancellationRequested` from
`cancelled` and includes the current operation; acknowledgment does not imply
that an in-flight resource step has finished.

The worker orders starting each resource step atomically against that request.
An already-started step finishes normally, and its result is recorded. The worker
then stops before the next resource. The application lease remains held until
the worker records the terminal outcome, preventing a conflicting apply while
the current mutation is still running. Repeated stop requests are idempotent.
Cancellation closes when the final resource boundary enters finalization;
checkpoint/snapshot and deployment bookkeeping then finish normally.
Resource actions do not support this cancellation endpoint.

A cancelled operation preserves succeeded results and marks resources never
started as skipped. Applied changes are never described as reverted. Create a
fresh plan after reviewing cluster state to continue. A failure of the current
step remains a failure even if cancellation was requested, and no automatic
retry is scheduled for that stop request.

## Worker loss and uncertain outcomes

If a worker loses its lease or stops before recording a response, existing
recovery marks the operation failed and pauses automatic reconciliation. It
preserves confirmed succeeded resources, reports an in-flight resource as failed
with remediation, and reports resources never started as skipped. An in-flight
mutation may have reached Kubernetes even if no response was received. It must
be reviewed against current cluster state; it is never assumed reverted or
safe to replay. Progress visibility resumes after restart; interrupted mutations
do not resume automatically.

An operation cannot report success with unfinished resource results. A metadata
or snapshot failure can still fail an operation whose resource mutations all
succeeded. Use the resource diff and remediation in the progress view to review
a failure and build the next plan. Historical operations without resource
results continue to display their existing aggregate progress.
