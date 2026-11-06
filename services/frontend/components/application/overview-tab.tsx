"use client"

import Link from "next/link"
import { ProfileVersionNotice } from "@/components/applications/profile-version-notice"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon } from "@hugeicons/core-free-icons"
import { ErrorDetailsButton } from "@/components/error-details"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusBadge } from "@/components/ui-kit"
import { ago } from "@/lib/relative-time"
import type { ApplicationStatus } from "@/lib/application-status"
import { applicationResourceNodes } from "@/lib/topology-view"
import { EnvironmentSummary } from "@/components/application/environment-summary"
import { HealthGateSummary } from "@/components/application/health-gate-summary"
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
  status,
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
  canUpgradeProfile,
  onCreatePlan,
  onRetryPlans,
  onSelectTab,
}: {
  application: Application
  status: ApplicationStatus
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
  canUpgradeProfile: boolean
  onCreatePlan: () => void
  onRetryPlans: () => void
  onSelectTab: (tab: string) => void
}) {
  const counts = latestPlan?.plan.changes.reduce(
    (acc, change) => ({ ...acc, [change.kind]: acc[change.kind] + 1 }),
    { create: 0, update: 0, delete: 0 }
  )
  const current = status.plan.state === "current" && latestPlan !== undefined
  const outdated =
    status.plan.state === "stale" || status.plan.state === "expired"
  const blocked = status.blocked
  const lastSync = operations.find(
    (operation) => operation.status === "succeeded"
  )
  const resources = topology ? applicationResourceNodes(topology) : []
  const observed = resources.filter(
    (node) => node.source === "kubernetes"
  ).length
  const planLabel =
    latestPlan?.plan.decommission || application.configurationMissing
      ? "Deletion plan"
      : "Deployment plan"
  const headline =
    plansError != null
      ? "Plan unavailable"
      : current
        ? `${blocked ? "Last known plan · " : ""}${latestPlan.plan.changes.length} change${latestPlan.plan.changes.length === 1 ? "" : "s"}`
        : latestPlan
          ? outdated
            ? status.plan.state === "expired"
              ? "Plan expired"
              : "Plan out of date"
            : `Plan ${latestPlan.status}`
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
              This application was removed from Git and its managed resources
              have been deleted. Finish removing the application from JustCD.
            </>
          ) : (
            <>
              This application was removed from Git. Review and approve its
              deletion plan before applying it. Workloads remain in place until
              then. Only namespaces explicitly managed by this application are
              included; shared namespaces are kept. Restore the definition to
              cancel deletion.
            </>
          )}
        </div>
      )}
      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1.6fr)_minmax(280px,1fr)]">
        <div className="min-w-0 space-y-5">
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
                    <p
                      className={`text-2xl font-semibold tracking-tight${current && blocked ? "text-muted-foreground" : ""}`}
                    >
                      {headline}
                    </p>
                    {plansError != null ? (
                      <p className="mt-1 text-sm text-muted-foreground">
                        Retry loading to check for changes.
                      </p>
                    ) : current ? (
                      <div className="mt-2 flex flex-wrap items-center gap-2">
                        {(["create", "update", "delete"] as const)
                          .filter((kind) => counts?.[kind])
                          .map((kind) => (
                            <Badge key={kind} variant="outline" radius="full">
                              {counts?.[kind]} {kind}
                            </Badge>
                          ))}
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
                      <>
                        {outdated && (
                          <Button
                            loading={creatingPlan}
                            disabled={!canRefreshPlan}
                            onClick={onCreatePlan}
                          >
                            Refresh plan
                          </Button>
                        )}
                        <Button
                          variant={current && !blocked ? "default" : "outline"}
                          onClick={() => onSelectTab("changes")}
                        >
                          {current ? "Review changes" : "View plan"}
                          {trailingArrow}
                        </Button>
                      </>
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
          <SyncHistoryChart
            applicationID={application.id}
            revision={operations
              .slice(0, 3)
              .map((operation) => `${operation.id}:${operation.status}`)
              .join(",")}
            onViewActivity={() => onSelectTab("activity")}
          />
        </div>
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
              {application.syncPolicy === "auto"
                ? "Auto · approve deletions only"
                : application.syncPolicy === "auto-safe"
                  ? "Auto-safe"
                  : "Manual sync"}
              {application.autoSyncPaused ? " · Auto-sync paused" : ""}
            </dd>
            <dt className="text-muted-foreground">Resources</dt>
            <dd>
              {topologyError ? (
                "Unavailable"
              ) : pending.topology || !topology ? (
                "—"
              ) : (
                <button
                  type="button"
                  className="underline-offset-4 hover:underline"
                  onClick={() => onSelectTab("topology")}
                >
                  {blocked
                    ? `${resources.length} declared`
                    : `${observed} of ${resources.length} observed`}
                </button>
              )}
            </dd>
            {application.lastSyncedRevision && (
              <>
                <dt className="text-muted-foreground">Last synced</dt>
                <dd className="break-all">
                  {lastSync?.finishedAt && (
                    <span>{ago(lastSync.finishedAt)} · </span>
                  )}
                  <span
                    className="font-mono text-xs"
                    title={application.lastSyncedRevision}
                  >
                    {application.lastSyncedRevision.slice(0, 12)}
                  </span>
                </dd>
              </>
            )}
          </dl>
          {application.publishingProfile && (
            <div className="border-t p-5">
              <ProfileVersionNotice
                application={application}
                canUpgrade={canUpgradeProfile}
                compact
              />
            </div>
          )}
          {application.repositoryConfigurationId && (
            <>
              <HealthGateSummary
                applicationId={application.id}
                onOpenActivity={() => onSelectTab("activity")}
              />
              <EnvironmentSummary applicationId={application.id} />
            </>
          )}
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
      {(pending.operations ||
        operationsError != null ||
        operations.length > 0) && (
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
          ) : null}
        </section>
      )}
    </div>
  )
}
