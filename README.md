<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="services/docs/public/brand/justcd-logo-light.svg">
    <img src="services/docs/public/brand/justcd-logo-dark.svg" alt="JustCD" width="160">
  </picture>
</p>

# JustCD

JustCD is a Git-driven Kubernetes delivery control plane. It renders YAML,
Kustomize, and Helm from Git, builds reviewable drift plans, and applies approved
changes with scoped credentials. It runs outside managed clusters without a
JustCD operator or CRDs; outbound agents support private Kubernetes APIs.

## Start JustCD

```sh
cp .env.example .env
# Fill both secrets with separate values from: openssl rand -hex 32
# Configure your public HTTPS URL and secure cookies for a server.
docker compose up -d --wait
```

Open `http://localhost:3000` and create the first administrator. For production,
follow the [Compose guide](services/docs/content/docs/getting-started/docker-compose.md)
or the [Helm chart](charts/justcd/README.md).

## Documentation

The documentation is a deployable Fumadocs service in `services/docs`, with
setup, daily workflows, controls, security, and operations guides. Start it
alongside the control plane:

```sh
docker compose --profile docs up -d --wait
```

Open `http://localhost:3001` or use **Documentation** in the frontend sidebar.
Set `JUSTCD_DOCS_URL` to your docs hostname for server deployments. Docs need no database, credentials, or backend and
can also run alone: `docker compose --profile docs up -d --wait docs`.
Release images use `ghcr.io/justlabv1/justcd:docs` and `:docs-vX.Y.Z` tags.
Until a release containing the service is published, build this checkout:

```sh
docker compose -f compose.yaml -f compose.build.yaml --profile docs up -d --build --wait
```

To develop the documentation:

```sh
cd services/docs
pnpm install --frozen-lockfile
pnpm dev
```

See [local development](services/docs/content/docs/getting-started/local-development.md)
for backend/frontend development and verification, and
[the docs service guide](services/docs/content/docs/getting-started/docs-service.md)
for standalone hosting and authoring. Documentation source lives in
`services/docs/content/docs`; recovery SQL and RBAC examples live in `scripts/operations`.
