import { applicationAttentionReason } from "./application-status.ts"
import type { Application } from "./types"

export type CardTone = "success" | "destructive" | "warning" | "neutral"
export type CardBadge = { key: "definition" | "delivery" | "runtime" | "paused" | "decommissioning"; title: string; status: string; tone: CardTone }
export type CardStatus = {
  /** Status badges in display order. At most one is `warning` or `destructive`. */
  badges: CardBadge[]
  /** Why the application itself needs a look. Null when fine, or when only its cluster is the cause. */
  reason: string | null
  /** Tone of the icon beside the reason line. */
  signal: "destructive" | "warning" | "neutral"
  /** Waiting on a person (a deletion review). The one state that earns a coloured card edge. */
  needsDecision: boolean
}

type CardApplication = Pick<Application, "health" | "healthCondition" | "statusIssues" | "decommissioning" | "autoSyncPaused" | "retryTerminalReason" | "retryNextAt"> & { configurationMissing?: boolean }

const rank: Record<CardTone, number> = { destructive: 3, warning: 2, neutral: 1, success: 0 }

/** Explicit on purpose: a delivery state is coloured exactly when it puts the application on the attention list. */
function deliveryTone(health: string): CardTone {
  if (health === "synced") return "success"
  if (["degraded", "failed", "error"].includes(health)) return "destructive"
  if (["out_of_sync", "deletion_pending"].includes(health)) return "warning"
  // unknown, syncing, pending, decommissioned and anything new: a fact, not a problem.
  return "neutral"
}

function runtimeTone(status: string | undefined): CardTone {
  if (status === "Healthy") return "success"
  if (status === "Degraded" || status === "Missing") return "destructive"
  if (status === "Progressing" || status === "Suspended" || status === "Unknown" || status === "Partial") return "warning"
  // Not observed yet.
  return "neutral"
}

/**
 * One derivation of what an application card or list row shows as status.
 *
 * Rules, covered by application-card-status.test.mjs:
 * - Every application on the attention list shows a coloured signal: a badge, or the reason icon.
 * - Only the worst badge is coloured; the rest are neutral facts (a success stays green).
 * - Blocked by an unreachable cluster, delivery and runtime are consequences and turn neutral. States
 *   that are the application's own (deletion review, paused, decommissioning) keep their colour.
 * - A pending deletion review is the signal on its own, in `warning`, whatever else is wrong.
 */
export function applicationCardStatus(app: CardApplication, options: { blocked?: boolean } = {}): CardStatus {
  const blocked = Boolean(options.blocked)
  const decommissioned = app.health === "decommissioned"
  const needsDecision = !decommissioned && (app.health === "deletion_pending" || Boolean(app.configurationMissing))
  const runtime = app.healthCondition?.status

  const items: (CardBadge & { consequence?: boolean })[] = [
    ...(app.configurationMissing ? [{ key: "definition" as const, title: "Application definition", status: "Definition missing", tone: decommissioned ? "neutral" as const : "warning" as const }] : []),
    { key: "delivery", title: "Delivery state", status: app.health, tone: deliveryTone(app.health), consequence: true },
    { key: "runtime", title: "Runtime state", status: runtime ?? "Unknown", tone: runtimeTone(runtime), consequence: true },
    ...(app.autoSyncPaused ? [{ key: "paused" as const, title: "Reconciliation", status: "Reconciliation paused", tone: "warning" as const }] : []),
    ...(app.decommissioning && !decommissioned ? [{ key: "decommissioning" as const, title: "Lifecycle", status: "Decommissioning", tone: "warning" as const }] : []),
  ]
  const own = items.map((item) => (blocked && item.consequence ? { ...item, tone: "neutral" as const } : item))

  let signalItem: (typeof own)[number] | undefined
  if (needsDecision) {
    signalItem = own.find((item) => item.key === (app.health === "deletion_pending" ? "delivery" : "definition"))
  } else {
    const worst = own.reduce((best, item) => (rank[item.tone] > rank[best.tone] ? item : best), own[0])
    if (rank[worst.tone] >= rank.warning) signalItem = worst
  }
  const badges: CardBadge[] = own.map(({ consequence: _consequence, ...item }) => {
    void _consequence
    if (signalItem && item.key === signalItem.key) return { ...item, tone: needsDecision ? "warning" : item.tone }
    return { ...item, tone: item.tone === "success" && !needsDecision ? "success" : "neutral" }
  })

  let reason: string | null
  if (blocked) {
    // The outage is stated once for the cluster; only the application's own decision is repeated here.
    reason = needsDecision ? (app.configurationMissing ? applicationAttentionReason(app as Application) : "Deletion review required") : null
  } else {
    reason = applicationAttentionReason(app as Application) ?? app.statusIssues?.[0]?.summary ?? null
    // The paused badge already says this.
    if (reason === "Reconciliation paused") reason = null
  }

  const retriesExhausted = !blocked && !app.decommissioning && Boolean(app.retryTerminalReason)
  const signal: CardStatus["signal"] = needsDecision
    ? "warning"
    : decommissioned
      ? "neutral"
      : retriesExhausted || signalItem?.tone === "destructive"
        ? "destructive"
        : "warning"

  return { badges, reason, signal, needsDecision }
}
