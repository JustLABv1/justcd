"use client"

import Link from "next/link"
import { useState } from "react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { DataGridList } from "@/components/data-grid-table"
import { EmptyState, StatusBadge } from "@/components/ui-kit"
import { WorkspaceIcon } from "@/components/workspace-ui"
import {
  needsAttention,
  type WorkspaceApplication,
} from "@/hooks/use-workspace"
import type { Project } from "@/lib/types"

export function ApplicationCard({ app }: { app: WorkspaceApplication }) {
  return (
    <Link
      href={`/applications/${app.id}`}
      className="workspace-card group flex min-w-0 flex-col overflow-hidden rounded-2xl border bg-card"
    >
      <div
        className={`h-0.5 ${app.health === "synced" ? "bg-emerald-500/70" : needsAttention(app) ? "bg-amber-500/80" : "bg-border"}`}
      />
      <div className="p-5">
        <div className="flex items-start justify-between gap-3">
          <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-primary/7 text-primary">
            <WorkspaceIcon name="app" />
          </span>
          <StatusBadge status={app.health} />
        </div>
        <h3
          className="mt-4 truncate text-base font-semibold tracking-tight group-hover:text-primary"
          title={app.name}
        >
          {app.name}
        </h3>
        <p className="mt-1 truncate text-xs text-muted-foreground">
          {app.projectName || app.renderer} <span aria-hidden="true">/</span>{" "}
          {app.manifestPath || "."}
        </p>
        <dl className="mt-5 grid grid-cols-[76px_minmax(0,1fr)] gap-x-3 gap-y-2.5 text-xs">
          <dt className="text-muted-foreground">Revision</dt>
          <dd className="flex min-w-0 items-center gap-1.5 font-mono">
            <WorkspaceIcon
              name="branch"
              className="size-3.5 shrink-0 text-muted-foreground"
            />
            <span className="truncate" title={app.revision}>
              {app.revision}
            </span>
          </dd>
          <dt className="text-muted-foreground">Target</dt>
          <dd
            className="truncate"
            title={app.namespaces.map((item) => item.namespace).join(", ")}
          >
            {app.namespaces.map((item) => item.namespace).join(", ") ||
              "No namespace"}
          </dd>
        </dl>
      </div>
      <div className="mt-auto flex items-center justify-between gap-2 border-t bg-muted/20 px-5 py-3 text-[11px] text-muted-foreground">
        <span className="flex min-w-0 items-center gap-2">
          <span className="rounded border bg-card px-1.5 py-0.5 font-mono">
            {app.renderer}
          </span>
          <span>
            {app.syncPolicy === "auto-safe" ? "Auto-safe sync" : "Manual sync"}
          </span>
        </span>
        <WorkspaceIcon
          name="arrow"
          className="size-4 shrink-0 group-hover:text-primary"
        />
      </div>
    </Link>
  )
}

