import assert from "node:assert/strict"
import test from "node:test"
import { APIError, api } from "./api.ts"

async function withFetch(responseOrError, run) {
  const previousFetch = globalThis.fetch
  globalThis.fetch = async () => {
    if (responseOrError instanceof Error) throw responseOrError
    return responseOrError
  }
  try {
    await run()
  } finally {
    globalThis.fetch = previousFetch
  }
}

test("APIError exposes v1 machine-readable metadata and remediation", async () => {
  await withFetch(
    new Response(
      JSON.stringify({
        schemaVersion: 1,
        error: "Kubernetes API connection failed",
        code: "kubernetes.connection_failed",
        category: "kubernetes",
        retryable: true,
        remediation: "Check the endpoint and credential permissions.",
        remediationUrl: "/settings/connections",
      }),
      { status: 422, headers: { "content-type": "application/json" } }
    ),
    async () => {
      await assert.rejects(api("/api/v1/clusters/test"), (error) => {
        assert.ok(error instanceof APIError)
        assert.equal(error.message, "Kubernetes API connection failed")
        assert.equal(error.schemaVersion, 1)
        assert.equal(error.code, "kubernetes.connection_failed")
        assert.equal(error.category, "kubernetes")
        assert.equal(error.retryable, true)
        assert.equal(
          error.remediation,
          "Check the endpoint and credential permissions."
        )
        assert.equal(error.remediationUrl, "/settings/connections")
        return true
      })
    }
  )
})

test("legacy error payloads remain readable and receive status-based guidance", async () => {
  await withFetch(
    new Response(JSON.stringify({ error: "permission denied" }), {
      status: 403,
      headers: { "content-type": "application/json" },
    }),
    async () => {
      await assert.rejects(api("/api/v1/projects"), (error) => {
        assert.ok(error instanceof APIError)
        assert.equal(error.message, "permission denied")
        assert.equal(error.schemaVersion, null)
        assert.equal(error.code, "authorization.denied")
        assert.equal(error.category, "authorization")
        assert.equal(error.retryable, false)
        assert.match(error.remediation, /project role/)
        return true
      })
    }
  )
})

test("error responses cannot direct remediation links off site", async () => {
  await withFetch(
    new Response(
      JSON.stringify({
        schemaVersion: 1,
        error: "invalid request",
        code: "request.invalid",
        category: "validation",
        retryable: false,
        remediation: "Review the submitted values.",
        remediationUrl: "https://example.test/collect",
      }),
      { status: 400, headers: { "content-type": "application/json" } }
    ),
    async () => {
      await assert.rejects(api("/api/v1/projects"), (error) => {
        assert.ok(error instanceof APIError)
        assert.equal(error.remediationUrl, null)
        return true
      })
    }
  )
})

test("network failures explain that mutation state must be refreshed", async () => {
  await withFetch(new TypeError("offline"), async () => {
    await assert.rejects(api("/api/v1/projects"), (error) => {
      assert.ok(error instanceof APIError)
      assert.equal(error.code, "network.unreachable")
      assert.equal(error.category, "network")
      assert.equal(error.retryable, false)
      assert.match(error.remediation, /refresh the current state/)
      return true
    })
  })
})
