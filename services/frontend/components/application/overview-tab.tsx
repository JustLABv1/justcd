"use client"

import Link from "next/link"
import { ProfileVersionNotice } from "@/components/applications/profile-version-notice"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon, HeartCheckIcon, Layers01Icon, Rocket01Icon } from "@hugeicons/core-free-icons"
import { useState, type ReactNode } from "react"
import { DeliverySection } from "@/components/overview/overview-parts"
import { useDeliveryMetrics } from "@/hooks/use-delivery-metrics"
import { Kpi } from "@/components/panelui/kpi"
import { ErrorDetailsButton } from "@/components/error-details"
import { Alert } from "@/components/panelui/alert"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Card } from "@/components/panelui/card"
import { Disclosure } from "@/components/ui/collapsible"
import { Skeleton } from "@/components/ui/skeleton"
import { SectionLoading } from "@/components/section-loading"
import { RollingNumber } from "@/components/panelui/rolling-number"
import { operationContext } from "@/components/application/helpers"
import { StatusBadge } from "@/components/ui-kit"
import { ago, runDuration } from "@/lib/relative-time"
import { summarizeRuntime } from "@/lib/runtime-summary"
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
  WorkspaceMember,
} from "@/lib/types"

const trailingArrow = (
  <HugeiconsIcon
    icon={ArrowRight01Icon}
    strokeWidth={2}
    data-icon="inline-end"
    aria-hidden="true"
  />
)

function OverviewKpi({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <Kpi surface render={<Button type="button" variant="ghost" onClick={onClick} />} className="h-full w-full items-stretch text-left whitespace-normal hover:shadow-md">
      {children}
    </Kpi>
  )
}

const rendererLabels: Record<string, string> = { yaml: "YAML", kustomize: "Kustomize", helm: "Helm" }
const syncPolicyLabels: Record<string, string> = { auto: "Auto", "auto-safe": "Auto-safe", manual: "Manual" }
const runtimeColor = { healthy: "success", progressing: "warning", unknown: "warning", degraded: "destructive" } as const

