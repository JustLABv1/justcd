# Notification integrations

Workspace owners configure destinations under **Workspace → Settings → Notifications**. Projects were renamed to workspaces; routing remains project scoped. Owners can create/edit routing and connection configuration, select events and minimum severity, queue a test, enable/disable a destination, inspect the last 100 deliveries and every attempt, and delete a destination. Viewers and deployers cannot access configuration or activity. Mutations require the existing session CSRF token and commit an audit event with the change. Configuration is encrypted with purpose-bound AES-GCM and never returned, including webhook URLs, headers, bodies, signing keys and SMTP passwords. Editing routing preserves saved configuration unless replacement is selected.

Supported events:

| Type | Severity | Trigger |
| --- | --- | --- |
| `drift` | warning | New current plan containing changes |
| `approval_requested` | warning | New current plan requiring approvals |
| `sync_failed` | error | Operation enters failed state |
| `unhealthy` | error | Application health enters Degraded or Missing |
| `operation_completed` | info (error on failure) | Operation enters succeeded, failed or cancelled |
| `rollback` | warning (error on failure) | Rollback operation completes |
| `test` | info | Owner requests test delivery, bypassing event/severity filters |

Plans are immutable and reused for identical state, so repeated planning does not emit an event for the same plan. Health events describe transitions rather than every poll. Events are captured transactionally into a database outbox. Capture failures emit a safe PostgreSQL warning with SQLSTATE and do not fail the deployment transaction. The separate worker performs network I/O, never the reconciler. Monitor PostgreSQL logs for capture failures and the activity endpoint for delivery failures. Failed delivery does not change application/operation state.

## Payload contract

Generic webhooks receive JSON with `schemaVersion: 1`, immutable `id`, `type`, `severity`, `workspaceId`, optional `applicationId`/`resourceId`, UTC `occurredAt`, and an absolute `url` back to JustCD. `JUSTCD_FRONTEND_URL` must point to the browser-facing URL. There are no arbitrary data fields: manifests, diffs, Secret values, credentials, free-text errors, and resource content are excluded by construction. Slack receives text plus versioned event metadata; Teams receives an Adaptive Card with versioned event facts and a JustCD link; email includes the event ID, schema version, IDs, timestamp and link as text.

The worker claims rows with PostgreSQL locks and `SKIP LOCKED`, so replicas cannot simultaneously deliver the same row. Delivery is **at least once**: a crash after the provider accepted a request but before commit can resend it. Deduplicate using `Idempotency-Key` (the immutable delivery ID) or the email `Message-ID`; the event ID is also supplied. Integrations that do not honor these identifiers can display duplicates.

HTTPS providers use a 15-second timeout, certificate verification, no redirects, and DNS address checks that deny private, loopback, and link-local networks. SMTP requires STARTTLS with certificate verification (normally port 587), supports username/password authentication and multiple recipients, and has a 15-second connection deadline. Use a reachable trusted SMTP relay. Notification endpoints are owner-selected outbound destinations; restrict network egress at deployment level as well.

HTTP 408, 429, 5xx and transport errors retry; other non-2xx responses are terminal. SMTP 4xx/network errors retry; 5xx and TLS setup failures are terminal. Five attempts maximum, with 30s, 60s, 120s and 240s delays before subsequent attempts. Status, attempt number/time, HTTP status and a safe result code are stored and audited. Provider response bodies and error text are never stored or logged. Disabling cancels queued deliveries; re-enabling applies to new events. Deleting removes the destination and its delivery history; audit events remain.

## Configurable webhooks and signatures

Generic webhooks support POST, PUT or PATCH, a JSON header map and an optional JSON body template. Managed signature/idempotency/transport headers cannot be overridden. Secrets in headers (for example GitLab `PRIVATE-TOKEN`) are encrypted with the rest of the configuration.

Templates use `{{event}}` for raw versioned JSON and `{{id}}`, `{{type}}`, `{{severity}}`, `{{url}}`, `{{workspaceId}}`, `{{applicationId}}`, `{{resourceId}}` for JSON-escaped scalar values. Do not surround these scalar placeholders with quotes. Example GitLab issue body:

```json
{"title": {{type}}, "description": {{url}}, "labels": "justcd"}
```

Set the URL to the GitLab project issues endpoint and add a scoped access token in `PRIVATE-TOKEN`. Use a signing secret of at least 32 characters on every HTTPS destination. Requests include `X-JustCD-Event-ID`, `X-JustCD-Timestamp` (Unix seconds), `Idempotency-Key`, and `X-JustCD-Signature`:

```text
v1=<hex HMAC-SHA256(signingSecret, timestamp + "." + exact_request_body)>
```

Custom receivers should verify the HMAC in constant time, reject old timestamps (for example outside five minutes), and persist the idempotency key before creating an external record. Slack/Teams may ignore these additional headers.

API routes live under `/api/v1/workspaces/{workspaceID}/notifications`: GET/POST collection, PUT/DELETE `/{destinationID}`, POST `/{destinationID}/test`, GET `/activity`. The test endpoint returns 202 with its event ID; refresh activity for the result. Destinations accept name/provider/enabled/events/minimumSeverity and a write-only `config` object. Email config uses smtpHost/smtpPort/from/to/username/password; HTTPS config uses url/signingSecret/method/headers/body.

Run backend unit tests and PostgreSQL integration tests (including `TestIntegrationNotifications`) with `JUSTCD_E2E_DATABASE_URL` set to a disposable database. Tests cover all six event triggers, owner authorization, CSRF, secret exclusion, formatting, signatures, retry identity and bounds, delivery observability, disabled destinations, deletion, and provider/network failures.
