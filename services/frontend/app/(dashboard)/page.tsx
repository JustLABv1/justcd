"use client"

import Link from "next/link"
import { PageHeading, EmptyState, StatusBadge } from "@/components/ui-kit"
import {
  ActionLink,
  CollectionSkeleton,
  LoadError,
  WorkspaceIcon,
} from "@/components/workspace-ui"
import { useWorkspace, needsAttention, isApplicationHealthy } from "@/hooks/use-workspace"
import { applicationAttentionReason } from "@/lib/application-status"
import { useWorkspaceSelection } from "@/hooks/workspace-selection"

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

  return (
    <>
      <PageHeading
        title={workspace ? `${workspace.name} overview` : "Overview"}
        description={workspace ? "A clear view of what’s running and what needs you next in this workspace." : "Select a workspace to view its delivery status."}
        actions={
          <>
            <ActionLink href="/workspaces/new" secondary>
              New workspace
            </ActionLink>
            <ActionLink href={workspaceId ? `/applications/new?workspaceId=${encodeURIComponent(workspaceId)}` : "/workspaces"}>
              <span aria-hidden="true">＋</span> New application
            </ActionLink>
          </>
        }
      />
      {error ? (
        <LoadError error={error} retry={refresh} />
      ) : loading ? (
        <CollectionSkeleton />
      ) : (
        <>
          <section
            aria-label="Delivery overview"
            className="mb-8 rounded-xl border bg-card"
          >
            <div className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-4">
              <h2 className="text-sm font-semibold">Deployment status</h2>
              <Link
                href="/applications"
                className="text-xs font-medium text-primary hover:underline"
              >
                View applications →
              </Link>
            </div>
            <div className="grid grid-cols-2 divide-x divide-border sm:grid-cols-4">
              {[
                {
                  label: "Applications",
                  value: applications.length,
                  color: "bg-primary",
                  href: workspaceId ? `/applications?workspaceId=${encodeURIComponent(workspaceId)}` : "/workspaces",
                },
                {
                  label: "In sync",
                  value: synced,
                  color: "bg-emerald-500",
                  href: workspaceId ? `/applications?workspaceId=${encodeURIComponent(workspaceId)}&status=synced` : "/workspaces",
                },
                {
                  label: "Needs attention",
                  value: attention.length,
                  color: "bg-amber-500",
                  href: workspaceId ? `/applications?workspaceId=${encodeURIComponent(workspaceId)}&status=attention` : "/workspaces",
                },
                {
                  label: "Other states",
                  value: other,
                  color: "bg-muted-foreground",
                  href: workspaceId ? `/applications?workspaceId=${encodeURIComponent(workspaceId)}&status=other` : "/workspaces",
                },
              ].map((item) => (
                <Link
                  key={item.label}
                  href={item.href}
                  className="p-5 transition-colors hover:bg-muted/40 sm:p-6"
                >
                  <span className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span className={`size-1.5 rounded-full ${item.color}`} />
                    {item.label}
                  </span>
                  <span className="mt-3 block text-3xl font-semibold tracking-tight tabular-nums">
                    {item.value}
                  </span>
                </Link>
              ))}
            </div>
            <div className="flex flex-wrap items-center justify-between gap-2 border-t px-6 py-3 text-xs text-muted-foreground">
              <span>
                {workspace?.name ?? "No workspace selected"} ·{" "}
                {applications.length
                  ? `${Math.round((synced / applications.length) * 100)}% of applications healthy`
                  : "No applications connected yet"}
              </span>
              {!applications.length && (
                <Link
                  href={workspaceId ? `/applications/new?workspaceId=${encodeURIComponent(workspaceId)}` : "/workspaces/new"}
                  className="font-medium text-primary hover:underline"
                >
                  {workspaceId ? "Create an application" : "Create your first workspace"}{" "}
                  →
                </Link>
              )}
            </div>
          </section>
          <div className="grid gap-8 xl:grid-cols-[minmax(0,1fr)_320px]">
            <section className="min-w-0">
              <div className="mb-4 flex items-center justify-between">
                <h2 className="text-base font-semibold tracking-tight">
                  Needs attention{" "}
                  <span className="ml-2 rounded-md bg-amber-500/10 px-2 py-1 text-xs text-amber-700 dark:text-amber-300">
                    {attention.length}
                  </span>
                </h2>
                <Link
                  href={workspaceId ? `/applications?status=attention&workspaceId=${encodeURIComponent(workspaceId)}` : "/workspaces"}
                  className="text-xs text-muted-foreground hover:text-foreground"
                >
                  View all →
                </Link>
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
                        <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-amber-500/10 text-amber-700 dark:text-amber-300">
                          <WorkspaceIcon name="app" className="size-4" />
                        </span>
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm font-medium">
                            {app.name}
                          </span>
                          <span className="mt-1 block truncate text-xs text-muted-foreground">
                            {app.workspaceName} · {app.revision}
                          </span>
                          <span className="mt-1 block text-xs text-amber-700 dark:text-amber-300">{applicationAttentionReason(app)}</span>
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
              <div className="mt-8 rounded-xl border bg-card p-5">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div><h2 className="text-sm font-semibold">Workspace connections</h2><p className="mt-1 text-sm text-muted-foreground">Manage repositories, deployment clusters, and credentials for {workspace?.name ?? "this workspace"}.</p></div>
                  {workspaceId && <Link href={`/workspaces/${workspaceId}?tab=connections`} className="text-xs font-medium text-primary hover:underline">Manage connections →</Link>}
                </div>
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
                <div className="mt-5 space-y-5">
                  {recent.length ? (
                    recent.map((app) => (
                      <Link
                        key={app.id}
                        href={`/applications/${app.id}`}
                        className="group flex gap-3"
                      >
                        <span
                          className={`mt-1.5 size-2 shrink-0 rounded-full ${isApplicationHealthy(app) ? "bg-emerald-500" : needsAttention(app) ? "bg-amber-500" : "bg-muted-foreground/40"}`}
                        />
                        <span className="min-w-0">
                          <span className="block truncate text-xs font-medium group-hover:text-primary">
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
                    ))
                  ) : (
                    <p className="text-sm leading-5 text-muted-foreground">
                      Checks will appear after you add an application.
                    </p>
                  )}
                </div>
              </section>
            </aside>
          </div>
        </>
      )}
    </>
  )
}
