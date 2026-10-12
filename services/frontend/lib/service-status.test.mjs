import assert from "node:assert/strict"
import test from "node:test"
import { GET } from "../app/api/service-status/route.ts"

async function withService(maintenance, fetch, run) {
  const previousFlag = process.env.JUSTCD_MAINTENANCE
  const previousFetch = globalThis.fetch
  process.env.JUSTCD_MAINTENANCE = maintenance
  globalThis.fetch = fetch
  try { await run() } finally {
    globalThis.fetch = previousFetch
    if (previousFlag === undefined) delete process.env.JUSTCD_MAINTENANCE
    else process.env.JUSTCD_MAINTENANCE = previousFlag
  }
}

test("maintenance takes precedence and does not contact the backend", async () => {
  await withService("true", () => { throw new Error("must not fetch") }, async () => {
    const response = await GET()
    assert.equal(response.status, 503)
    assert.equal(response.headers.get("cache-control"), "no-store")
    assert.deepEqual(await response.json(), { status: "maintenance" })
  })
})

test("online requires a successful JustCD health response", async () => {
  await withService("false", async (url, init) => {
    assert.equal(url.pathname, "/healthz")
    assert.equal(init.cache, "no-store")
    assert.ok(init.signal instanceof AbortSignal)
    return Response.json({ status: "ok" })
  }, async () => {
    const response = await GET()
    assert.equal(response.status, 200)
    assert.deepEqual(await response.json(), { status: "online" })
  })
})

test("unreachable, unhealthy and non-JustCD backends are offline", async () => {
  for (const fetch of [
    async () => { throw new Error("connection refused") },
    async () => Response.json({ status: "ok" }, { status: 503 }),
    async () => Response.json({ status: "unknown" }),
    async () => new Response("<html>gateway error</html>"),
  ]) {
    await withService("false", fetch, async () => {
      const response = await GET()
      assert.equal(response.status, 503)
      assert.deepEqual(await response.json(), { status: "offline" })
    })
  }
})
