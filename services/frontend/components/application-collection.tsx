"use client"

import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import { StarIcon, Tag01Icon, ViewIcon as SavedViewIcon, Tick02Icon, Add01Icon } from "@hugeicons/core-free-icons"
import { ApplicationLabelsDialog } from "@/components/application-labels-dialog"
import { ConnectionDialog } from "@/components/connection-dialog"
import { DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { FormField } from "@/components/ui-kit"
import { applicationViewSignature } from "@/lib/application-views"
import type { ApplicationPreferences, ApplicationSavedView, ApplicationViewConfig } from "@/lib/application-views"
import { useRouter } from "next/navigation"
import { useEffect, useMemo, useState, type ReactNode } from "react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Filters } from "@/components/reui/filters/filters"
import { createFilterQuery, flattenFilterConditions } from "@/components/reui/filters/filters-query"
import type { FilterField, FilterQuery } from "@/components/reui/filters/filters-types"
import { DataGridList, type GridColumn } from "@/components/data-grid-table"
import { ActionMenu, RowActions, type ActionItem } from "@/components/action-menu"
import { Badge } from "@/components/reui/badge"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { ApplicationClusterFilter } from "@/components/application-cluster-filter"
import { ToggleGroup, ToggleGroupItem, ToggleGroupCount } from "@/components/ui/toggle-group"
import { Delete02Icon, Edit02Icon, ViewIcon } from "@hugeicons/core-free-icons"
import { EmptyState, StatusBadge } from "@/components/ui-kit"
import { WorkspaceIcon, CollectionSkeleton } from "@/components/workspace-ui"
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
import { api, errorMessage } from "@/lib/api"

function ApplicationActions({ app, canManage, onDeleted, onEditLabels }: { app: WorkspaceApplication; canManage: boolean; onDeleted?: (id: string) => void; onEditLabels?: () => void }) {
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
  if (onEditLabels) items.push({ label: app.repositoryConfigurationId ? "View labels in Git" : "Edit labels", icon: Tag01Icon, onSelect: onEditLabels })
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

function FavoriteButton({ name, favorite, busy, onClick }: { name: string; favorite: boolean; busy?: boolean; onClick: () => void }) {
  return <Button type="button" variant="ghost" size="icon-sm" aria-label={`${favorite ? "Remove" : "Add"} ${name} ${favorite ? "from" : "to"} favorites`} aria-pressed={favorite} disabled={busy} onClick={onClick} title={favorite ? "Remove from favorites" : "Add to favorites"}><HugeiconsIcon icon={StarIcon} strokeWidth={1.8} aria-hidden="true" className={favorite ? "fill-warning text-warning-foreground dark:text-warning" : "text-muted-foreground"} /></Button>
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
  app, canManage, onDeleted, showCluster = true, clusterLevel, blockedBy, now, favorite, favoriteBusy, onFavorite, onEditLabels,
}: {
  app: WorkspaceApplication; canManage?: boolean; onDeleted?: (id: string) => void
  favorite?: boolean; favoriteBusy?: boolean; onFavorite?: () => void; onEditLabels?: () => void
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
        <div className="flex shrink-0 items-center gap-1">{onFavorite && <FavoriteButton name={app.name} favorite={Boolean(favorite)} busy={favoriteBusy} onClick={onFavorite} />}<ApplicationActions app={app} canManage={Boolean(canManage)} onDeleted={onDeleted} onEditLabels={onEditLabels} /></div>
      </header>
      <div className="mt-3 flex flex-wrap items-center gap-1.5">
        <span title="Delivery state"><StatusBadge status={app.health} /></span>
        <span title="Runtime state"><StatusBadge status={app.healthCondition?.status ?? "Unknown"} /></span>
        {app.autoSyncPaused && <StatusBadge status="Reconciliation paused" />}
        {app.configurationMissing && <Badge variant="destructive-light" radius="full">Definition missing</Badge>}
      </div>
      {Object.keys(app.labels ?? {}).length > 0 && <div aria-label="Application labels" className="mt-2 flex flex-wrap gap-1">{Object.entries(app.labels ?? {}).sort(([a], [b]) => a.localeCompare(b)).map(([key, value]) => <Badge key={key} variant="outline" className="max-w-full truncate text-xs" title={`${key}=${value}`}>{key}{value && <>: {value}</>}</Badge>)}</div>}
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
  onApplicationUpdated,
  renderHeader,
}: {
  applications: WorkspaceApplication[]
  workspaces?: Workspace[]
  clusterStatuses?: Record<string, ClusterStatus>
  initialFilter?: string
  createHref?: string
  canCreate?: boolean
  workspaceRole?: Workspace["role"]
  onDeleted?: (id: string) => void
  onApplicationUpdated?: (app: WorkspaceApplication) => void
  renderHeader?: (viewMenu?: ReactNode) => ReactNode
}) {
  const [query, setQuery] = useState("")
  const [filter, setFilter] = useState(initialFilter)
  const [clusterFilter, setClusterFilter] = useState("")
  const [filterQuery, setFilterQuery] = useState<FilterQuery>(() => createFilterQuery())
  const [view, setView] = useState<"cards" | "list">("cards")
  const [sort, setSort] = useState("attention")
  const [groupBy, setGroupBy] = useState<GroupMode>("cluster")
  const toast = useToast()
  const [labelKey, setLabelKey] = useState("")
  const [favoritesOnly, setFavoritesOnly] = useState(false)
  const [favoriteIds, setFavoriteIds] = useState<string[]>([])
  const [favoriteBusy, setFavoriteBusy] = useState<string[]>([])
  const [savedViews, setSavedViews] = useState<ApplicationSavedView[]>([])
  const [activeViewId, setActiveViewId] = useState("")
  const [preferencesReady, setPreferencesReady] = useState(false)
  const [preferencesError, setPreferencesError] = useState("")
  const [preferencesVersion, setPreferencesVersion] = useState(0)
  const [viewBusy, setViewBusy] = useState(false)
  const [saveDialog, setSaveDialog] = useState(false)
  const [viewName, setViewName] = useState("")
  const [viewError, setViewError] = useState("")
  const [editingLabels, setEditingLabels] = useState<WorkspaceApplication | null>(null)
  function applyView(config?: ApplicationViewConfig) {
    setGroupBy(config?.groupBy ?? "cluster"); setLabelKey(config?.labelKey ?? "")
    setQuery(config?.query ?? ""); setFilter(config?.status ?? initialFilter)
    setClusterFilter(config?.clusterId ?? ""); setView(config?.layout ?? "cards")
    setSort(config?.sort ?? "attention"); setFavoritesOnly(config?.favoritesOnly ?? false)
    setFilterQuery(config?.filters ?? createFilterQuery()); setOpenGroups({})
  }
  useEffect(() => {
    const controller = new AbortController()
    void api<ApplicationPreferences>("/api/v1/application-preferences", { signal: controller.signal }).then(result => {
      if (controller.signal.aborted) return
      setFavoriteIds(result.favoriteApplicationIds); setSavedViews(result.views)
      const selected = result.views.find(view => view.isDefault)
      if (selected) {
        const config = selected.configuration
        setActiveViewId(selected.id); setGroupBy(config.groupBy); setLabelKey(config.labelKey ?? "")
        setQuery(config.query); setFilter(initialFilter !== "all" ? initialFilter : config.status)
        setClusterFilter(config.clusterId); setView(config.layout); setSort(config.sort)
        setFavoritesOnly(config.favoritesOnly); setFilterQuery(config.filters ?? createFilterQuery())
      }
      setPreferencesError(""); setPreferencesReady(true)
    }).catch(cause => { if (!controller.signal.aborted) setPreferencesError(errorMessage(cause)) })
    return () => controller.abort()
  }, [initialFilter, preferencesVersion])
  async function toggleFavorite(app: WorkspaceApplication) {
    if (favoriteBusy.includes(app.id)) return
    const favorite = !favoriteIds.includes(app.id)
    setFavoriteBusy(current => [...current, app.id])
    try {
      await api(`/api/v1/applications/${encodeURIComponent(app.id)}/favorite`, { method: "PUT", body: JSON.stringify({ favorite }) })
      setFavoriteIds(current => favorite ? [...new Set([...current, app.id])] : current.filter(id => id !== app.id))
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setFavoriteBusy(current => current.filter(id => id !== app.id)) }
  }
  async function selectSavedView(id: string) {
    setViewBusy(true)
    try {
      await api("/api/v1/application-views/default", { method: "PUT", body: JSON.stringify({ viewId: id === "default" ? "" : id }) })
      setActiveViewId(id === "default" ? "" : id)
      applyView(savedViews.find(view => view.id === id)?.configuration)
      setSavedViews(current => current.map(view => ({ ...view, isDefault: view.id === id })))
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setViewBusy(false) }
  }
  const currentViewConfig: ApplicationViewConfig = { groupBy, labelKey, query, status: filter, clusterId: clusterFilter, layout: view, sort, favoritesOnly, filters: filterQuery }
  const activeSavedView = savedViews.find(view => view.id === activeViewId)
  const viewChanged = Boolean(activeSavedView && applicationViewSignature(activeSavedView.configuration) !== applicationViewSignature(currentViewConfig))
  async function saveView(asNew: boolean) {
    if (groupBy === "label" && !labelKey) { setViewError("Choose a label key before saving."); return }
    setViewBusy(true); setViewError("")
    try {
      const result = await api<ApplicationSavedView>(asNew ? "/api/v1/application-views" : `/api/v1/application-views/${encodeURIComponent(activeViewId)}`, { method: asNew ? "POST" : "PUT", body: JSON.stringify({ name: asNew ? viewName : activeSavedView?.name, configuration: currentViewConfig }) })
      setSavedViews(current => [...current.filter(view => view.id !== result.id).map(view => ({ ...view, isDefault: false })), result])
      setActiveViewId(result.id); setSaveDialog(false); toast.success(`View “${result.name}” saved. It will reopen next time.`)
    } catch (cause) { setViewError(errorMessage(cause)); if (!asNew) toast.error(errorMessage(cause), cause) }
    finally { setViewBusy(false) }
  }
  async function deleteView() {
    if (!activeSavedView) return
    setViewBusy(true)
    try {
      await api(`/api/v1/application-views/${encodeURIComponent(activeViewId)}`, { method: "DELETE" })
      setSavedViews(current => current.filter(view => view.id !== activeViewId)); setActiveViewId(""); applyView()
      toast.success("Saved view deleted.")
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setViewBusy(false) }
  }
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

  const labelKeys = [...new Set([...available.flatMap(app => Object.keys(app.labels ?? {})), ...(labelKey ? [labelKey] : [])])].sort()
  const filterFields: FilterField[] = [
    ...(workspaces ? [{ id: "workspace", label: "Workspace", type: "select" as const, options: workspaces.map((item) => ({ value: item.id, label: item.name })), operators: [{ value: "is", label: "is" }] }] : []),
    { id: "namespace", label: "Namespace", type: "select", options: [...new Set(available.flatMap((app) => app.namespaces.map((item) => item.namespace)))].sort().map((value) => ({ value, label: value })), operators: [{ value: "is", label: "is" }] },
    { id: "reconciliation", label: "Reconciliation", type: "select", options: [{ value: "paused", label: "Paused" }, { value: "active", label: "Active" }], operators: [{ value: "is", label: "is" }] },
    { id: "renderer", label: "Renderer", type: "select", options: [{ value: "yaml", label: "Plain YAML / JSON" }, { value: "helm", label: "Helm" }, { value: "kustomize", label: "Kustomize" }], operators: [{ value: "is", label: "is" }] },
  ]
  const conditions = flattenFilterConditions(filterQuery)
  const counts = {
    all: available.length,
    favorites: available.filter(app => favoriteIds.includes(app.id)).length,
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
        (!favoritesOnly || favoriteIds.includes(app.id)) &&
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
        `${app.name} ${app.workspaceName ?? ""} ${app.clusterName ?? ""} ${app.clusterId} ${app.namespaces.map((item) => item.namespace).join(" ")} ${app.revision} ${Object.entries(app.labels ?? {}).map(([key, value]) => `${key} ${value} ${key}=${value}`).join(" ")} ${app.autoSyncPaused ? "reconciliation paused disabled" : "reconciliation active"}`
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
    const groups = groupApplications(visible, groupBy, classify, labelKey)
    // Broken clusters first so a single outage is the first thing on the page.
    if (groupBy === "cluster") groups.sort((a, b) => levelRank[clusters.get(a.key)?.health.level ?? "unknown"] - levelRank[clusters.get(b.key)?.health.level ?? "unknown"] || a.label.localeCompare(b.label))
    return groups
  }, [visible, groupBy, labelKey, clusters])

  const canManageApp = (app: WorkspaceApplication) => workspaceRole === "owner" || Boolean(workspaces?.some((workspace) => workspace.id === app.workspaceId && workspace.role === "owner"))
  const filtering = Boolean(query.trim() || clusterFilter || conditions.length || filter !== "all" || favoritesOnly)
  const defaultOpen = (bucket: ApplicationBucket<WorkspaceApplication>) =>
    groupBy === "none" || filtering || buckets.length === 1 || (groupBy === "status" ? bucket.key !== "synced" : bucket.applications.some(needsAttention) || (clusters.get(bucket.key)?.health.level ?? "healthy") !== "healthy")
  const resetFilters = () => { setQuery(""); setFilter("all"); setClusterFilter(""); setFilterQuery(createFilterQuery()); setFavoritesOnly(false) }

  const viewMenu = <ActionMenu
    variant="outline"
    icon={SavedViewIcon}
    label={<><span className="sr-only">Saved views: </span><span className="max-w-40 truncate">{activeSavedView?.name ?? "Views"}</span>{viewChanged && <span role="status" aria-label="Unsaved view changes" className="size-1.5 rounded-full bg-primary" />}</>}
    items={[
      { label: "Default view", icon: activeViewId ? undefined : Tick02Icon, disabled: !preferencesReady || viewBusy, onSelect: () => void selectSavedView("default") },
      ...savedViews.map(saved => ({ label: `View: ${saved.name}`, icon: saved.id === activeViewId ? Tick02Icon : undefined, disabled: viewBusy, onSelect: () => void selectSavedView(saved.id) })),
      { label: "Save as new view…", icon: Add01Icon, separatorBefore: true, disabled: !preferencesReady || viewBusy || (groupBy === "label" && !labelKey), onSelect: () => { setViewName(""); setViewError(""); setSaveDialog(true) } },
      ...(activeSavedView ? [
        { label: "Update current view", disabled: viewBusy || !viewChanged, onSelect: () => void saveView(false) },
        { label: "Discard view changes", disabled: viewBusy || !viewChanged, onSelect: () => applyView(activeSavedView.configuration) },
        { label: "Delete saved view", disabled: viewBusy, destructive: true, confirm: { title: `Delete view “${activeSavedView.name}”?`, description: "Your applications and favorites will be kept.", confirmLabel: "Delete view", onConfirm: deleteView } },
      ] : []),
    ]}
  />

  if (!preferencesReady && !preferencesError) return <>{renderHeader?.()}<CollectionSkeleton /></>

  if (!available.length)
    return (
      <>{renderHeader?.()}<div className="rounded-xl border border-dashed bg-card">
        <EmptyState
          title="Your next deployment starts here"
          description={`Connect a Git repository and a Kubernetes target to bring your first application online.${canCreate ? "" : " Ask a workspace owner to create one."}`}
          href={canCreate ? createHref : undefined}
          action="Create application"
        />
      </div></>
    )

  const editLabelsFor = (app: WorkspaceApplication) => canManageApp(app) ? () => setEditingLabels(app) : undefined
  const listColumns = (showCluster: boolean): GridColumn<WorkspaceApplication>[] => [
    { id: "favorite", title: "Favorite", size: 64, cell: app => <FavoriteButton name={app.name} favorite={favoriteIds.includes(app.id)} busy={!preferencesReady || favoriteBusy.includes(app.id)} onClick={() => void toggleFavorite(app)} /> },
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
    { id: "labels", title: "Labels", size: 200, cell: app => { const text = Object.entries(app.labels ?? {}).sort(([a], [b]) => a.localeCompare(b)).map(([key, value]) => `${key}=${value}`).join(" · "); return <span className="block truncate text-xs text-muted-foreground" title={text}>{text || "—"}</span> } },
    { id: "target", title: "Namespace", size: 150, cell: (app) => <span className="block truncate text-xs">{app.namespaces.map((item) => item.namespace).join(", ") || "—"}</span> },
    { id: "revision", title: "Revision", size: 120, cell: (app) => <span className="block truncate font-mono text-xs">{app.revision}</span> },
    {
      id: "checked", title: "Checked", size: 110,
      cell: (app) => <span className={`text-xs ${isCheckStalled(app, now) ? "font-medium text-warning-foreground dark:text-warning" : "text-muted-foreground"}`} title={app.lastCheckedAt ? new Date(app.lastCheckedAt).toLocaleString() : undefined}>{ago(app.lastCheckedAt, now)}</span>,
    },
    { id: "policy", title: "Sync policy", size: 110, cell: (app) => <span className="text-xs">{app.autoSyncPaused ? "paused" : app.syncPolicy}</span> },
    { id: "actions", title: "", size: 64, cell: (app) => <ApplicationActions app={app} canManage={canManageApp(app)} onDeleted={handleDeleted} onEditLabels={editLabelsFor(app)} /> },
  ]

  const renderBucket = (bucket: ApplicationBucket<WorkspaceApplication>) => {
    const health = groupBy === "cluster" ? clusters.get(bucket.key)?.health : undefined
    const blockedIds = new Set(health?.level === "critical" ? health.affectedApplications.map((app) => app.id) : [])
    const body = view === "cards" ? (
      <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,300px),1fr))] gap-3">
        {bucket.applications.map((app) => (
          <ApplicationCard
            key={app.id} app={app} canManage={canManageApp(app)} onDeleted={handleDeleted} now={now}
            favorite={favoriteIds.includes(app.id)} favoriteBusy={!preferencesReady || favoriteBusy.includes(app.id)} onFavorite={() => void toggleFavorite(app)} onEditLabels={editLabelsFor(app)}
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
    <>
      {renderHeader?.(viewMenu)}
      <div>
      {preferencesError && <div role="alert" className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-warning/30 p-3 text-sm"><p>Favorites and saved views could not be loaded.</p><Button variant="outline" size="sm" onClick={() => setPreferencesVersion(current => current + 1)}>Retry preferences</Button></div>}
      <ConnectionDialog open={saveDialog} onOpenChange={setSaveDialog} title="Save application view" description="Save your grouping, filters, sort order and layout. This view is personal and will reopen when you return." busy={viewBusy} size="sm">
        <form className="space-y-4" onSubmit={event => { event.preventDefault(); void saveView(true) }}><FormField label="View name" htmlFor="application-view-name"><Input id="application-view-name" value={viewName} maxLength={80} placeholder="Production by team" autoFocus required disabled={viewBusy} onChange={event => setViewName(event.target.value)} /></FormField>{viewError && <p role="alert" className="text-sm text-destructive">{viewError}</p>}<DialogFooter><DialogCancel disabled={viewBusy} /><Button type="submit" loading={viewBusy} disabled={!viewName.trim()}>Save view</Button></DialogFooter></form>
      </ConnectionDialog>
      {editingLabels && <ApplicationLabelsDialog key={editingLabels.id} application={editingLabels} onClose={() => setEditingLabels(null)} onSaved={labels => onApplicationUpdated?.({ ...editingLabels, labels })} />}
      <section aria-label="Filter applications" className="mb-3 rounded-xl border bg-card">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b px-4 py-3 sm:px-5">
          <div className="max-w-full overflow-x-auto">
          <ToggleGroup className="w-max" aria-label="Filter by health" value={[filter]} onValueChange={(value) => { if (value[0]) setFilter(value[0]) }}>
            {([["all", "All apps"], ["attention", "Needs attention"], ["synced", "In sync"], ["other", "Other"], ["paused", "Reconciliation paused"]] as const).map(([value, label]) => (
              <ToggleGroupItem key={value} value={value}>
                {label}
                <ToggleGroupCount>{counts[value]}</ToggleGroupCount>
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          </div>
          <Button variant={favoritesOnly ? "secondary" : "ghost"} size="sm" aria-pressed={favoritesOnly} disabled={!preferencesReady} onClick={() => setFavoritesOnly(current => !current)}><HugeiconsIcon icon={StarIcon} aria-hidden="true" className={favoritesOnly ? "fill-current" : ""} />Favorites <span className="tabular-nums">{counts.favorites}</span></Button>
        </div>
        <div className="flex flex-wrap items-center gap-3 px-4 py-3 sm:px-5">
          <div className="relative w-full min-w-0 sm:w-auto sm:min-w-56 sm:flex-1 lg:max-w-sm">
            <WorkspaceIcon name="search" className="pointer-events-none absolute top-2.5 left-3 size-4 text-muted-foreground" />
            <Input aria-label="Search applications" placeholder="Search applications, labels, clusters…" value={query} onChange={(event) => setQuery(event.target.value)} className="h-9 bg-background pl-9" />
          </div>
          {clusters.size > 1 && <ApplicationClusterFilter
            clusters={[...clusters.values()].sort((a, b) => levelRank[a.health.level] - levelRank[b.health.level] || a.name.localeCompare(b.name))}
            value={clusterFilter}
            onValueChange={setClusterFilter}
          />}
          <Filters fields={filterFields} query={filterQuery} onQueryChange={setFilterQuery} size="sm" className="min-w-0 flex-1" />
          <FormSelect
            ariaLabel="Group applications"
            value={groupBy}
            onValueChange={(value) => { setGroupBy(value as GroupMode); setOpenGroups({}); if (value === "label" && !labelKey) setLabelKey(labelKeys[0] ?? "") }}
            className="w-44 shrink-0"
            items={[
              { value: "cluster", label: "Group: Cluster" },
              { value: "namespace", label: "Group: Namespace" },
              { value: "status", label: "Group: Status" },
              { value: "label", label: "Group: Label" },
              { value: "none", label: "No grouping" },
            ]}
          />
          {groupBy === "label" && <FormSelect ariaLabel="Group by label key" value={labelKey} onValueChange={value => { setLabelKey(value); setOpenGroups({}) }} placeholder="Choose a label key" className="w-44" items={labelKeys.map(key => ({ value: key, label: key }))} />}
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
    </>
  )
}
