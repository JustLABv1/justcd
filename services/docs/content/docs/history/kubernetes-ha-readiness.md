---
title: "Kubernetes HA readiness assessment"
---

> Historical assessment: this records the state at the time of review. Follow the current setup and operations guides for deployment instructions.

Assessed 2026-10-16 against the current working tree. This is a source review,
not certification of a running HA installation. No application or chart behavior
was changed by this assessment.

## Conclusion

JustCD already supports coordinated concurrent backend replicas and a frontend
without browser-session affinity. The remaining work is mainly deployment
hardening and proving upgrades across release versions. Raising replica counts
alone does not establish uninterrupted upgrades.

Recommended starting topology: at least two frontend and two backend replicas,
spread across worker nodes; a redundant ingress tier; and an external HA
PostgreSQL service with a stable read/write endpoint. All backend replicas must
share the database and the same durable encryption key. Three application
replicas across three nodes/zones provide more maintenance headroom where the
cluster and availability target justify it. A shared application filesystem or
Redis session store is not required by the current session/API proxy design.

## Existing foundations

- `charts/justcd/values.yaml` exposes independent frontend/backend replica counts,
  resource requests, affinity and topology spread constraints. Both default to
  one replica; scheduling constraints default to empty.
- `services/backend/internal/store/store.go` stores sessions and OIDC login state
  in PostgreSQL. Requests can move between replicas without sticky sessions.
  Initial administrator creation is serialized across replicas.
- Queue claims use PostgreSQL transactions and row locks. Per-application leases
  plus session advisory execution guards coordinate mutation workers. See
  `internal/store/execution_guard.go`, `store.go`, and `internal/syncer/worker.go`.
- Background plan/approval tasks are durable; the browser polls their status.
  Notification delivery also has database-coordinated claims and at-least-once
  delivery semantics. Multiple replicas do not imply exactly-once external writes.
- Startup migrations are serialized by a PostgreSQL advisory lock and applied
  transactionally per migration. Concurrent-startup coverage exists in
  `internal/store/migration_concurrency_integration_test.go`.
- `services/backend/shutdown.go` stops acquisition, drains active handlers and
  workers, then cancels execution at the deadline. The backend chart explicitly
  uses `maxSurge: 1` and `maxUnavailable: 0`.
- The Next.js frontend proxies to the backend Kubernetes Service, with API fetches
  and proxy responses configured as `no-store`. Its Docker image includes the
  standalone server and static assets from the same build.
- Existing integration coverage includes queue contention, execution guards,
  shutdown, and crash recovery. These are valuable foundations, but do not replace
  an ingress-level, mixed-release rolling-upgrade test.

## Prioritized changes

### 1. Publish and verify an HA chart profile

Add a documented `values-ha.yaml` with at least two replicas per component,
explicit hostname spread, and optional zone spread. Use the chart's existing
selector labels. Decide deliberately whether insufficient topology should block
scheduling or allow temporary co-location. Reserve capacity for surge Pods.

Add configurable `policy/v1` PodDisruptionBudgets for each component, normally
`minAvailable: 1` for two replicas. PDBs protect eviction-based voluntary
disruptions such as node drains; Deployment rollout availability is controlled
separately by its update strategy. Avoid enabling a budget that permanently
blocks maintenance for a single-replica installation.

Make the frontend rollout strategy explicit and configurable, matching the
backend's `maxUnavailable: 0`, `maxSurge: 1`. Add configurable termination grace,
`minReadySeconds`, and rollout deadlines where useful. Kubernetes currently
supplies the frontend's default rolling strategy and termination grace.

### 2. Prove frontend behavior while versions overlap

`services/frontend/next.config.ts` has no deployment identifier or explicit skew
handling. Each frontend image contains only its own build's assets. During a
rollout, HTML, navigation data, and subsequent asset requests can reach different
versions; a browser already open before the rollout matters as much as a new one.

Build one immutable image per release and reuse it across replicas. Configure a
release-specific Next.js `deploymentId` during the build and test navigation
across versions. An identifier helps detect skew; it does not itself retain old
JavaScript files or configure ingress routing. Retain immutable assets from the
previous release in shared asset hosting, or use routing that keeps requests
with the appropriate release while old clients transition. Test cold caches and
lazy-loaded chunks, not just the initial page load. Blue/green service switching
alone does not preserve assets for browsers running the previous release.

No Server Actions were found in the inspected source, so adding a shared Server
Actions encryption key is not a current prerequisite. Likewise, introduce shared
Next.js data caching only if application caching features require it; the current
API proxy intentionally bypasses caching.

### 3. Establish release compatibility and migration rules

The migration lock prevents concurrent migrators; it does not make schema changes
safe for old code that is still serving traffic. Both components update
independently, and current chart documentation asks operators to deploy matching
versions without defining an old/new compatibility window.

Define and test supported N/N+1 combinations for frontend/backend APIs, queued
task payloads/results, persisted plans, and worker behavior. Use additive
expand/contract migrations: introduce compatible schema first, migrate usage,
then remove obsolete schema only after old binaries and relevant jobs are gone.
Classify releases that need maintenance explicitly. Chart rollback does not undo
database migrations.

Startup currently gives database initialization, including migrations, a shared
20-second context in `services/backend/main.go`. A 150-second Kubernetes startup
probe allowance does not extend that application deadline. Make migration timing
configurable or provide a dedicated, serialized migration command/Job with a
separate timeout and schema compatibility gate. Account for DDL locks and large
backfills; a migration Job alone cannot make incompatible DDL safe.

