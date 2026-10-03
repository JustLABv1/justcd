"use client"

import Link from "next/link"
import { useRouter } from "next/navigation"
import { useEffect, useMemo, useState } from "react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Filters } from "@/components/reui/filters/filters"
import { createFilterQuery, flattenFilterConditions } from "@/components/reui/filters/filters-query"
import type { FilterField, FilterQuery } from "@/components/reui/filters/filters-types"
import { DataGridList, type GridColumn } from "@/components/data-grid-table"
import { RowActions, type ActionItem } from "@/components/action-menu"
import { Badge } from "@/components/reui/badge"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Delete02Icon, Edit02Icon, ViewIcon } from "@hugeicons/core-free-icons"
import { EmptyState, StatusBadge } from "@/components/ui-kit"
import { WorkspaceIcon } from "@/components/workspace-ui"
import { tone } from "@/components/health-tone"
import { useToast } from "@/components/toast-provider"
import {
  isApplicationHealthy,
  needsAttention,
  type WorkspaceApplication,
} from "@/hooks/use-workspace"
import type { ClusterStatus } from "@/hooks/use-cluster-statuses"
import { applicationAttentionReason } from "@/lib/application-status"
import { groupApplications, type ApplicationBucket, type GroupMode, type StatusBucket } from "@/lib/application-grouping"
import { clusterHealth, isCheckStalled, type ConnectionHealth, type HealthLevel } from "@/lib/connection-health"
import { ago } from "@/lib/relative-time"
import type { Workspace } from "@/lib/types"
import { api } from "@/lib/api"

function ApplicationActions({ app, canManage, onDeleted }: { app: WorkspaceApplication; canManage: boolean; onDeleted?: (id: string) => void }) {
  const router = useRouter()
  const toast = useToast()
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
  const items: ActionItem[] = [{ label: "View application", icon: ViewIcon, href: `/applications/${app.id}` }]
  if (canManage) {
    items.push({ label: "Edit application", icon: Edit02Icon, href: `/applications/${app.id}/edit` })
    items.push({
      label: "Delete application",
      icon: Delete02Icon,
      destructive: true,
      confirm: {
        title: `Delete ${app.name}?`,
        description: "Choose whether JustCD keeps the managed Kubernetes resources or prepares a deletion plan for review.",
        confirmLabel: "Delete application",
        onConfirm: remove,
        children: <>
          <FormSelect ariaLabel={`Managed cluster resources for ${app.name}`} value={policy} onValueChange={setPolicy} items={[{ value: "keep", label: "Keep resources in Kubernetes" }, { value: "delete", label: "Delete through a reviewed plan" }]} />
          {policy === "delete" && <p className="mt-2 text-sm text-muted-foreground">Resources are not deleted immediately. Review and approve the deletion plan on the application page.</p>}
        </>,
      },
    })
  }
  return <RowActions label={app.name} items={items} />
}

const checkSources = (app: WorkspaceApplication) => app.statusIssues.map((issue) => issue.source === "git" ? "Git" : issue.source === "cluster" ? "Cluster" : issue.source).join(" + ")

const classify = (app: WorkspaceApplication): StatusBucket => needsAttention(app) ? "attention" : isApplicationHealthy(app) ? "synced" : "other"
const levelRank: Record<HealthLevel, number> = { critical: 0, warning: 1, unknown: 2, healthy: 3 }
const rendererLabel = { helm: "Helm", kustomize: "Kustomize", yaml: "Plain YAML" } as const

function Dot({ level }: { level: HealthLevel }) {
  return <span aria-hidden="true" className={`inline-block size-2 shrink-0 rounded-full ${tone[level].dot}`} />
}

/** Why the application is not fine, most specific first. Connection outages are reported once per cluster, not per card. */
function reasonFor(app: WorkspaceApplication, blockedBy?: ConnectionHealth) {
  if (blockedBy) return { text: `Blocked by cluster: ${blockedBy.label.toLowerCase()}`, blocked: true }
  const text = applicationAttentionReason(app) ?? app.statusIssues?.[0]?.summary
  return text ? { text, blocked: false } : null
}

