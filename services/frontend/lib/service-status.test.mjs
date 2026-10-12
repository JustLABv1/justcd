import assert from "node:assert/strict"
import test from "node:test"
import { GET } from "../app/api/service-status/route.ts"

async function withFetch(fetch, run) {
  const previousFetch = globalThis.fetch
  globalThis.fetch = fetch
  try { await run() } finally { globalThis.fetch = previousFetch }
}

const request = new Request("http://localhost/api/service-status", { headers: { cookie: "justcd_session=test-session" } })

test("backend maintenance is returned with 503 and cannot be cached", async () => {
  await withFetch(async () => Response.json({ status: "maintenance", maintenanceEnabled: true, adminBypass: false }), async () => {
    const response = await GET(request)
    assert.equal(response.status, 503)
    assert.equal(response.headers.get("cache-control"), "no-store")
    assert.equal(response.headers.get("vary"), "Cookie")
    assert.equal((await response.json()).status, "maintenance")
  })
})

test("session cookie is forwarded and backend-validated admin bypass stays online", async () => {
  await withFetch(async (url, init) => {
    assert.equal(url.pathname, "/api/v1/service-status")
    assert.equal(init.headers.cookie, "justcd_session=test-session")
    assert.equal(init.cache, "no-store")
    assert.equal(init.redirect, "error")
    assert.ok(init.signal instanceof AbortSignal)
    return Response.json({ status: "online", maintenanceEnabled: true, adminBypass: true })
  }, async () => {
    const response = await GET(request)
    assert.equal(response.status, 200)
    assert.deepEqual(await response.json(), { status: "online", maintenanceEnabled: true, adminBypass: true })
  })
})

test("unreachable, unhealthy and malformed backends are offline", async () => {
  for (const fetch of [
    async () => { throw new Error("connection refused") },
    async () => Response.json({ status: "online", maintenanceEnabled: false, adminBypass: false }, { status: 503 }),
    async () => Response.json({ status: "unknown" }),
    async () => Response.json({ status: "online" }),
    async () => new Response("<html>gateway error</html>"),
  ]) {
    await withFetch(fetch, async () => {
      const response = await GET(request)
      assert.equal(response.status, 503)
      assert.deepEqual(await response.json(), { status: "offline" })
    })
  }
})
