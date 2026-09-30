import type { Application } from "./types"

export function needsAttention(app: Application) {
  return app.autoSyncPaused || [
    "out_of_sync",
    "deletion_pending",
    "degraded",
    "failed",
    "error",
  ].includes(app.health) || [
    "Progressing",
    "Degraded",
    "Suspended",
    "Missing",
    "Unknown",
    "Partial",
  ].includes(app.healthCondition?.status ?? "")
}

export function isApplicationHealthy(app: Application) {
  return !app.autoSyncPaused && app.health === "synced" && app.healthCondition?.status === "Healthy"
}

