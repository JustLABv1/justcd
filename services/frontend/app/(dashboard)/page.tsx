"use client"

import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import { Add01Icon, Alert02Icon, ArrowRight01Icon, MinusSignCircleIcon, Tick02Icon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { PageHeading, EmptyState, Panel, StatusBadge } from "@/components/ui-kit"
import {
  ActionLink,
  CollectionSkeleton,
  LoadError,
  WorkspaceIcon,
} from "@/components/workspace-ui"
import { useWorkspace, needsAttention, isApplicationHealthy } from "@/hooks/use-workspace"
import { applicationAttentionReason } from "@/lib/application-status"
import { useWorkspaceSelection } from "@/hooks/workspace-selection"

function TrailingLink({ href, children, label }: { href: string; children: React.ReactNode; label?: string }) {
  return (
    <Button render={<Link href={href} aria-label={label} />} nativeButton={false} variant="link" size="sm" className="px-0">
      {children}
      <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={1.8} aria-hidden="true" />
    </Button>
  )
}

export default function OverviewPage() {
  const { applications, loading, error, refresh } = useWorkspace()
  const { workspace, workspaceId } = useWorkspaceSelection()
  const attention = applications.filter(needsAttention)
  const synced = applications.filter(isApplicationHealthy).length
  const other = applications.filter((app) => !needsAttention(app) && !isApplicationHealthy(app)).length
  const recent = [...applications]
    .sort(
      (a, b) =>
        (Date.parse(b.lastCheckedAt || b.createdAt) || 0) -
        (Date.parse(a.lastCheckedAt || a.createdAt) || 0)
    )
    .slice(0, 4)
  const total = applications.length
  const listHref = (status?: string) =>
    workspaceId ? `/applications?workspaceId=${encodeURIComponent(workspaceId)}${status ? `&status=${status}` : ""}` : "/workspaces"
  const segments = [
    { label: "In sync", value: synced, bar: "bg-success", href: listHref("synced") },
    { label: "Needs attention", value: attention.length, bar: "bg-warning", href: listHref("attention") },
    { label: "Other states", value: other, bar: "bg-muted-foreground/40", href: listHref("other") },
  ]

  return (
    <>
      <PageHeading
        title={workspace ? `${workspace.name} overview` : "Overview"}
        description={workspace ? "A clear view of what’s running and what needs you next in this workspace." : "Select a workspace to view its delivery status."}
        actions={
          <ActionLink href={workspaceId ? `/applications/new?workspaceId=${encodeURIComponent(workspaceId)}` : "/workspaces"}>
            <HugeiconsIcon icon={Add01Icon} strokeWidth={1.8} aria-hidden="true" /> New application
          </ActionLink>
        }
      />
      {error ? (
        <LoadError error={error} retry={refresh} />
      ) : loading ? (
        <CollectionSkeleton />
      ) : (
        <>
          <Panel
            title="Deployment status"
            action={<TrailingLink href="/applications">View applications</TrailingLink>}
            className="mb-8"
          >
            <div className="p-5 sm:p-6">
              <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-2">
                <div>
                  <p className="text-sm font-medium text-muted-foreground">Applications</p>
                  <p className="mt-1 text-3xl font-semibold tracking-tight tabular-nums">{total}</p>
                </div>
                <p className="text-sm text-muted-foreground">
                  {workspace?.name ?? "No workspace selected"} ·{" "}
                  {total ? `${Math.round((synced / total) * 100)}% of applications healthy` : "No applications connected yet"}
                </p>
              </div>
              <div
                role="img"
                aria-label={`${synced} in sync, ${attention.length} need attention, ${other} in other states`}
                className="mt-5 flex h-3 overflow-hidden rounded-full bg-muted"
              >
                {total > 0 && segments.map((segment) => segment.value > 0 && (
                  <span key={segment.label} className={segment.bar} style={{ width: `${(segment.value / total) * 100}%` }} />
                ))}
              </div>
              <ul className="mt-4 grid gap-2 sm:grid-cols-3">
                {segments.map((segment) => (
                  <li key={segment.label}>
                    <Link href={segment.href} className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5 text-sm transition-colors hover:bg-muted/40">
                      <span className="flex items-center gap-2 text-muted-foreground">
                        <span aria-hidden="true" className={`size-2 rounded-full ${segment.bar}`} />
                        {segment.label}
                      </span>
                      <span className="text-lg font-semibold tabular-nums">{segment.value}</span>
                    </Link>
                  </li>
                ))}
              </ul>
              {!total && (
                <div className="mt-4">
                  <TrailingLink href={workspaceId ? `/applications/new?workspaceId=${encodeURIComponent(workspaceId)}` : "/workspaces/new"}>
                    {workspaceId ? "Create an application" : "Create your first workspace"}
                  </TrailingLink>
                </div>
              )}
            </div>
          </Panel>
          <div className="grid gap-8 xl:grid-cols-[minmax(0,1fr)_320px]">
            <section className="min-w-0" aria-labelledby="needs-attention-heading">
              <div className="mb-4 flex items-center justify-between gap-3">
                <h2 id="needs-attention-heading" className="flex items-center gap-2 text-sm font-semibold">
                  Needs attention
                  <Badge variant="warning-light" radius="full">{attention.length}</Badge>
                </h2>
                <TrailingLink href={workspaceId ? `/applications?status=attention&workspaceId=${encodeURIComponent(workspaceId)}` : "/workspaces"}>View all</TrailingLink>
              </div>
              <div className="overflow-hidden rounded-xl border bg-card">
                {attention.length ? (
                  <div className="divide-y">
                    {attention.slice(0, 5).map((app) => (
                      <Link
                        key={app.id}
                        href={`/applications/${app.id}`}
                        className="flex flex-wrap items-center gap-3 p-4 transition-colors hover:bg-muted/40 sm:p-5"
                      >
                        <span aria-hidden="true" className="grid size-9 shrink-0 place-items-center rounded-lg bg-warning/10 text-warning-foreground dark:text-warning">
                          <WorkspaceIcon name="app" className="size-4" />
                        </span>
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm font-medium">
                            {app.name}
                          </span>
                          <span className="mt-1 block truncate text-xs text-muted-foreground">
                            {app.workspaceName} · {app.revision}
                          </span>
                          <span className="mt-1 block text-sm text-warning-foreground dark:text-warning">{applicationAttentionReason(app)}</span>
                        </span>
                        <StatusBadge status={app.health} />{app.autoSyncPaused && <StatusBadge status="Reconciliation paused" />}
                        <WorkspaceIcon
                          name="arrow"
                          className="size-4 text-muted-foreground"
                        />
                      </Link>
                    ))}
                  </div>
                ) : (
                  <EmptyState
                    title={
                      applications.length
                        ? "Nothing needs attention"
                        : "A fresh start"
                    }
                    description={
                      applications.length
                        ? "No drift, degradation, or pending deletions reported. Other sync states are available in Applications."
                        : "Your applications will appear here when there’s something to review."
                    }
                  />
                )}
              </div>
            </section>
            <aside className="space-y-6">
              <section className="rounded-xl border bg-card p-5">
                <div className="flex items-center justify-between">
                  <h2 className="text-sm font-semibold">Latest checks</h2>
                  <WorkspaceIcon
                    name="branch"
                    className="size-4 text-muted-foreground"
                  />
                </div>
                <p className="mt-1 text-sm text-muted-foreground">
                  Most recently checked applications
                </p>
                <ul className="mt-5 space-y-5">
                  {recent.length ? (
                    recent.map((app) => {
                      const state = isApplicationHealthy(app)
                        ? { label: "In sync", icon: Tick02Icon, className: "text-success-foreground dark:text-success" }
                        : needsAttention(app)
                          ? { label: "Needs attention", icon: Alert02Icon, className: "text-warning-foreground dark:text-warning" }
                          : { label: "Other state", icon: MinusSignCircleIcon, className: "text-muted-foreground" }
                      return (
                        <li key={app.id}>
                          <Link href={`/applications/${app.id}`} className="group flex gap-3">
                            <span className={`mt-0.5 shrink-0 ${state.className}`}>
                              <HugeiconsIcon icon={state.icon} strokeWidth={2} className="size-4" aria-hidden="true" />
                              <span className="sr-only">{state.label}: </span>
                            </span>
                            <span className="min-w-0">
                              <span className="block truncate text-sm font-medium group-hover:text-primary">
                                {app.name}
                              </span>
                              <span className="mt-1 block text-xs leading-5 text-muted-foreground">
                                {app.lastCheckedAt
                                  ? new Date(app.lastCheckedAt).toLocaleString()
                                  : "Not checked yet"}
                              </span>
                              <span className="mt-1 block truncate font-mono text-xs text-muted-foreground">
                                {app.lastSyncedRevision || app.revision}
                              </span>
                            </span>
                          </Link>
                        </li>
                      )
                    })
                  ) : (
                    <li className="text-sm leading-5 text-muted-foreground">
                      Checks will appear after you add an application.
                    </li>
                  )}
                </ul>
              </section>
              {workspaceId && (
                <section aria-labelledby="quick-actions-heading" className="rounded-xl border bg-card px-5 py-4">
                  <h2 id="quick-actions-heading" className="text-sm font-semibold">Quick actions</h2>
                  <p className="mt-1 text-sm text-muted-foreground">Repositories, clusters, and credentials for {workspace?.name ?? "this workspace"}.</p>
                  <div className="mt-2"><TrailingLink href={`/workspaces/${workspaceId}?tab=connections`}>Manage connections</TrailingLink></div>
                </section>
              )}
            </aside>
          </div>
        </>
      )}
    </>
  )
}