export function ApplicationCollection({
  applications,
  projects,
  initialFilter = "all",
  createHref = "/applications/new",
  canCreate = true,
}: {
  applications: WorkspaceApplication[]
  projects?: Project[]
  initialFilter?: string
  createHref?: string
  canCreate?: boolean
}) {
  const [query, setQuery] = useState("")
  const [filter, setFilter] = useState(initialFilter)
  const [projectId, setProjectId] = useState("all")
  const [view, setView] = useState<"cards" | "table">("cards")
  const [sort, setSort] = useState("attention")
  const counts = {
    all: applications.length,
    attention: applications.filter(needsAttention).length,
    synced: applications.filter((app) => app.health === "synced").length,
    other: applications.filter(
      (app) => !needsAttention(app) && app.health !== "synced"
    ).length,
  }
  const visible = applications
    .filter((app) => {
      const matchesStatus =
        filter === "all" ||
        (filter === "attention"
          ? needsAttention(app)
          : filter === "synced"
            ? app.health === "synced"
            : !needsAttention(app) && app.health !== "synced")
      return (
        matchesStatus &&
        (projectId === "all" || app.projectId === projectId) &&
        `${app.name} ${app.projectName ?? ""} ${app.namespaces.map((item) => item.namespace).join(" ")} ${app.revision}`
          .toLowerCase()
          .includes(query.trim().toLowerCase())
      )
    })
    .sort((a, b) =>
      sort === "name"
        ? a.name.localeCompare(b.name)
        : sort === "newest"
          ? new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
          : Number(needsAttention(b)) - Number(needsAttention(a)) ||
            a.name.localeCompare(b.name)
    )

  if (!applications.length)
    return (
      <div className="rounded-2xl border border-dashed bg-card">
        <EmptyState
          title="Your next deployment starts here"
          description="Connect a Git repository and a Kubernetes target to bring your first application online."
          href={canCreate ? createHref : undefined}
          action="Create application"
        />
      </div>
    )
  return (
    <div>
      <div className="mb-5 flex flex-col justify-between gap-4 border-b pb-4 min-[1100px]:flex-row min-[1100px]:items-center">
        <div
          className="flex flex-wrap gap-1"
          role="group"
          aria-label="Filter by health"
        >
          {(
            [
              ["all", "All apps"],
              ["attention", "Needs attention"],
              ["synced", "In sync"],
              ["other", "Other"],
            ] as const
          ).map(([value, label]) => (
            <button
              key={value}
              type="button"
              aria-pressed={filter === value}
              onClick={() => setFilter(value)}
              className={`flex min-h-9 items-center gap-2 rounded-lg px-3 text-xs font-medium transition-colors ${filter === value ? "bg-foreground text-background" : "text-muted-foreground hover:bg-muted"}`}
            >
              {label}
              <span
                className={`rounded px-1.5 py-0.5 text-[10px] tabular-nums ${filter === value ? "bg-background/15" : "bg-muted"}`}
              >
                {counts[value]}
              </span>
            </button>
          ))}
        </div>
        <div
          className="flex w-fit gap-1 rounded-lg border bg-card p-1"
          role="group"
          aria-label="Application view"
        >
          {(["cards", "table"] as const).map((value) => (
            <button
              type="button"
              key={value}
              aria-pressed={view === value}
              onClick={() => setView(value)}
              className={`rounded-md px-3 py-1.5 text-xs capitalize ${view === value ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground"}`}
            >
              {value}
            </button>
          ))}
        </div>
      </div>
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <div className="relative min-w-48 flex-1">
          <WorkspaceIcon
            name="search"
            className="pointer-events-none absolute top-2.5 left-3 size-4 text-muted-foreground"
          />
          <Input
            aria-label="Search applications"
            placeholder="Search applications, namespaces, revisions…"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            className="h-9 bg-card pl-9"
          />
        </div>
        {projects && (
          <FormSelect
            ariaLabel="Filter by project"
            value={projectId}
            onValueChange={setProjectId}
            className="w-44"
            items={[
              { value: "all", label: "All projects" },
              ...projects.map((project) => ({
                value: project.id,
                label: project.name,
              })),
            ]}
          />
        )}
        <FormSelect
          ariaLabel="Sort applications"
          value={sort}
          onValueChange={setSort}
          className="w-40"
          items={[
            { value: "attention", label: "Attention first" },
            { value: "name", label: "Name A–Z" },
            { value: "newest", label: "Newest first" },
          ]}
        />
      </div>
      <p role="status" className="mb-3 text-xs text-muted-foreground">
        {visible.length} of {applications.length} applications
      </p>
      {visible.length ? (
        view === "cards" ? (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,280px),1fr))] gap-4">
            {visible.map((app) => (
              <ApplicationCard key={app.id} app={app} />
            ))}
          </div>
        ) : (
          <DataGridList
            rows={visible}
            columns={[
              {
                id: "name",
                title: "Application",
                size: 230,
                cell: (app) => (
                  <Link
                    href={`/applications/${app.id}`}
                    className="block truncate font-medium hover:text-primary"
                  >
                    {app.name}
                    <span className="block truncate text-xs font-normal text-muted-foreground">
                      {app.projectName || app.manifestPath}
                    </span>
                  </Link>
                ),
              },
              {
                id: "health",
                title: "Health",
                size: 150,
                cell: (app) => <StatusBadge status={app.health} />,
              },
              {
                id: "target",
                title: "Namespace",
                size: 160,
                cell: (app) => (
                  <span className="block truncate text-xs">
                    {app.namespaces.map((item) => item.namespace).join(", ") ||
                      "—"}
                  </span>
                ),
              },
              {
                id: "revision",
                title: "Revision",
                size: 130,
                cell: (app) => (
                  <span className="block truncate font-mono text-xs">
                    {app.revision}
                  </span>
                ),
              },
              {
                id: "policy",
                title: "Sync policy",
                size: 120,
                cell: (app) => (
                  <span className="text-xs">{app.syncPolicy}</span>
                ),
              },
            ]}
          />
        )
      ) : (
        <div className="rounded-2xl border border-dashed bg-card pb-6 text-center">
          <EmptyState
            title="No matching applications"
            description="Try another search or clear the filters to see all applications."
          />
          <Button
            variant="outline"
            onClick={() => {
              setQuery("")
              setFilter("all")
              setProjectId("all")
            }}
          >
            Clear filters
          </Button>
        </div>
      )}
    </div>
  )
}
