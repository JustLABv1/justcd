import assert from "node:assert/strict"
import test from "node:test"
import { summarizeSyncHistory } from "./sync-history.ts"

const history = (buckets) => ({
  buckets,
  startedAt: "2026-10-11T12:00:00Z",
  endedAt: "2026-10-12T12:00:00Z",
  bucketSeconds: 3600,
})
const bucket = (counts = {}) => ({
  startedAt: "2026-10-11T12:00:00Z",
  succeeded: 0,
  failed: 0,
  cancelled: 0,
  active: 0,
  ...counts,
})

test("success rate includes cancellations and excludes active runs", () => {
  const result = summarizeSyncHistory(
    history([
      bucket({ succeeded: 8, failed: 1 }),
      bucket({ cancelled: 1, active: 25 }),
    ])
  )
  assert.deepEqual(result, {
    succeeded: 8,
    failed: 1,
    cancelled: 1,
    active: 25,
    total: 35,
    successRate: 80,
  })
})

test("empty and active-only histories do not claim a success rate", () => {
  for (const buckets of [[], [bucket()], [bucket({ active: 3 })]]) {
    assert.equal(summarizeSyncHistory(history(buckets)).successRate, null)
  }
})

test("failed-only histories have a zero success rate", () => {
  assert.equal(
    summarizeSyncHistory(history([bucket({ failed: 5 })])).successRate,
    0
  )
})
