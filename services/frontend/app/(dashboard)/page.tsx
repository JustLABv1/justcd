"use client"

import Link from "next/link"
import { PageHeading, EmptyState, StatusBadge } from "@/components/ui-kit"
import {
  ActionLink,
  CollectionSkeleton,
  LoadError,
  ProjectCard,
  WorkspaceIcon,
} from "@/components/workspace-ui"
import { useWorkspace, needsAttention } from "@/hooks/use-workspace"

export default function OverviewPage() {
  const { projects, applications, loading, error, refresh } = useWorkspace()
  const attention = applications.filter(needsAttention)
  const synced = applications.filter((app) => app.health === "synced").length
  const other = applications.length - synced - attention.length
  const recent = [...applications]
    .sort(
      (a, b) =>
        (Date.parse(b.lastCheckedAt || b.createdAt) || 0) -
        (Date.parse(a.lastCheckedAt || a.createdAt) || 0)
    )
    .slice(0, 4)

  return (
    <>
      <p className="mb-2 text-[10px] font-semibold tracking-[0.18em] text-muted-foreground uppercase">
        Your delivery workspace
      </p>
      <PageHeading
        title="Overview"
        description="A clear view of what’s running and what needs you next."
        actions={
          <>
            <ActionLink href="/projects/new" secondary>
              New project
            </ActionLink>
            <ActionLink href="/applications/new">
              <span aria-hidden="true">＋</span> New application
            </ActionLink>
          </>
        }
      />
      {error ? (
        <LoadError message={error} retry={refresh} />
      ) : loading ? (
        <CollectionSkeleton />
      ) : (
        <>
          <section
            aria-label="Delivery overview"
            className="mb-8 rounded-2xl border bg-card"
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
                  href: "/applications",
                },
                {
                  label: "In sync",
                  value: synced,
                  color: "bg-emerald-500",
                  href: "/applications?status=synced",
                },
                {
                  label: "Needs attention",
                  value: attention.length,
                  color: "bg-amber-500",
                  href: "/applications?status=attention",
                },
                {
                  label: "Other states",
                  value: other,
                  color: "bg-muted-foreground",
                  href: "/applications?status=other",
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
                {projects.length} project{projects.length === 1 ? "" : "s"} ·{" "}
                {applications.length
                  ? `${Math.round((synced / applications.length) * 100)}% of applications in sync`
                  : "No applications connected yet"}
              </span>
              {!applications.length && (
                <Link
                  href={projects.length ? "/applications/new" : "/projects/new"}
                  className="font-medium text-primary hover:underline"
                >
                  {projects.length
                    ? "Create an application"
                    : "Create your first project"}{" "}
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
                  href="/applications?status=attention"
                  className="text-xs text-muted-foreground hover:text-foreground"
                >
                  View all →
                </Link>
              </div>
              <div className="overflow-hidden rounded-2xl border bg-card">
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
                            {app.projectName} · {app.revision}
                          </span>
                        </span>
                        <StatusBadge status={app.health} />
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
              <div className="mt-8 mb-4 flex items-center justify-between">
                <h2 className="text-base font-semibold tracking-tight">
                  Projects{" "}
                  <span className="ml-1 text-sm font-normal text-muted-foreground">
                    {projects.length}
                  </span>
                </h2>
                <Link
                  href="/projects"
                  className="text-xs text-muted-foreground hover:text-foreground"
                >
                  View all →
                </Link>
              </div>
              {projects.length ? (
                <div className="grid gap-4 md:grid-cols-2">
                  {projects.slice(0, 4).map((project) => {
                    const apps = applications.filter(
                      (app) => app.projectId === project.id
                    )
                    return (
                      <ProjectCard
                        key={project.id}
                        project={project}
                        total={apps.length}
                        synced={
                          apps.filter((app) => app.health === "synced").length
                        }
                        attention={apps.filter(needsAttention).length}
                      />
                    )
                  })}
                </div>
              ) : (
                <div className="rounded-2xl border border-dashed bg-card">
                  <EmptyState
                    title="Create your first project"
                    description="Give your applications a home, connect repositories, and invite your team."
                    href="/projects/new"
                    action="Create project"
                  />
                </div>
              )}
            </section>
            <aside className="space-y-6">
              <section className="rounded-2xl border bg-card p-5">
                <div className="flex items-center justify-between">
                  <h2 className="text-sm font-semibold">Latest checks</h2>
                  <WorkspaceIcon
                    name="branch"
                    className="size-4 text-muted-foreground"
                  />
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
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
                          className={`mt-1.5 size-2 shrink-0 rounded-full ${app.health === "synced" ? "bg-emerald-500" : needsAttention(app) ? "bg-amber-500" : "bg-muted-foreground/40"}`}
                        />
                        <span className="min-w-0">
                          <span className="block truncate text-xs font-medium group-hover:text-primary">
                            {app.name}
                          </span>
                          <span className="mt-1 block text-[11px] leading-5 text-muted-foreground">
                            {app.lastCheckedAt
                              ? new Date(app.lastCheckedAt).toLocaleString()
                              : "Not checked yet"}
                          </span>
                          <span className="mt-1 block truncate font-mono text-[10px] text-muted-foreground">
                            {app.lastSyncedRevision || app.revision}
                          </span>
                        </span>
                      </Link>
                    ))
                  ) : (
                    <p className="text-xs leading-5 text-muted-foreground">
                      Checks will appear after you add an application.
                    </p>
                  )}
                </div>
              </section>
              <section className="px-1">
                <p className="text-[10px] font-semibold tracking-widest text-muted-foreground uppercase">
                  Workspace essentials
                </p>
                <div className="mt-3 space-y-1">
                  {[
                    {
                      href: "/settings/git-sources",
                      title: "Connect a repository",
                      description: "Bring your manifests into JustCD",
                      icon: "branch" as const,
                    },
                    {
                      href: "/settings/clusters",
                      title: "Manage cluster targets",
                      description: "Choose where applications run",
                      icon: "server" as const,
                    },
                  ].map((item) => (
                    <Link
                      key={item.href}
                      href={item.href}
                      className="flex gap-3 rounded-lg py-3 hover:bg-muted/50"
                    >
                      <WorkspaceIcon
                        name={item.icon}
                        className="mt-0.5 size-4 shrink-0 text-muted-foreground"
                      />
                      <span>
                        <span className="block text-xs font-medium">
                          {item.title}
                        </span>
                        <span className="mt-1 block text-[11px] text-muted-foreground">
                          {item.description}
                        </span>
                      </span>
                    </Link>
                  ))}
                </div>
              </section>
            </aside>
          </div>
        </>
      )}
    </>
  )
}
