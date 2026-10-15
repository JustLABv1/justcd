# Audit evidence and exports (schema version 1)

The audit trail records configuration changes, access decisions, plans, and operation outcomes. Select an event to open its detail panel. Details show recorded actor and target identifiers, timestamps, and redacted event fields. Deployment shows evidence captured for the event; Raw shows its API representation. Related lists events for the same resource among the loaded results; Resource history queries the complete trail. IP addresses, authentication methods, and sessions are not inferred when they were not recorded.

## Authorization and filtering

Instance administrators can read and export the entire instance. Other authenticated users must supply `workspaceId` and hold at least a viewer role in that workspace, including an effective OIDC membership. Retention changes require an owner or instance administrator and CSRF protection. Events without a recoverable workspace remain administrator-only. A workspace filter excludes instance-wide events, including account and session events.

`GET /api/v1/audit` returns `{schemaVersion:1, items:[...], hasMore:boolean}`. It returns up to 50 records, ordered by descending audit ID. Pass the last ID as `before` for the next page. `GET /api/v1/audit/{eventID}` reads one authorized event, including old events linked directly from a panel.

List and export endpoints accept the same parameters:

| Parameter | Meaning |
| --- | --- |
| `workspaceId` | Exact workspace ID; required for non-administrators |
| `group` | `all` (default), `changes`, `deployments`, or `access` |
| `q` | Case-insensitive literal search of event ID, action, actor, and resource identity/name; maximum 500 bytes |
| `resourceId`, `actorId` | Exact stable identifier |
| `eventId` | One exact audit ID |
| `from` | Inclusive RFC3339 timestamp |
| `until` | Exclusive RFC3339 timestamp |
| `before` | Exclusive ID cursor |

Search runs on the server, including older records. It does not search arbitrary detail payloads. Export buttons use the active workspace, group, search, and resource filters and include all matches, independently of the number of loaded UI pages.

## Portable records

`GET /api/v1/audit/export?format=json` downloads a JSON envelope containing `schemaVersion`, `exportedAt`, `items`, and a final `complete:true`. `format=csv` downloads UTF-8 CSV with a header row. Responses use `Cache-Control: no-store` and an attachment filename. JSON is recommended for automated evidence processing.

Each item contains these stable fields:

- `schemaVersion` (currently `1`), numeric `id`, `workspaceId`.
- Optional `actorId`, `actorName`. Account deletion retains the actor ID and changes its name to `Deleted user` through the existing anonymization flow.
- `action`, `resourceType`, `resourceId`, `resourceName`, and RFC3339 `createdAt`.
- `details`: a conservative export projection of identifiers, revision/digest, approval IDs, numeric counts, error code, retention days, and anonymization flag.
- `deploymentRecord`: captured evidence or `null` if unavailable.
- `sha256`: record integrity checksum, described below.

Deployment evidence includes its own `schemaVersion`, application/workspace/plan IDs, commit revision, plan digest and creation time, cluster/namespace targets, required approvals, captured approval IDs/actors/digests/timestamps, policy mode/configuration digest/rules/resource identities, and the operation ID/type/result/error code/start/finish times. Actor and event time are on the containing audit record. Missing historical fields remain null or empty. Plan-only events have no operation yet; a queued event preserves the queued state and a later completion event preserves the outcome.

Snapshots are captured transactionally by the audit insert trigger so writers that use direct SQL and writers that use `Store.Audit` have the same behavior. Existing rows are backfilled from source records still available at migration time. Captured scope, resource name, and deployment evidence survive application/plan/operation deletion. Evidence lost before this migration cannot be reconstructed. Deletion events can recover scope from prior scoped events for that resource.

Exports exclude credential references/values, manifests, Secret data, Helm values/settings, approval comments, free-text policy evidence/remediation, and arbitrary custom event fields. Unknown fields are omitted. The API detail view applies the existing credential/Secret redaction and removes historical rollback settings. CSV string values that could execute spreadsheet formulas are prefixed with an apostrophe; JSON preserves their original values.

## Streaming and consistency

The server queries at most 500 records per batch and flushes output between batches. It fixes the highest matching ID on the first batch, so later inserts cannot extend a running export. It respects request cancellation and aborts the connection on a query failure after output has begun. JSON completion is marked only after the final batch. Direct backend responses also provide the `X-Audit-Export-Complete` HTTP trailer; intermediaries may discard trailers. Consumers must reject interrupted downloads and JSON without `complete:true`.

The export is a bounded traversal, not a database snapshot: concurrent retention, account anonymization, or other administrative changes may affect later batches. Existing HTTP server/proxy timeouts still apply; narrow the time window for exceptionally large histories.

## Checksums

A checksum detects record changes; it is not a signature or proof of origin. The SHA-256 input is the event object without `sha256`, encoded with Go `encoding/json`: recursively sorted object keys, compact encoding, preserved JSON numbers (`Decoder.UseNumber`), UTF-8, and default HTML escaping. Re-encoding the JSON export using the same rules reproduces the digest. The CSV checksum refers to the original JSON event before spreadsheet-safe prefixes; use the JSON export for verification. Cryptographic signing and key distribution are not enabled.

An independent Go verifier can decode each item into `map[string]any` with `UseNumber`, remove the `sha256` property, call `json.Marshal`, and compare `sha256.Sum256` with the supplied hex digest. An additive or incompatible schema change must document its version and retain consumer compatibility deliberately.

## Retention

`GET /api/v1/workspaces/{workspaceID}/audit-retention` returns `{days, defaultDays}`. `PUT` accepts `{days: integer}` from 0 through 36500. The default is **0, keep indefinitely**, preserving existing behavior. The audit page exposes this setting when an authorized owner selects a workspace. Each update is itself audited.

The hourly maintenance loop deletes at most 5,000 expired events per run across workspaces. Lowering retention permanently deletes older records over subsequent cleanup runs; export required evidence first. Retention uses `created_at`, not audit ID. It does not expire operational plans or other application history. Instance-wide events and evidence for deleted workspaces are retained indefinitely because there is no surviving workspace policy to apply. Restoring indefinite retention does not restore deleted evidence.
