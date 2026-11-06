import assert from "node:assert/strict"
import test from "node:test"
import { parseImage } from "./container-image.ts"

test("parses tags, digests and registry ports", () => {
  assert.deepEqual(parseImage("registry.example/app:1.4.0"), { repository: "registry.example/app", version: "1.4.0", digest: false })
  assert.deepEqual(parseImage("localhost:5000/team/app"), { repository: "localhost:5000/team/app", version: "latest", digest: false })
  assert.deepEqual(parseImage("localhost:5000/team/app:v2"), { repository: "localhost:5000/team/app", version: "v2", digest: false })
  assert.deepEqual(parseImage("ghcr.io/o/app@sha256:0123456789abcdef0123456789abcdef"), { repository: "ghcr.io/o/app", version: "sha256:0123456789ab", digest: true })
})
