"use client"

import { DeploymentScheduleNotice } from "@/components/deployment-schedule-notice"
import { AIAssistant } from "@/components/ai-assistant"
import { WorkspaceDigest } from "@/components/ai-digest"
import { TriageStatus, useTriage } from "@/components/ai-triage"
import Link from "next/link"
import { useEffect, useMemo, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Add01Icon, Alert02Icon, Refresh01Icon } from "@hugeicons/core-free-icons"
import { Alert } from "@/components/panelui/alert"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Card } from "@/components/panelui/card"
import { Frame } from "@/components/panelui/frame"
import { Tabs } from "@/components/panelui/tabs"
import { FrameLink, LinkRow, StatusPill, SummaryKpis } from "@/components/overview/overview-parts"
import { PageHeading } from "@/components/ui-kit"
import { CollectionSkeleton, LoadError } from "@/components/workspace-ui"
import { useWorkspace, needsAttention, isApplicationHealthy } from "@/hooks/use-workspace"
import { useWorkspaceHealth } from "@/hooks/use-workspace-health"
import { useWorkspaceSelection } from "@/hooks/workspace-selection"
import { SyncHistoryChartView } from "@/components/application/sync-history-chart"
import { useSyncHistory } from "@/hooks/use-sync-history"
import { summarizeSyncHistory } from "@/lib/sync-history"
import { ago } from "@/lib/relative-time"
import { applicationAttentionReason } from "@/lib/application-status"
import { clusterHealth, gitSourceHealth, isCheckStalled, type ConnectionHealth } from "@/lib/connection-health"
import type { TriageSeverity } from "@/lib/types"

type ConnectionRow = { key: string; kind: "Cluster" | "Git source"; name: string; sub: string; health: ConnectionHealth }

const severityLabel: Record<TriageSeverity, string> = { critical: "Critical", high: "High", medium: "Medium", low: "Low" }
const severityVariant = { critical: "destructive", high: "warning", medium: "info", low: "success" } as const
const ATTENTION_LIMIT = 5
const SYNC_LIMIT = 5

