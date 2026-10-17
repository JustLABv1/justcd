---
title: "Application runtime health"
---

JustCD reports Kubernetes runtime health separately from Git reconciliation
health. An application can therefore be `synced` while its runtime condition is
`Progressing` or `Degraded`; a successful apply is not evidence that workloads
are ready.

## Supported workload evaluation

Health evaluation is deterministic and uses only bounded status fields from
live Kubernetes observations. It uses explicit `Ready` conditions for custom resources, including the
condition generation when provided. It does not infer health from arbitrary
condition types or the existence of an object.

| Kind | Healthy | Progressing | Degraded |
| --- | --- | --- | --- |
| Deployment | Ready, updated, and available replicas meet the desired count; or when Kubernetes omits the status block, both `Available=True` and `Progressing=True` are reported. | Ready, updated, or available replicas are below the desired count. | `Progressing=False`, `Failed=True`, or `ReplicaFailure=True`. |
| StatefulSet | Ready and updated replicas meet the desired count. | Ready or updated replicas are below the desired count. | An explicit `Failed` or `ReplicaFailure` condition. |
| DaemonSet | Ready and updated pods meet the desired scheduled count, with no misscheduled pods. | Pods are not yet ready/updated or are misscheduled. | An explicit `Failed` or `ReplicaFailure` condition. |
| Job | `Complete=True` or successful completions meet the requested count. | The Job has not completed and has no reported failure. | `Failed=True` or one or more failed Pods. |
| Pod | Phase is `Succeeded`, or phase is `Running` with `Ready=True`. | Phase is `Pending`, or a running Pod has `Ready=False`. | Phase is `Failed`, a failure condition is true, or a container is in a known image/start/crash-loop failure state. |

An explicit Job or application suspension yields `Suspended` unless a higher
priority failure or incomplete observation exists. Desired replica counts that
are omitted by Kubernetes default to one; a desired count of zero is healthy.
For an observed workload status, omitted readiness/update counters are treated
as zero. Built-in non-workload objects such as Services and ConfigMaps do not
affect runtime health completeness; StackGres SGInstanceProfile, SGScript, and
SGObjectStorage definitions are also configuration objects without independent
readiness (script execution and backup storage usage belong to the consuming
cluster). Other custom resources without a `Ready` condition remain unassessed.

## Application condition precedence

The evaluator returns one of `Healthy`, `Progressing`, `Degraded`, `Suspended`,
`Missing`, `Unknown`, or `Partial`, plus a stable reason code, message, affected
resources, warnings, and timestamps. Precedence is deterministic:

1. Any observed workload failure is `Degraded`.
2. A managed supported workload not found in a complete observation is `Missing`.
3. Read/list failures yield `Partial` when any health evidence exists, otherwise
   `Unknown`. Failed reads never make an application appear `Healthy` or
   incorrectly `Missing`.
4. Custom resources without a usable readiness signal or supported resources without enough
   status yields `Unknown` when no other health can be inferred, and `Partial`
   when mixed with inferred health.
5. Explicit suspension is `Suspended`.
6. Incomplete supported workloads are `Progressing`; only complete supported
   health evidence can yield `Healthy`.

A custom resource's explicit `Ready=True` condition yields `Healthy`;
`Ready=False` yields `Progressing`, and `Ready=Unknown` yields `Unknown`.
A condition with an `observedGeneration` older than the object's generation
is `Progressing`, even if it reports ready. Explicit failure conditions retain
precedence. Missing readiness conditions remain unassessed and yield `Partial`
when mixed with known health. Configuration objects do not cause `Partial`. Demo/sample observations
are always `Unknown`, never live `Healthy`.

## Observation and history

An independent runtime poller checks for observations due every 15 seconds,
including applications whose auto-sync is paused or whose operations are still
running. Git polling intervals and reconciliation retry states do not gate it.
Application checks and resource-topology refreshes also request observations.
An application-scoped PostgreSQL advisory lock serializes observers across API
requests and backend replicas; observations less than ten seconds old are reused.
Observation reads namespaced
managed resources as well as workload descendants, retaining only bounded
status fields, labels, and ownership metadata. Kubernetes conditions expose
type, status, reason, message, and last-transition time; replica/completion
counts and known container failures are also included. User-facing details are
bounded and do not retain the raw Kubernetes status object.

The current condition is included in application list/detail responses as
`healthCondition`. Its `lastTransitionTime` changes when status or reason
changes; `observedAt` advances on each evaluation. The API exposes the newest
condition transitions (newest first):

```http
GET /api/v1/applications/{applicationID}/health-history?limit=25
```

Transition records include the status, reason, message, affected resources, and
change time. JustCD retains at most the latest 100 transitions per application.

## Application UI

The application heading shows separate Sync and Runtime badges on every tab.
Runtime freshness uses the health condition's `observedAt`, rather than the Git
reconciliation check time, and marks observations older than 60 seconds stale.
The detail page reloads topology and health history every five seconds while
visible, resumes immediately on tab visibility or network recovery, and retains
the last topology while retrying failed requests. These updates use polling;
Kubernetes watches and streamed browser events are not required.
Workload cards show counts and selectable pod indicators for ready, starting,
failing, terminating, completed, unknown, and sample states. Starting, failing,
terminating, and unknown pods remain visible when the workload is collapsed;
ReplicaSets and healthy pods can be expanded for details. Non-workload cards
retain their compact height. Connections use thin orthogonal paths.
Pod details include bounded waiting reasons, container readiness, and restart
counts. Applications with no managed resources show an explanation that existing
resources must first be claimed or deployed; observation never adopts resources.
The health details dialog explains partial assessment; configuration resources
show "No workload health check" in the resource graph instead of suggesting
missing workload status.

Activity uses ten operations per page. Its API accepts `limit` (1–100, default
50) and `offset` (0–1,000,000, default 0), returns `items` and `hasMore`, and sorts
newest first with an ID tie-breaker. Older pages are fetched on demand. New
operations can shift offset-based page boundaries; revisiting page one shows
the latest history.
