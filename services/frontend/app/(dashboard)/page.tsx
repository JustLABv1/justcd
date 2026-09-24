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
            className="mb-8 overflow-hidden rounded-2xl border bg-card"
            aria-label="Delivery overview"
          >
            <div className="grid lg:grid-cols-[1.15fr_1fr]">
              <div className="relative overflow-hidden bg-[#142b2a] p-6 text-white sm:p-8">
                <div
                  aria-hidden="true"
                  className="pointer-events-none absolute -top-16 -right-16 size-64 rounded-full border border-white/5 before:absolute before:inset-8 before:rounded-full before:border before:border-white/5 after:absolute after:inset-16 after:rounded-full after:border after:border-white/5"
                />
                <p className="relative text-[10px] font-medium tracking-[0.18em] text-emerald-200/80 uppercase">
                  Delivery pulse
                </p>
                <h2 className="relative mt-5 max-w-sm text-2xl leading-tight font-medium tracking-tight sm:text-3xl">
                  {!applications.length
                    ? "Ready when you are."
                    : attention.length
                      ? "A few things need your attention."
                      : synced === applications.length
                        ? "Everything is in sync."
                        : "Your workspace is taking shape."}
                </h2>
                <p className="relative mt-3 max-w-md text-sm leading-6 text-slate-300">
                  {!applications.length
                    ? "Bring your Git repositories and Kubernetes applications together in one workspace."
                    : attention.length
                      ? `${attention.length} application${attention.length === 1 ? " has" : "s have"} changes or health issues to review.`
                      : `${synced} of ${applications.length} applications in sync. ${other ? `${other} awaiting a confirmed sync state.` : "You’re up to date with your desired state."}`}
                </p>
                <Link
                  href={
                    applications.length
                      ? `/applications${attention.length ? "?status=attention" : ""}`
                      : projects.length
                        ? "/applications/new"
                        : "/projects/new"
                  }
                  className="relative mt-6 inline-flex items-center gap-2 rounded-lg border border-white/20 bg-white/10 px-3 py-2 text-xs font-medium hover:bg-white/15"
                >
                  {applications.length
                    ? attention.length
                      ? "Review applications"
                      : "Explore applications"
                    : "Set up your workspace"}
                  <WorkspaceIcon name="arrow" className="size-4" />
                </Link>
              </div>
              <div className="flex flex-col justify-center p-6 sm:p-8">
                <div className="flex items-end justify-between">
                  <div>
                    <p className="text-xs text-muted-foreground">
                      Applications in sync
                    </p>
                    <p className="mt-2 text-5xl font-medium tracking-tight tabular-nums">
                      {synced}
                      <span className="ml-2 text-xl font-normal text-muted-foreground">
                        / {applications.length}
                      </span>
                    </p>
                  </div>
                  <span className="pb-1 text-sm text-muted-foreground tabular-nums">
                    {applications.length
                      ? `${Math.round((synced / applications.length) * 100)}%`
                      : "—"}
                  </span>
                </div>
                <div
                  className="mt-6 flex h-2 overflow-hidden rounded-full bg-muted"
                  aria-hidden="true"
                >
                  {applications.length > 0 && (
                    <>
                      <span
                        className="bg-emerald-500"
                        style={{
                          width: `${(synced / applications.length) * 100}%`,
                        }}
                      />
                      <span
                        className="bg-amber-400"
                        style={{
                          width: `${(attention.length / applications.length) * 100}%`,
                        }}
                      />
                    </>
                  )}
                </div>
                <div className="mt-4 flex flex-wrap gap-x-5 gap-y-2 text-xs text-muted-foreground">
                  <span className="flex items-center gap-2">
                    <span className="size-1.5 rounded-full bg-emerald-500" />
                    {synced} in sync
                  </span>
                  <span className="flex items-center gap-2">
                    <span className="size-1.5 rounded-full bg-amber-400" />
                    {attention.length} need attention
                  </span>
                  <span className="flex items-center gap-2">
                    <span className="size-1.5 rounded-full bg-muted-foreground/40" />
                    {other} other
                  </span>
                </div>
                <div className="mt-6 flex items-center justify-between border-t pt-4 text-xs">
                  <span className="text-muted-foreground">
                    Across {projects.length} project
                    {projects.length === 1 ? "" : "s"}
                  </span>
                  <Link
                    href="/applications"
                    className="font-medium hover:text-primary"
                  >
                    View all applications →
                  </Link>
                </div>
              </div>
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
