# JustCD Helm chart

This chart deploys the JustCD web server and API. PostgreSQL is external. The
backend connects directly to configured Git repositories and Kubernetes API
servers; the chart does not grant it in-cluster Kubernetes permissions.

## Prerequisites

- A reachable PostgreSQL database whose user can create and migrate tables.
- Published `justcd-frontend` and `justcd-backend` images built from this repo.
- A stable 32-byte encryption key. Keep it backed up: stored credentials cannot
  be decrypted after changing or losing it.
- An HTTPS ingress endpoint for production, including an Ingress controller and
  TLS certificate. `publicUrl` must match the browser-facing origin and OIDC
  callback origin.

Create a local `.env.production.local` file containing exactly the values below
and keep it out of version control:

```text
JUSTCD_DATABASE_URL=postgres://justcd:YOUR_PASSWORD@postgres.example.com:5432/justcd?sslmode=verify-full
JUSTCD_ENCRYPTION_KEY=YOUR_64_CHARACTER_HEX_KEY
```

Generate the key with `openssl rand -hex 32`. Create the Secret in the release
namespace:

```sh
kubectl create namespace justcd
kubectl -n justcd create secret generic justcd-config --from-env-file=.env.production.local
```

Put deployment settings in `deploy-values.yaml`:

```yaml
publicUrl: https://justcd.example.com
backend:
  existingSecret: justcd-config
  image:
    repository: registry.example.com/justcd-backend
    tag: "0.1.0"
frontend:
  image:
    repository: registry.example.com/justcd-frontend
    tag: "0.1.0"
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: justcd.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: justcd-tls
      hosts:
        - justcd.example.com
```

Install or upgrade:

```sh
helm upgrade --install justcd ./charts/justcd --namespace justcd --create-namespace -f deploy-values.yaml
```

On a fresh installation, create the first administrator at `/signup`. JustCD
then opens `/onboarding`, where the readiness checklist verifies the database
and public URL, asks an administrator to confirm the encryption key is durably
backed up, and walks through workspace, Kubernetes credential/endpoint/namespace,
Git source, and first-application setup. Progress is derived from persisted
configuration and saved safe connection tests, so the flow resumes across
sessions without blocking normal navigation.

The cluster workflow is not limited to first-run setup. Administrators can
open it from any workspace's Connections tab to reuse an existing credential or
add one inline, register another API endpoint, bind a namespace, and rerun the
same verification checks.

The Kubernetes check performs discovery and `SelfSubjectAccessReview` calls;
it does not create or modify cluster resources. Diagnostic results distinguish
configuration, permission, connectivity, and service-health failures. Secret
credential values stay encrypted in the backend and are not exposed by the
readiness API.

The Ingress routes only to the frontend. Its same-origin `/api` proxy calls
the backend service inside the namespace. The frontend and backend images can
be promoted between environments without rebuilding for the internal API URL.

### Background plan and approval tasks

Plan generation and approvals return `202 Accepted` with a PostgreSQL-backed
task. The frontend polls short status requests while workers fetch Git and read
Kubernetes state independently of the browser connection. A page reload or
navigation does not cancel the task. Deploy matching backend and frontend
versions: API clients must poll `/api/v1/tasks/{taskID}` and read its terminal
result instead of expecting the plan or approval in the POST response.

