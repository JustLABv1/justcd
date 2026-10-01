"use client"

import Link from "next/link"
import { useRouter } from "next/navigation"
import { useState } from "react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Filters } from "@/components/reui/filters/filters"
import { createFilterQuery, flattenFilterConditions } from "@/components/reui/filters/filters-query"
import type { FilterField, FilterQuery } from "@/components/reui/filters/filters-types"
import { DataGridList } from "@/components/data-grid-table"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { EmptyState, StatusBadge } from "@/components/ui-kit"
import { WorkspaceIcon } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import {
  isApplicationHealthy,
  needsAttention,
  type WorkspaceApplication,
} from "@/hooks/use-workspace"
import type { Workspace } from "@/lib/types"
import { api } from "@/lib/api"

function ApplicationActions({ app, canManage, onDeleted }: { app: WorkspaceApplication; canManage: boolean; onDeleted?: (id: string) => void }) {
  const router = useRouter()
  const toast = useToast()
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [policy, setPolicy] = useState("keep")
  async function remove() {
    const result = await api<{ deleted: boolean }>(`/api/v1/applications/${encodeURIComponent(app.id)}?resources=${policy}`, { method: "DELETE" })
    if (result.deleted) {
      toast.success(`${app.name} deleted.`)
      onDeleted?.(app.id)
    } else {
      toast.info("Deletion plan created. Review it before applying.")
      router.push(`/applications/${app.id}?tab=changes`)
    }
  }
  return <>
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="icon" aria-label={`Actions for ${app.name}`} title={`Actions for ${app.name}`} />}><span className="text-lg leading-none" aria-hidden="true">⋯</span></DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-36">
        <DropdownMenuItem onClick={() => router.push(`/applications/${app.id}`)}>View application</DropdownMenuItem>
        {canManage && <><DropdownMenuItem onClick={() => router.push(`/applications/${app.id}/edit`)}>Edit application</DropdownMenuItem><DropdownMenuItem variant="destructive" onClick={() => setDeleteOpen(true)}>Delete application</DropdownMenuItem></>}
      </DropdownMenuContent>
    </DropdownMenu>
    <ConfirmDisclosure open={deleteOpen} onOpenChange={setDeleteOpen} title={`Delete ${app.name}?`} description="Choose whether JustCD keeps the managed Kubernetes resources or prepares a deletion plan for review." confirmLabel="Continue" onConfirm={remove}>
      <FormSelect ariaLabel="Managed cluster resources" value={policy} onValueChange={setPolicy} items={[{ value: "keep", label: "Keep resources in Kubernetes" }, { value: "delete", label: "Delete through a reviewed plan" }]} />
      {policy === "delete" && <p className="mt-2 text-sm text-muted-foreground">Resources are not deleted immediately. Review and approve the deletion plan on the application page.</p>}
    </ConfirmDisclosure>
  </>
}

function RuntimeHealth({ condition }: { condition: WorkspaceApplication["healthCondition"] }) {
  const status = condition?.status ?? "Unknown"
  return <div className="flex flex-wrap items-center gap-2"><span className="text-xs text-muted-foreground">Runtime</span><StatusBadge status={status} /></div>
}

export function ApplicationCard({ app, canManage, onDeleted }: { app: WorkspaceApplication; canManage?: boolean; onDeleted?: (id: string) => void }) {
  const namespaces = app.namespaces.map((item) => item.namespace).join(", ")
  const revision = /^[a-f0-9]{40}$|^[a-f0-9]{64}$/i.test(app.revision)
    ? app.revision.slice(0, 8)
    : app.revision

  return (
    <article className="flex min-w-0 flex-col rounded-xl border bg-card p-5">
      <header className="flex items-start justify-between gap-3">
        <div className="min-w-0 pt-1">
          <h3 className="truncate text-base font-semibold tracking-tight" title={app.name}>
            <Link href={`/applications/${app.id}`} className="rounded-sm hover:underline underline-offset-4">
              {app.name}
            </Link>
          </h3>
          <p className="mt-1 truncate text-sm text-muted-foreground" title={app.workspaceName || app.workspaceId}>
            {app.workspaceName || app.workspaceId}
          </p>
        </div>
        <ApplicationActions app={app} canManage={Boolean(canManage)} onDeleted={onDeleted} />
      </header>

      <div className="mt-4 space-y-2">
        <div className="flex flex-wrap items-center gap-2"><span className="text-xs text-muted-foreground">Sync</span><StatusBadge status={app.health} />{app.autoSyncPaused && <StatusBadge status="Reconciliation paused" />}{app.repositoryConfigurationId && <span className="text-xs text-muted-foreground">Managed by Git</span>}{app.configurationMissing && <span className="text-xs text-destructive">Definition missing</span>}</div>
        <RuntimeHealth condition={app.healthCondition} />
        {app.statusIssues?.length > 0 && <p className="mt-2 text-xs leading-5 text-destructive" title={app.statusIssues.map((issue) => issue.summary).join("\n")}>{app.statusIssues.map((issue) => issue.source === "git" ? "Git" : issue.source === "cluster" ? "Cluster" : issue.source).join(" + ")} check failed · <Link href={`/applications/${app.id}`} className="underline underline-offset-2">Details</Link></p>}
      </div>

      <dl className="my-5 grid grid-cols-[70px_minmax(0,1fr)] items-baseline gap-x-3 gap-y-2.5 text-xs">
        <dt className="text-muted-foreground">Path</dt>
        <dd className="truncate font-mono" title={app.manifestPath || "."}>
          {app.manifestPath || "."}
        </dd>
        <dt className="text-muted-foreground">Revision</dt>
        <dd className="truncate font-mono" title={app.revision}>
          {revision}
        </dd>
        <dt className="text-muted-foreground">Namespace</dt>
        <dd className="truncate" title={namespaces || "No namespace"}>
          {namespaces || "No namespace"}
        </dd>
      </dl>

      <footer className="mt-auto flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t pt-3 text-xs text-muted-foreground">
        <span>{app.renderer === "helm" ? "Helm" : app.renderer === "kustomize" ? "Kustomize" : app.renderer}</span>
        <span>{app.autoSyncPaused ? "Reconciliation paused" : app.syncPolicy === "auto-safe" ? "Auto-safe sync" : "Manual sync"}</span>
      </footer>
    </article>
  )
}

