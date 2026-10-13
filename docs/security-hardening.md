# Authentication and secret operations

`JUSTCD_ENV` defaults to `production`. Production requires HTTPS `JUSTCD_PUBLIC_URL` and `JUSTCD_FRONTEND_URL` and `JUSTCD_COOKIE_SECURE=true` (the backend default); startup rejects insecure settings. Set both URLs to the browser-facing origin, even when a TLS reverse proxy connects to the backend over HTTP. The Compose example explicitly uses `JUSTCD_ENV=development` for localhost; switch it to production and configure HTTPS/secure cookies before exposing it. Any other environment value is rejected.

The API and frontend send nosniff, frame denial, referrer and permissions policies. Production sends HSTS. Frontend CSP limits scripts/resources/connects/forms to the app origin, denies objects and framing, and allows inline script/style for Next.js hydration and current UI styling. Development additionally permits eval and WebSocket connections for hot reload. These are baseline headers; deployments requiring nonce-only CSP should enforce nonces through their edge/application configuration. The frontend proxy forwards browser Origin and Sec-Fetch-Site to the backend; login, signup and MFA writes reject cross-site origins. Session-authenticated writes continue to require matching CSRF cookie/header and stored CSRF hash. Session cookies are Secure, HttpOnly, host scoped and SameSite=Strict; the CSRF cookie is intentionally readable. OIDC state cookies are HttpOnly, SameSite=Lax, time limited and bound to single-use database state, with nonce and PKCE verification.

## Sessions and MFA

Open **Settings → Account security** (available to every user) to list active sessions and revoke any session, including the current one. Session IDs are opaque management identifiers, not the bearer token or its hash. The API scopes listing/revocation to the authenticated user; DELETE requires CSRF. Every authenticated request reads session state from PostgreSQL, so revocation takes effect on the next request across replicas; requests already executing can finish. Revocation is audited. Authentication creates fresh random session/CSRF tokens and never adopts a supplied session ID.

Local password accounts may enroll TOTP under Account security. Re-enter the password, add the displayed key to an authenticator app (SHA-1, six digits, 30 seconds), and confirm its code within ten minutes. Enrollment is inactive until confirmed. Confirmation revokes all old sessions and issues a new session after factor verification. Save the ten recovery codes immediately; they are shown only once. They contain 128 bits of randomness, are stored only as hashes, and each can be consumed exactly once. TOTP accepts one adjacent time step for clock skew and rejects previously consumed steps across replicas. Keep clocks synchronized.

Login first verifies the password. When MFA is enabled, it returns `mfaRequired: true` without creating a session; resubmit the password and authenticator/recovery code to complete sign-in. The frontend handles this challenge. Enrollment, challenges, failed verification, recovery usage and removal are audited without secrets/codes/passwords. Removing MFA requires the password and a valid factor/recovery code, and signs out all sessions. If recovery codes were lost before saving, retain the authenticator, sign in, remove MFA and enroll again to generate a new set. OIDC sign-ins rely on the identity provider's MFA policy; local MFA protects the local password path.

Recovery is deliberately not an unauthenticated bypass. If both the authenticator and recovery codes are lost, contact the operator for identity-verified account recovery under the organization's policy. Never reset MFA just based on an email message. Operators should separately retain a protected emergency administrator account.

## Rate limits

PostgreSQL atomic counters share limits across backend replicas. Limits are checked before password hashing/discovery/provider testing where applicable:

| Operation | Bucket and limit |
| --- | --- |
| Login and signup | Backend peer IP: 120 per 15 minutes |
| Login | Normalized account digest: 8 per 15 minutes, independent of existence |
| OIDC start/callback | Backend peer IP: 60 per minute |
| Git/Kubernetes credential tests | Authenticated user: 10 per minute |
| MFA enrollment/confirmation/removal | Authenticated user: 8 per 15 minutes |

The app does not trust arbitrary forwarded IP headers. Behind the frontend proxy, the peer bucket is shared, with a higher cap; account/user buckets prevent one account's abuse from locking out unrelated accounts. Use trusted edge rate limiting for client IP protection before the proxy on high-traffic deployments. Limits count all attempts, including successes, and reset after their fixed window. Rate-limit responses are generic and identical for known/unknown accounts; logs contain only the category, never email/password. Responses include Retry-After. Watch `security rate limit exceeded` warnings and HTTP 429 metrics/logs. Counter keys are SHA-256 digests, and counters older than a day are pruned hourly. Database failures fail closed.

## Encryption-key rotation and recovery

The backend supports the maintenance command `justcd rotate-encryption-key` (or `go run . rotate-encryption-key` from services/backend). It reads the current `JUSTCD_ENCRYPTION_KEY`, the replacement `JUSTCD_NEW_ENCRYPTION_KEY`, and normal database/config settings from the environment. Supply keys through your secret manager or a protected environment file; never put them in command arguments, tickets or logs. Keys use the existing 32-byte raw/hex/base64 format.

1. Take and verify a database backup, and securely retain its matching old key. A database backup without its key cannot recover encrypted values.
2. Stop **all** backend replicas, workers, agents issuing requests and other database writers. Drain operations first. Merely enabling UI maintenance does not stop workers and is insufficient. Keep the frontend offline during rotation.
3. Run one backend process with the current key and JUSTCD_NEW_ENCRYPTION_KEY, using the same version/schema and database as the deployment. Rotation takes exclusive table locks and one transaction and exits without starting HTTP/workers. Allow enough time for rollback snapshots and task payloads; command timeout is 30 minutes.
4. On success, update JUSTCD_ENCRYPTION_KEY to the replacement on **every** backend/worker, remove JUSTCD_NEW_ENCRYPTION_KEY, then restart the deployment. Verify credential tests, OIDC, MFA and a rollback snapshot before resuming normal work.
5. Retain old keys securely for retained backups; destroy them only when matching backups are retired. Database bytes after rotation require the new key.

Rotation re-encrypts credentials, OIDC client secrets, source-control webhook/status tokens, Git push webhook secrets, rollback snapshots, both agent task payloads, MFA seeds, and notification configuration if that feature is installed. Every value retains its purpose binding. Hash-only session, agent and recovery identities require no re-encryption. Audit records contain only the value count.

If a value fails decryption or a database error occurs, **the whole transaction rolls back**. Restart using the old key, investigate without exposing ciphertext/secret material, and retry after resolving the problem. If the process dies before commit, PostgreSQL rolls it back. If the command response was lost, do not guess: keep writers stopped, inspect the `security.encryption_key_rotated` audit event and verify a known credential using the candidate key in an isolated read-only maintenance process before restarting. If rotation committed but rollout must be reverted, run the same command with the new key as current and old key as replacement while all writers remain stopped. Alternatively restore the verified pre-rotation backup **with its old key** using the documented restore procedure.

Tests include RFC TOTP vectors/replay/expiry/skew, recovery consumption, CSRF/origin rejection, session fixation/cookie policies, cross-replica revocation/throttling, provider-test/callback throttling, and atomic key rotation/rollback. Run `go test ./...`, and opt into PostgreSQL tests with JUSTCD_E2E_DATABASE_URL pointing to a disposable database and `go test -tags integration ./internal/store ./internal/api`.
