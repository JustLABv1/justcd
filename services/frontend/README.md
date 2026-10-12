# JustCD web

This Next.js application contains the authenticated JustCD dashboard and a
same-origin API proxy to the Go service. Set `JUSTCD_API_URL` to the backend
origin (default `http://localhost:8080`) before starting the dev server.

```sh
pnpm install --frozen-lockfile
JUSTCD_API_URL=http://localhost:8080 pnpm dev
```

Build checks: `pnpm typecheck`, `pnpm lint`, and `pnpm build`. The resource and
operation tables use the ReUI Data Grid with TanStack Table v9.

Service pages: `/maintenance`, `/offline`, and the automatic 404 for unknown
routes. Instance administrators toggle maintenance at **Instance settings →
Maintenance** (`/settings/maintenance`). The setting is persisted in PostgreSQL
and changes are audited; no restart is required. Signed-in instance admins
automatically bypass maintenance and see a banner. The maintenance page keeps
administrator sign-in reachable at `/login`.

Regular users' authenticated API requests receive a maintenance 503, while
session inspection and logout stay available. Auth endpoints remain reachable
for administrator sign-in. Background jobs, webhooks, and cluster agents keep
running. The former frontend `JUSTCD_MAINTENANCE` environment flag is no longer
used. Deploy the updated backend alongside the frontend so that its new
`/api/v1/service-status` endpoint and instance-settings migration are available.

Backend failures trigger a separate `/api/service-status` availability check before
showing the offline page, so an individual Git or Kubernetes failure does not
take over the app. While offline, the page checks every 15 seconds when visible,
offers a manual reconnect button, and reloads the current URL after recovery.
The existing `/healthz` endpoint remains a frontend liveness check.
