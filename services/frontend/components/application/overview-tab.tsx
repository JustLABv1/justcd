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
import { OverviewMetrics } from "@/components/application/overview-metrics"
import { SyncHistoryChart } from "@/components/application/sync-history-chart"
import type {
  Application,
  Operation,
  PlanRecord,
  ResourceTopology,
} from "@/lib/types"

const trailingArrow = (
  <HugeiconsIcon
    icon={ArrowRight01Icon}
    strokeWidth={2}
    data-icon="inline-end"
    aria-hidden="true"
  />
)

function SectionLoading({ label }: { label: string }) {
  return (
    <div role="status" aria-label={label} className="space-y-3 p-5">
      <span className="sr-only">{label}…</span>
      <div aria-hidden="true" className="space-y-3">
        <Skeleton className="h-5 w-2/3 max-w-72 motion-reduce:animate-none" />
        <Skeleton className="h-4 w-full max-w-lg motion-reduce:animate-none" />
      </div>
    </div>
  )
}

export function OverviewTab({
  application,
  latestPlan,
  plansError,
  retryingPlans,
  topology,
  topologyError,
  pending,
  operations,
  operationsError,
  creatingPlan,
  canRefreshPlan,
  onCreatePlan,
  onRetryPlans,
  onSelectTab,
}: {
  application: Application
  latestPlan?: PlanRecord
  plansError: unknown | null
  retryingPlans: boolean
  topology: ResourceTopology | null
  topologyError: unknown | null
  pending: { topology: boolean; plans: boolean; operations: boolean }
  operations: Operation[]
  operationsError: unknown | null
  creatingPlan: boolean
  canRefreshPlan: boolean
  onCreatePlan: () => void
  onRetryPlans: () => void
  onSelectTab: (tab: string) => void
}) {
  const counts = latestPlan?.plan.changes.reduce(
    (acc, change) => ({ ...acc, [change.kind]: acc[change.kind] + 1 }),
    { create: 0, update: 0, delete: 0 }
  )
  const current = latestPlan?.status === "current"
  const planLabel =
    latestPlan?.plan.decommission || application.configurationMissing
      ? "Deletion plan"
      : "Deployment plan"
  const headline =
    plansError != null
      ? "Plan unavailable"
      : current
        ? `${latestPlan.plan.changes.length} planned change${latestPlan.plan.changes.length === 1 ? "" : "s"}`
        : latestPlan
          ? `Plan ${latestPlan.status}`
          : "No plan yet"

  return (
    <div className="space-y-5">
      {application.configurationMissing && (
        <div
          role="alert"
          className="rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm"
        >
          {application.health === "decommissioned" ? (
            <>
              This application was removed from Git and its managed resources have
              been deleted. Finish removing the application from JustCD.
            </>
          ) : (
            <>
              This application was removed from Git. Review and approve its deletion
              plan before applying it. Workloads remain in place until then. Only
              namespaces explicitly managed by this application are included;
              shared namespaces are kept. Restore the definition to cancel deletion.
            </>
          )}
        </div>
      )}
      <OverviewMetrics
        application={application}
        topology={topology}
        pending={pending}
        topologyError={topologyError}
        onSelectTab={onSelectTab}
      />
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1.6fr)_minmax(280px,1fr)]">
        <section
          aria-label={planLabel}
          className="min-w-0 rounded-xl border bg-card"
        >
          <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
            <h2 className="text-sm font-semibold">{planLabel}</h2>
          </div>
          <div className="space-y-4 p-5">
            {pending.plans ? (
              <Skeleton className="h-16 w-full max-w-sm motion-reduce:animate-none" />
            ) : (
              <>
                <div>
                  <p className="text-2xl font-semibold tracking-tight">
                    {headline}
                  </p>
                  {plansError != null ? (
                    <p className="mt-1 text-sm text-muted-foreground">
                      Retry loading to check for changes.
                    </p>
                  ) : current ? (
                    <div className="mt-2 flex flex-wrap items-center gap-2">
                      <Badge variant="success-light" radius="full">
                        {counts?.create} create
                      </Badge>
                      <Badge variant="info-light" radius="full">
                        {counts?.update} update
                      </Badge>
                      <Badge variant="destructive-light" radius="full">
                        {counts?.delete} delete
                      </Badge>
                      {latestPlan.plan.requiresApproval && (
                        <Badge variant="warning-light" radius="full">
                          Approval required
                        </Badge>
                      )}
                    </div>
                  ) : (
                    <p className="mt-1 text-sm text-muted-foreground">
                      {application.lastSyncedRevision
                        ? `Last synced ${application.lastSyncedRevision.slice(0, 12)}`
                        : "Not synced yet"}
                    </p>
                  )}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  {plansError != null ? (
                    <Button
                      variant="outline"
                      loading={retryingPlans}
                      onClick={onRetryPlans}
                    >
                      Retry loading
                    </Button>
                  ) : latestPlan ? (
                    <Button
                      variant={current ? "default" : "outline"}
                      onClick={() => onSelectTab("changes")}
                    >
                      {current ? "Review changes" : "View plan"}
                      {trailingArrow}
                    </Button>
                  ) : (
                    <Button
                      loading={creatingPlan}
                      disabled={!canRefreshPlan}
                      onClick={onCreatePlan}
                    >
                      Create plan{trailingArrow}
                    </Button>
                  )}
                  {plansError != null && (
                    <ErrorDetailsButton error={plansError} />
                  )}
                </div>
              </>
            )}
          </div>
        </section>
        <section
          aria-label="Source"
          className="min-w-0 rounded-xl border bg-card"
        >
          <div className="flex items-center justify-between gap-3 border-b px-5 py-4">
            <h2 className="text-sm font-semibold">Source</h2>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => onSelectTab("activity")}
            >
              Source details{trailingArrow}
            </Button>
          </div>
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-3 p-5 text-sm">
            <dt className="text-muted-foreground">Revision</dt>
            <dd className="font-medium break-all">{application.revision}</dd>
            <dt className="text-muted-foreground">Source</dt>
            <dd className="break-all">
              {application.repositoryConfigurationId
                ? `Git-managed${application.configurationCommit ? ` · ${application.configurationCommit.slice(0, 7)}` : ""}`
                : application.renderer}
            </dd>
            <dt className="text-muted-foreground">Sync policy</dt>
            <dd>
              {application.syncPolicy === "auto-safe"
                ? "Auto-safe"
                : "Manual sync"}
              {application.autoSyncPaused ? " · Auto-sync paused" : ""}
            </dd>
            {application.lastSyncedRevision && (
              <>
                <dt className="text-muted-foreground">Last synced</dt>
                <dd className="font-mono text-xs break-all">
                  {application.lastSyncedRevision.slice(0, 12)}
                </dd>
              </>
            )}
          </dl>
          {application.repositoryConfigurationId && (
            <div className="border-t px-5 py-3">
              <Disclosure summary="Application definition">
                <p className="text-sm break-all text-muted-foreground">
                  {application.configurationPath}
                </p>
                <Link
                  className="mt-2 inline-block text-sm text-primary underline underline-offset-4"
                  href={`/workspaces/${application.workspaceId}/connections/git-sources`}
                >
                  Repository discovery settings
                </Link>
              </Disclosure>
            </div>
          )}
        </section>
      </div>
      <SyncHistoryChart
        applicationID={application.id}
        revision={operations
          .slice(0, 3)
          .map((operation) => `${operation.id}:${operation.status}`)
          .join(",")}
        onViewActivity={() => onSelectTab("activity")}
      />
      <section
        aria-label="Recent activity"
        className="rounded-xl border bg-card"
      >
        <div className="flex items-center justify-between gap-3 px-5 py-4">
          <h2 className="text-sm font-semibold">Recent activity</h2>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => onSelectTab("activity")}
          >
            View all{trailingArrow}
          </Button>
        </div>
        {pending.operations ? (
          <SectionLoading label="Loading recent activity" />
        ) : operationsError != null ? (
          <div className="flex flex-wrap items-center justify-between gap-3 px-5 pb-5 text-sm">
            <span>Recent activity could not be loaded.</span>
            <ErrorDetailsButton error={operationsError} />
          </div>
        ) : operations.length ? (
          <ol className="divide-y border-t">
            {operations.slice(0, 3).map((operation) => (
              <li
                key={operation.id}
                className="flex flex-wrap items-center gap-3 px-5 py-4 text-sm"
              >
                <StatusBadge status={operation.status} />
                <span className="min-w-0 flex-1 break-words">
                  {operation.message || `${operation.type} operation`}
                </span>
                <time
                  className="text-sm text-muted-foreground"
                  dateTime={operation.startedAt}
                >
                  {new Date(operation.startedAt).toLocaleString()}
                </time>
              </li>
            ))}
          </ol>
        ) : (
          <p className="px-5 pb-5 text-sm text-muted-foreground">
            No operations recorded yet.
          </p>
        )}
      </section>
    </div>
  )
}
