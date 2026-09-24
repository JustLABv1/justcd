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
