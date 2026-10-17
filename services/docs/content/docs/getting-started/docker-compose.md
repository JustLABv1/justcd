---
title: "Docker Compose installation"
---

Run JustCD on a single server without installing the control plane in Kubernetes.
The stack includes PostgreSQL 17, the Go backend (including Git, SSH, Helm, and
Kustomize), and the Next.js frontend. Target applications still run in Kubernetes.
The server needs outbound access to your Git hosts, identity provider, and
Kubernetes APIs; use an [outbound cluster agent](/operations/cluster-agents) for private APIs.

## Install

Install Docker Engine and the Docker Compose plugin (`docker compose`). Use a
current version with support for `up --wait`. Published JustCD images currently
target Linux amd64; see [source builds](#build-from-source) for another architecture.
You also need Git and OpenSSL for the commands below.

```sh
git clone https://github.com/JustLABv1/justcd.git
cd justcd
cp .env.example .env
chmod 600 .env
openssl rand -hex 32
openssl rand -hex 32
```

Edit `.env`: paste the first generated value into `JUSTCD_POSTGRES_PASSWORD`
and the second into `JUSTCD_ENCRYPTION_KEY`. Use the hex password as generated:
the stack embeds it in a PostgreSQL URL, so arbitrary passwords would need URL
encoding. Keep the encryption key stable and back up `.env` securely. Losing the
key makes saved Git, Kubernetes, and OIDC credentials unreadable.

For a server, set `JUSTCD_PUBLIC_URL` to your browser-facing HTTPS origin and
`JUSTCD_COOKIE_SECURE=true`, and configure the proxy described below before signup.
For an initial local HTTP check, keep the example's localhost URL and insecure
cookie setting and use an SSH tunnel:

```sh
# On your workstation; keep this connection open.
ssh -L 3000:127.0.0.1:3000 user@your-server
```

Start the stack from the checkout on your server:

```sh
docker compose config --quiet
docker compose pull
docker compose up -d --wait
docker compose ps
```

Compose waits for database health before starting the backend, and for backend
health before starting the frontend, using Docker's
[dependency health checks](https://docs.docker.com/compose/how-tos/startup-order/).
The backend applies schema migrations on startup. Only the frontend port is
published; the database and backend communicate on the Compose network.

Open your configured public URL (or `http://localhost:3000` through the tunnel),
visit `/signup`, and create the first administrator. Complete onboarding to add
workspaces, Git sources, and Kubernetes connections. Protect access until the
first administrator has registered. For OIDC, register the exact callback URL
returned by JustCD using this same public origin.

PostgreSQL data lives in the named `justcd_postgres-data` volume. Application
configuration and encrypted credentials are stored there; containers can be
recreated without losing them. Keep the same Compose project name and volume
when restarting or moving the installation.

## HTTPS on a server

Point your domain at the server and configure an HTTPS reverse proxy on the host
to forward **all paths** to `http://127.0.0.1:3000`. The frontend proxies API
requests to the backend, including authentication callbacks, webhooks, and agent
requests. Preserve the public Host header, forwarded scheme, and streaming
responses; allow enough upstream time for long-running requests.

Set these values in `.env` and recreate the services with `docker compose up -d --wait`:

```dotenv
JUSTCD_PUBLIC_URL=https://justcd.example.com
JUSTCD_COOKIE_SECURE=true
JUSTCD_BIND_ADDRESS=127.0.0.1
JUSTCD_PORT=3000
```

Terminate TLS at the proxy and keep the backend/database ports unpublished.
If your proxy is another container, attach it to `justcd_default` and forward
to `frontend:3000`; `127.0.0.1` inside that container refers to the proxy itself.
Ensure the proxy supports WebSocket upgrades for outbound cluster agents.

For direct HTTP access on a trusted network, set `JUSTCD_BIND_ADDRESS` to the
server's interface address (or `0.0.0.0`), set `JUSTCD_PUBLIC_URL` to its matching
HTTP URL and port, and keep `JUSTCD_COOKIE_SECURE=false`. Use HTTPS for an
internet-facing installation.

## Pin images and upgrade

The default empty `JUSTCD_IMAGE_TAG_SUFFIX` pulls the moving `frontend` and
`backend` tags. For production, choose a published release and set its suffix
(including the leading hyphen), for example `JUSTCD_IMAGE_TAG_SUFFIX=-v0.1.0`.
That resolves both services to `frontend-v0.1.0` and `backend-v0.1.0`; the example
is a tag format, so check available releases before choosing it.

Before upgrading, stop the application services and take a backup as below.
Keep the previous image tags/digests and encryption key. Update the suffix, then:

```sh
docker compose pull
docker compose up -d --wait
docker compose logs --tail=100 backend frontend
```

Verify login, credential decryption, and Git/Kubernetes connection tests after
the upgrade. Migrations only run forward: an older backend must not run against
a database migrated by a newer version. Recovery requires the previous images
and a restored pre-upgrade database, with the original encryption key. See the
[operations runbook](/operations/production-operations) for recovery and worker-fencing
requirements before reconnecting a restored control plane to real clusters.

PostgreSQL stays on major version 17. Changing its image to another major version
requires a PostgreSQL migration; do not reuse the existing data directory with a
different major version.

## Backup and restore

Stop application services to quiesce workers and keep the database running:

```sh
docker compose stop frontend backend
docker compose exec -T postgres pg_dump -U justcd -d justcd -Fc > justcd.dump
chmod 600 justcd.dump
docker compose up -d --wait
```

Check that `pg_dump` succeeded before restarting or using the backup. Store the
dump and a protected copy of `.env` off the server. The database volume alone is
not a backup. The stack gives backend workers one minute to stop, including their
30-second execution grace period.

To restore, fence the original control plane first. In an installation using the
matching pre-backup application version and encryption key, stop the frontend
and backend, and restore into an **empty** `justcd` database:

```sh
docker compose up -d --wait postgres
docker compose stop frontend backend
docker compose exec -T postgres pg_restore -U justcd -d justcd --exit-on-error < justcd.dump
```

Follow the operations runbook to disable restored automatic reconciliation,
discovery, PR reporting, and preview cleanup before starting application services.
From the repository root, run its cold-restore quarantine SQL while all backends
remain stopped:

```sh
docker compose exec -T postgres psql -U justcd -d justcd -v ON_ERROR_STOP=1 < scripts/operations/quarantine-restored.sql
```

Then run `docker compose up -d --wait`, verify the restored configuration, and
re-enable automation deliberately after reviewing cluster state.

## Build from source

The optional override builds the checkout using the existing service Dockerfiles.
It uses local image names and needs no Go, Node.js, or pnpm installation on the
host. Use it for unreleased changes or architectures without published images:

```sh
docker compose -f compose.yaml -f compose.build.yaml up -d --build --wait
```

Use both `-f` arguments for subsequent commands against a source-built stack so
Compose keeps selecting the local images. PostgreSQL is still pulled separately.

## Troubleshooting and stopping

```sh
docker compose ps
docker compose logs --tail=100 postgres backend frontend
curl --fail http://127.0.0.1:3000/healthz
curl --fail http://127.0.0.1:3000/api/v1/health
docker compose down
```

Adjust the curl port if you changed `JUSTCD_PORT`. Health checks confirm processes
are reachable; onboarding and connection tests verify database and target access.
`down` preserves the database volume. **`docker compose down --volumes` permanently
deletes the database.**

Missing secrets fail Compose validation before any container is created. If GHCR
requires authentication, log in with an account that can read the packages. If the
database reports password authentication failures after editing `.env`, restore
the original password or change the role password inside PostgreSQL first:
`POSTGRES_PASSWORD` only initializes a new data directory. Startup failures from
migrations, invalid encryption keys, or unhealthy services appear in backend logs.

## Optional documentation service

Run `docker compose --profile docs up -d --wait` to add the independently deployable
Fumadocs service at `http://localhost:3001`. It has no backend or database dependency.
Use `JUSTCD_DOCS_PORT` and `JUSTCD_DOCS_BIND_ADDRESS` to change its listener. For source
builds, include `-f compose.yaml -f compose.build.yaml` with `--profile docs`.
See [docs hosting and authoring](/getting-started/docs-service) for standalone containers,
release tags, Helm, and HTTPS on a dedicated hostname.
