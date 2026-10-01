"use client"

import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon } from "@hugeicons/core-free-icons"
import { ErrorDetailsButton } from "@/components/error-details"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusBadge } from "@/components/ui-kit"
import type { Application, Operation, PlanRecord, ResourceTopology } from "@/lib/types"

const trailingArrow = <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} data-icon="inline-end" aria-hidden="true" />

function SectionLoading({ label }: { label: string }) {
  return <div role="status" aria-label={label} className="space-y-3 p-5"><span className="sr-only">{label}…</span><div aria-hidden="true" className="space-y-3"><Skeleton className="h-5 w-2/3 max-w-72 motion-reduce:animate-none" /><Skeleton className="h-4 w-full max-w-lg motion-reduce:animate-none" /></div></div>
}

export function OverviewTab({ application, latestPlan, plansError, retryingPlans, topology, topologyError, pending, operations, operationsError, canDeploy, busy, hasPendingOperation, creatingPlan, planBlockedByNamespace, onCreatePlan, onRetryPlans, onSelectTab }: {
  application: Application
  latestPlan?: PlanRecord
  plansError: unknown | null
  retryingPlans: boolean
  topology: ResourceTopology | null
  topologyError: unknown | null
  pending: { topology: boolean; plans: boolean; operations: boolean }
  operations: Operation[]
  operationsError: unknown | null
  canDeploy: boolean
  busy: boolean
  hasPendingOperation: boolean
  creatingPlan: boolean
  planBlockedByNamespace: boolean
  onCreatePlan: () => void
  onRetryPlans: () => void
  onSelectTab: (tab: string) => void
}) {
  const counts = latestPlan?.plan.changes.reduce((acc, change) => ({ ...acc, [change.kind]: acc[change.kind] + 1 }), { create: 0, update: 0, delete: 0 })
  const current = latestPlan?.status === "current"
  const headline = plansError != null ? "Plan unavailable" : current ? `${latestPlan.plan.changes.length} planned change${latestPlan.plan.changes.length === 1 ? "" : "s"}` : latestPlan ? `Plan ${latestPlan.status}` : "No plan yet"
  const resourcesNote = pending.topology ? "Loading resources…" : topologyError ? "Resource count unavailable" : topology ? `${topology.nodes.length} observed resources` : "Resources not observed yet"

  return <div className="space-y-5">
    {application.configurationMissing && <div role="alert" className="rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm">The application definition is missing from Git. Automatic processing is stopped; workloads remain in place. Restore the file to resume.</div>}
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1.6fr)_minmax(280px,1fr)]">
      <section aria-label="Status and plan" className="min-w-0 rounded-xl border bg-card">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
          <div className="flex flex-wrap items-center gap-3"><h2 className="text-sm font-semibold">Runtime health</h2><StatusBadge status={application.healthCondition?.status ?? "Unknown"} /></div>
          <Button size="sm" variant="ghost" onClick={() => onSelectTab("topology")}>{resourcesNote}{trailingArrow}</Button>
        </div>
        <div className="space-y-4 p-5">
          {pending.plans ? <Skeleton className="h-16 w-full max-w-sm motion-reduce:animate-none" /> : <>
            <div>
              <p className="text-2xl font-semibold tracking-tight">{headline}</p>
              {plansError != null ? <p className="mt-1 text-sm text-muted-foreground">Retry loading to check for changes.</p> : current
                ? <div className="mt-2 flex flex-wrap items-center gap-2"><Badge variant="success-light" radius="full">{counts?.create} create</Badge><Badge variant="info-light" radius="full">{counts?.update} update</Badge><Badge variant="destructive-light" radius="full">{counts?.delete} delete</Badge>{latestPlan.plan.requiresApproval && <Badge variant="warning-light" radius="full">Approval required</Badge>}</div>
                : <p className="mt-1 text-sm text-muted-foreground">{application.lastSyncedRevision ? `Last synced ${application.lastSyncedRevision.slice(0, 12)}` : "Not synced yet"}</p>}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {plansError != null
                ? <Button variant="outline" loading={retryingPlans} onClick={onRetryPlans}>Retry loading</Button>
                : latestPlan
                  ? <Button variant={current ? "default" : "outline"} onClick={() => onSelectTab("changes")}>{current ? "Review changes" : "View plan"}{trailingArrow}</Button>
                  : <Button loading={creatingPlan} disabled={busy || hasPendingOperation || !canDeploy || application.configurationMissing || application.decommissioning || planBlockedByNamespace} onClick={onCreatePlan}>Create plan{trailingArrow}</Button>}
              {plansError != null && <ErrorDetailsButton error={plansError} />}
            </div>
          </>}
        </div>
      </section>
      <section aria-label="Source" className="min-w-0 rounded-xl border bg-card">
        <div className="flex items-center justify-between gap-3 border-b px-5 py-4"><h2 className="text-sm font-semibold">Source</h2><Button size="sm" variant="ghost" onClick={() => onSelectTab("activity")}>Source details{trailingArrow}</Button></div>
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-3 p-5 text-sm">
          <dt className="text-muted-foreground">Revision</dt><dd className="break-all font-medium">{application.revision}</dd>
          <dt className="text-muted-foreground">Source</dt><dd className="break-all">{application.repositoryConfigurationId ? `Git-managed${application.configurationCommit ? ` · ${application.configurationCommit.slice(0, 7)}` : ""}` : application.renderer}</dd>
          <dt className="text-muted-foreground">Sync policy</dt><dd>{application.syncPolicy === "auto-safe" ? "Auto-safe" : "Manual sync"}{application.autoSyncPaused ? " · Auto-sync paused" : ""}</dd>
          {application.lastSyncedRevision && <><dt className="text-muted-foreground">Last synced</dt><dd className="break-all font-mono text-xs">{application.lastSyncedRevision.slice(0, 12)}</dd></>}
        </dl>
        {application.repositoryConfigurationId && <div className="border-t px-5 py-3"><Disclosure summary="Application definition"><p className="break-all text-sm text-muted-foreground">{application.configurationPath}</p><Link className="mt-2 inline-block text-sm text-primary underline underline-offset-4" href={`/workspaces/${application.workspaceId}/connections/git-sources`}>Repository discovery settings</Link></Disclosure></div>}
      </section>
    </div>
    <section aria-label="Recent activity" className="rounded-xl border bg-card">
      <div className="flex items-center justify-between gap-3 px-5 py-4"><h2 className="text-sm font-semibold">Recent activity</h2><Button size="sm" variant="ghost" onClick={() => onSelectTab("activity")}>View all{trailingArrow}</Button></div>
      {pending.operations ? <SectionLoading label="Loading recent activity" /> : operationsError != null ? <div className="flex flex-wrap items-center justify-between gap-3 px-5 pb-5 text-sm"><span>Recent activity could not be loaded.</span><ErrorDetailsButton error={operationsError} /></div> : operations.length ? <ol className="divide-y border-t">{operations.slice(0, 3).map((operation) => <li key={operation.id} className="flex flex-wrap items-center gap-3 px-5 py-4 text-sm"><StatusBadge status={operation.status} /><span className="min-w-0 flex-1 break-words">{operation.message || `${operation.type} operation`}</span><time className="text-sm text-muted-foreground" dateTime={operation.startedAt}>{new Date(operation.startedAt).toLocaleString()}</time></li>)}</ol> : <p className="px-5 pb-5 text-sm text-muted-foreground">No operations recorded yet.</p>}
    </section>
  </div>
}
