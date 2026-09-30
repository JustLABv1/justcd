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

The backend applies database migrations at startup. It runs one replica with a
recreate rollout because that process also runs sync workers. The chart does
not install PostgreSQL or create credentials for target clusters. Set
`cookieSecure: false` only for a local HTTP installation. Both images expose
`/healthz` for Kubernetes probes; the backend's endpoint reports process
availability after startup, not the health of every external connection.

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
[`docs/observability.md`](../../docs/observability.md) for metric names,
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
The backend remains a single replica with a Recreate strategy to avoid
concurrent reconciliation workers.

The manifests can be rendered locally; SCC admission and image compatibility
with the assigned UID must also be verified on your OpenShift cluster.

## Private CA certificates for OIDC

To trust your Keycloak certificate, reference an existing Secret in the release
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
Certificate hostname and expiry checks remain enabled. The certificate must
cover the hostname in your configured issuer URL.

Apply with the usual `helm upgrade --install` command. After rotating the
certificate in the Secret, restart the backend deployment: Go caches its CA
pool, so updating the mounted file alone does not reload trust. Do not override
`SSL_CERT_DIR` in `backend.extraEnv` when using this option.
