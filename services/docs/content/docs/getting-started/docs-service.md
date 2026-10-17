---
title: "Deploy and develop the docs service"
description: "An independent documentation site beside the frontend and backend."
---

The service uses Fumadocs and Next.js with a standalone Node runtime. It serves at
`/` on its own port or hostname. It needs no backend URL, database, login, or secret.
Documentation content is public to anyone who can reach the service; use your reverse
proxy's access controls if the installation should be private.

## Open docs from JustCD

Use **Documentation** in the desktop sidebar or **Docs** in the mobile header and
sign-in screen. Links open in a new tab. Configure the frontend's runtime environment
variable `JUSTCD_DOCS_URL` with the browser-facing docs origin, such as
`https://docs.justcd.example.com`. Use a public hostname reachable by users, not the
internal Compose service address `http://docs:3000`.

Compose defaults this setting to `http://localhost:3001`; change it for a server or
if you change `JUSTCD_DOCS_PORT`. For Helm, set the top-level `docsUrl` value alongside
`docs.enabled` and your docs Ingress host. Recreate the frontend after changing the
setting; its image does not need rebuilding. Without a configured URL, frontend
links open the corresponding versioned content on GitHub.

## Docker Compose

From the repository root, after configuring `.env`:

```sh
docker compose --profile docs up -d --wait
# Or run only documentation:
docker compose --profile docs up -d --wait docs
```

Open `http://localhost:3001`. `JUSTCD_DOCS_PORT` changes the published port;
`JUSTCD_DOCS_BIND_ADDRESS` defaults to `127.0.0.1`. The docs profile is optional and
has no service dependencies. Compose still validates the control plane's required
variables in `.env` when loading the combined file.

The release image is `ghcr.io/justlabv1/justcd:docs`; use `:docs-vX.Y.Z` to pin a release
that includes this service. `JUSTCD_IMAGE_TAG_SUFFIX` applies to docs as well as the other
services. Before the first such release, use the source build:

```sh
docker compose -f compose.yaml -f compose.build.yaml --profile docs up -d --build --wait docs
```

For a completely standalone container (without loading Compose or its environment):

```sh
docker build -t justcd-docs:local services/docs
docker run --rm -p 127.0.0.1:3001:3000 justcd-docs:local
```

Set container `PORT` and `HOSTNAME` if needed (defaults: `3000` and `0.0.0.0`).
`GET /healthz` returns 200 for process health. Docs remain available if the control
plane is stopped. They do not expose or proxy the JustCD API.

## Helm and HTTPS

Enable the optional service in the [Helm installation](/getting-started/helm):

```yaml
docsUrl: https://docs.justcd.example.com
docs:
  enabled: true
  image:
    repository: ghcr.io/justlabv1/justcd
    tag: docs-vX.Y.Z # choose an existing release
  ingress:
    enabled: true
    className: nginx
    hosts:
      - host: docs.justcd.example.com
        paths:
          - path: /
            pathType: Prefix
    tls:
      - secretName: justcd-docs-tls
        hosts: [docs.justcd.example.com]
```

The docs Deployment, ClusterIP Service, and optional Ingress are separate from the
frontend. Use a dedicated hostname and forward all paths, including `/_next`,
`/api/search`, and `/healthz`. Do not mount this build below `/docs` or rewrite its
prefix: the compiled routes assume `/`. Compose deployments can proxy
`https://docs.justcd.example.com` to `http://127.0.0.1:3001` in the same way.

## Local development and authoring

```sh
cd services/docs
pnpm install --frozen-lockfile
pnpm dev
```

Development uses port 3001. Write pages under `content/docs` with `title` and an optional
`description` in YAML frontmatter. Use `.md` for plain Markdown or `.mdx` when importing
Fumadocs components. Update the section's `meta.json` to control sidebar order. Link
to routes such as `/guides/workflow`; do not link to relative source `.md` paths.
Public assets live in `public/brand`; executable recovery examples remain in
`scripts/operations` at the repository root.

Use the Fumadocs page, sidebar, search, theme, code, and mobile-navigation components.
These supply the documentation interactions without coupling the docs build to the
application's frontend component tree. Custom application controls should follow the
repository's shared ReUI/ShadCN policy.

```sh
pnpm check:links
pnpm lint
pnpm typecheck
pnpm build
pnpm start --port 3001
```

Content is compiled into the image. Rebuild and redeploy the docs service to publish
edits. There is no runtime content editor or separate content database.
