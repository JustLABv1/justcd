"use client"

import Link from "next/link"
import { ProfileVersionNotice } from "@/components/applications/profile-version-notice"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon, Layers01Icon, RefreshIcon, Rocket01Icon } from "@hugeicons/core-free-icons"
import type { ReactNode } from "react"
import { Kpi } from "@/components/panelui/kpi"
import { ErrorDetailsButton } from "@/components/error-details"
import { Alert } from "@/components/panelui/alert"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Card } from "@/components/panelui/card"
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

function OverviewKpi({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <Kpi surface render={<Button type="button" variant="ghost" onClick={onClick} />} className="h-full w-full items-stretch text-left whitespace-normal hover:shadow-md">
      {children}
    </Kpi>
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
        <Alert tone="destructive" role="alert">
          <Alert.Icon />
          <Alert.Title>Removed from Git</Alert.Title>
          <Alert.Description>
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
          </Alert.Description>
        </Alert>
      )}
          <Card size="sm" aria-label={planLabel} className="min-w-0">
            <Card.Content className="flex flex-wrap items-center justify-between gap-x-6 gap-y-4">

              {pending.plans ? (
                <Skeleton className="h-14 w-full max-w-sm motion-reduce:animate-none" />
              ) : (
                <>
                  <div className="min-w-0 space-y-1">
                    <h2 className="text-xs font-medium text-muted-foreground">{planLabel}</h2>
                    <p
                      className={`text-base font-semibold${current && blocked ? "text-muted-foreground" : ""}`}
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
                            <Badge key={kind} variant="outline">
                              {counts?.[kind]} {kind}
                            </Badge>
                          ))}
                        {latestPlan.plan.requiresApproval && (
                          <Badge variant="warning">
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
            </Card.Content>
          </Card>
      <div className="grid gap-3 sm:grid-cols-3">
        <OverviewKpi onClick={() => onSelectTab("topology")}>
          <Kpi.Header><Kpi.Icon tone={blocked ? "bad" : "neutral"}><HugeiconsIcon icon={Layers01Icon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>Resources</Kpi.Title></Kpi.Header>
          <Kpi.Content><Kpi.Value>{topologyError ? "–" : pending.topology || !topology ? "–" : blocked ? resources.length : observed}<span className="ml-1 text-base font-medium text-muted-foreground">{topologyError || pending.topology || !topology ? "" : blocked ? "declared" : `of ${resources.length} observed`}</span></Kpi.Value></Kpi.Content>
          {!topologyError && !pending.topology && topology && !blocked && <Kpi.Progress value={observed} maxValue={Math.max(resources.length, 1)} color="success" aria-label={`${observed} of ${resources.length} resources observed`} />}
        </OverviewKpi>
        <OverviewKpi onClick={() => onSelectTab("activity")}>
          <Kpi.Header><Kpi.Icon tone="neutral"><HugeiconsIcon icon={Rocket01Icon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>Last sync</Kpi.Title></Kpi.Header>
          <Kpi.Content><Kpi.Value>{lastSync?.finishedAt ? ago(lastSync.finishedAt) : application.lastSyncedRevision ? "Synced" : "Never"}</Kpi.Value></Kpi.Content>
          <Kpi.Footer>{application.lastSyncedRevision ? <span className="font-mono" title={application.lastSyncedRevision}>{application.lastSyncedRevision.slice(0, 12)}</span> : "Not synced yet"}</Kpi.Footer>
        </OverviewKpi>
        <OverviewKpi onClick={() => onSelectTab("settings")}>
          <Kpi.Header><Kpi.Icon tone={application.autoSyncPaused ? "bad" : "neutral"}><HugeiconsIcon icon={RefreshIcon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>Sync policy</Kpi.Title></Kpi.Header>
          <Kpi.Content><Kpi.Value className="text-2xl">{application.syncPolicy === "auto" ? "Auto" : application.syncPolicy === "auto-safe" ? "Auto-safe" : "Manual"}</Kpi.Value></Kpi.Content>
          <Kpi.Footer>{application.autoSyncPaused ? "Auto-sync paused" : application.syncPolicy === "auto" ? "Approve deletions only" : application.syncPolicy === "auto-safe" ? "Safe changes apply automatically" : "Changes wait for you to sync"}</Kpi.Footer>
        </OverviewKpi>
      </div>
      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1.6fr)_minmax(280px,1fr)]">
        <div className="min-w-0 space-y-5">
          <SyncHistoryChart
            applicationID={application.id}
            revision={operations
              .slice(0, 3)
              .map((operation) => `${operation.id}:${operation.status}`)
              .join(",")}
            onViewActivity={() => onSelectTab("activity")}
          />
        </div>
        <div className="min-w-0 space-y-5">
        <Card size="sm" aria-label="Source" className="min-w-0">
          <Card.Header className="items-center">
            <Card.Title as="h2">Source</Card.Title>
            <Card.Action>
              <Button size="sm" variant="outline" onClick={() => onSelectTab("settings")}>
                Configure{trailingArrow}
              </Button>
            </Card.Action>
          </Card.Header>
          <Card.Content>
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-3 text-sm">
            <dt className="text-muted-foreground">Revision</dt>
            <dd className="font-medium break-all">{application.revision}</dd>
            <dt className="text-muted-foreground">Renderer</dt>
            <dd className="break-all">{application.renderer === "yaml" ? "YAML" : application.renderer === "kustomize" ? "Kustomize" : application.renderer === "helm" ? "Helm" : application.renderer}</dd>
            {application.repositoryConfigurationId && <>
              <dt className="text-muted-foreground">Managed by</dt>
              <dd className="break-all">Git{application.configurationCommit ? ` · ${application.configurationCommit.slice(0, 7)}` : ""}</dd>
            </>}
            </dl>
          {application.publishingProfile && (
            <div className="pt-4">
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
            <div className="pt-4">
              <Disclosure summary="Application definition">
                <p className="text-sm break-all text-muted-foreground">
                  {application.configurationPath}
                </p>
                <Button variant="outline" size="sm" className="mt-2" nativeButton={false} render={<Link href={`/workspaces/${application.workspaceId}/connections/git-sources`} />}>Repository discovery settings</Button>
              </Disclosure>
            </div>
          )}
          </Card.Content>
        </Card>
      {(pending.operations ||
        operationsError != null ||
        operations.length > 0) && (
        <Card size="sm" aria-label="Recent activity">
          <Card.Header>
            <Card.Title as="h2">Recent activity</Card.Title>
            <Card.Action>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => onSelectTab("activity")}
              >
                View all{trailingArrow}
              </Button>
            </Card.Action>
          </Card.Header>
          <Card.Content>
          {pending.operations ? (
            <SectionLoading label="Loading recent activity" />
          ) : operationsError != null ? (
            <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
              <span>Recent activity could not be loaded.</span>
              <ErrorDetailsButton error={operationsError} />
            </div>
          ) : operations.length ? (
            <ol className="divide-y">
              {operations.slice(0, 3).map((operation) => (
                <li
                  key={operation.id}
                  className="flex flex-wrap items-center gap-x-3 gap-y-1 py-3 text-sm first:pt-0 last:pb-0"
                >
                  <StatusBadge status={operation.status} />
                  <span className="min-w-0 flex-1 break-words">
                    {operation.message || `${operation.type} operation`}
                  </span>
                  <time
                    className="shrink-0 text-xs text-muted-foreground"
                    dateTime={operation.startedAt}
                    title={new Date(operation.startedAt).toLocaleString()}
                  >
                    {ago(operation.startedAt)}
                  </time>
                </li>
              ))}
            </ol>
          ) : null}
          </Card.Content>
        </Card>
      )}
        </div>
      </div>
    </div>
  )
}