export function ApplicationCard({
  app, canManage, onDeleted, showCluster = true, clusterLevel, blockedBy, now,
}: {
  app: WorkspaceApplication; canManage?: boolean; onDeleted?: (id: string) => void
  showCluster?: boolean; clusterLevel?: HealthLevel; blockedBy?: ConnectionHealth; now?: number
}) {
  const namespaces = app.namespaces.map((item) => item.namespace).join(", ") || "No namespace"
  const revision = /^[a-f0-9]{40}$|^[a-f0-9]{64}$/i.test(app.revision) ? app.revision.slice(0, 8) : app.revision
  const reason = reasonFor(app, blockedBy)
  const stalled = isCheckStalled(app, now)
  return (
    <article className={`flex min-w-0 flex-col rounded-xl border bg-card p-4 ${blockedBy ? "border-dashed opacity-75" : ""}`}>
      <header className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="truncate text-base font-semibold tracking-tight" title={app.name}>
            <Link href={`/applications/${app.id}`} className="rounded-sm underline-offset-4 hover:underline">{app.name}</Link>
          </h3>
          <p className="mt-0.5 flex min-w-0 items-center gap-1.5 truncate text-sm text-muted-foreground">
            {showCluster && <><Dot level={clusterLevel ?? "unknown"} /><span className="truncate" title={app.clusterId}>{app.clusterName || app.clusterId}</span><span aria-hidden="true">/</span></>}
            <span className="truncate" title={namespaces}>{namespaces}</span>
          </p>
        </div>
        <ApplicationActions app={app} canManage={Boolean(canManage)} onDeleted={onDeleted} />
      </header>
      <div className="mt-3 flex flex-wrap items-center gap-1.5">
        <span title="Delivery state"><StatusBadge status={app.health} /></span>
        <span title="Runtime state"><StatusBadge status={app.healthCondition?.status ?? "Unknown"} /></span>
        {app.autoSyncPaused && <StatusBadge status="Reconciliation paused" />}
        {app.configurationMissing && <Badge variant="destructive-light" radius="full">Definition missing</Badge>}
      </div>
      {reason && (
        <p className={`mt-2 line-clamp-2 text-sm ${reason.blocked ? "text-muted-foreground" : "text-warning-foreground dark:text-warning"}`} title={app.statusIssues?.map((issue) => issue.summary).join("\n") || reason.text}>
          {reason.text}{app.statusIssues?.length > 0 && !reason.blocked && <> · <span className="text-muted-foreground">{checkSources(app)} check failed</span></>}
        </p>
      )}
      <footer className="mt-auto flex flex-wrap items-center justify-between gap-x-3 gap-y-1 pt-3 text-xs text-muted-foreground">
        <span className="truncate" title={`${app.revision} · ${app.manifestPath || "."} · ${app.syncPolicy}`}>
          <span className="font-mono">{revision}</span> · {rendererLabel[app.renderer]} · {app.autoSyncPaused ? "paused" : app.syncPolicy === "auto-safe" ? "auto-safe" : "manual"}
        </span>
        <span className={stalled ? "font-medium text-warning-foreground dark:text-warning" : ""} title={app.lastCheckedAt ? new Date(app.lastCheckedAt).toLocaleString() : undefined}>
          {stalled ? "Checks stalled · " : "Checked "}{ago(app.lastCheckedAt, now)}
        </span>
      </footer>
    </article>
  )
}

function GroupHeader({ label, bucket, mode, health, open }: { label: string; bucket: ApplicationBucket<WorkspaceApplication>; mode: GroupMode; health?: ConnectionHealth; open: boolean }) {
  const apps = bucket.applications
  const counts = { synced: 0, attention: 0, other: 0 }
  for (const app of apps) counts[classify(app)]++
  const level = health?.level ?? (counts.attention ? "warning" : "healthy")
  return (
    <span className="flex w-full min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 text-left">
      <span aria-hidden="true" className={`grid size-8 shrink-0 place-items-center rounded-lg ${tone[level].bg} ${tone[level].text}`}>
        <WorkspaceIcon name={mode === "cluster" ? "server" : mode === "namespace" ? "folder" : "app"} className="size-4" />
      </span>
      <span className="min-w-0 flex-1 basis-56">
        <span className="flex flex-wrap items-center gap-x-2">
          <span className="truncate text-sm font-semibold">{label}</span>
          {health && <span className={`text-xs font-medium ${tone[health.level].text}`}><span className="sr-only">{health.level}: </span>{health.label}</span>}
        </span>
        {health && health.level !== "healthy" && (
          <span className="mt-0.5 block text-xs text-muted-foreground">
            {health.detail}{health.affectedApplications.length > 0 && ` ${health.affectedApplications.length} application${health.affectedApplications.length === 1 ? "" : "s"} affected.`}
          </span>
        )}
      </span>
      <span className="flex shrink-0 items-center gap-3">
        <span role="img" aria-label={`${counts.synced} in sync, ${counts.attention} need attention, ${counts.other} other`} className="flex h-1.5 w-24 overflow-hidden rounded-full bg-muted">
          {[["bg-success", counts.synced], ["bg-warning", counts.attention], ["bg-muted-foreground/40", counts.other]].map(([color, n]) => Number(n) > 0 && <span key={String(color)} className={String(color)} style={{ width: `${(Number(n) / apps.length) * 100}%` }} />)}
        </span>
        <span className="text-xs tabular-nums text-muted-foreground">{apps.length} app{apps.length === 1 ? "" : "s"}{counts.attention > 0 && <> · <span className="text-warning-foreground dark:text-warning">{counts.attention} to check</span></>}</span>
      </span>
      <span className="sr-only">{open ? "Collapse" : "Expand"} {label}</span>
    </span>
  )
}

