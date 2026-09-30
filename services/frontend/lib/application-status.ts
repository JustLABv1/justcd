import type { Application } from "./types"

export function needsAttention(app: Application) {
  return Boolean(app.configurationMissing) || app.autoSyncPaused || [
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
  return !app.configurationMissing && !app.autoSyncPaused && app.health === "synced" && app.healthCondition?.status === "Healthy"
}


export function applicationAttentionReason(app: Application): string | null {
  if (app.configurationMissing) return "Git definition missing"
  if (app.autoSyncPaused) return "Reconciliation paused"
  const condition = app.healthCondition
  if (condition && ["Progressing", "Degraded", "Suspended", "Missing", "Unknown", "Partial"].includes(condition.status)) {
    return `Runtime ${condition.status.toLowerCase()}${condition.message ? `: ${condition.message}` : ""}`
  }
  if (needsAttention(app)) return `Delivery ${app.health.replaceAll("_", " ")}`
  return null
}
