import assert from "node:assert/strict"
import test from "node:test"
import { clusterHealth, gitSourceHealth, isCheckStalled, overallLevel } from "./connection-health.ts"

const now = Date.parse("2026-10-02T12:00:00Z")
const app = (extra = {}) => ({ id: "a", clusterId: "c1", sourceId: "g1", statusIssues: [], pollSeconds: 60, createdAt: "2026-10-01T00:00:00Z", ...extra })

test("offline agent is critical", () => {
  const result = clusterHealth({ id: "c1", connectionMode: "agent" }, { mode: "agent", online: false, agent: { revoked: false, lastSeenAt: "2026-10-02T11:50:00Z" } }, [], now)
  assert.equal(result.level, "critical")
  assert.equal(result.label, "Agent offline")
})

test("cluster errors reported by applications mark a direct cluster", () => {
  const apps = [app({ statusIssues: [{ source: "cluster", summary: "unreachable" }] })]
  assert.equal(clusterHealth({ id: "c1", connectionMode: "direct" }, undefined, apps, now).level, "critical")
  const perm = [app({ statusIssues: [{ source: "cluster", code: "kubernetes.permission_denied", summary: "denied" }] })]
  assert.equal(clusterHealth({ id: "c1", connectionMode: "direct" }, undefined, perm, now).level, "warning")
})

test("git issues mark the source and ownership conflicts are ignored", () => {
  assert.equal(gitSourceHealth({ id: "g1" }, [app({ statusIssues: [{ source: "git", summary: "x" }] })]).level, "critical")
  assert.equal(clusterHealth({ id: "c1", connectionMode: "direct" }, undefined, [app({ statusIssues: [{ source: "cluster", code: "kubernetes.ownership_conflict", summary: "x" }] })], now).level, "healthy")
})

test("stalled checks respect the polling interval", () => {
  assert.equal(isCheckStalled(app({ lastCheckedAt: "2026-10-02T11:59:00Z" }), now), false)
  assert.equal(isCheckStalled(app({ lastCheckedAt: "2026-10-02T11:00:00Z" }), now), true)
  assert.equal(isCheckStalled(app({ lastCheckedAt: "2026-10-02T11:00:00Z", autoSyncPaused: true }), now), false)
  assert.equal(overallLevel(["healthy", "warning", "critical"]), "critical")
})
