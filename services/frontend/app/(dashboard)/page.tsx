"use client"

import Link from "next/link"
import { useEffect, useMemo, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  Add01Icon,
  ArrowRight01Icon,
  Clock01Icon,
} from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { PageHeading, EmptyState, Panel, StatusBadge } from "@/components/ui-kit"
import { ActionLink, CollectionSkeleton, LoadError, WorkspaceIcon } from "@/components/workspace-ui"
import { useWorkspace, needsAttention, isApplicationHealthy } from "@/hooks/use-workspace"
import { useWorkspaceHealth } from "@/hooks/use-workspace-health"
import { useWorkspaceSelection } from "@/hooks/workspace-selection"
import { tone } from "@/components/health-tone"
import { ago } from "@/lib/relative-time"
import { applicationAttentionReason } from "@/lib/application-status"
import { clusterHealth, gitSourceHealth, isCheckStalled, overallLevel, type ConnectionHealth, type HealthLevel } from "@/lib/connection-health"

function TrailingLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <Button render={<Link href={href} />} nativeButton={false} variant="link" size="sm" className="px-0">
      {children}
      <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={1.8} aria-hidden="true" />
    </Button>
  )
}

function LevelIcon({ level, className = "size-4" }: { level: HealthLevel; className?: string }) {
  return (
    <>
      <HugeiconsIcon icon={tone[level].icon} strokeWidth={2} className={`${className} ${tone[level].text}`} aria-hidden="true" />
      <span className="sr-only">{level}: </span>
    </>
  )
}

function Metric({ label, value, note, level, href }: { label: string; value: string; note: string; level: HealthLevel; href: string }) {
  return (
    <Link href={href} className="group rounded-xl border bg-card p-4 transition-colors hover:bg-muted/40 sm:p-5">
      <div className="flex items-center justify-between gap-2">
        <p className="text-sm font-medium text-muted-foreground">{label}</p>
        <span aria-hidden="true" className={`size-2 rounded-full ${tone[level].dot}`} />
      </div>
      <p className="mt-2 text-3xl font-semibold tracking-tight tabular-nums">{value}</p>
      <p className="mt-1 truncate text-sm text-muted-foreground">{note}</p>
    </Link>
  )
}

type ConnectionRow = { key: string; kind: "Cluster" | "Git source"; name: string; sub: string; health: ConnectionHealth }

function ConnectionList({ rows, href }: { rows: ConnectionRow[]; href: string }) {
  if (!rows.length) {
    return <EmptyState title="No connections yet" description="Connect a Git source and a cluster to start delivering applications." href={href} action="Add connections" actionVariant="outline" />
  }
  return (
    <ul className="divide-y">
      {rows.map((row) => (
        <li key={row.key} className="flex items-start gap-3 px-5 py-3.5">
          <span aria-hidden="true" className={`mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg ${tone[row.health.level].bg} ${tone[row.health.level].text}`}>
            <WorkspaceIcon name={row.kind === "Cluster" ? "server" : "branch"} className="size-4" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <span className="truncate text-sm font-medium">{row.name}</span>
              <span className="text-xs text-muted-foreground">{row.kind}</span>
            </div>
            <p className={`mt-0.5 text-sm ${row.health.level === "healthy" ? "text-muted-foreground" : tone[row.health.level].text}`}>
              <LevelIcon level={row.health.level} className="mr-1 inline size-3.5 align-[-2px]" />
              <span className="font-medium">{row.health.label}</span> · {row.health.detail}
            </p>
            {row.health.affectedApplications.length > 0 && (
              <p className="mt-0.5 truncate text-xs text-muted-foreground">
                Affects {row.health.affectedApplications.length} application{row.health.affectedApplications.length === 1 ? "" : "s"}: {row.health.affectedApplications.map((app) => app.name).join(", ")}
              </p>
            )}
          </div>
        </li>
      ))}
    </ul>
  )
}

