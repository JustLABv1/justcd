import assert from "node:assert/strict"
import test from "node:test"
import {
  planReviewStatus,
  makeEntry,
  matchesEntry,
  riskCounts,
  immutablePaths,
  groupLabel,
} from "./plan-review.ts"
const change = (kind = "update", extra = {}) => ({
  kind,
  identity: {
    apiVersion: "v1",
    kind: "ConfigMap",
    namespace: "team",
    name: "settings",
  },
  before: { data: { image: "v1" } },
  after: { data: { image: "v2" } },
  ...extra,
})
test("risk counts and filters exactly reflect the immutable applied and ignored payload", () => {
  const changes = [
      change("delete"),
      change("update", {
        takeover: true,
        identity: {
          apiVersion: "v1",
          kind: "Namespace",
          namespace: "",
          name: "test",
          clusterScoped: true,
        },
      }),
      change("create", { ignoredPaths: ["/data/owner"] }),
    ],
    ignored = [change("delete")]
  const plan = { changes, ignored, digest: "unchanged" }
  const before = JSON.stringify(plan)
  const entries = [
    ...changes.map((c) => makeEntry(c, false)),
    ...ignored.map((c) => makeEntry(c, true)),
  ]
  const counts = riskCounts(entries)
  for (const [risk, count] of Object.entries(counts))
    assert.equal(entries.filter((e) => matchesEntry(e, "", risk)).length, count)
  assert.equal(counts.deletion, 1)
  assert.equal(counts.cluster, 1)
  assert.equal(counts.ownership, 1)
  assert.equal(counts.ignored, 2)
  assert.equal(entries.filter((e) => matchesEntry(e, "v2", "update")).length, 1)
  for (const grouping of ["risk", "namespace", "action", "kind", "child"])
    entries.map((e) => groupLabel(e, grouping))
  assert.equal(JSON.stringify(plan), before)
})
test("known immutable-path evidence does not label ordinary updates as replacement", () => {
  assert.deepEqual(immutablePaths(change()), [])
  const deployment = change("update", {
    identity: {
      apiVersion: "apps/v1",
      kind: "Deployment",
      namespace: "team",
      name: "api",
    },
    before: { spec: { selector: { matchLabels: { app: "old" } } } },
    after: { spec: { selector: { matchLabels: { app: "new" } } } },
  })
  assert.deepEqual(immutablePaths(deployment), ["/spec/selector"])
  assert.deepEqual(immutablePaths({ ...deployment, kind: "create" }), [])
  const reordered = {
    ...deployment,
    before: { spec: { selector: { matchLabels: { a: 1, b: 2 } } } },
    after: { spec: { selector: { matchLabels: { b: 2, a: 1 } } } },
  }
  assert.deepEqual(immutablePaths(reordered), [])
})
test("child grouping and searches preserve same resource identity in different targets", () => {
  const a = makeEntry(change(), false, "a:key", "Production", "a"),
    b = makeEntry(change(), false, "b:key", "Staging", "b")
  assert.notEqual(a.key, b.key)
  assert.equal(groupLabel(a, "child"), "Production")
  assert.equal(matchesEntry(a, "production", "all"), true)
  assert.equal(matchesEntry(b, "production", "all"), false)
})

test("redacted immutable Secret changes retain reviewed path evidence", () => {
  const secret = change("update", {
    identity: {
      apiVersion: "v1",
      kind: "Secret",
      namespace: "team",
      name: "secret",
    },
    before: { immutable: true, data: "[redacted]" },
    after: { immutable: true, data: "[redacted]" },
    changedPaths: ["/data/password"],
  })
  assert.deepEqual(immutablePaths(secret), ["/data"])
})


test("current plans stop being actionable at their expiry deadline", () => {
  const record = { status: "current", expiresAt: "2026-10-14T10:00:00Z" }
  const deadline = Date.parse(record.expiresAt)
  assert.equal(planReviewStatus(record, deadline - 1), "current")
  assert.equal(planReviewStatus(record, deadline), "expired")
  assert.equal(planReviewStatus(record, deadline + 1), "expired")
  for (const status of ["applied", "stale", "failed"]) assert.equal(planReviewStatus({ ...record, status }, deadline + 1), status)
})