export default function OverviewPage() {
  const { applications, loading, error, refresh } = useWorkspace()
  const { workspace, workspaceId } = useWorkspaceSelection()
  const health = useWorkspaceHealth(workspaceId)
  const [now, setNow] = useState(() => Date.now())
  const [syncRevision, setSyncRevision] = useState(0)
  const syncHistory = useSyncHistory("workspaces", workspaceId, String(syncRevision))
  const refreshOverview = () => {
    refresh()
    setSyncRevision((value) => value + 1)
  }
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 15000)
    return () => window.clearInterval(timer)
  }, [])

  const model = useMemo(() => {
    const attention = applications.filter(needsAttention)
    const synced = applications.filter(isApplicationHealthy).length
    const other = applications.length - attention.length - synced
    const stalled = applications.filter((app) => isCheckStalled(app, now) && !needsAttention(app))
    const rows: ConnectionRow[] = [
      ...health.clusters.map((cluster) => ({
        key: `c-${cluster.id}`, kind: "Cluster" as const, name: cluster.name,
        sub: cluster.connectionMode,
        health: clusterHealth(cluster, health.agents[cluster.id], applications, now),
      })),
      ...health.gitSources.map((source) => ({
        key: `g-${source.id}`, kind: "Git source" as const, name: source.name, sub: source.repositoryUrl,
        health: gitSourceHealth(source, applications),
      })),
    ].sort((a, b) => ["critical", "warning", "unknown", "healthy"].indexOf(a.health.level) - ["critical", "warning", "unknown", "healthy"].indexOf(b.health.level))
    return { attention, synced, other, stalled, rows }
  }, [applications, health, now])

  const total = applications.length
  const qs = workspaceId ? `?workspaceId=${encodeURIComponent(workspaceId)}` : ""
  const listHref = (status?: string) => (workspaceId ? `/applications${qs}${status ? `&status=${status}` : ""}` : "/workspaces")
  const connectionsHref = workspaceId ? `/workspaces/${workspaceId}/connections/git-sources` : "/workspaces"
  const { attention, synced, stalled, rows } = model
  const triage = useTriage(workspaceId ?? "")
  const ranked = new Set(triage.triage?.items.map((item) => item.applicationId) ?? [])
  // Applications ranked by triage are shown there; the rest (for example stalled checks) keep the plain rows.
  const rest = [...attention, ...stalled].filter((app) => !ranked.has(app.id))
  const failedLabels = { clusters: "clusters", gitSources: "Git sources", approvals: "approvals" } as const

  const syncHistoryData = syncHistory.result.history
  const syncSummary = syncHistoryData
    ? { ...summarizeSyncHistory(syncHistoryData), series: syncHistoryData.buckets.map((bucket) => bucket.succeeded + bucket.failed + bucket.cancelled + bucket.active) }
    : null
  const problems = rows.filter((row) => row.health.level !== "healthy")
  const syncApplications = syncHistory.result.history?.applications ?? []
  // Ranked applications first, then whatever else needs a look (for example stalled checks).
  const attentionItems = [
    ...(triage.triage?.items ?? []).map((item) => ({
      id: item.applicationId,
      name: item.applicationName,
      href: `/applications/${item.applicationId}${item.actionTab && item.actionTab !== "overview" ? `?tab=${item.actionTab}` : ""}`,
      severity: item.severity,
      headline: item.headline.replace(new RegExp(`^${item.applicationName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s*[:\\-–—]\\s*`, "i"), ""),
      next: item.action,
    })),
    ...rest.map((app) => ({
      id: app.id,
      name: app.name,
      href: `/applications/${app.id}`,
      severity: undefined,
      headline: applicationAttentionReason(app) ?? `No check ${ago(app.lastCheckedAt || app.createdAt, now)} (every ${app.pollSeconds}s expected)`,
      next: undefined,
    })),
  ]
  const visibleAttention = attentionItems.slice(0, ATTENTION_LIMIT)
  const urgent = attentionItems.some((item) => item.severity === "critical" || item.severity === "high")

  return (
    <>
      <PageHeading
        title={workspace ? workspace.name : "Overview"}
        description={workspace ? "What needs you right now, and how delivery is going." : "Select a workspace to view its delivery status."}
        actions={
          <>
            {workspaceId && <AIAssistant key={`ask-${workspaceId}`} workspaceId={workspaceId} features={["ask"]} buttonLabel="Ask AI" />}
            <Button type="button" variant="ghost" onClick={refreshOverview}><HugeiconsIcon icon={Refresh01Icon} strokeWidth={1.8} aria-hidden="true" />Refresh</Button>
            <Button render={<Link href={workspaceId ? `/applications/new${qs}` : "/workspaces"} />} nativeButton={false}>
              <HugeiconsIcon icon={Add01Icon} strokeWidth={1.8} aria-hidden="true" />New application
            </Button>
          </>
        }
      />
      {workspaceId && <DeploymentScheduleNotice key={workspaceId} scope="workspaces" id={workspaceId} />}
      {error ? (
        <LoadError error={error} retry={refresh} />
      ) : loading && !total ? (
        <CollectionSkeleton />
      ) : (
        <div className="space-y-6">
          {health.failed.length > 0 && <p role="status" className="text-sm text-muted-foreground">Could not load {health.failed.map((key) => failedLabels[key]).join(", ")}. Some numbers may be incomplete.</p>}

          {problems.length > 0 && (
            <Alert tone={problems.some((row) => row.health.level === "critical") ? "destructive" : "warning"}>
              <Alert.Icon />
              <Alert.Title>{problems.length === 1 ? `${problems[0].name} needs attention` : `${problems.length} connections need attention`}</Alert.Title>
              <Alert.Description>
                {problems.slice(0, 2).map((row) => `${row.name}: ${row.health.label.toLowerCase()}`).join(" · ")}
                {problems.length > 2 ? ` · +${problems.length - 2} more` : ""}
              </Alert.Description>
              <Alert.Actions><Button render={<Link href={connectionsHref} />} nativeButton={false} size="sm" variant="outline">Review connections</Button></Alert.Actions>
            </Alert>
          )}

          <SummaryKpis
            total={total}
            healthy={synced}
            attention={attention.length + stalled.length}
            approvals={health.approvals.length}
            syncs={syncSummary}
            syncRange={syncHistory.range}
            hrefs={{ healthy: listHref("synced"), attention: listHref("attention"), approvals: "/approvals", syncs: "#insights" }}
          />

          <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(0,380px)]">
            <Frame>
              <Frame.Header>
                <Frame.Title as="h2" className="flex items-center gap-2">
                  {attentionItems.length > 0 && <HugeiconsIcon icon={Alert02Icon} strokeWidth={1.8} aria-hidden="true" className={`size-4 ${urgent ? "text-destructive" : "text-warning"}`} />}
                  Needs attention
                  {attentionItems.length > 0 && <Badge variant={urgent ? "destructive" : "warning"} size="sm">{attentionItems.length}</Badge>}
                </Frame.Title>
                <Frame.Description>{triage.triage?.source === "ai" && triage.triage.summary ? triage.triage.summary : "Most important first."}</Frame.Description>
                <Frame.Action><TriageStatus triage={triage.triage} starting={triage.starting} onRun={() => void triage.run()} /></Frame.Action>
              </Frame.Header>
              <Frame.Panel>
                {visibleAttention.length ? visibleAttention.map((item) => (
                  <LinkRow key={item.id} href={item.href}>
                    <Frame.Content>
                      <span className="flex flex-wrap items-center gap-2">
                        <Frame.Title as="p">{item.name}</Frame.Title>
                        {item.severity && <Badge variant={severityVariant[item.severity]} size="sm">{severityLabel[item.severity]}</Badge>}
                      </span>
                      <span className="text-sm">{item.headline}</span>
                      {item.next && <span className="text-sm text-muted-foreground">Next: {item.next}</span>}
                    </Frame.Content>
                  </LinkRow>
                )) : (
                  <div className="px-4 py-10 text-center">
                    <p className="text-sm font-medium">{total ? "Nothing needs attention" : "A fresh start"}</p>
                    <p className="mt-1 text-sm text-muted-foreground">{total ? "No drift, degradation, pauses, or stalled checks." : "Create your first application to start delivering."}</p>
                    {!total && workspaceId && <Button render={<Link href={`/applications/new${qs}`} />} nativeButton={false} size="sm" className="mt-4">Create application</Button>}
                  </div>
                )}
                {attentionItems.length > ATTENTION_LIMIT && (
                  <Frame.Row className="justify-between text-sm text-muted-foreground">
                    <span>{attentionItems.length - ATTENTION_LIMIT} more</span>
                    <FrameLink href={listHref("attention")}>View all</FrameLink>
                  </Frame.Row>
                )}
              </Frame.Panel>
              {triage.error && !triage.triage && <p role="status" className="px-4 py-2 text-xs text-muted-foreground">Priority ranking is unavailable right now.</p>}
            </Frame>

            <div className="min-w-0 space-y-6">
              {health.approvals.length > 0 && (
                <Frame>
                  <Frame.Header>
                    <Frame.Title as="h2" className="flex items-center gap-2">Waiting for review<Badge variant="secondary" size="sm">{health.approvals.length}</Badge></Frame.Title>
                    <Frame.Action><FrameLink href="/approvals">Open inbox</FrameLink></Frame.Action>
                  </Frame.Header>
                  <Frame.Panel>
                    {health.approvals.slice(0, 4).map((item) => (
                      <LinkRow key={item.planId} href={`/applications/${item.applicationId}?tab=changes`}>
                        <Frame.Content>
                          <Frame.Title as="p">{item.applicationName}</Frame.Title>
                          <Frame.Description>{item.changeCount} change{item.changeCount === 1 ? "" : "s"} · {item.approvedApprovals}/{item.requiredApprovals} approvals · {ago(item.createdAt, now)}</Frame.Description>
                        </Frame.Content>
                        {item.deletions.length > 0 && <Frame.Actions><Badge variant="destructive" size="sm">{item.deletions.length} deletion{item.deletions.length === 1 ? "" : "s"}</Badge></Frame.Actions>}
                      </LinkRow>
                    ))}
                  </Frame.Panel>
                </Frame>
              )}

              <Frame>
                <Frame.Header>
                  <Frame.Title as="h2">Recent syncs</Frame.Title>
                  <Frame.Action><FrameLink href={listHref()}>Applications</FrameLink></Frame.Action>
                </Frame.Header>
                <Frame.Panel>
                  {syncHistory.result.loading ? <p role="status" className="px-4 py-6 text-sm text-muted-foreground">Loading syncs…</p>
                    : syncHistory.result.error ? (
                      <div role="status" className="flex items-center justify-between gap-3 px-4 py-4 text-sm text-muted-foreground">
                        <span>Syncs could not be loaded.</span>
                        <Button size="sm" variant="outline" onClick={syncHistory.retry}>Retry</Button>
                      </div>
                    ) : syncApplications.filter((app) => app.startedAt).length ? syncApplications.filter((app) => app.startedAt).slice(0, SYNC_LIMIT).map((app) => (
                      <LinkRow key={app.applicationId} href={`/applications/${encodeURIComponent(app.applicationId)}?tab=activity`}>
                        <Frame.Content>
                          <Frame.Title as="p">{app.name}</Frame.Title>
                          <Frame.Description>{app.type === "rollback" ? "Rollback" : "Sync"} · {ago(app.startedAt!, now)}</Frame.Description>
                        </Frame.Content>
                        <Frame.Actions><StatusPill status={app.status || "Not run"} /></Frame.Actions>
                      </LinkRow>
                    )) : <p className="px-4 py-6 text-center text-sm text-muted-foreground">No syncs yet.</p>}
                </Frame.Panel>
              </Frame>

              {health.loaded && <Card size="sm">
                <Card.Header>
                  <Card.Title as="h2">Connections</Card.Title>
                  <Card.Description>{rows.length ? (problems.length ? `${rows.length - problems.length} of ${rows.length} reachable` : `All ${rows.length} reachable`) : "None connected yet"}</Card.Description>
                  <Card.Action><Button variant="outline" size="sm" nativeButton={false} render={<Link href={connectionsHref} />}>Manage</Button></Card.Action>
                </Card.Header>
              </Card>}
            </div>
          </div>

          {workspaceId && (
            <Tabs defaultValue="activity" id="insights">
              <Tabs.List>
                <Tabs.Trigger value="activity">Sync activity</Tabs.Trigger>
                {total > 0 && <Tabs.Trigger value="digest">Activity digest</Tabs.Trigger>}
              </Tabs.List>
              <Tabs.Content value="activity"><SyncHistoryChartView state={syncHistory} /></Tabs.Content>
              {total > 0 && <Tabs.Content value="digest"><WorkspaceDigest key={workspaceId} workspaceId={workspaceId} /></Tabs.Content>}
            </Tabs>
          )}
        </div>
      )}
    </>
  )
}
