# JustCD

JustCD is a Git-driven Kubernetes delivery control plane that runs outside the
managed clusters. It connects directly to the Kubernetes API; it does not
install a JustCD operator, controller, or JustCD-specific CRDs.

The current v1 includes local password authentication and OIDC login, projects
and roles, encrypted Git/Kubernetes credentials, cluster and namespace
connections, YAML/Kustomize/Helm rendering, drift plans, resource topology and inventory,
audited sync operations, and explicit owner approval for deletion and
cluster-scoped changes. `auto-safe` checks drift in the background and applies
only namespaced creates/updates. It never prunes automatically.

## Local development

You need Go 1.27+, Node.js, pnpm, PostgreSQL, and `git`. Install `helm` and
`kustomize` if you want to use those renderers. Create a PostgreSQL role and
database (or point the URL in the environment file at an existing database):

```sh
psql postgres -c "CREATE ROLE justcd LOGIN PASSWORD 'change-me';"
createdb --owner=justcd justcd
```

Copy the backend example, edit the database URL and replace both bootstrap
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

To add sample project/application states to that same database, load your
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
operation, and a Helm shop with connected Ingress, Services, Deployments,
ReplicaSets, Pods, ConfigMaps, Secret, PVC, and autoscaler. ReplicaSets and Pods
in that demo are marked as sample observations. Its Git and Kubernetes endpoints use the reserved `.invalid` domain;
they are intentionally unreachable, so the demo cannot sync to a real cluster.

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

## First setup

1. Create the first administrator at `/signup`, then create a project and add a local
   user or configure an OIDC provider.
2. Add a global Kubernetes token or static kubeconfig credential, then register
   the Kubernetes API endpoint and CA. Leave cluster-scope access disabled
   unless the application genuinely manages cluster-wide resources.
3. Bind one or more namespaces to the project. Each binding may use its own
   credential or inherit the cluster default.
4. Add an HTTPS/SSH Git source and a credential when needed, then create an
   application with a tracked branch, manifest path, renderer, and namespace
   bindings.
5. Build a plan and review its resource-level diff before syncing. Plans expire
   after 15 minutes. Deletes and cluster-scoped changes require a project
   owner’s one-time approval. The executor rechecks the plan, resource UID,
   resource version, and ownership immediately before mutation.

For OIDC, use the `redirectUrl` returned by provider creation as the exact
callback URL in the identity provider. The public URL must point to the Next.js
application (whose same-origin API proxy forwards callbacks to the backend).
Group-to-project role mappings are configured in Connections & access.

## Pull request plans and previews

Open an application's **Pull requests** tab and choose **Enable PR reporting**
(or **Edit settings** for an existing connection). JustCD derives the provider
and repository from the application's existing Git source; you do not connect
the repository a second time. For an unknown Git host, confirm that it is a
self-hosted GitLab instance and supply its HTTPS API URL if needed. The
review list remains the default view; reporting settings open only on
request. For GitLab.com JustCD uses
`https://gitlab.com/api/v4`; for a self hosted instance use its HTTPS API URL,
such as `https://gitlab.example.com/api/v4`. Supply a webhook secret and a token permitted to read
pull requests and write commit statuses. Copy the displayed webhook URL into
the repository webhook settings and enable pull request or merge request
and push events. A branch push triggers a fresh plan for every application in
the project tracking that repository and branch, including application-group
children. Manual applications stop at plan review; auto-safe applications keep
their existing safety and approval rules. The poller remains a fallback.
GitLab's signed webhooks are supported; older instances can use the
legacy secret token. The host running JustCD must trust the GitLab instance's
TLS certificate.

For a Git source without a PR reporting connection, a project owner can configure
a push webhook in the Git source settings or with
`PUT /api/v1/git-sources/{sourceID}/push-webhook` and JSON
`{"secret":"at-least-16-characters"}`. The response contains a webhook URL.
That URL accepts native GitHub or GitLab push events using their normal webhook
signature headers, or a generic signed envelope from another provider or CI.
POST `{"ref":"refs/heads/main","after":"<40-character-commit-SHA>"}` to that URL,
setting `X-JustCD-Signature-256` to `sha256=` followed by the hex HMAC-SHA256
of the exact request body using the secret. Set `X-JustCD-Delivery` to a unique
delivery ID when available. This endpoint accepts branch updates only, not
tag or branch-deletion events. Delivery IDs are deduplicated; the audit trail
records the reported SHA and the commit actually resolved when planning.

Each PR or MR is checked against the provider's current head and rendered from
its `head` Git ref. JustCD verifies that the rendered commit equals the head
SHA reported by the provider. Without a preview profile, the resulting plan is
stored for review only and cannot be applied to production. A commit status on
the PR or MR links to the plan in JustCD.

To enable deployments, configure the application's preview profile. Provide a
preview manifest path or Helm values, a database strategy, a namespace prefix,
an ingress host suffix, CPU and memory quotas, maximum active previews, and a
maximum lifetime. The profile may reference secrets already provisioned inside
the preview namespace; JustCD never copies production secrets or databases.
The configured values or overlay must point to preview-safe data services.
For Helm values, `{{namespace}}`, `{{number}}`, and `{{sha}}` are replaced with
the preview namespace, PR/MR number, and exact head SHA. For example, an
Ingress host can be `web.{{namespace}}.preview.example.com`; every rendered
Ingress host and TLS host must be inside that namespace-specific domain. A
wildcard DNS record and certificate for the preview domain simplify routing.

JustCD creates a dedicated namespace and ResourceQuota for each preview. The
cluster needs a cluster-scope credential that can create and delete namespaces
and manage ResourceQuotas. The project also needs a namespace credential for
the generated application. Rendered cluster-scoped resources, Secret objects,
unexpected secret references, external-facing Services, and out-of-domain
Ingress hosts are rejected. Fork previews are disabled by default; opting in
requires owner approval and a profile with no allowed secrets. Fork workloads
must disable service account token mounting.

Closing, merging, or expiring a preview creates a normal decommission plan.
After that plan is applied and all JustCD-managed resources are gone, JustCD
deletes the owned namespace and releases its preview slot. Everything placed
in that namespace, including separately provisioned preview secrets, is
removed with it. Cleanup that cannot verify ownership remains pending for
review.

## Verification

```sh
cd services/backend && go test ./...
cd services/frontend && pnpm typecheck && pnpm lint && pnpm build
```

The backend API is documented at `/api/v1/openapi.yaml` and health checks are
available at `/api/v1/health`.

## Container deployment

Build and publish the two images from their service directories:

```sh
docker build -t registry.example.com/justcd-backend:0.1.0 services/backend
docker build -t registry.example.com/justcd-frontend:0.1.0 services/frontend
docker push registry.example.com/justcd-backend:0.1.0
docker push registry.example.com/justcd-frontend:0.1.0
```

The frontend runtime uses a nonroot distroless Node image. The backend runtime
uses Alpine with Git and SSH, which are needed to access repositories. Build
tools and package managers stay in the build stages. Scan the exact images you
intend to deploy after every build, for example:

```sh
trivy image --scanners vuln --severity HIGH,CRITICAL registry.example.com/justcd-backend:0.1.0
trivy image --scanners vuln --severity HIGH,CRITICAL registry.example.com/justcd-frontend:0.1.0
```

Rebuild and rescan regularly as base images and vulnerability data change.

Deploy with the [Helm chart](charts/justcd/README.md). It requires an external
PostgreSQL database, a Secret holding `JUSTCD_DATABASE_URL` and
`JUSTCD_ENCRYPTION_KEY`, and the browser-facing `publicUrl`.