### 4. Separate readiness and liveness; verify traffic draining

Both chart probes use `/healthz`. The backend endpoint reports process health
after startup and does not check PostgreSQL; the frontend endpoint only returns
an OK response. A Pod can therefore be Ready while its required data path fails.

Keep liveness cheap and independent of external service health. Add a bounded
backend readiness check for database access and local draining state. Decide
whether frontend readiness should remain local or include a bounded backend
check, and document the routing consequences when all backends are unavailable.
Do not make individual target-cluster, Git, or OIDC outages take the entire
backend out of service.

The backend closes its listener and rejects newly arriving requests during
shutdown. Endpoint removal and ingress propagation are asynchronous, so measure
requests that still reach a terminating Pod. If needed, introduce a bounded
pre-drain propagation interval that keeps serving traffic before listener closure;
include it in the termination budget. Verify frontend SIGTERM under the built
standalone server and active proxy requests. Never automatically replay ambiguous
mutation POSTs to mask shutdown failures.

### 5. Preserve long-running operations during routine upgrades

Backend execution can run for up to 30 minutes and background tasks for five
minutes, while the default drain is 30 seconds and Kubernetes termination grace
is 60 seconds. Work exceeding the drain can be cancelled. Interrupted mutations
are intentionally marked as requiring review rather than replayed automatically.
This protects target state but can disrupt a deployment during a JustCD upgrade.

For an initial HA profile, size shutdown grace to measured operation durations,
and set termination grace at least 20 seconds longer, as the current chart
documents. For a stronger upgrade guarantee, provide a worker quiesce procedure:
stop new acquisition on retiring workers, keep API traffic on other replicas,
wait for active operations to finish, then terminate. A future API/worker
Deployment split could make these lifecycles independently manageable; it is not
required for the existing queue coordination to work.

Do not claim transparent transfer of an in-flight Kubernetes mutation. A hard
failure can leave its outcome ambiguous and require inspection and a fresh plan.
The current 90-second operation lease and 30-second recovery sweep also mean
recovery is not instantaneous.

### 6. Make the database and ingress highly available too

Use a tested PostgreSQL HA solution and failover endpoint, backups/PITR, and a
restore drill. Advisory locks require direct connections or session pooling;
transaction/statement pooling is unsupported. Verify behavior when failover drops
lock-owning sessions, including cancellation and ambiguous-operation recovery.

The pool limit is currently hardcoded to 64 connections per backend replica.
Two normal replicas plus one surge replica can budget up to 192 connections;
three plus one surge can budget 256. Allow additional administration/migration
capacity, and account for terminating Pods that still hold connections. Make
pool/worker sizing configurable before adding aggressive autoscaling. Each
backend currently has 20 operation executors and four background-task workers.

Run ingress replicas on separate nodes as well. Preserve the same public origin,
TLS and cookie settings. HA of JustCD does not imply HA of every dependency:
the optional cluster-agent chart currently uses one replica and `Recreate`.
Treat agent availability as a separate follow-up if agent-backed target access
must also survive upgrades without interruption.

## Acceptance tests before declaring rolling upgrades supported

1. Deploy two frontend and two backend replicas on distinct nodes using the real
   ingress path; distribute authenticated requests across all Pods. Check sessions,
   CSRF, OIDC callback state, approvals, and durable task polling.
2. Run release N traffic while migrating and rolling to N+1. Keep an N browser
   open; navigate and load previously unused chunks throughout the transition.
   Verify supported mixed API/worker combinations and schema-compatible rollback.
3. Upgrade during a deployment longer than the default drain. With the intended
   drain/quiesce settings, verify successful completion and no duplicate resource
   mutations. Separately verify documented review-required behavior after a hard
   failure or expired shutdown deadline.
4. Evict/drain one node, restart Pods, and kill an executing backend. Confirm
   remaining capacity, PDB enforcement, bounded recovery, and preserved progress.
5. Fail over PostgreSQL under load. Confirm readiness behavior, session continuity
   after reconnect, loss of execution guards, and safe handling of uncertain work.
6. Include a slow migration and a deliberately incompatible release. Compatible
   rollouts must meet an agreed availability/error budget; incompatible rollouts
   must be prevented or explicitly require maintenance.

Collect ingress request failures, missing asset errors, task/operation outcomes,
queue age, drain duration, DB connections, and target mutation evidence. Existing
concurrency/crash tests should remain in CI alongside these release-level tests.

## Validation performed for this assessment

- Helm lint passed with both replica counts set to two (only the optional chart
  icon recommendation was emitted).
- All four existing lifecycle/shutdown tests passed using
  `go test shutdown.go shutdown_test.go -count=1` in `services/backend`.
- Reviewed source and existing migration, contention, and shutdown tests. A live
  Kubernetes rollout, PostgreSQL failover, and N/N+1 compatibility drill were not
  performed. Passing template validation is not evidence of runtime HA.

## Reference documentation

- [Next.js self-hosting: multiple instances and version skew](https://nextjs.org/docs/app/guides/self-hosting)
- [Next.js deploymentId](https://nextjs.org/docs/app/api-reference/config/next-config-js/deploymentId)
- [Kubernetes disruption behavior and PDB scope](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/)
- [Kubernetes startup, readiness, and liveness probes](https://kubernetes.io/docs/concepts/workloads/pods/probes/)
- Existing repository guidance: `charts/justcd/README.md`,
  `docs/production-operations.md`, `docs/cluster-agents.md`.
