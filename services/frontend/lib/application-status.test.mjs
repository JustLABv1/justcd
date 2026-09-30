import assert from "node:assert/strict"
import test from "node:test"
import { needsAttention, isApplicationHealthy } from "./application-status.ts"

test("delivery summary categories never double-count synced but degraded applications", () => {
  const applications = [
    { health: "synced", healthCondition: { status: "Degraded" } },
    { health: "out_of_sync", healthCondition: { status: "Unknown" } },
  ]
  const healthy = applications.filter(isApplicationHealthy).length
  const attention = applications.filter(needsAttention).length
  const other = applications.filter(app => !needsAttention(app) && !isApplicationHealthy(app)).length
  assert.equal(healthy, 0)
  assert.equal(attention, 2)
  assert.equal(other, 0)
  assert.equal(healthy + attention + other, applications.length)
})

test("paused reconciliation remains visible even with healthy runtime and synced delivery", () => {
  const app = { autoSyncPaused: true, health: "synced", healthCondition: { status: "Healthy" } }
  assert.equal(needsAttention(app), true)
  assert.equal(isApplicationHealthy(app), false)
  assert.equal(isApplicationHealthy({ ...app, autoSyncPaused: false }), true)
})


test("missing Git definitions need attention while workloads remain healthy", () => {
  const app = { configurationMissing: true, health: "synced", healthCondition: { status: "Healthy" } }
  assert.equal(needsAttention(app), true)
  assert.equal(isApplicationHealthy(app), false)
})
