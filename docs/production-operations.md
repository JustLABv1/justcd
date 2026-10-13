# Production operations and disaster recovery

Use this guide with the [Helm installation instructions](../charts/justcd/README.md).
The tested baseline is the web/API chart, external PostgreSQL 16 and direct
Kubernetes connections. [Cluster agents](cluster-agents.md) require separate
backup of their deployment, bootstrap/trust configuration and token recovery.
Retain the replica/rollout settings supported by your pinned chart and backend
version; do not scale older worker implementations by hand.

## Installation and configuration

1. Assign an operations owner and choose a recovery point objective (RPO),
   recovery time objective (RTO), backup retention and alert destination. Daily
   dumps can lose up to 24 hours of database changes. Shorter RPOs require more
   frequent backups or provider-managed PostgreSQL WAL/PITR. A successful dump
   is not a successful restore.
2. Provision external PostgreSQL with durable storage, restricted network
   access and TLS verification (`sslmode=verify-full`). The dedicated database
   owner needs startup schema/table migration permissions, not PostgreSQL
   superuser. Back up database roles, TLS trust and provider settings separately:
   database-only dumps omit cluster-wide roles.
3. Publish backend/frontend images from the same reviewed release and record
   immutable image digests, chart version/source commit, nonsecret values and
   database major version. Avoid mutable `latest` images. Test private CA mounts,
   image-pull Secrets, ingress and resource limits before enrolling targets.
4. Generate the encryption key once (`openssl rand -hex 32`) in a private session.
   Keep it in a secrets manager and an independent recovery backup. Record the
   vault version identifier, never its value. Changing the key does not rotate
   credentials; it makes existing encrypted values unreadable.
5. Create `backend.existingSecret` using owner-only files outside Git as in the
   chart guide. Use `umask 077`; never put passwords, keys, tokens, kubeconfigs,
   OIDC secrets or exporter headers in values, shell history, CI logs or PRs.
   Kubernetes Secret base64 is not encryption; configure encryption at rest and
   restricted access to Secrets in the hosting cluster.
6. Configure HTTPS `publicUrl`, secure cookies and ingress TLS. Keep the backend
   Service and `/metrics` internal. Restrict egress to PostgreSQL, Git, target
   APIs and OTLP. The frontend proxies same-origin API calls. The chart's service
   account does not need target Kubernetes permissions; configured connection
   credentials provide that access.
7. Run the chart guide's `helm upgrade --install` with `--wait --timeout 5m`.
   Check both deployments and `/healthz`, then first-admin signup, onboarding,
   local emergency admin access, OIDC, safe connection tests and a reviewed
   canary plan/apply. Restrict initial signup until your administrator exists.
   Process health alone does not verify credential decryption or target access.

## Background plan and approval tasks

Plan generation and approval requests return `202 Accepted` immediately after
saving a task in PostgreSQL. Git reads, cluster inspection and approval freshness
checks execute in a bounded background worker pool. The task's `Location` header
points to `/api/v1/tasks/{taskID}`. Poll until `status` is `succeeded` or `failed`;
then read `result` and `httpStatus`. Failed results preserve structured error
metadata, including ownership conflicts and refreshed stale plans. An accepted
task does not mean an approval has been recorded.

Clients should send a unique `Idempotency-Key` per intended action and reuse it
when retrying a lost queue response. Concurrent requests for the same actor,
application and action share active work. Keys cannot be reused with different
input. The approval POST requires the reviewed `planDigest`; the worker reloads
the user and workspace permissions and recalculates plan freshness before saving
approval. Batch approval queues one task per plan and can partially succeed.

The frontend shows queued/running work on the application page and approval
inbox, and restores task status after navigation or reload. Task reads require
both the original requester and current workspace access. Request inputs and
lease tokens are never exposed in the status response. Deploy the backend and
frontend together; this changes the plan/approval POST response contract from a
completed action to a background task.

