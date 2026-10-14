import assert from "node:assert/strict"
import test from "node:test"
import { podState, podLabel, podSummary, runtimeFreshness } from "./runtime-health.ts"

const pod = (extra = {}) => ({ source: "kubernetes", identity: { kind: "Pod" }, ...extra })

test("running does not imply ready and waiting failures take precedence", () => {
  assert.equal(podState(pod({ phase: "Running", readiness: "Not ready" })), "starting")
  const crash = pod({ phase: "Running", readiness: "Ready", healthSummary: { failureReason: "CrashLoopBackOff" } })
  assert.equal(podState(crash), "failing")
  assert.equal(podLabel(crash), "CrashLoopBackOff")
  assert.equal(podLabel(pod({ phase: "Pending", healthSummary: { waitingReason: "ContainerCreating" } })), "ContainerCreating")
})

test("terminating, completed, unknown and sample pods stay distinct", () => {
  assert.equal(podState(pod({ phase: "Running", readiness: "Ready", healthSummary: { terminating: true } })), "terminating")
  assert.equal(podState(pod({ phase: "Succeeded" })), "completed")
  assert.equal(podState(pod()), "unknown")
  assert.equal(podSummary([pod({ phase: "Running", readiness: "Ready" }), pod({ phase: "Pending" }), pod({ source: "sample", readiness: "Ready" })]), "1 ready · 1 starting · 1 sample")
})

test("runtime freshness uses observation time and marks missing or invalid evidence stale", () => {
  const now = Date.parse("2026-10-14T10:00:00Z")
  assert.deepEqual(runtimeFreshness("2026-10-14T09:59:52Z", now), { stale: false, label: "Runtime observed 8s ago" })
  assert.equal(runtimeFreshness("2026-10-14T09:58:00Z", now).stale, true)
  assert.equal(runtimeFreshness(undefined, now).stale, true)
  assert.equal(runtimeFreshness("invalid", now).stale, true)
})
