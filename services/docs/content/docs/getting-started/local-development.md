---
title: "Local development"
---

You need Go 1.27+, Node.js, pnpm, PostgreSQL, and `git`. Install `helm` and
`kustomize` if you want to use those renderers. Create a PostgreSQL role and
database (or point the URL in the environment file at an existing database):

```sh
psql postgres -c "CREATE ROLE justcd LOGIN PASSWORD 'change-me';"
createdb --owner=justcd justcd
```

Copy the backend example, edit the database URL and replace the database and encryption-key
placeholders. Generate an encryption key with `openssl rand -hex 32` and paste
its output into `JUSTCD_ENCRYPTION_KEY`:

```sh
cd services/backend
cp .env.example .env
```

The Go process reads environment variables; it does not automatically load
`.env`. Export the file's values in the terminal before starting the backend:

```sh
set -a
. ./.env
set +a
go run .
```

`JUSTCD_ENCRYPTION_KEY` must be 32 random bytes, or their hex/base64 encoding.
Keep it stable and back it up: losing it makes stored Git, Kubernetes, and OIDC
credentials unreadable. On a fresh database, visit `/signup` to create the
first administrator; no bootstrap password is needed in the environment.
Schema migrations are applied at backend startup. Keep `.env`
and `.envrc` out of version control; the backend's `.gitignore` excludes both.

To add sample workspace/application states to that same database, load your
backend environment and run the demo seeder:

```sh
cd services/backend
set -a
. ./.envrc # or ./.env if you use the example file
set +a
go run ./cmd/demo-seed
```

The seed is additive and idempotent. It creates a clearly named demo workspace,
fake encrypted credentials, example drift/diff and approval plans, a failed
operation, a removed-from-Git application with an owner-reviewed deletion plan
for its workloads and explicitly managed Namespace, and a Helm shop with connected Ingress, Services, Deployments,
ReplicaSets, Pods, ConfigMaps, Secret, PVC, and autoscaler. ReplicaSets and Pods
in that demo are marked as sample observations. Its Git and Kubernetes endpoints use the reserved `.invalid` domain;
they are intentionally unreachable, so the demo cannot sync to a real cluster.

Route highlighting reports backend readiness from matching, non-terminating
ready pods and OpenShift admission conditions. Green means the configured
backend is ready; it does not certify DNS, TLS, network policy, or external HTTP
reachability. Missing permissions or incomplete observations remain unknown.

For real applications, the topology endpoint derives links from rendered Helm,
Kustomize, or YAML manifests and reads controller-created ReplicaSets, Jobs,
and Pods from the cluster using the configured namespace credential. Grant
read/list access to those resource kinds to see descendants. Observation is
read-only; missing permissions are shown as a partial-topology warning.

Start the web application in a second terminal:

```sh
cd services/frontend
pnpm install --frozen-lockfile
JUSTCD_API_URL=http://localhost:8080 pnpm dev
```

Open [http://localhost:3000](http://localhost:3000). The Next.js server proxies
`/api/v1` requests to the Go backend so browser code never handles service
credentials directly.



## Verification

```sh
cd services/backend && go test ./...
cd services/frontend && pnpm typecheck && pnpm lint && pnpm build
```

The isolated integration smoke suite checks upgrades from each of the three
most recent migration versions, preserves existing data, reruns migrations to
check idempotence, and exercises the reconciliation poller's SQL query. It also verifies repository
application discovery against a temporary Git server, including invalid
definitions, namespace access, removal, and restoration:

```sh
bash scripts/integration.sh smoke
```

The full suite also creates a disposable kind cluster and runs YAML,
Kustomize, and Helm delivery workflows through the real Kubernetes API:

```sh
# Install kind first; if installed with `go install`, add $(go env GOPATH)/bin to PATH.
bash scripts/integration.sh full
```

Both commands need Docker or Podman and keep logs in `artifacts/e2e/`. They
create and remove only their own PostgreSQL container and kind cluster. The
full suite is run on pull requests by [integration.yml](https://github.com/JustLABv1/justcd/blob/main/.github/workflows/integration.yml).

Database migrations run forward when the backend starts. Before upgrading a
deployed instance, take a PostgreSQL backup and keep the previous application
image. To recover from a failed upgrade, stop the backend and workers, restore
the backup into a new database, point the previous image at that database, and
start it. Do not delete migration ledger rows or run an older image against a
schema that a newer image has already migrated. The integration suite verifies
forward upgrades; disaster recovery depends on a tested backup and restore
procedure for your deployment.

Backend shutdown stops new queue acquisition and allows in-flight operations to
finish for `JUSTCD_SHUTDOWN_GRACE_PERIOD` (default `30s`, supported range `1s`
through `10m`). After that period execution is cancelled, and workers finish
cleanup before the database closes. See the [Helm shutdown and recovery guide](/getting-started/helm#worker-shutdown-and-recovery)
for pod termination settings and interrupted-operation recovery.

The backend API is documented at `/api/v1/openapi.yaml` and health checks are
available at `/api/v1/health`.