export function ApplicationCollection({
  applications,
  workspaces,
  clusterStatuses = {},
  initialFilter = "all",
  createHref = "/applications/new",
  canCreate = true,
  workspaceRole,
  onDeleted,
}: {
  applications: WorkspaceApplication[]
  workspaces?: Workspace[]
  clusterStatuses?: Record<string, ClusterStatus>
  initialFilter?: string
  createHref?: string
  canCreate?: boolean
  workspaceRole?: Workspace["role"]
  onDeleted?: (id: string) => void
}) {
  const [query, setQuery] = useState("")
  const [filter, setFilter] = useState(initialFilter)
  const [clusterFilter, setClusterFilter] = useState("")
  const [filterQuery, setFilterQuery] = useState<FilterQuery>(() => createFilterQuery())
  const [view, setView] = useState<"cards" | "list">("cards")
  const [sort, setSort] = useState("attention")
  const [groupBy, setGroupBy] = useState<GroupMode>("cluster")
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>({})
  const [removedIds, setRemovedIds] = useState<string[]>([])
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 15000)
    return () => window.clearInterval(timer)
  }, [])
  const handleDeleted = (id: string) => { setRemovedIds((current) => [...current, id]); onDeleted?.(id) }
  const available = useMemo(() => applications.filter((app) => !removedIds.includes(app.id)), [applications, removedIds])

  const clusters = useMemo(() => {
    const byId = new Map<string, { id: string; name: string; health: ConnectionHealth; count: number }>()
    for (const app of available) {
      if (byId.has(app.clusterId)) continue
      const status = clusterStatuses[app.clusterId]
      const cluster = status?.cluster ?? { id: app.clusterId, connectionMode: "direct" as const }
      byId.set(app.clusterId, {
        id: app.clusterId,
        name: status?.cluster.name ?? app.clusterName ?? app.clusterId,
        health: clusterHealth(cluster, status?.agent, available.filter((item) => item.clusterId === app.clusterId), now),
        count: available.filter((item) => item.clusterId === app.clusterId).length,
      })
    }
    return byId
  }, [available, clusterStatuses, now])

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
    other: available.filter((app) => !needsAttention(app) && !isApplicationHealthy(app)).length,
  }
  const visible = available
    .filter((app) => {
      const matchesStatus =
        filter === "all" ||
        (filter === "attention" ? needsAttention(app)
          : filter === "synced" ? isApplicationHealthy(app)
          : filter === "paused" ? app.autoSyncPaused
          : !needsAttention(app) && !isApplicationHealthy(app))
      return (
        matchesStatus &&
        (!clusterFilter || app.clusterId === clusterFilter) &&
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
        `${app.name} ${app.workspaceName ?? ""} ${app.clusterName ?? ""} ${app.clusterId} ${app.namespaces.map((item) => item.namespace).join(" ")} ${app.revision} ${app.autoSyncPaused ? "reconciliation paused disabled" : "reconciliation active"}`
          .toLowerCase()
          .includes(query.trim().toLowerCase())
      )
    })
    .sort((a, b) =>
      sort === "name"
        ? a.name.localeCompare(b.name)
        : sort === "newest"
          ? new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
          : Number(needsAttention(b)) - Number(needsAttention(a)) || a.name.localeCompare(b.name)
    )

  const buckets = useMemo(() => {
    const groups = groupApplications(visible, groupBy, classify)
    // Broken clusters first so a single outage is the first thing on the page.
    if (groupBy === "cluster") groups.sort((a, b) => levelRank[clusters.get(a.key)?.health.level ?? "unknown"] - levelRank[clusters.get(b.key)?.health.level ?? "unknown"] || a.label.localeCompare(b.label))
    return groups
  }, [visible, groupBy, clusters])

  const canManageApp = (app: WorkspaceApplication) => workspaceRole === "owner" || Boolean(workspaces?.some((workspace) => workspace.id === app.workspaceId && workspace.role === "owner"))
  const filtering = Boolean(query.trim() || clusterFilter || conditions.length || filter !== "all")
  const defaultOpen = (bucket: ApplicationBucket<WorkspaceApplication>) =>
    groupBy === "none" || filtering || buckets.length === 1 || (groupBy === "status" ? bucket.key !== "synced" : bucket.applications.some(needsAttention) || (clusters.get(bucket.key)?.health.level ?? "healthy") !== "healthy")
  const resetFilters = () => { setQuery(""); setFilter("all"); setClusterFilter(""); setFilterQuery(createFilterQuery()) }

  if (!available.length)
    return (
      <div className="rounded-xl border border-dashed bg-card">
        <EmptyState
          title="Your next deployment starts here"
          description={`Connect a Git repository and a Kubernetes target to bring your first application online.${canCreate ? "" : " Ask a workspace owner to create one."}`}
          href={canCreate ? createHref : undefined}
          action="Create application"
        />
      </div>
    )

  const listColumns = (showCluster: boolean): GridColumn<WorkspaceApplication>[] => [
    {
      id: "name", title: "Application", size: 220,
      cell: (app) => (
        <Link href={`/applications/${app.id}`} className="block truncate font-medium hover:text-primary">
          {app.name}
          <span className="block truncate text-xs font-normal text-muted-foreground">{app.workspaceName || app.manifestPath}</span>
        </Link>
      ),
    },
    {
      id: "health", title: "Health", size: 260,
      cell: (app) => {
        const blocked = clusters.get(app.clusterId)?.health
        const reason = reasonFor(app, blocked?.level === "critical" && blocked.affectedApplications.some((item) => item.id === app.id) ? blocked : undefined)
        return (
          <div className="space-y-1.5">
            <div className="flex flex-wrap gap-1.5">
              <StatusBadge status={app.health} /><StatusBadge status={app.healthCondition?.status ?? "Unknown"} />
              {app.autoSyncPaused && <StatusBadge status="Reconciliation paused" />}
              {app.configurationMissing && <Badge variant="destructive-light" radius="full">Definition missing</Badge>}
            </div>
            {reason && <p className="line-clamp-2 text-xs text-muted-foreground" title={reason.text}>{reason.text}</p>}
          </div>
        )
      },
    },
    ...(showCluster ? [{
      id: "cluster", title: "Cluster", size: 160,
      cell: (app: WorkspaceApplication) => <span className="flex items-center gap-1.5 truncate text-xs font-medium" title={`${app.clusterName || app.clusterId} (${app.clusterId})`}><Dot level={clusters.get(app.clusterId)?.health.level ?? "unknown"} />{app.clusterName || app.clusterId}</span>,
    }] : []),
    { id: "target", title: "Namespace", size: 150, cell: (app) => <span className="block truncate text-xs">{app.namespaces.map((item) => item.namespace).join(", ") || "—"}</span> },
    { id: "revision", title: "Revision", size: 120, cell: (app) => <span className="block truncate font-mono text-xs">{app.revision}</span> },
    {
      id: "checked", title: "Checked", size: 110,
      cell: (app) => <span className={`text-xs ${isCheckStalled(app, now) ? "font-medium text-warning-foreground dark:text-warning" : "text-muted-foreground"}`} title={app.lastCheckedAt ? new Date(app.lastCheckedAt).toLocaleString() : undefined}>{ago(app.lastCheckedAt, now)}</span>,
    },
    { id: "policy", title: "Sync policy", size: 110, cell: (app) => <span className="text-xs">{app.autoSyncPaused ? "paused" : app.syncPolicy}</span> },
    { id: "actions", title: "", size: 64, cell: (app) => <ApplicationActions app={app} canManage={canManageApp(app)} onDeleted={handleDeleted} /> },
  ]

  const renderBucket = (bucket: ApplicationBucket<WorkspaceApplication>) => {
    const health = groupBy === "cluster" ? clusters.get(bucket.key)?.health : undefined
    const blockedIds = new Set(health?.level === "critical" ? health.affectedApplications.map((app) => app.id) : [])
    const body = view === "cards" ? (
      <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,300px),1fr))] gap-3">
        {bucket.applications.map((app) => (
          <ApplicationCard
            key={app.id} app={app} canManage={canManageApp(app)} onDeleted={handleDeleted} now={now}
            showCluster={groupBy !== "cluster"} clusterLevel={clusters.get(app.clusterId)?.health.level}
            blockedBy={blockedIds.has(app.id) ? health : undefined}
          />
        ))}
      </div>
    ) : (
      <DataGridList rows={bucket.applications} columns={listColumns(groupBy !== "cluster")} />
    )
    if (groupBy === "none") return <div key={bucket.key}>{body}</div>
    const open = openGroups[bucket.key] ?? defaultOpen(bucket)
    return (
      <Collapsible key={bucket.key} open={open} onOpenChange={(next) => setOpenGroups((current) => ({ ...current, [bucket.key]: next }))} className={`rounded-xl border bg-card/40 ${health && health.level !== "healthy" ? tone[health.level].border : ""}`}>
        <CollapsibleTrigger className="w-full justify-between gap-3 px-4 py-3">
          <GroupHeader label={bucket.label} bucket={bucket} mode={groupBy} health={health} open={open} />
        </CollapsibleTrigger>
        <CollapsibleContent><div className="border-t p-4">{body}</div></CollapsibleContent>
      </Collapsible>
    )
  }

  return (
    <div>
      <section aria-label="Filter applications" className="mb-3 rounded-xl border bg-card">
        <div className="overflow-x-auto border-b px-4 py-3 sm:px-5">
          <ToggleGroup className="w-max gap-1 border-0 bg-transparent p-0" aria-label="Filter by health" value={[filter]} onValueChange={(value) => { if (value[0]) setFilter(value[0]) }}>
            {([["all", "All apps"], ["attention", "Needs attention"], ["synced", "In sync"], ["other", "Other"], ["paused", "Reconciliation paused"]] as const).map(([value, label]) => (
              <ToggleGroupItem key={value} value={value} className="h-8 shrink-0 gap-2">
                {label}
                <span className="rounded bg-background/60 px-1.5 py-0.5 text-xs leading-none tabular-nums">{counts[value]}</span>
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>
        {clusters.size > 1 && (
          <div className="overflow-x-auto border-b px-4 py-3 sm:px-5">
            <ToggleGroup className="w-max gap-1 border-0 bg-transparent p-0" aria-label="Filter by cluster" value={[clusterFilter]} onValueChange={(value) => setClusterFilter(value[0] ?? "")}>
              {[...clusters.values()].sort((a, b) => levelRank[a.health.level] - levelRank[b.health.level] || a.name.localeCompare(b.name)).map((cluster) => (
                <ToggleGroupItem key={cluster.id} value={cluster.id} className="h-8 shrink-0 gap-2" title={`${cluster.health.label}. ${cluster.health.detail}`}>
                  <Dot level={cluster.health.level} /><span className="sr-only">{cluster.health.level}: </span>
                  {cluster.name}
                  <span className="rounded bg-background/60 px-1.5 py-0.5 text-xs leading-none tabular-nums">{cluster.count}</span>
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
        )}
        <div className="flex flex-wrap items-center gap-3 px-4 py-3 sm:px-5">
          <div className="relative w-full min-w-0 sm:w-auto sm:min-w-56 sm:flex-1 lg:max-w-sm">
            <WorkspaceIcon name="search" className="pointer-events-none absolute top-2.5 left-3 size-4 text-muted-foreground" />
            <Input aria-label="Search applications" placeholder="Search applications, clusters, namespaces, revisions…" value={query} onChange={(event) => setQuery(event.target.value)} className="h-9 bg-background pl-9" />
          </div>
          <Filters fields={filterFields} query={filterQuery} onQueryChange={setFilterQuery} size="sm" className="min-w-0 flex-1" />
          <FormSelect
            ariaLabel="Group applications"
            value={groupBy}
            onValueChange={(value) => setGroupBy(value as GroupMode)}
            className="w-44 shrink-0"
            items={[
              { value: "cluster", label: "Group: Cluster" },
              { value: "namespace", label: "Group: Namespace" },
              { value: "status", label: "Group: Status" },
              { value: "none", label: "No grouping" },
            ]}
          />
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
        <p role="status" className="text-sm text-muted-foreground">Showing {visible.length} of {available.length} applications{buckets.length > 1 && groupBy !== "none" ? ` in ${buckets.length} groups` : ""}</p>
        <ToggleGroup aria-label="Application view" value={[view]} onValueChange={(value) => { if (value[0]) setView(value[0] as "cards" | "list") }}>
          <ToggleGroupItem value="cards">Cards</ToggleGroupItem>
          <ToggleGroupItem value="list">List</ToggleGroupItem>
        </ToggleGroup>
      </div>
      {visible.length ? (
        <div className="space-y-3">{buckets.map(renderBucket)}</div>
      ) : (
        <div className="rounded-xl border border-dashed bg-card pb-6 text-center">
          <EmptyState title="No matching applications" description="Try another search or clear the filters to see all applications." />
          <Button variant="outline" onClick={resetFilters}>Clear filters</Button>
        </div>
      )}
    </div>
  )
}
