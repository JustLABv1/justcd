"use client"

import { useEffect, useState } from "react"
import { runtimeFreshness } from "@/lib/runtime-health"
import { HugeiconsIcon } from "@hugeicons/react"
import { Alert02Icon, HeartCheckIcon, ArrowRight01Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/panelui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { AppDialog } from "@/components/app-dialog"
import { StatusBadge } from "@/components/ui-kit"
import type { ApplicationStatus } from "@/lib/application-status"
import type { Application, ApplicationHealthTransition } from "@/lib/types"

/** Single application status line: sync state, blockers, runtime health and freshness. */
/** The runtime states that are a problem in their own right, not a passing rollout. */
export function runtimeProblem(status: ApplicationStatus) {
  return status.runtime.reliable && status.runtime.observed && ["Degraded", "Missing"].includes(status.runtime.status)
}

export function RuntimeHealthStrip({ application, healthHistory, status, reconnecting = false, open: controlledOpen, onOpenChange }: { application: Application; healthHistory: ApplicationHealthTransition[]; status: ApplicationStatus; reconnecting?: boolean; open?: boolean; onOpenChange?: (open: boolean) => void }) {
  const [uncontrolledOpen, setUncontrolledOpen] = useState(false)
  const open = controlledOpen ?? uncontrolledOpen
  const setOpen = (next: boolean) => { setUncontrolledOpen(next); onOpenChange?.(next) }
  const [now, setNow] = useState(() => Date.now())
  // "Observed 10s ago" does not need per-second precision; a coarse tick keeps the header from re-rendering every second.
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 10_000); return () => window.clearInterval(timer) }, [])
  const condition = application.healthCondition
  const freshness = runtimeFreshness(condition?.observedAt, now)
  // An unhealthy runtime must not hide behind a green sync badge, so it gets a badge of its own.
  const runtimeAlert = status.runtime.reliable && status.runtime.observed && !["Healthy", "Unknown"].includes(status.runtime.status)
  const parts = runtimeAlert ? status.parts.filter((part) => !part.startsWith("Runtime ")) : status.parts
  return <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
    {/* One signal per record: when the runtime is worse than the sync state, only the runtime badge is coloured. */}
    <StatusBadge status={status.sync} tone={runtimeAlert ? "neutral" : undefined} />
    {runtimeAlert && <StatusBadge status={`Runtime ${status.runtime.status}`} />}
    {parts.length > 0 && <span className="text-muted-foreground">{parts.slice(0, 2).join(" · ")}{parts.length > 2 ? ` · +${parts.length - 2}` : ""}</span>}
    <Button type="button" variant="ghost" size="sm" className="h-7 gap-1.5 px-2 text-xs" onClick={() => setOpen(true)}>
      <HugeiconsIcon icon={HeartCheckIcon} strokeWidth={1.8} aria-hidden="true" />
      Health details
      <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={1.8} aria-hidden="true" data-icon="inline-end" />
    </Button>
    <span className={freshness.stale || reconnecting ? "text-warning-foreground dark:text-warning" : ""} title={condition?.observedAt ? `${new Date(condition.observedAt).toLocaleString()} · Updates automatically` : undefined}>{freshness.label.replace("Runtime observed", "Observed")}{reconnecting ? " · Reconnecting" : freshness.stale && condition?.observedAt ? " · Stale" : ""}{condition?.status === "Partial" ? " · Partial observation" : ""}</span>
    <AppDialog open={open} onOpenChange={setOpen} variant="info" size="lg" title="Status and runtime health" description="Runtime readiness is separate from Git sync state. Partial means some runtime resources could not be assessed; configuration objects do not need workload health checks.">
      <div className="space-y-4">
        <StatusBadge status={condition?.status ?? "Unknown"} />
        <p className="text-sm"><strong>{condition?.reason ?? "HealthNotObserved"}:</strong> {condition?.message ?? "Live Kubernetes health has not been observed yet."}</p>
        <p className="text-sm text-muted-foreground">Last transition {condition?.lastTransitionTime ? new Date(condition.lastTransitionTime).toLocaleString() : "not recorded"}{condition?.observedAt ? ` · observed ${new Date(condition.observedAt).toLocaleString()}` : ""}</p>
        {(condition?.warnings?.length ?? 0) > 0 && <ul className="space-y-1 text-sm">{condition?.warnings.map((warning, index) => <li key={`${warning}-${index}`} className="flex items-start gap-2"><HugeiconsIcon icon={Alert02Icon} strokeWidth={2} aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-warning-foreground dark:text-warning" /><span>{warning}</span></li>)}</ul>}
        {(condition?.resources?.length ?? 0) > 0 && <div className="pt-3"><h3 className="text-sm font-semibold">Resources needing attention</h3><ul className="mt-2 space-y-2">{condition?.resources.map((item, index) => <li key={`${item.identity.kind}-${item.identity.namespace}-${item.identity.name}-${index}`} className="flex flex-wrap items-start gap-2 py-2.5 text-sm"><StatusBadge status={item.status} /><span className="min-w-0 flex-1"><strong>{item.identity.kind} {item.identity.namespace ? `${item.identity.namespace}/` : ""}{item.identity.name}</strong><span className="mt-1 block text-muted-foreground">{item.reason}{item.readiness ? ` · ${item.readiness}` : ""}{item.phase ? ` · phase ${item.phase}` : ""} · {item.message}</span></span></li>)}</ul></div>}
        <Disclosure className="pt-3" summary={`Recent condition transitions (${healthHistory.length})`}>
          {healthHistory.length ? <ol className="space-y-2">{healthHistory.map((item) => <li key={item.id} className="flex flex-wrap items-center gap-2 text-sm"><StatusBadge status={item.status} /><strong>{item.reason}</strong><span className="min-w-0 flex-1 text-muted-foreground">{item.message}</span><time className="text-muted-foreground" dateTime={item.changedAt}>{new Date(item.changedAt).toLocaleString()}</time></li>)}</ol> : <p className="text-sm text-muted-foreground">No health transitions have been recorded yet.</p>}
        </Disclosure>
      </div>
    </AppDialog>
  </div>
}
