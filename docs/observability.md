# Observability

JustCD exposes Prometheus metrics and can export distributed traces through an
OTLP/HTTP collector. Both are disabled by default. The Helm chart keeps the
metrics endpoint on the cluster-internal backend Service; the public Ingress
continues to route through the frontend.

## Prometheus metrics

Set `backend.observability.metrics.enabled: true` in Helm values. Scrape the
backend Service at `/metrics` on its configured HTTP port. The chart adds
`prometheus.io/scrape`, `prometheus.io/path`, and `prometheus.io/port`
annotations when enabled.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `justcd_http_requests_total` | Counter | `method`, `route`, `status` | Completed API and health requests. |
| `justcd_http_request_duration_seconds` | Histogram | `method`, `route` | HTTP request latency. |
| `justcd_reconciliation_checks_total` | Counter | `result` | Automatic plan checks, grouped as `in_sync`, `drift`, or `failed`. |
| `justcd_reconciliation_check_duration_seconds` | Histogram | `result` | Time to build an automatic plan. |
| `justcd_drift_changes_total` | Counter | `kind` | Create, update, and delete changes found by automatic checks. |
| `justcd_operations_completed_total` | Counter | `type`, `status` | Completed sync, rollback, and decommission operations. |
| `justcd_operation_duration_seconds` | Histogram | `type`, `status` | End-to-end operation duration. |
| `justcd_operation_queue_depth` | Gauge | `status` | Durable queued and running operation counts. |
| `justcd_plan_approvals_total` | Counter | `state` | Recorded approvals that are pending or meet the plan threshold. |

Label vocabularies are bounded: HTTP methods are standard verbs plus `OTHER`,
routes are fixed route groups rather than raw URLs, HTTP statuses are grouped by
class, operation types and statuses are normalized enums, and change kinds are
create/update/delete. Metrics never carry user, project, application, plan,
operation, resource, namespace, repository, or cluster identifiers.

The HTTP counters omit `/metrics` scrapes so scrape traffic does not distort API
latency or request rates. Prometheus's default Go runtime and process collectors
are included.

## OpenTelemetry traces and logs

Set `backend.observability.tracing.enabled: true` and configure the OTLP/HTTP
endpoint. The chart accepts an optional Kubernetes Secret reference for
`OTEL_EXPORTER_OTLP_HEADERS`; `sampleRatio` defaults to 0.1 and is applied with
parent-based sampling. Incoming W3C `traceparent` context is propagated through
API requests, plan building, rendering, PostgreSQL queries, and queued apply
workers. Queued operations persist only the W3C traceparent so work performed
by a background worker remains in the initiating request's trace.

Structured JSON logs include `request_id`, `trace_id`, and `span_id` when
available. Apply logs include the application, plan, and operation IDs. A
request ID is returned as `X-Request-ID` for support correlation.

Telemetry deliberately excludes request paths with identifiers, request or
response bodies, headers, SQL statements, SQL parameters, database connection
details, Kubernetes manifests, Secret data, credential values, and error
messages. PostgreSQL spans use only the operation name. Error spans include a
static error type but never the error string. Structured log handlers redact
error text and fields whose keys indicate passwords, tokens, secrets,
credentials, URLs, manifests, payloads, or headers.

## Grafana dashboard

Import [`charts/justcd/dashboards/justcd-overview.json`](../charts/justcd/dashboards/justcd-overview.json)
and select the Prometheus datasource. The dashboard covers request rate and
p95 latency, operation queue depth, operation outcomes, reconciliation
failures, drift changes, and plan approvals.

Suggested alert starting points (tune to the deployment's traffic and SLOs):

- API error ratio above 5% for 10 minutes, using `justcd_http_requests_total`
  and 5xx status groups.
- API p95 latency above 2 seconds for 10 minutes, using
  `justcd_http_request_duration_seconds`.
- Any queued operation older than 15 minutes, or a queue depth that keeps
  increasing for 10 minutes. The metric reports counts; age should be checked
  from the operations API or database.
- Reconciliation failures above 10% over 15 minutes, excluding periods with no
  automatic checks.
- No successful operation for 24 hours only when the service has expected
  deployment traffic; quiet environments should not page on this condition.
