import assert from "node:assert/strict"
import test from "node:test"
import { filterRollbackTargets } from "./activity-history.ts"

const targets = Array.from({ length: 12 }, (_, i) => ({ id: `target-${i}`, kind: i % 2 ? "pre_operation" : "successful_sync", operationId: `operation-${i}`, revision: i === 10 ? "older-ABC-commit" : "main", resourceCount: 1, createdAt: "2026-10-12T10:00:00Z" }))

test("rollback search finds an older target before pagination", () => {
 assert.deepEqual(filterRollbackTargets(targets, " abc ", "").map(t => t.id), ["target-10"])
 assert.deepEqual(filterRollbackTargets(targets, "operation-11", "pre_operation").map(t => t.id), ["target-11"])
 assert.equal(filterRollbackTargets(targets, "abc", "pre_operation").length, 0)
})
test("rollback type filters and empty queries retain order for five-item pages", () => {
 const filtered = filterRollbackTargets(targets, "", "successful_sync")
 assert.deepEqual(filtered.slice(0, 5).map(t => t.id), ["target-0", "target-2", "target-4", "target-6", "target-8"])
 assert.deepEqual(filtered.slice(5, 10).map(t => t.id), ["target-10"])
 assert.equal(filterRollbackTargets(targets, "missing", "").length, 0)
 assert.equal(filterRollbackTargets(targets, "successful deployment", "").length, 6)
 assert.deepEqual(filterRollbackTargets(targets, " ", ""), targets)
})
