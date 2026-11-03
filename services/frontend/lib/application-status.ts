import type { Application, PlanRecord } from "./types"

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
  if (app.configurationMissing) return app.health === "decommissioned" ? "Removed from Git · decommissioned" : "Removed from Git · deletion review required"
  if (app.autoSyncPaused) return "Reconciliation paused"
  const condition = app.healthCondition
  if (condition && ["Progressing", "Degraded", "Suspended", "Missing", "Unknown", "Partial"].includes(condition.status)) {
    return `Runtime ${condition.status.toLowerCase()}${condition.message ? `: ${condition.message}` : ""}`
  }
  if (needsAttention(app)) return `Delivery ${app.health.replaceAll("_", " ")}`
  return null
}

export type PlanState = "none" | "current" | "stale" | "expired" | "applied"
export type ApplicationStatus = {
  /** Git sync state label as reported by the control plane. */
  sync: string
  runtime: { status: string; observed: boolean; reliable: boolean }
  plan: { state: PlanState; changes: number }
  problems: number
  conflicts: number
  paused: boolean
  /** Pending changes: only a current, unexpired plan has actionable changes. */
  pendingChanges: number
  /** True when something prevents sync, so plan data should not look final. */
  blocked: boolean
  /** Short phrases, most important first, for the single status line. */
  parts: string[]
}

/** One derivation of "how is this application doing", shared by every component. */
export function deriveApplicationStatus(
  application: Application,
  { plan = null, conflictCount = 0, now = Date.now() }: { plan?: PlanRecord | null; conflictCount?: number; now?: number } = {}
): ApplicationStatus {
  const problems = application.statusIssues?.length ?? 0
  const runtimeStatus = application.healthCondition?.status ?? "Unknown"
  const observed = Boolean(application.healthCondition?.observedAt)
  const reliable = !(problems > 0 && ["Missing", "Unknown", "Partial"].includes(runtimeStatus))
  const state: PlanState = !plan ? "none"
    : plan.status === "applied" ? "applied"
    : plan.status === "current" ? (Date.parse(plan.expiresAt) <= now ? "expired" : "current")
    : "stale"
  const changes = plan?.plan.changes.length ?? 0
  const paused = Boolean(application.autoSyncPaused && !application.branchTest)
  const blocked = problems > 0 || conflictCount > 0
  // The attention bar and plan card carry the detail; keep this to the headline.
  const parts = [
    blocked && "Sync blocked",
    reliable && observed && runtimeStatus !== "Unknown" && `Runtime ${runtimeStatus.toLowerCase()}`,
    !reliable && !blocked && "Runtime not observed",
    paused && "Auto-sync paused",
  ].filter((part): part is string => Boolean(part))
  return {
    sync: application.health,
    runtime: { status: runtimeStatus, observed, reliable },
    plan: { state, changes },
    problems,
    conflicts: conflictCount,
    paused,
    pendingChanges: state === "current" ? changes : 0,
    blocked,
    parts,
  }
}
