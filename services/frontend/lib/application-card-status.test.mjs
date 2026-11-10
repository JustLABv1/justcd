import assert from "node:assert/strict"
import test from "node:test"
import { applicationCardStatus } from "./application-card-status.ts"
import { needsAttention } from "./application-status.ts"

const deliveries = ["synced", "out_of_sync", "deletion_pending", "degraded", "failed", "error", "unknown", "syncing", "pending", "decommissioned", "something_new"]
const runtimes = [undefined, "Healthy", "Progressing", "Degraded", "Suspended", "Missing", "Unknown", "Partial"]
const flagSets = []
for (const configurationMissing of [false, true]) for (const autoSyncPaused of [false, true]) for (const decommissioning of [false, true]) {
  for (const retry of [{}, { retryNextAt: "2026-11-10T00:00:00Z" }, { retryTerminalReason: "max_attempts" }]) {
    for (const statusIssues of [[], [{ source: "git", summary: "could not clone Git source", observedAt: "" }]]) flagSets.push({ configurationMissing, autoSyncPaused, decommissioning, statusIssues, ...retry })
  }
}
const all = []
for (const health of deliveries) for (const status of runtimes) for (const flags of flagSets) all.push({ health, healthCondition: status ? { status } : undefined, ...flags })

const coloured = (tone) => tone === "warning" || tone === "destructive"

test("every application on the attention list shows a coloured signal", () => {
  for (const app of all) {
    if (!needsAttention(app) || app.health === "decommissioned") continue
    const status = applicationCardStatus(app)
    const badge = status.badges.some((item) => coloured(item.tone))
    const icon = status.reason !== null && coloured(status.signal)
    assert.ok(badge || icon, `no signal for ${JSON.stringify(app)}`)
  }
})

test("a card never carries more than one coloured badge", () => {
  for (const app of all) for (const blocked of [false, true]) {
    const count = applicationCardStatus(app, { blocked }).badges.filter((item) => coloured(item.tone)).length
    assert.ok(count <= 1, `${count} coloured badges for ${JSON.stringify(app)}`)
  }
})

test("an application that is fine shows nothing coloured and no reason", () => {
  const status = applicationCardStatus({ health: "synced", healthCondition: { status: "Healthy" }, statusIssues: [], decommissioning: false })
  assert.deepEqual(status.badges.map((item) => item.tone), ["success", "success"])
  assert.equal(status.reason, null)
  assert.equal(status.needsDecision, false)
})

test("runtime unknown is a warning with a reason, not a grey badge", () => {
  const status = applicationCardStatus({ health: "synced", healthCondition: { status: "Unknown", message: "No supported managed workloads are available to evaluate." }, statusIssues: [], decommissioning: false })
  assert.equal(status.badges.find((item) => item.key === "runtime").tone, "warning")
  assert.match(status.reason, /^Runtime unknown/)
  assert.equal(status.signal, "warning")
})

test("a runtime that was never observed stays neutral", () => {
  const status = applicationCardStatus({ health: "unknown", statusIssues: [], decommissioning: false })
  assert.deepEqual(status.badges.map((item) => item.tone), ["neutral", "neutral"])
  assert.equal(status.reason, null)
})

test("exhausted retries are red even when the badges are green", () => {
  const status = applicationCardStatus({ health: "synced", healthCondition: { status: "Healthy" }, statusIssues: [], decommissioning: false, retryTerminalReason: "max_attempts" })
  assert.equal(status.signal, "destructive")
  assert.match(status.reason, /retries exhausted/)
})

test("blocked by its cluster, delivery and runtime turn neutral and the reason is not repeated", () => {
  const status = applicationCardStatus({ health: "degraded", healthCondition: { status: "Missing" }, statusIssues: [{ source: "cluster", summary: "cluster unreachable", observedAt: "" }], decommissioning: false, retryTerminalReason: "max_attempts" }, { blocked: true })
  assert.ok(status.badges.every((item) => item.tone === "neutral"))
  assert.equal(status.reason, null)
})

test("blocked applications keep the states that are their own", () => {
  const paused = applicationCardStatus({ health: "degraded", healthCondition: { status: "Unknown" }, statusIssues: [], decommissioning: false, autoSyncPaused: true }, { blocked: true })
  assert.equal(paused.badges.find((item) => item.key === "paused").tone, "warning")
  const review = applicationCardStatus({ health: "deletion_pending", healthCondition: { status: "Missing" }, statusIssues: [], decommissioning: false, configurationMissing: true }, { blocked: true })
  assert.equal(review.needsDecision, true)
  assert.equal(review.badges.find((item) => item.key === "delivery").tone, "warning")
  assert.match(review.reason, /deletion review required/)
})

test("a pending deletion review is the single warning signal", () => {
  const status = applicationCardStatus({ health: "deletion_pending", healthCondition: { status: "Missing" }, statusIssues: [], decommissioning: false, configurationMissing: true })
  assert.equal(status.needsDecision, true)
  assert.equal(status.signal, "warning")
  assert.deepEqual(status.badges.filter((item) => coloured(item.tone)).map((item) => [item.key, item.tone]), [["delivery", "warning"]])
})

test("a decommissioned application is informational", () => {
  const status = applicationCardStatus({ health: "decommissioned", healthCondition: { status: "Missing" }, statusIssues: [], decommissioning: false, configurationMissing: true })
  assert.equal(status.needsDecision, false)
  assert.equal(status.signal, "neutral")
  assert.equal(status.badges.find((item) => item.key === "definition").tone, "neutral")
})

test("an application being decommissioned says so", () => {
  const status = applicationCardStatus({ health: "synced", healthCondition: { status: "Healthy" }, statusIssues: [], decommissioning: true })
  assert.equal(status.badges.find((item) => item.key === "decommissioning").tone, "warning")
})
