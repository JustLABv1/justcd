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