export default function OverviewPage() {
  const { applications, loading, error, refresh } = useWorkspace()
  const { workspace, workspaceId } = useWorkspaceSelection()
  const health = useWorkspaceHealth(workspaceId)
  const [now, setNow] = useState(() => Date.now())
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
    const connectionsDown = rows.filter((row) => row.health.level === "critical").length
    const connectionsWarn = rows.filter((row) => row.health.level === "warning" || row.health.level === "unknown").length
    const recent = [...applications]
      .sort((a, b) => (Date.parse(b.lastCheckedAt || b.createdAt) || 0) - (Date.parse(a.lastCheckedAt || a.createdAt) || 0))
      .slice(0, 5)
    const level = overallLevel([
      connectionsDown ? "critical" : connectionsWarn ? "warning" : "healthy",
      attention.some((app) => app.health === "failed" || app.health === "error" || app.healthCondition?.status === "Degraded") ? "critical" : attention.length || stalled.length ? "warning" : "healthy",
      health.failed.length ? "warning" : "healthy",
    ])
    return { attention, synced, other, stalled, rows, connectionsDown, connectionsWarn, recent, level }
  }, [applications, health, now])

  const total = applications.length
  const qs = workspaceId ? `?workspaceId=${encodeURIComponent(workspaceId)}` : ""
  const listHref = (status?: string) => (workspaceId ? `/applications${qs}${status ? `&status=${status}` : ""}` : "/workspaces")
  const connectionsHref = workspaceId ? `/workspaces/${workspaceId}?tab=connections` : "/workspaces"
  const { attention, synced, other, stalled, rows, connectionsDown, connectionsWarn, recent, level } = model

  const problems: string[] = []
  if (connectionsDown) problems.push(`${connectionsDown} connection${connectionsDown === 1 ? "" : "s"} down`)
  if (connectionsWarn) problems.push(`${connectionsWarn} connection${connectionsWarn === 1 ? "" : "s"} degraded`)
  if (attention.length) problems.push(`${attention.length} application${attention.length === 1 ? " needs" : "s need"} attention`)
  if (stalled.length) problems.push(`${stalled.length} with stalled checks`)
  if (health.approvalTotal) problems.push(`${health.approvalTotal} approval${health.approvalTotal === 1 ? "" : "s"} waiting`)
  const failedLabels = { clusters: "clusters", gitSources: "Git sources", approvals: "approvals" } as const
  const headline = !workspaceId ? "No workspace selected"
    : level === "healthy" ? (total || rows.length ? "Everything is running smoothly" : "Nothing connected yet")
    : level === "critical" ? "Action required" : "Some things need a look"

  return (
    <>
      <PageHeading
        title={workspace ? `${workspace.name} overview` : "Overview"}
        description={workspace ? "Live health of connections, deployments, and pending reviews in this workspace." : "Select a workspace to view its delivery status."}
        actions={
          <>
            <Button type="button" variant="outline" onClick={refresh}>Refresh</Button>
            <ActionLink href={workspaceId ? `/applications/new${qs}` : "/workspaces"}>
              <HugeiconsIcon icon={Add01Icon} strokeWidth={1.8} aria-hidden="true" /> New application
            </ActionLink>
          </>
        }
      />
      {error ? (
        <LoadError error={error} retry={refresh} />
      ) : loading && !total ? (
        <CollectionSkeleton />
      ) : (
        <div className="space-y-6">
          <section
            role={level === "healthy" ? "status" : "alert"}
            className={`flex flex-wrap items-center gap-4 rounded-xl border p-4 sm:p-5 ${tone[level].border} ${tone[level].bg}`}
          >
            <span aria-hidden="true" className={`grid size-10 shrink-0 place-items-center rounded-full bg-background/60 ${tone[level].text}`}>
              <HugeiconsIcon icon={tone[level].icon} strokeWidth={2} className="size-5" />
            </span>
            <div className="min-w-0 flex-1">
              <h2 className="text-base font-semibold">{headline}</h2>
              <p className="mt-0.5 text-sm text-muted-foreground">
                {problems.length ? problems.join(" · ") : "All connections respond, no applications need attention, and no reviews are waiting."}
              </p>
              {health.failed.length > 0 && (
                <p className="mt-1 text-sm text-warning-foreground dark:text-warning">
                  Could not load {health.failed.map((key) => failedLabels[key]).join(", ")}; their status is not included above.
                </p>
              )}
            </div>
          </section>

          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <Metric
              label="Healthy applications"
              value={total ? `${Math.round((synced / total) * 100)}%` : "–"}
              note={`${synced} of ${total} in sync`}
              level={!total ? "unknown" : synced === total ? "healthy" : attention.length ? "warning" : "unknown"}
              href={listHref()}
            />
            <Metric
              label="Connections"
              value={health.loaded ? `${rows.length - connectionsDown - connectionsWarn}/${rows.length}` : "…"}
              note={connectionsDown ? `${connectionsDown} down` : connectionsWarn ? `${connectionsWarn} degraded` : "All reachable"}
              level={connectionsDown ? "critical" : connectionsWarn || health.failed.length ? "warning" : health.loaded ? "healthy" : "unknown"}
              href={connectionsHref}
            />
            <Metric
              label="Needs attention"
              value={String(attention.length + stalled.length)}
              note={stalled.length ? `${stalled.length} stalled checks` : "Drift, errors, pauses"}
              level={attention.length || stalled.length ? "warning" : "healthy"}
              href={listHref("attention")}
            />
            <Metric
              label="Pending approvals"
              value={health.loaded ? String(health.approvalTotal) : "…"}
              note={health.approvals[0] ? `Oldest ${ago(health.approvals[health.approvals.length - 1]?.createdAt, now)}` : "Nothing to review"}
              level={health.approvalTotal ? "warning" : health.loaded ? "healthy" : "unknown"}
              href="/approvals"
            />
          </div>

          <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(0,420px)]">
            <div className="min-w-0 space-y-6">
              <Panel
                title="Needs attention"
                description="Applications with drift, runtime problems, pauses, or stalled checks."
                action={<TrailingLink href={listHref("attention")}>View all</TrailingLink>}
              >
                {attention.length + stalled.length ? (
                  <ul className="divide-y">
                    {[...attention, ...stalled].slice(0, 6).map((app) => {
                      const reason = applicationAttentionReason(app) ?? `No check ${ago(app.lastCheckedAt || app.createdAt, now)} (every ${app.pollSeconds}s expected)`
                      return (
                        <li key={app.id}>
                          <Link href={`/applications/${app.id}`} className="flex flex-wrap items-center gap-3 px-5 py-4 transition-colors hover:bg-muted/40">
                            <span aria-hidden="true" className="grid size-9 shrink-0 place-items-center rounded-lg bg-warning/10 text-warning-foreground dark:text-warning">
                              <WorkspaceIcon name="app" className="size-4" />
                            </span>
                            <span className="min-w-0 flex-1">
                              <span className="block truncate text-sm font-medium">{app.name}</span>
                              <span className="mt-0.5 block truncate text-xs text-muted-foreground">
                                {app.clusterName ?? "cluster"} · {app.revision}
                              </span>
                              <span className="mt-1 block text-sm text-warning-foreground dark:text-warning">{reason}</span>
                              {app.statusIssues?.[0] && (
                                <span className="mt-0.5 block text-xs text-muted-foreground">{app.statusIssues[0].summary}</span>
                              )}
                            </span>
                            <StatusBadge status={app.health} />
                            {app.autoSyncPaused && <StatusBadge status="Reconciliation paused" />}
                            <WorkspaceIcon name="arrow" className="size-4 text-muted-foreground" />
                          </Link>
                        </li>
                      )
                    })}
                  </ul>
                ) : (
                  <EmptyState
                    title={total ? "Nothing needs attention" : "A fresh start"}
                    description={total ? "No drift, degradation, pauses, or stalled checks." : "Create your first application to start delivering."}
                    href={total || !workspaceId ? undefined : `/applications/new${qs}`}
                    action="Create application"
                  />
                )}
              </Panel>

              <Panel
                title="Pending approvals"
                description="Plans waiting for a decision."
                action={<TrailingLink href="/approvals">Open inbox</TrailingLink>}
              >
                {health.approvals.length ? (
                  <ul className="divide-y">
                    {health.approvals.map((item) => (
                      <li key={item.planId}>
                        <Link href={`/applications/${item.applicationId}?tab=changes`} className="flex flex-wrap items-center gap-3 px-5 py-3.5 transition-colors hover:bg-muted/40">
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-sm font-medium">{item.applicationName}</span>
                            <span className="mt-0.5 block text-xs text-muted-foreground">
                              {item.changeCount} changes · {item.approvedApprovals}/{item.requiredApprovals} approvals · requested {ago(item.createdAt, now)} · expires {new Date(item.expiresAt).toLocaleString()}
                            </span>
                          </span>
                          {item.deletions.length > 0 && <Badge size="sm" radius="full" variant="destructive-light">{item.deletions.length} deletion{item.deletions.length === 1 ? "" : "s"}</Badge>}
                          {item.clusterScoped.length > 0 && <Badge size="sm" radius="full" variant="warning-light">Cluster scoped</Badge>}
                          <Badge size="sm" radius="full" variant="primary-light" className="capitalize">{item.kind || "sync"}</Badge>
                        </Link>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="px-5 py-8 text-center text-sm text-muted-foreground">
                    {health.failed.includes("approvals") ? "Approvals could not be loaded." : "No plans are waiting for review."}
                  </p>
                )}
              </Panel>
            </div>

            <div className="min-w-0 space-y-6">
              <Panel
                title="Connection health"
                description="Clusters, agents, and Git sources used by this workspace."
                action={<TrailingLink href={connectionsHref}>Manage</TrailingLink>}
              >
                {!health.loaded ? <p className="px-5 py-8 text-center text-sm text-muted-foreground">Checking connections…</p> : <ConnectionList rows={rows} href={connectionsHref} />}
              </Panel>

              <Panel title="Delivery status" description={`${total} application${total === 1 ? "" : "s"}`}>
                <div className="p-5">
                  <div
                    role="img"
                    aria-label={`${synced} in sync, ${attention.length} need attention, ${other} in other states`}
                    className="flex h-2.5 overflow-hidden rounded-full bg-muted"
                  >
                    {total > 0 && [
                      { n: synced, c: "bg-success" },
                      { n: attention.length, c: "bg-warning" },
                      { n: other, c: "bg-muted-foreground/40" },
                    ].map((segment, index) => segment.n > 0 && <span key={index} className={segment.c} style={{ width: `${(segment.n / total) * 100}%` }} />)}
                  </div>
                  <ul className="mt-4 grid grid-cols-3 gap-2">
                    {[
                      { label: "In sync", n: synced, dot: "bg-success", href: listHref("synced") },
                      { label: "Attention", n: attention.length, dot: "bg-warning", href: listHref("attention") },
                      { label: "Other", n: other, dot: "bg-muted-foreground/40", href: listHref("other") },
                    ].map((item) => (
                      <li key={item.label}>
                        <Link href={item.href} className="block rounded-lg border px-3 py-2 transition-colors hover:bg-muted/40">
                          <span className="flex items-center gap-1.5 text-xs text-muted-foreground"><span aria-hidden="true" className={`size-2 rounded-full ${item.dot}`} />{item.label}</span>
                          <span className="mt-1 block text-xl font-semibold tabular-nums">{item.n}</span>
                        </Link>
                      </li>
                    ))}
                  </ul>
                </div>
              </Panel>

              <Panel title="Latest checks" description="Most recently checked applications">
                {recent.length ? (
                  <ul className="divide-y">
                    {recent.map((app) => {
                      const state: HealthLevel = isApplicationHealthy(app) ? "healthy" : needsAttention(app) ? "warning" : "unknown"
                      return (
                        <li key={app.id}>
                          <Link href={`/applications/${app.id}`} className="flex items-center gap-3 px-5 py-3 transition-colors hover:bg-muted/40">
                            <LevelIcon level={state} />
                            <span className="min-w-0 flex-1">
                              <span className="block truncate text-sm font-medium">{app.name}</span>
                              <span className="block truncate font-mono text-xs text-muted-foreground">{app.lastSyncedRevision || app.revision}</span>
                            </span>
                            <span className={`flex shrink-0 items-center gap-1 text-xs ${isCheckStalled(app, now) ? "text-warning-foreground dark:text-warning" : "text-muted-foreground"}`}>
                              <HugeiconsIcon icon={Clock01Icon} strokeWidth={1.8} className="size-3.5" aria-hidden="true" />
                              {ago(app.lastCheckedAt, now)}
                            </span>
                          </Link>
                        </li>
                      )
                    })}
                  </ul>
                ) : (
                  <p className="px-5 py-8 text-center text-sm text-muted-foreground">Checks appear after you add an application.</p>
                )}
              </Panel>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