export function ApplicationCollection({
  applications,
  workspaces,
  initialFilter = "all",
  createHref = "/applications/new",
  canCreate = true,
  workspaceRole,
  onDeleted,
}: {
  applications: WorkspaceApplication[]
  workspaces?: Workspace[]
  initialFilter?: string
  createHref?: string
  canCreate?: boolean
  workspaceRole?: Workspace["role"]
  onDeleted?: (id: string) => void
}) {
  const [query, setQuery] = useState("")
  const [filter, setFilter] = useState(initialFilter)
  const [filterQuery, setFilterQuery] = useState<FilterQuery>(() => createFilterQuery())
  const [view, setView] = useState<"cards" | "table">("cards")
  const [sort, setSort] = useState("attention")
  const [removedIds, setRemovedIds] = useState<string[]>([])
  const handleDeleted = (id: string) => { setRemovedIds((current) => [...current, id]); onDeleted?.(id) }
  const available = applications.filter((app) => !removedIds.includes(app.id))
  const filterFields: FilterField[] = [
    ...(workspaces ? [{ id: "workspace", label: "Workspace", type: "select" as const, options: workspaces.map((item) => ({ value: item.id, label: item.name })), operators: [{ value: "is", label: "is" }] }] : []),
    { id: "namespace", label: "Namespace", type: "select", options: [...new Set(available.flatMap((app) => app.namespaces.map((item) => item.namespace)))].sort().map((value) => ({ value, label: value })), operators: [{ value: "is", label: "is" }] },
    { id: "reconciliation", label: "Reconciliation", type: "select", options: [{ value: "paused", label: "Paused" }, { value: "active", label: "Active" }], operators: [{ value: "is", label: "is" }] },
    { id: "renderer", label: "Renderer", type: "select", options: [{ value: "yaml", label: "Plain YAML / JSON" }, { value: "helm", label: "Helm" }, { value: "kustomize", label: "Kustomize" }], operators: [{ value: "is", label: "is" }] },
  ]
  const conditions = flattenFilterConditions(filterQuery)
  const counts = {
    all: available.length,
    attention: available.filter(needsAttention).length,
    synced: available.filter(isApplicationHealthy).length,
    paused: available.filter((app) => app.autoSyncPaused).length,
    other: available.filter(
      (app) => !needsAttention(app) && !isApplicationHealthy(app)
    ).length,
  }
  const visible = available
    .filter((app) => {
      const matchesStatus =
        filter === "all" ||
        (filter === "attention"
          ? needsAttention(app)
          : filter === "synced"
            ? isApplicationHealthy(app)
            : filter === "paused"
              ? app.autoSyncPaused
              : !needsAttention(app) && !isApplicationHealthy(app))
      return (
        matchesStatus &&
        conditions.every((condition) => {
          const rawValue = String(condition.values[0] ?? "")
          const value = rawValue.toLowerCase()
          if (!value) return true
          switch (condition.field) {
            case "workspace": return app.workspaceId === rawValue
            case "namespace": return app.namespaces.some((item) => item.namespace.toLowerCase() === value)
            case "reconciliation": return value === "paused" ? app.autoSyncPaused : !app.autoSyncPaused
            case "renderer": return app.renderer === value
            default: return true
          }
        }) &&
        `${app.name} ${app.workspaceName ?? ""} ${app.namespaces.map((item) => item.namespace).join(" ")} ${app.revision} ${app.autoSyncPaused ? "reconciliation paused disabled" : "reconciliation active"}`
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

  if (!available.length)
    return (
      <div className="rounded-xl border border-dashed bg-card">
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
      <section
        aria-label="Filter applications"
        className="mb-3 rounded-xl border bg-card"
      >
        <div
          className="overflow-x-auto border-b px-4 py-3 sm:px-5"
        >
          <div
            className="flex w-max gap-1"
            role="group"
            aria-label="Filter by health"
          >
            {(
              [
                ["all", "All apps"],
                ["attention", "Needs attention"],
                ["synced", "In sync"],
                ["other", "Other"],
                ["paused", "Reconciliation paused"],
              ] as const
            ).map(([value, label]) => (
              <Button
                key={value}
                type="button"
                aria-pressed={filter === value}
                onClick={() => setFilter(value)}
                size="sm"
                variant={filter === value ? "secondary" : "ghost"}
                className="h-8 shrink-0 gap-2"
              >
                {label}
                <span className="rounded bg-background/60 px-1.5 py-0.5 text-xs leading-none tabular-nums">
                  {counts[value]}
                </span>
              </Button>
            ))}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-3 px-4 py-3 sm:px-5">
          <div className="relative w-full min-w-0 sm:w-auto sm:min-w-56 sm:flex-1 lg:max-w-sm">
            <WorkspaceIcon
              name="search"
              className="pointer-events-none absolute top-2.5 left-3 size-4 text-muted-foreground"
            />
            <Input
              aria-label="Search applications"
              placeholder="Search applications, namespaces, revisions…"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              className="h-9 bg-background pl-9"
            />
          </div>
          <Filters fields={filterFields} query={filterQuery} onQueryChange={setFilterQuery} size="sm" className="min-w-0 flex-1" />
          <FormSelect
            ariaLabel="Sort applications"
            value={sort}
            onValueChange={setSort}
            className="w-40 shrink-0"
            items={[
              { value: "attention", label: "Attention first" },
              { value: "name", label: "Name A–Z" },
              { value: "newest", label: "Newest first" },
            ]}
          />
        </div>
      </section>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2 px-1">
        <p role="status" className="text-sm text-muted-foreground">Showing {visible.length} of {available.length} applications</p>
        <div className="flex w-fit gap-1 rounded-lg border bg-card p-1" role="group" aria-label="Application view">
          {(["cards", "table"] as const).map((value) => <Button type="button" key={value} aria-pressed={view === value} onClick={() => setView(value)} size="sm" variant={view === value ? "secondary" : "ghost"} className="capitalize">{value}</Button>)}
        </div>
      </div>
      {visible.length ? (
        view === "cards" ? (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,280px),1fr))] gap-4">
            {visible.map((app) => (
              <ApplicationCard key={app.id} app={app} canManage={workspaceRole === "owner" || workspaces?.some((workspace) => workspace.id === app.workspaceId && workspace.role === "owner")} onDeleted={handleDeleted} />
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
                      {app.workspaceName || app.manifestPath}
                    </span>
                  </Link>
                ),
              },
              {
                id: "health",
                title: "Health",
                size: 150,
                cell: (app) => <div className="space-y-2"><div className="flex flex-wrap gap-1.5"><StatusBadge status={app.health} /><StatusBadge status={app.healthCondition?.status ?? "Unknown"} />{app.autoSyncPaused && <StatusBadge status="Reconciliation paused" />}{app.repositoryConfigurationId && <span className="text-xs text-muted-foreground">Managed by Git</span>}{app.configurationMissing && <span className="text-xs text-destructive">Definition missing</span>}</div><p className="text-sm text-muted-foreground" title={app.healthCondition?.message}>{app.healthCondition?.reason ?? "Health not observed"} · {app.healthCondition?.lastTransitionTime ? new Date(app.healthCondition.lastTransitionTime).toLocaleString() : "Not observed yet"}</p>{app.statusIssues?.length > 0 && <span className="block text-xs text-destructive">{app.statusIssues.map((issue) => issue.source === "git" ? "Git" : issue.source === "cluster" ? "Cluster" : issue.source).join(" + ")} check failed</span>}</div>,
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
                  <div className="space-y-1 text-xs"><span>{app.syncPolicy}</span>{app.autoSyncPaused && <span className="block text-amber-600 dark:text-amber-400">Reconciliation paused</span>}</div>
                ),
              },
              {
                id: "actions",
                title: "",
                size: 64,
                cell: (app) => <ApplicationActions app={app} canManage={workspaceRole === "owner" || Boolean(workspaces?.some((workspace) => workspace.id === app.workspaceId && workspace.role === "owner"))} onDeleted={handleDeleted} />,
              },
            ]}
          />
        )
      ) : (
        <div className="rounded-xl border border-dashed bg-card pb-6 text-center">
          <EmptyState
            title="No matching applications"
            description="Try another search or clear the filters to see all applications."
          />
          <Button
            variant="outline"
            onClick={() => {
              setQuery("")
              setFilter("all")
              setFilterQuery(createFilterQuery())
            }}
          >
            Clear filters
          </Button>
        </div>
      )}
    </div>
  )
}
