# Application runtime health

JustCD reports Kubernetes runtime health separately from Git reconciliation
health. An application can therefore be `synced` while its runtime condition is
`Progressing` or `Degraded`; a successful apply is not evidence that workloads
are ready.

## Supported workload evaluation

Health evaluation is deterministic and uses only bounded status fields from
live Kubernetes observations. It does not inspect manifests for custom health
conventions.

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
affect runtime health completeness; custom API groups remain unassessed.

## Application condition precedence

The evaluator returns one of `Healthy`, `Progressing`, `Degraded`, `Suspended`,
`Missing`, `Unknown`, or `Partial`, plus a stable reason code, message, affected
resources, warnings, and timestamps. Precedence is deterministic:

1. Any observed workload failure is `Degraded`.
2. A managed supported workload not found in a complete observation is `Missing`.
3. Read/list failures yield `Partial` when any health evidence exists, otherwise
   `Unknown`. Failed reads never make an application appear `Healthy` or
   incorrectly `Missing`.
4. Unsupported custom-resource health or supported resources without enough
   status yields `Unknown` when no other health can be inferred, and `Partial`
   when mixed with inferred health.
5. Explicit suspension is `Suspended`.
6. Incomplete supported workloads are `Progressing`; only complete supported
   health evidence can yield `Healthy`.

Custom resources are not assumed healthy from their existence or from an
arbitrary `status.conditions` array. If an application contains both supported
workloads and custom resources, the result is `Partial` so the inferred portion
is not mistaken for a complete application assessment. Demo/sample observations
are always `Unknown`, never live `Healthy`.

## Observation and history

The poller refreshes workload health along with application checks, and the
resource-topology refresh updates it immediately. Kubernetes conditions expose
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