export function OverviewTab({
  application,
  status,
  plans,
  members,
  plansError,
  retryingPlans,
  topology,
  topologyError,
  pending,
  operations,
  operationsError,
  canUpgradeProfile,
  onRetryPlans,
  onSelectTab,
  onOpenHealth,
}: {
  application: Application
  status: ApplicationStatus
  plans: PlanRecord[]
  members: WorkspaceMember[]
  plansError: unknown | null
  retryingPlans: boolean
  topology: ResourceTopology | null
  topologyError: unknown | null
  pending: { topology: boolean; plans: boolean; operations: boolean }
  operations: Operation[]
  operationsError: unknown | null
  canUpgradeProfile: boolean
  onRetryPlans: () => void
  onSelectTab: (tab: string) => void
  onOpenHealth: () => void
}) {
  const latestPlan = plans[0]
  const counts = latestPlan?.plan.changes.reduce(
    (acc, change) => ({ ...acc, [change.kind]: acc[change.kind] + 1 }),
    { create: 0, update: 0, delete: 0 }
  )
  // A current plan with nothing to apply is "up to date", not "0 changes to review".
  const current = status.plan.state === "current" && latestPlan !== undefined && (latestPlan.plan.changes.length > 0 || Boolean(latestPlan.plan.decommission))
  const outdated = status.plan.state === "stale" || status.plan.state === "expired"
  const blocked = status.blocked
  const lastSync = operations.find((operation) => operation.status === "succeeded")
  const lastSyncDuration = lastSync ? runDuration(lastSync.startedAt, lastSync.finishedAt) : null
  const resources = topology ? applicationResourceNodes(topology) : []
  const observed = resources.filter((node) => node.source === "kubernetes").length
  const runtime = summarizeRuntime(topology)
  const topologyReady = !topologyError && !pending.topology && Boolean(topology)
  const syncedRevision = application.lastSyncedRevision
  const planLabel = latestPlan?.plan.decommission || application.configurationMissing ? "Deletion plan" : "Deployment plan"
  const changeCount = latestPlan?.plan.changes.length ?? 0
  const headline =
    plansError != null ? "Plan unavailable"
      : current ? `${blocked ? "Last known plan · " : ""}${changeCount} change${changeCount === 1 ? "" : "s"} to review`
        : status.plan.state === "expired" ? "Plan expired"
          : status.plan.state === "stale" ? "Plan out of date"
            : status.plan.state === "applied" ? "Up to date"
              : latestPlan ? "No pending changes"
                : "No plan yet"
  const planDetail =
    plansError != null ? "Retry loading to check for changes."
      : outdated ? "Git or the cluster changed since the last plan. Refresh the plan to compare again."
        : !latestPlan ? "Refresh the plan to compare Git with the cluster."
          : syncedRevision ? <>Deployed <span className="font-medium text-foreground">{application.revision}</span> @ <span className="font-mono" title={syncedRevision}>{syncedRevision.slice(0, 7)}</span></>
            : "Not synced yet"
  const runtimeFooter = !topologyReady ? "Waiting for the first observation"
    : runtime.total === 0 ? "No workloads with a health check"
      : runtime.degraded ? `${runtime.degraded} degraded${runtime.progressing ? ` · ${runtime.progressing} progressing` : ""}`
        : runtime.progressing ? `${runtime.progressing} progressing`
          : runtime.unknown ? `${runtime.unknown} not reported`
            : "All workloads ready"

  // Refetched when a run finishes, like the chart below it.
  const [deliveryRange, setDeliveryRange] = useState<"7d" | "30d">("7d")
  const delivery = useDeliveryMetrics("applications", application.id, deliveryRange, operations.slice(0, 3).map((operation) => `${operation.id}:${operation.status}`).join(","))

  return (
    <div className="space-y-5">
      {application.configurationMissing && (
        <Alert tone="destructive" role="alert">
          <Alert.Icon />
          <Alert.Title>Removed from Git</Alert.Title>
          <Alert.Description>
            {application.health === "decommissioned"
              ? "This application was removed from Git and its managed resources have been deleted. Finish removing the application from JustCD."
              : "This application was removed from Git. Review and approve its deletion plan before applying it. Workloads remain in place until then. Only namespaces explicitly managed by this application are included; shared namespaces are kept. Restore the definition to cancel deletion."}
          </Alert.Description>
        </Alert>
      )}
      <Card size="sm" aria-label={planLabel} className="min-w-0">
        <Card.Content className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
          {pending.plans ? (
            <Skeleton className="h-12 w-full max-w-sm motion-reduce:animate-none" />
          ) : (
            <>
              <div className="min-w-0 space-y-1">
                <h2 className="text-xs font-medium text-muted-foreground">{planLabel}</h2>
                <p className={`text-base font-semibold ${current && blocked ? "text-muted-foreground" : ""}`}>{headline}</p>
                {current ? (
                  <div className="mt-2 flex flex-wrap items-center gap-2">
                    {(["create", "update", "delete"] as const)
                      .filter((kind) => counts?.[kind])
                      .map((kind) => <Badge key={kind} variant="outline">{counts?.[kind]} {kind}</Badge>)}
                    {latestPlan.plan.requiresApproval && <Badge variant="warning">Approval required</Badge>}
                  </div>
                ) : (
                  <p className="text-sm text-muted-foreground">{planDetail}</p>
                )}
              </div>
              {/* Refreshing the plan lives in the page header; this card only leads to the plan. */}
              <div className="flex flex-wrap items-center gap-2">
                {plansError != null ? (
                  <>
                    <Button variant="outline" loading={retryingPlans} onClick={onRetryPlans}>Retry loading</Button>
                    <ErrorDetailsButton error={plansError} />
                  </>
                ) : latestPlan ? (
                  <Button variant={current && !blocked ? "default" : "outline"} onClick={() => onSelectTab("changes")}>
                    {current ? "Review changes" : "View plan"}{trailingArrow}
                  </Button>
                ) : null}
              </div>
            </>
          )}
        </Card.Content>
      </Card>
      <div className="grid gap-3 sm:grid-cols-3">
        <OverviewKpi onClick={onOpenHealth}>
          <Kpi.Header><Kpi.Icon tone={runtime.degraded ? "bad" : "neutral"}><HugeiconsIcon icon={HeartCheckIcon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>Runtime health</Kpi.Title></Kpi.Header>
          <Kpi.Content><Kpi.Value>{topologyReady && runtime.total ? <><RollingNumber value={runtime.healthy} /><span className="ml-1 text-base font-medium text-muted-foreground">of {runtime.total} healthy</span></> : "–"}</Kpi.Value></Kpi.Content>
          {topologyReady && runtime.total > 0 && runtime.worst && <Kpi.Progress value={runtime.healthy} maxValue={runtime.total} color={runtimeColor[runtime.worst]} aria-label={`${runtime.healthy} of ${runtime.total} workloads healthy`} />}
          <Kpi.Footer className={runtime.degraded ? "text-destructive" : undefined}>{runtimeFooter}</Kpi.Footer>
        </OverviewKpi>
        <OverviewKpi onClick={() => onSelectTab("topology")}>
          <Kpi.Header><Kpi.Icon tone={blocked ? "bad" : "neutral"}><HugeiconsIcon icon={Layers01Icon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>Resources</Kpi.Title></Kpi.Header>
          <Kpi.Content><Kpi.Value>{topologyReady ? <><RollingNumber value={blocked ? resources.length : observed} /><span className="ml-1 text-base font-medium text-muted-foreground">{blocked ? "declared" : `of ${resources.length} observed`}</span></> : "–"}</Kpi.Value></Kpi.Content>
          {/* Observation coverage, not health: drawn neutral so it never reads as "all good". */}
          {topologyReady && !blocked && <Kpi.Progress value={observed} maxValue={Math.max(resources.length, 1)} color="primary" aria-label={`${observed} of ${resources.length} resources observed`} />}
        </OverviewKpi>
        <OverviewKpi onClick={() => onSelectTab("activity")}>
          <Kpi.Header><Kpi.Icon tone="neutral"><HugeiconsIcon icon={Rocket01Icon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>Last sync</Kpi.Title></Kpi.Header>
          <Kpi.Content><Kpi.Value>{lastSync?.finishedAt ? ago(lastSync.finishedAt) : syncedRevision ? "Synced" : "Never"}</Kpi.Value></Kpi.Content>
          <Kpi.Footer>{lastSync ? `${operationContext(lastSync, plans, members).source}${lastSyncDuration ? ` · took ${lastSyncDuration}` : ""}` : "Not synced yet"}</Kpi.Footer>
        </OverviewKpi>
      </div>
      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1.6fr)_minmax(280px,1fr)]">
        <div className="min-w-0 space-y-5">
          <DeliverySection {...delivery} range={deliveryRange} onRangeChange={setDeliveryRange} description={`This application's syncs and rollbacks over the last ${deliveryRange === "30d" ? 30 : 7} days, compared with the period before.`} />
          <SyncHistoryChart
            applicationID={application.id}
            revision={operations.slice(0, 3).map((operation) => `${operation.id}:${operation.status}`).join(",")}
          />
        </div>
        <div className="min-w-0 space-y-5">
          <Card size="sm" aria-label="Source" className="min-w-0">
            <Card.Header className="items-center">
              <Card.Title as="h2">Source</Card.Title>
              <Card.Action>
                <Button size="sm" variant="outline" onClick={() => onSelectTab("settings")}>Configure{trailingArrow}</Button>
              </Card.Action>
            </Card.Header>
            <Card.Content>
              <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-3 text-sm">
                <dt className="text-muted-foreground">Revision</dt>
                <dd className="font-medium break-all">{application.revision}</dd>
                <dt className="text-muted-foreground">Renderer</dt>
                <dd className="break-all">{rendererLabels[application.renderer] ?? application.renderer}</dd>
                <dt className="text-muted-foreground">Sync policy</dt>
                <dd>{syncPolicyLabels[application.syncPolicy] ?? application.syncPolicy}{application.autoSyncPaused ? <span className="text-muted-foreground"> · paused</span> : null}</dd>
                {application.repositoryConfigurationId && <>
                  <dt className="text-muted-foreground">Managed by</dt>
                  <dd className="break-all">Git{application.configurationCommit ? ` · ${application.configurationCommit.slice(0, 7)}` : ""}</dd>
                </>}
              </dl>
              {application.publishingProfile && (
                <div className="mt-4 border-t pt-4">
                  <ProfileVersionNotice application={application} canUpgrade={canUpgradeProfile} compact />
                </div>
              )}
              {application.repositoryConfigurationId && (
                <>
                  <HealthGateSummary applicationId={application.id} onOpenActivity={() => onSelectTab("activity")} />
                  <EnvironmentSummary applicationId={application.id} />
                  <div className="pt-4">
                    <Disclosure summary="Application definition">
                      <p className="text-sm break-all text-muted-foreground">{application.configurationPath}</p>
                      <Button variant="outline" size="sm" className="mt-2" nativeButton={false} render={<Link href={`/workspaces/${application.workspaceId}/connections/git-sources`} />}>Repository discovery settings</Button>
                    </Disclosure>
                  </div>
                </>
              )}
            </Card.Content>
          </Card>
          {(pending.operations || operationsError != null || operations.length > 0) && (
            <Card size="sm" aria-label="Recent activity">
              <Card.Header className="items-center">
                <Card.Title as="h2">Recent activity</Card.Title>
                <Card.Action>
                  <Button size="sm" variant="outline" onClick={() => onSelectTab("activity")}>View all{trailingArrow}</Button>
                </Card.Action>
              </Card.Header>
              <Card.Content>
                {pending.operations ? (
                  <SectionLoading label="Loading recent activity" lines={2} className="space-y-3" />
                ) : operationsError != null ? (
                  <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
                    <span>Recent activity could not be loaded.</span>
                    <ErrorDetailsButton error={operationsError} />
                  </div>
                ) : (
                  <ol className="divide-y">
                    {operations.slice(0, 3).map((operation) => {
                      const context = operationContext(operation, plans, members)
                      return (
                        <li key={operation.id} className="flex items-start gap-3 py-3 text-sm first:pt-0 last:pb-0">
                          <StatusBadge status={operation.status} />
                          <div className="min-w-0 flex-1">
                            <p className="break-words">{operation.message || `${operation.type ?? "sync"} operation`}</p>
                            <p className="mt-0.5 text-xs text-muted-foreground">
                              {[context.revision && <span key="rev" className="font-mono">{context.revision.slice(0, 7)}</span>, context.source, context.duration && `took ${context.duration}`]
                                .filter(Boolean)
                                .flatMap((part, index) => index ? [" · ", part] : [part])}
                            </p>
                          </div>
                          <time className="shrink-0 text-xs text-muted-foreground" dateTime={operation.startedAt} title={new Date(operation.startedAt).toLocaleString()}>{ago(operation.startedAt)}</time>
                        </li>
                      )
                    })}
                  </ol>
                )}
              </Card.Content>
            </Card>
          )}
        </div>
      </div>
    </div>
  )
}
