import type { Application, PlanRecord } from "./types"

/** Automatic reconciliation failed: retries are scheduled or have been exhausted. */
export function reconciliationStalled(app: Application) {
  return !app.decommissioning && Boolean(app.retryTerminalReason || app.retryNextAt)
}

export function needsAttention(app: Application) {
  return Boolean(app.configurationMissing) || app.autoSyncPaused || reconciliationStalled(app) || [
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
  return !app.configurationMissing && !app.autoSyncPaused && !reconciliationStalled(app) && app.health === "synced" && app.healthCondition?.status === "Healthy"
}


export function applicationAttentionReason(app: Application): string | null {
  if (app.configurationMissing) return app.health === "decommissioned" ? "Removed from Git · decommissioned" : "Removed from Git · deletion review required"
  if (app.autoSyncPaused) return "Reconciliation paused"
  if (app.retryTerminalReason) return "Auto-sync stopped · retries exhausted"
  if (app.retryNextAt) return "Sync failed · retry scheduled"
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
    !paused && application.retryTerminalReason && "Auto-sync stopped",
    !paused && !application.retryTerminalReason && application.retryNextAt && "Sync retrying",
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

export type ApplicationTab = "overview" | "topology" | "changes" | "activity" | "settings"

/** The application tab that holds the fix for a rule-detected problem. */
export function attentionTab(app: Pick<Application, "configurationMissing" | "decommissioning" | "health" | "retryTerminalReason" | "retryNextAt" | "healthCondition">): ApplicationTab {
  if (app.configurationMissing || app.decommissioning || ["out_of_sync", "deletion_pending"].includes(app.health)) return "changes"
  if (app.retryTerminalReason || app.retryNextAt) return "activity"
  if (["Progressing", "Degraded", "Suspended", "Missing", "Unknown", "Partial"].includes(app.healthCondition?.status ?? "")) return "topology"
  return "overview"
}

/** Button label for a link to an application tab, named after what the reader does there. */
export const tabActionLabel: Record<string, string> = { topology: "View resources", changes: "Review changes", activity: "View activity", settings: "Open settings", overview: "Open application" }