Each backend replica runs four task workers. Queued tasks survive restarts;
running tasks renew a 90-second lease and have a five-minute execution limit.
Workers drain during the existing shutdown grace period. Interrupted tasks are
marked failed without automatic replay, because approval may already have been
recorded. The UI shows the outcome and asks users to refresh current state
before retrying. See the
[operations guide](../../services/docs/content/docs/operations/production-operations.md#background-plan-and-approval-tasks).

The backend applies database migrations at startup under a PostgreSQL migration
lock. `backend.replicaCount` defaults to one and can be increased for multiple
API/worker replicas. The chart does
not install PostgreSQL or create credentials for target clusters. Set
`cookieSecure: false` only for a local HTTP installation. Both images expose
`/healthz` for Kubernetes probes; the backend's endpoint reports process
availability after startup, not the health of every external connection.

### Worker shutdown and recovery

On SIGTERM or SIGINT the backend stops acquiring queued operations and stops
its planning, repository, and PR polling loops. Already claimed operations
continue executing and renewing their database leases during
`backend.shutdownGracePeriod` (default `30s`, exposed as
`JUSTCD_SHUTDOWN_GRACE_PERIOD`). Active HTTP requests can also finish during
this period. New requests receive HTTP 503 while the backend drains.

When the drain period expires, in-flight execution and requests are cancelled.
The process waits for worker and request cleanup before closing PostgreSQL;
operation failure recording has a five-second timeout. Telemetry flushing may
take another ten seconds. Set `backend.terminationGracePeriodSeconds` (default
`60`) at least twenty seconds longer than the drain period, and increase both
settings for deployments that need more time to finish operations.

Shutdown cancellation does not undo Kubernetes changes. Interrupted mutations
require inspection of cluster state and a fresh reviewed plan; they are not
automatically replayed. A hard kill can leave an operation running until its
90-second lease expires and the recovery sweep marks it interrupted. The sweep
runs every 30 seconds. Keep PostgreSQL available during draining and recovery.

### Multiple backend replicas

All replicas must run the same JustCD version and use the same writable
PostgreSQL database/schema, encryption key, and public URL. Set
`backend.replicaCount: 2` (or more) to run concurrent API and worker instances.
Do not configure a separate database per replica. Browser sessions, queue
entries, approvals, leases, progress, and audit history live in PostgreSQL.

Each replica has a bounded pool of 20 executors. Queue claims serialize the
cluster concurrency/rate gate, and only one operation can hold an application's
lease. An additional session advisory lock prevents a suspended executor from
overlapping its replacement even after lease expiry. Repository discovery also
uses session advisory locks; startup migrations are serialized per schema.
Planning can run on multiple replicas, but an active operation prevents saving
a replacement plan. Repeating an identical current plan reuses its identity and
approvals; plan creation and its audit event commit together. Claiming a queued operation and recording terminal
completion use database transactions.

Use PostgreSQL 16 or newer with a primary read/write connection and privileges
to create/migrate the application tables. Connect directly or through a pooler
in **session** mode. Transaction/statement pooling is unsupported because it
cannot preserve advisory-lock ownership. Budget up to 64 database connections
per backend replica, plus capacity for database administration. Keep the
PostgreSQL primary and network available during work. Kubernetes writes use
UID/resource-version and ownership preconditions; losing a database connection
stops lease renewal and execution, but cannot undo an already accepted request.

The default rolling strategy allows one surge replica and zero unavailable
replicas. Allow for the surge replica when sizing PostgreSQL connections.
When upgrading from a version without these coordination rules, first use
`backend.strategy: {type: Recreate, rollingUpdate: null}` so all old workers stop
before new workers start, then increase the replica count. For later releases,
use a rolling upgrade only when their schema/API compatibility permits it.

Crash recovery never automatically replays an ambiguous mutation. Recovery
atomically fails the old operation and plan, preserves completed/current resource
progress, pauses reconciliation, clears retry scheduling, releases the expired
lease, and emits one audit event. Review live state, build a new plan, and obtain
new approvals where required. If a create reached Kubernetes before its inventory
row was stored, review and explicitly adopt that object through the ownership
conflict flow before replanning. Existing reviewed rollback snapshots remain
available. Already-applied resources retain their identities; a fresh plan
converges remaining work. A committed completion is idempotent and cannot be
changed into failure by a late executor or a later recovery sweep.

The integration suite verifies simultaneous migration startup, competing
claims/guards, lease loss, graceful draining, and SIGKILL at acquisition,
resource mutation, progress recording, and before/after terminal commit for
sync, rollback, and decommission. Planner death before/after durable plan
creation is covered as well. Each interrupted mutation is recovered and then
converged using a fresh reviewed plan with two backend instances.

## Metrics and traces

Prometheus metrics and OpenTelemetry tracing are disabled by default. Enable
Prometheus scraping on the cluster-internal backend Service with:

```yaml
backend:
  observability:
    metrics:
      enabled: true
```

When enabled, the Service receives standard Prometheus scrape annotations and
the backend serves `/metrics` on its existing HTTP port. The Ingress continues
to route only to the frontend.

Send traces to an OTLP/HTTP collector with:

```yaml
backend:
  observability:
    tracing:
      enabled: true
      endpoint: http://otel-collector.observability.svc:4318
      sampleRatio: 0.1
      headersSecret: justcd-otel-headers
      headersSecretKey: OTEL_EXPORTER_OTLP_HEADERS
```

The optional Secret key should contain the OTLP exporter header string, for
example `Authorization=Bearer <token>`. Store exporter credentials in a Secret;
do not put them in Helm values or commit them. See
[`services/docs/content/docs/operations/observability.md`](../../services/docs/content/docs/operations/observability.md) for metric names,
cardinality limits, the starter Grafana dashboard, and alert recommendations.

## OpenShift and pod configuration

The default pod security context uses UID/GID 10001. For OpenShift SCCs that
assign IDs from the namespace range, remove the fixed IDs in your deployment
values while retaining the other security defaults:

```yaml
frontend:
  podSecurityContext:
    runAsUser: null
    runAsGroup: null
backend:
  podSecurityContext:
    runAsUser: null
    runAsGroup: null
```

Keep `backend.existingSecret` configured as above. The Secret must exist in the
release namespace and contain both required keys. Empty or whitespace-only
Secret names fail during Helm rendering.

Both `frontend` and `backend` expose the following values:

| Value | Purpose |
| --- | --- |
| `podSecurityContext` | Pod UID/GID, fsGroup, SELinux options and seccomp configuration |
| `securityContext` | Container privileges, capabilities and root filesystem settings |
| `serviceAccountName` | Existing service account, including one with appropriate SCC access |
| `automountServiceAccountToken` | Token mounting, disabled by default |
| `podAnnotations` | Pod annotations for platform integrations |
| `nodeSelector`, `tolerations`, `affinity`, `topologySpreadConstraints` | Placement and scheduling |
| `extraEnv` | Additional environment entries, including Secret/ConfigMap references |
| `extraVolumes`, `extraVolumeMounts` | Writable temporary storage or mounted configuration |

Helm merges security-context maps: set individual default fields to `null` to
remove them, or set the entire context to `null` to omit it. An empty map does
not clear defaults. If enabling `readOnlyRootFilesystem`, provide writable
volumes for paths used by the application and its tools, including `/tmp`.
Backend replicas coordinate through PostgreSQL as described above; configure
`backend.replicaCount` and `backend.strategy` to match your availability and
upgrade requirements.

The manifests can be rendered locally; SCC admission and image compatibility
with the assigned UID must also be verified on your OpenShift cluster.

## Private CA certificates for OIDC and GitLab

To trust your Keycloak or GitLab certificate, reference an existing Secret in the release
namespace:

```yaml
backend:
  customCA:
    existingSecret: keycloak-ca
    secretKey: ca.crt
```

The selected key must contain PEM certificates. For a certificate signed by a
private CA, use the CA certificate/bundle. For a genuinely self-signed server
certificate stored in a TLS Secret, set `secretKey: tls.crt`. Only that key is
mounted; the private key is not mounted.

The backend mounts the certificate read-only and adds its directory through
`SSL_CERT_DIR`, retaining the image's public CA trust. This applies to Go TLS
clients, including OIDC discovery, token exchange, and signing-key retrieval.
For HTTPS Git clone/fetch, the chart also sets `GIT_SSL_CAINFO` to the mounted
bundle and `GIT_SSL_CAPATH` to the image's public CA directory. GitLab API calls
use the Go trust settings above. If Keycloak and GitLab use different private
CAs or self-signed certificates, include both certificates in the selected PEM
bundle. SSH Git connections continue to use the credential's pinned known_hosts.

Certificate hostname and expiry checks remain enabled. The certificate must
cover the hostname in your configured issuer URL.

Apply with the usual `helm upgrade --install` command. After rotating the
certificate in the Secret, restart the backend deployment: Go caches its CA
pool, so updating the mounted file alone does not reload trust. Do not override
`SSL_CERT_DIR`, `GIT_SSL_CAINFO`, or `GIT_SSL_CAPATH` in `backend.extraEnv` when using this option.

See the [production operations runbook](../../services/docs/content/docs/operations/production-operations.md) for coordinated backups, cold restore, least-privilege target RBAC, troubleshooting and tested recovery drills.

Production mode now requires an HTTPS `publicUrl` and `cookieSecure: true`; the backend defaults to `JUSTCD_ENV=production`. For local chart testing with HTTP, explicitly set `JUSTCD_ENV=development` in backend and frontend `extraEnv`. See [security hardening](../../services/docs/content/docs/operations/security-hardening.md) for session management, MFA, shared rate limits and the offline encryption-key rotation procedure.

## Optional documentation service

Set `docs.enabled: true` to deploy Fumadocs independently of the frontend and
backend. It needs no Secret or backend connection. `docs.image` defaults to the
moving `ghcr.io/justlabv1/justcd:docs` image; pin `docs.image.tag` to a published
`docs-vX.Y.Z` release for reproducible deployments. The image becomes available
with the first release containing this service.

Set top-level `docsUrl` to the browser-facing docs origin (for example,
`https://docs.justcd.example.com`) so the frontend Documentation link opens the
deployed site. Without it, the link opens the versioned source on GitHub.

`docs.service.port` defaults to 3000. Enable `docs.ingress.enabled` and configure
`docs.ingress.hosts`, `className`, annotations, and TLS for a dedicated hostname.
The service serves at `/`; path-prefix hosting below `/docs` is not supported by
this build. The frontend Ingress continues to serve the application separately.
See the [docs deployment guide](../../services/docs/content/docs/getting-started/docs-service.md).
