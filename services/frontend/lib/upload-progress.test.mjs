import assert from "node:assert/strict"
import test from "node:test"
import { api, APIError } from "./api.ts"

async function withUpload(status, payload, run) {
  const old = globalThis.XMLHttpRequest
  globalThis.XMLHttpRequest = class {
    upload = {}
    status = status
    statusText = status === 200 ? "OK" : "Bad Request"
    responseText = JSON.stringify(payload)
    open(method, path) { assert.equal(method, "POST"); assert.equal(path, "/api/v1/publishing") }
    setRequestHeader() {}
    getResponseHeader() { return "application/json" }
    send(body) {
      assert.ok(body instanceof FormData)
      this.upload.onprogress({ lengthComputable: true, loaded: 5, total: 10 })
      this.upload.onload()
      this.onload()
    }
  }
  try { await run() } finally { globalThis.XMLHttpRequest = old }
}

test("upload reports transfer progress and preserves the API result", async () => {
  await withUpload(200, { pipelineUrl: "https://gitlab.invalid/pipeline/1" }, async () => {
    const updates = []
    const result = await api("/api/v1/publishing", { method: "POST", body: new FormData() }, p => updates.push(p))
    assert.deepEqual(updates, [50, 100])
    assert.equal(result.pipelineUrl, "https://gitlab.invalid/pipeline/1")
  })
})

test("upload retains structured validation errors after transfer completes", async () => {
  await withUpload(400, { error: "Invalid archive", code: "request.invalid", category: "validation" }, async () => {
    await assert.rejects(api("/api/v1/publishing", { method: "POST", body: new FormData() }, () => {}), error => error instanceof APIError && error.status === 400 && error.message === "Invalid archive" && error.code === "request.invalid")
  })
})