Queued tasks survive backend restarts. Running tasks renew a 90-second lease,
have a five-minute execution limit and drain during the backend's shutdown grace
period. Expired leases become `task.interrupted` failures without automatic
replay: an approval may already have committed. Refresh plans, approvals and
operations before resubmitting interrupted work. Task outcomes and idempotency
keys remain stored until their application is deleted.

For slow or failed tasks, inspect `task.execute`, `plan.build` and
`plan.calculate` traces, backend logs, Git connectivity, Kubernetes API latency
and agent connectivity. Gateway timeouts now affect only the short submission
and polling requests for these actions. If an HTML 504 still occurs on them,
check database/API availability and gateway configuration. Other synchronous
API actions continue to use the backend's 90-second HTTP write timeout.

## Least-privilege target RBAC

Start with [rbac-namespaced.yaml](operations/rbac-namespaced.yaml). Its dedicated
service account can manage **only ConfigMaps** in the existing `justcd-target`
namespace. Bind this credential to that namespace in JustCD. Add explicit API
groups, types and verbs required by rendered resources and health checks.
Deployment health also needs reads of related ReplicaSets/Pods; mutation needs
patch/create/delete as appropriate. Logs and exec require separate `pods/log`
and `pods/exec` grants. Secrets, RBAC objects and wildcard privileges are absent.

Namespace Roles cannot manage Namespaces, CRDs, ClusterRoles or other
cluster-scoped types. Use a different identity and the optional
[rbac-cluster-scoped.yaml](operations/rbac-cluster-scoped.yaml) only when reviewed
plans require namespace creation, and set it as JustCD's cluster-scope credential.
Keep the limited identity for normal workloads. This example can manage **any**
namespace: `create` cannot be constrained by `resourceNames`. Remove write verbs
if only reads are needed and use admission policy/review to limit names. Other
cluster-scoped types require separate, explicitly reviewed ClusterRole grants.

Verify exact permissions (the caller needs impersonation permission):

```sh
kubectl auth can-i create configmaps -n justcd-target \
  --as=system:serviceaccount:justcd-target:justcd-deployer # yes
kubectl auth can-i create configmaps -n default \
  --as=system:serviceaccount:justcd-target:justcd-deployer # no
kubectl auth can-i create namespaces \
  --as=system:serviceaccount:justcd-target:justcd-deployer # no
kubectl auth can-i create clusterroles \
  --as=system:serviceaccount:justcd-target:justcd-cluster-deployer # no
```

