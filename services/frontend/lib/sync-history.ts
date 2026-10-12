export type SyncHistoryBucket = {
  startedAt: string
  succeeded: number
  failed: number
  cancelled: number
  active: number
}
export type SyncHistory = {
  startedAt: string
  endedAt: string
  bucketSeconds: number
  buckets: SyncHistoryBucket[]
}

export function summarizeSyncHistory(history: SyncHistory) {
  const totals = history.buckets.reduce(
    (sum, bucket) => ({
      succeeded: sum.succeeded + bucket.succeeded,
      failed: sum.failed + bucket.failed,
      cancelled: sum.cancelled + bucket.cancelled,
      active: sum.active + bucket.active,
    }),
    { succeeded: 0, failed: 0, cancelled: 0, active: 0 }
  )
  const completed = totals.succeeded + totals.failed + totals.cancelled
  return {
    ...totals,
    total: completed + totals.active,
    successRate: completed ? (totals.succeeded / completed) * 100 : null,
  }
}
