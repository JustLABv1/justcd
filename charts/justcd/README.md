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

The Ingress routes only to the frontend. Its same-origin `/api` proxy calls
the backend service inside the namespace. The frontend and backend images can
be promoted between environments without rebuilding for the internal API URL.

The backend applies database migrations at startup. It runs one replica with a
recreate rollout because that process also runs sync workers. The chart does
not install PostgreSQL or create credentials for target clusters. Set
`cookieSecure: false` only for a local HTTP installation. Both images expose
`/healthz` for Kubernetes probes; the backend's endpoint reports process
availability after startup, not the health of every external connection.