Prefer short-lived TokenRequest credentials when your rotation automation can
refresh them before expiry. Keep token output private and never commit kubeconfigs.
Pod creation can expose service-account or Secret data within that namespace.
See Kubernetes [RBAC good practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
and [service accounts](https://kubernetes.io/docs/concepts/security/service-accounts/).
The safe connection test uses discovery and SelfSubjectAccessReview; it cannot
prove every manifest/admission policy will allow later mutation.

## Coordinated backup

Keep the database archive and key backup in separate encrypted stores with
independently tested recovery access. Database metadata, manifests and audit data
can remain sensitive even when credentials are encrypted. Define retention,
immutable/offsite copies and vault recovery ownership. Prefer provider-native
backups/PITR in addition to dumps for a strict RPO.

Stop user submissions, pause auto-sync, and wait for queued/running operations
to finish. Stop **all** backends using the database, including other hosts or
clusters, to stop sync/repository/PR workflows. Record previously unpaused
applications and desired replica counts. Scaling one deployment does not fence
another deployment sharing the database.

```sh
kubectl -n justcd get deployment justcd-backend -o jsonpath='{.spec.replicas}'
kubectl -n justcd scale deployment justcd-backend --replicas=0
kubectl -n justcd wait --for=delete pod \
  -l app.kubernetes.io/instance=justcd,app.kubernetes.io/component=backend --timeout=120s
```

Use PostgreSQL 16 tools with an off-repo service definition and protected
`.pgpass` or secret injection. Assume `justcd-source` and `justcd-restored` are
entries in `pg_service.conf`; `PGSERVICEFILE`/`PGPASSFILE` point to owner-only files
with TLS verification. The restored entry must point to a **new empty database**
owned by the replacement JustCD role. Passwords must not appear in CLI arguments.
Run the following on a Linux administration host with `age` installed:

```sh
umask 077
pg_dump --dbname=service=justcd-source --format=custom \
  --no-owner --no-acl --file=justcd.dump
pg_restore --list justcd.dump > justcd.contents
sha256sum justcd.dump > justcd.dump.sha256
age --recipient "$BACKUP_RECIPIENT" --output justcd.dump.age justcd.dump
```

[pg_dump](https://www.postgresql.org/docs/16/app-pgdump.html) gives a consistent
PostgreSQL snapshot, but does not coordinate that snapshot with Kubernetes.
Stopping workers reduces in-flight ambiguity. Custom archives use
[pg_restore](https://www.postgresql.org/docs/16/app-pgrestore.html).
Verify upload checksums/retention, then remove plaintext files through your
encrypted workspace lifecycle. A checksum detects corruption, not malicious
replacement: retain it in trusted storage and use authenticated encryption.

Back up the exact key from your vault to an independently protected recovery
store and record its version in the inventory. If exporting from Kubernetes is
necessary, use a private temporary directory and never enable shell tracing:

```sh
umask 077
kubectl -n justcd get secret justcd-config \
  -o jsonpath='{.data.JUSTCD_ENCRYPTION_KEY}' | base64 --decode > encryption-key
age --recipient "$KEY_BACKUP_RECIPIENT" \
  --output encryption-key.age encryption-key
```

Store key ciphertext separately from the database ciphertext. Back up nonsecret
values, chart/images, migration versions, CA bundles, OIDC settings and target
identities. Repositories and Kubernetes persistent volumes need **their own**
backups: the JustCD database does not back up deployed application data. Restart
with the recorded replica count and resume policies deliberately after validation.

## Cold restore

1. Declare the incident, block submissions and fence **all old backends** from
   PostgreSQL and target APIs before starting restored workers. Otherwise old
   and restored workers can race against newer live Kubernetes state.
2. Provision an empty database with the same schema/search-path. Retrieve the
   matching key, original chart/images and trust bundles. Verify the backup
   before starting newer images whose startup migrations would change it.
3. Decrypt into restricted temporary storage and restore as the database owner:

   ```sh
   age --decrypt --identity "$BACKUP_IDENTITY_FILE" --output justcd.dump justcd.dump.age
   sha256sum --check justcd.dump.sha256
   pg_restore --dbname=service=justcd-restored --no-owner --no-acl \
     --exit-on-error --single-transaction justcd.dump
   psql --dbname=service=justcd-restored -v ON_ERROR_STOP=1 \
     -c 'SELECT version FROM schema_migrations ORDER BY version;'
   ```

4. Compare credential IDs/ciphertext, audit entries/timestamps, plan
   IDs/digests/status and operation IDs/status/progress against the inventory.
   Decrypt representative Git and Kubernetes credentials with the original key
   without logging values. Table counts alone cannot prove recovery. The
   isolated drill below verifies exact fingerprints and real decryption.
5. Before workers start, execute the tested cold-restore transition:

   ```sh
   psql --dbname=service=justcd-restored -v ON_ERROR_STOP=1 \
     --file=docs/operations/quarantine-restored.sql
   ```

   [quarantine-restored.sql](operations/quarantine-restored.sql) records the prior automation switches in a recovery audit event, disables source-control/preview polling and cleanup, repository discovery and repository PR settings, pauses auto-sync,
   cancels old queued work, marks restored running work failed, preserves partial
   progress/completed history, removes leases, invalidates current plans and
   appends an audit event. Application pause alone does not fence PR preview mutation or repository discovery; all these switches must remain disabled before startup. It does not mutate Kubernetes. Keep the original dump
   for forensics. This assumes the documented schema and is **not** a live retry
   or maintenance command; old workers must already be fenced.
6. Restore the Secret using the original key and new TLS-verified database URL
   from protected files, then start the recorded Helm version. Keep file contents
   exact; do not add a newline to a raw 32-byte key. For hexadecimal keys:

   ```sh
   age --decrypt --identity "$KEY_BACKUP_IDENTITY_FILE" \
     --output encryption-key encryption-key.age
   # database-url is supplied privately by your secrets manager, not an echo command.
   kubectl -n justcd create secret generic justcd-config \
     --from-file=JUSTCD_DATABASE_URL=database-url \
     --from-file=JUSTCD_ENCRYPTION_KEY=encryption-key \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

7. Confirm readiness, local/OIDC login, workspace memberships, decrypted
   connection tests and restored history. Secret environment changes require new
   Pods. Compare live resources with restored inventory: the cluster may have
   changed since the backup. Build **new** plans/approvals, review partial changes,
   ownership and pruning, and explicitly adopt only verified resources missing
   from old inventory. Do not delete/recreate them just to remove a conflict.
8. Run a reviewed canary, then use the recovery audit event’s `priorAutomation` inventory to resume selected application, repository, source-control and PR/preview switches deliberately. Review outstanding preview cleanup/expiry and Git-managed settings before re-enabling them; enabling repository discovery can recreate or overwrite application/PR settings from Git. Do not bulk-replay old switches or blindly restore them from SQL. Reopen submissions after that review. Record
   actual RPO/RTO, discrepancies and follow-up actions. No database backup can
   guarantee exactly-once mutations across PostgreSQL and Kubernetes.

## Recovery boundaries

| Incident | Action and limitation |
| --- | --- |
| Pod/node lost, DB/key intact | Replace Pods using the same chart/images. Inspect interrupted work and live resources, then build fresh plans. No database restore needed. |
| DB lost, key available | Restore archive/PITR point, quarantine pending work, compare live state and canary. History after the recovery point is absent. |
| Key lost, DB available | Recover the **exact** key. A new key/password reset cannot decrypt credentials, OIDC client secrets or rollback snapshot payloads. Without the original key, replace secrets from upstream systems and discard unusable encrypted rollback recovery paths after preserving forensics. Audit/plan/operation metadata remains readable. No built-in global re-encryption/key-rotation procedure exists. |
| Key/DB compromised | Fence workers and revoke/rotate upstream Git, Kubernetes and OIDC credentials. Restore trusted copies and review memberships/sessions/audit. Backup restoration does not revoke exposed secrets. |
| Target cluster lost | Restore cluster and volumes using its own recovery system. Reconnect verified credentials and build fresh plans. Manifest rollback snapshots do not restore PV contents or external services. |
| Git/registry/OIDC outage | Restore upstream access/trust; use tested local emergency admin access if necessary. Avoid bypassing TLS. Existing workloads can continue while JustCD cannot plan/authenticate. |

## Tested recovery drill

Run `bash scripts/restore-drill.sh` on an isolated host with Go (from `go.mod`),
Docker/Podman, kind, kubectl, Helm, OpenSSL, ripgrep, and [age/age-keygen](https://github.com/FiloSottile/age). The runner builds actual
repository Dockerfiles, creates a unique kind cluster and installs the supported
Helm chart with `existingSecret` and PostgreSQL 16. It seeds synthetic encrypted
Git/Kubernetes credentials, plans, terminal/queued/running operations and audit
history; stops workers; encrypts the dump/key with separate age recipients, decrypts and restores into a new DB; compares exact ciphertext,
records and timestamps; and decrypts both credential types without exposing them.
It verifies wrong-key rejection, runs the quarantine script, preserves partial
progress, verifies repository discovery, PR discovery/polling/review/cleanup selectors return no work and prior switches are recorded; starts the Helm backend using the original key/new DB, checks health
and verifies history again. Positive/negative RBAC tests verify namespace and
cluster access. Its HTTP/plaintext-DB settings are limited to the disposable
cluster. Cleanup deletes only its own cluster/files; it never uses your current
kubeconfig or developer database.

For locally available release images, set `JUSTCD_DRILL_BACKEND_IMAGE` and
`JUSTCD_DRILL_FRONTEND_IMAGE`, and retain their digests with the result. Rehearse
quarterly, before risky upgrades and after vault/backup-policy changes. Also run
a staging drill exercising actual vault permissions, encrypted archive storage,
TLS/ingress, OIDC, a read-only representative repo and a sandbox namespace.
Record elapsed restore time, newest recovered audit timestamp, connection/canary
results and cleanup without attaching secrets. The synthetic drill does not
prove access to your vault, cloud PITR or recovery of application volume data.

## Monitoring, capacity and troubleshooting

Use [observability.md](observability.md), the Grafana dashboard and starter alert
thresholds. Alert on unhealthy Pods, DB connectivity/storage, 5xx/latency,
sustained queue growth, reconciliation failures, backup age, last successful
drill and upcoming token/CA expiry. Restrict metrics/OTLP transport and collector
retention. Queue depth is not queue age; inspect operation timestamps separately.

Size from representative Git/rendering workload, resource count, target latency
and concurrency. At the tested baseline `482d673`, one backend has a 20-connection
PostgreSQL pool and 20 worker slots; default cluster limits are two concurrent
operations and 30 starts/minute. Later releases may change this: inspect the
pinned chart/source and budget every replica plus administration/migrations.
Start with chart resources, load-test Helm/Kustomize, and monitor throttling,
OOM/restarts, RSS, DB connections, disk growth and backup duration. Allow writable
`/tmp` for Git/rendering with read-only roots. Increase target throughput only
after measuring API throttling and approval load. No supported audit-retention
setting is implied; do not prune audit tables ad hoc.

| Symptom | Checks and action |
| --- | --- |
| Startup failure | Check redacted logs, required Secret keys, DB DNS/TLS/role permissions, migration ledger and image compatibility. Do not drop migration tables. |
| Ready Pods/browser failure | Check ingress host/TLS, `publicUrl`, frontend/backend routing, network policies and cookie origin. Health is not login verification. |
| Decryption failure | Confirm exact key version and credential ID/purpose. Restore the key; distinguish this from upstream token expiry. Never generate a replacement as a fix. |
| Kubernetes forbidden | Safe connection test plus exact resource/verb/namespace `auth can-i` and admission policy. Grant only needed permissions to the correct identity. |
| Git/OIDC TLS failure | Verify hostname/expiry/CA bundle. Restart backend after trust rotation to reload CA roots; do not disable verification. |
| Queue/backoff growth | Inspect operation progress, target throttling, cluster rate/concurrency and plan size. Resolve error, then review fresh plans; avoid live SQL status edits. |
| Ownership conflicts after restore | Compare UID/labels/inventory/timeline. Adopt only verified resources with a fresh plan; do not force-delete. |

Support evidence: release/chart digests, sanitized values, migration versions,
plan/operation/request/trace IDs, timestamps, redacted logs and last drill result.
Exclude credentials, raw secret manifests and database dumps.

## Upgrade and rollback

Read release/migration notes, run the candidate drill, schedule maintenance, take
a coordinated backup and record old image digests and `helm history justcd -n
justcd`. Review `helm template`, then upgrade with `--wait --timeout 5m` using
supported rollout settings. Concurrent old/new versions require explicit schema
compatibility, not just healthy Pods. Verify migrations, login/connections,
history, queue state and canary before resuming policies; monitor errors/backlog.

`helm rollback justcd REVISION -n justcd --wait --timeout 5m` restores release
manifests, **not** PostgreSQL schema/data. Use it only if the old image is
compatible with the current schema. Otherwise fence workers and cold-restore the
pre-upgrade DB plus matching key/chart/images, accepting the recorded RPO and
reviewing target changes since backup. Do not invent down migrations or assume
an older image will reverse startup migrations. Test compatibility and rollback
on staging first.

Authentication security, session revocation, local MFA and transactional encryption-key rotation are documented in the [security hardening runbook](security-hardening.md). Rotation requires stopping all backend writers; UI maintenance alone is insufficient.
