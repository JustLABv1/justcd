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
routes. Set `JUSTCD_MAINTENANCE=true` on the frontend process and restart it to
show maintenance across the app; unset it and restart to restore access. This
flag controls the frontend experience, not backend jobs or API access.

Backend failures trigger a separate `/api/service-status` health check before
showing the offline page, so an individual Git or Kubernetes failure does not
take over the app. While offline, the page checks every 15 seconds when visible,
offers a manual reconnect button, and reloads the current URL after recovery.
The existing `/healthz` endpoint remains a frontend liveness check.
