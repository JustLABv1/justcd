import assert from "node:assert/strict"
import test from "node:test"
import { rolloutFocusResource, rolloutResourceKey } from "./rollout-timeline.ts"

const resource = (name, status = "queued", extra = {}) => ({
  identity: {
    clusterId: "cluster",
    apiVersion: "v1",
    kind: "ConfigMap",
    namespace: "demo",
    name,
  },
  status,
  action: "update",
  attemptCount: 1,
  ...extra,
})
const operation = (resources, extra = {}) => ({
  status: "running",
  progress: { resources },
  ...extra,
})

test("following advances past a completed resource even when current is stale", () => {
  const first = resource("a", "running"),
    next = resource("b")
  const op = operation([first, next])
  op.progress.current = first.identity
  assert.equal(rolloutFocusResource(op), first)
  first.status = "succeeded"
  assert.equal(rolloutFocusResource(op), next)
  next.status = "running"
  assert.equal(rolloutFocusResource(op), next)
  assert.deepEqual(
    op.progress.resources.map((r) => r.identity.name),
    ["a", "b"]
  )
})

test("the reported running or retrying identity wins over another in-flight row", () => {
  const a = resource("a", "running"),
    b = resource("b", "retrying")
  const op = operation([a, b])
  op.progress.current = b.identity
  assert.equal(rolloutFocusResource(op), b)
})

test("failures stop following before queued work, and terminal runs ignore stale current", () => {
  const failed = resource("failed", "failed"),
    queued = resource("queued")
  assert.equal(rolloutFocusResource(operation([failed, queued])), failed)
  const op = operation([resource("stale", "running"), failed, queued], {
    status: "failed",
  })
  op.progress.current = op.progress.resources[0].identity
  assert.equal(rolloutFocusResource(op), failed)
})

test("completed and cancelled runs settle on the latest attempted result", () => {
  const newer = resource("newer", "succeeded", {
    finishedAt: "2026-10-15T10:05:00Z",
  })
  const older = resource("older", "succeeded", {
    finishedAt: "2026-10-15T10:00:00Z",
  })
  for (const status of ["succeeded", "cancelled"]) {
    assert.equal(
      rolloutFocusResource(
        operation([newer, older, resource("not attempted", "skipped")], {
          status,
        })
      ),
      newer
    )
  }
})

test("empty and historical progress are safe; identity includes the cluster and API", () => {
  assert.equal(rolloutFocusResource({ status: "queued" }), undefined)
  const first = resource("a", "succeeded"),
    last = resource("b", "succeeded")
  assert.equal(
    rolloutFocusResource(operation([first, last], { status: "succeeded" })),
    last
  )
  assert.notEqual(
    rolloutResourceKey(first.identity),
    rolloutResourceKey({ ...first.identity, clusterId: "another" })
  )
  assert.notEqual(
    rolloutResourceKey(first.identity),
    rolloutResourceKey({ ...first.identity, apiVersion: "apps/v1" })
  )
})

test("partial apply stays distinct from completion and skipped work", async () => {
  const { rolloutStatus, rolloutSummary, resourceStageStatus } =
    await import("./rollout-timeline.ts")
  const partial = resource("a", "failed", {
    mutationApplied: true,
    stage: "ownership_cleanup",
  })
  assert.equal(rolloutStatus(partial), "Applied · follow-up failed")
  assert.equal(resourceStageStatus(partial, "applying"), "complete")
  assert.equal(resourceStageStatus(partial, "ownership_cleanup"), "failed")
  assert.equal(resourceStageStatus(partial, "recording"), "not started")
  assert.equal(
    rolloutSummary([partial, resource("b", "skipped")]),
    "1 applied · 0 fully completed · 1 not attempted"
  )
  assert.equal(rolloutStatus(resource("old", "failed")), "failed")
})

test("recreation timeline separates deletion, creation, and optional adoption", async () => {
  const { resourceStagesFor, resourceStageStatus, rolloutStatus } = await import("./rollout-timeline.ts")
  const orphan = resource("db", "running", { action: "recreate", propagationPolicy: "Orphan", stage: "waiting_statefulset_dependents", mutationApplied: true })
  assert.ok(resourceStagesFor(orphan).some(s => s.id === "waiting_statefulset_adoption"))
  assert.ok(!resourceStagesFor({ ...orphan, propagationPolicy: "Background" }).some(s => s.id === "waiting_statefulset_adoption"))
  assert.equal(resourceStageStatus(orphan, "deleting_statefulset"), "complete")
  assert.equal(resourceStageStatus(orphan, "recreating_statefulset"), "not started")
  assert.equal(rolloutStatus({ ...orphan, status: "failed" }), "Recreation incomplete")
})
