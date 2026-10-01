"use client"

import { useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Alert02Icon, HeartCheckIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { AppDialog } from "@/components/ui/dialog"
import { StatusBadge } from "@/components/ui-kit"
import type { Application, ApplicationHealthTransition } from "@/lib/types"

/** Slim "last checked" line with a button that opens the runtime health details dialog. */
export function RuntimeHealthStrip({ application, healthHistory }: { application: Application; healthHistory: ApplicationHealthTransition[] }) {
  const [open, setOpen] = useState(false)
  const condition = application.healthCondition
  return <div className="mb-5 flex flex-wrap items-center justify-between gap-3 text-sm text-muted-foreground">
    <span>{application.lastCheckedAt ? `Last checked ${new Date(application.lastCheckedAt).toLocaleString()}` : "Application checks pending"}</span>
    <Button type="button" variant="ghost" size="sm" onClick={() => setOpen(true)}><HugeiconsIcon icon={HeartCheckIcon} strokeWidth={1.8} aria-hidden="true" />Health details</Button>
    <AppDialog open={open} onOpenChange={setOpen} variant="info" size="lg" title="Runtime health" description="Kubernetes workload health is separate from Git sync state.">
      <div className="space-y-4">
        <StatusBadge status={condition?.status ?? "Unknown"} />
        <p className="text-sm"><strong>{condition?.reason ?? "HealthNotObserved"}:</strong> {condition?.message ?? "Live Kubernetes health has not been observed yet."}</p>
        <p className="text-sm text-muted-foreground">Last transition {condition?.lastTransitionTime ? new Date(condition.lastTransitionTime).toLocaleString() : "not recorded"}{condition?.observedAt ? ` · observed ${new Date(condition.observedAt).toLocaleString()}` : ""}</p>
        {(condition?.warnings?.length ?? 0) > 0 && <ul className="space-y-1 text-sm">{condition?.warnings.map((warning, index) => <li key={`${warning}-${index}`} className="flex items-start gap-2"><HugeiconsIcon icon={Alert02Icon} strokeWidth={2} aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-warning-foreground dark:text-warning" /><span>{warning}</span></li>)}</ul>}
        {(condition?.resources?.length ?? 0) > 0 && <div className="border-t pt-3"><h3 className="text-sm font-semibold">Resources needing attention</h3><ul className="mt-2 space-y-2">{condition?.resources.map((item, index) => <li key={`${item.identity.kind}-${item.identity.namespace}-${item.identity.name}-${index}`} className="flex flex-wrap items-start gap-2 rounded-lg border p-2.5 text-sm"><StatusBadge status={item.status} /><span className="min-w-0 flex-1"><strong>{item.identity.kind} {item.identity.namespace ? `${item.identity.namespace}/` : ""}{item.identity.name}</strong><span className="mt-1 block text-muted-foreground">{item.reason}{item.readiness ? ` · ${item.readiness}` : ""}{item.phase ? ` · phase ${item.phase}` : ""} · {item.message}</span></span></li>)}</ul></div>}
        <Disclosure className="border-t pt-3" summary={`Recent condition transitions (${healthHistory.length})`}>
          {healthHistory.length ? <ol className="space-y-2">{healthHistory.map((item) => <li key={item.id} className="flex flex-wrap items-center gap-2 text-sm"><StatusBadge status={item.status} /><strong>{item.reason}</strong><span className="min-w-0 flex-1 text-muted-foreground">{item.message}</span><time className="text-muted-foreground" dateTime={item.changedAt}>{new Date(item.changedAt).toLocaleString()}</time></li>)}</ol> : <p className="text-sm text-muted-foreground">No health transitions have been recorded yet.</p>}
        </Disclosure>
      </div>
    </AppDialog>
  </div>
}
