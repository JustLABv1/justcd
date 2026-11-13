"use client"

import Link from "next/link"
import { ProfileVersionBadge } from "@/components/applications/profile-version-notice"
import { HugeiconsIcon } from "@hugeicons/react"
import { FilterHorizontalIcon, SlidersHorizontalIcon, StarIcon, Tag01Icon, ViewIcon as SavedViewIcon, Tick02Icon, Add01Icon } from "@hugeicons/core-free-icons"
import { ApplicationLabelsDialog } from "@/components/application-labels-dialog"
import { ConnectionDialog } from "@/components/connection-dialog"
import { DialogCancel, DialogFooter } from "@/components/app-dialog"
import { FormField } from "@/components/ui-kit"
import { applicationViewSignature } from "@/lib/application-views"
import type { ApplicationPreferences, ApplicationSavedView, ApplicationViewConfig } from "@/lib/application-views"
import { useRouter } from "next/navigation"
import { useEffect, useMemo, useState, type ReactNode } from "react"
import { Card } from "@/components/panelui/card"
import { Alert } from "@/components/panelui/alert"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Input } from "@/components/panelui/input"
import { Tabs } from "@/components/panelui/tabs"
import { Chip } from "@/components/panelui/chip"
import { Menu } from "@/components/panelui/menu"
import { Sheet } from "@/components/panelui/sheet"
import { useMediaQuery } from "@/hooks/use-media-query"
import { PanelSelect } from "@/components/applications/panel-wizard"
import { applicationCardStatus, badgeLabel, type CardStatus } from "@/lib/application-card-status"
import { attentionTab, tabActionLabel } from "@/lib/application-status"
import type { ApplicationSyncActivity } from "@/lib/sync-history"
import { Filters } from "@/components/reui/filters/filters"
import { createFilterQuery } from "@/components/reui/filters/filters-query"
import { applicationFilterFieldIds, applicationFilterFields, readApplicationField } from "@/components/application-filter-fields"
import { countActiveConditions, matchesFilterQuery } from "@/lib/filter-eval"
import type { FilterQuery } from "@/components/reui/filters/filters-types"
import { DataGridList, type GridColumn } from "@/components/data-grid-table"
import { ActionMenu, RowActions, type ActionItem } from "@/components/action-menu"
import { CollapsibleGroup } from "@/components/ui/collapsible"
import { ApplicationClusterFilter } from "@/components/application-cluster-filter"
import { Alert02Icon, Delete02Icon, Edit02Icon, ViewIcon } from "@hugeicons/core-free-icons"
import { EmptyState } from "@/components/ui-kit"
import { WorkspaceIcon, CollectionSkeleton } from "@/components/workspace-ui"
import { tone } from "@/components/health-tone"
import { useToast } from "@/components/toast-provider"
import {
  isApplicationHealthy,
  needsAttention,
  type WorkspaceApplication,
} from "@/hooks/use-workspace"
import type { ClusterStatus } from "@/hooks/use-cluster-statuses"
import { groupApplications, type ApplicationBucket, type GroupMode, type StatusBucket } from "@/lib/application-grouping"
import { clusterHealth, isCheckStalled, type ConnectionHealth, type HealthLevel } from "@/lib/connection-health"
import { ago } from "@/lib/relative-time"
import type { Workspace } from "@/lib/types"
import { api, apiPost, errorMessage } from "@/lib/api"

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
          <PanelSelect ariaLabel={`Managed cluster resources for ${app.name}`} value={policy} onValueChange={setPolicy} items={[{ value: "keep", label: "Keep resources in Kubernetes" }, { value: "delete", label: "Delete through a reviewed plan" }]} />
          {policy === "delete" && <p className="mt-2 text-sm text-muted-foreground">Resources are not deleted immediately. Review and approve the deletion plan on the application page.</p>}
        </>,
      },
    })
  }
  return <RowActions label={app.name} items={items} />
}

function FavoriteButton({ name, favorite, busy, onClick }: { name: string; favorite: boolean; busy?: boolean; onClick: () => void }) {
  return <Button type="button" variant="ghost" size="icon-sm" aria-label={`${favorite ? "Remove" : "Add"} ${name} ${favorite ? "from" : "to"} favorites`} aria-pressed={favorite} disabled={busy} onClick={onClick} title={favorite ? "Remove from favorites" : "Add to favorites"}><HugeiconsIcon icon={StarIcon} strokeWidth={1.8} aria-hidden="true" className={favorite ? "fill-foreground text-foreground" : "text-muted-foreground"} /></Button>
}

const checkSources = (app: WorkspaceApplication) => app.statusIssues.map((issue) => issue.source === "git" ? "Git" : issue.source === "cluster" ? "Cluster" : issue.source).join(" + ")

const classify = (app: WorkspaceApplication): StatusBucket => needsAttention(app) ? "attention" : isApplicationHealthy(app) ? "synced" : "other"
const levelRank: Record<HealthLevel, number> = { critical: 0, warning: 1, unknown: 2, healthy: 3 }
// Publishing metadata has dedicated profile badges; keep raw IDs out of summaries.
function summaryLabels(app: WorkspaceApplication) {
  return Object.entries(app.labels ?? {}).filter(([key]) => key !== "justcd.io/published" && key !== "justcd.io/publishing-profile").sort(([a], [b]) => a.localeCompare(b))
}

const groupLabels: Record<GroupMode, string> = { cluster: "Cluster", namespace: "Namespace", status: "Status", label: "Label", none: "No grouping" }
const sortLabels: Record<string, string> = { attention: "Attention first", name: "Name A–Z", newest: "Newest first" }

const rendererLabel = { helm: "Helm", kustomize: "Kustomize", yaml: "Plain YAML" } as const

function Dot({ level }: { level: HealthLevel }) {
  return <span aria-hidden="true" className={`inline-block size-2 shrink-0 rounded-full ${tone[level].dot}`} />
}

const badgeVariant = { success: "success", destructive: "destructive", warning: "warning", neutral: "secondary" } as const
const runDot: Record<string, string> = { succeeded: "bg-success", failed: "bg-destructive", cancelled: "bg-warning", running: "bg-info", queued: "bg-info" }

/** The latest runs as dots, oldest on the left, so a flapping application is visible at a glance. */
function RecentRuns({ runs }: { runs: string[] }) {
  const ordered = [...runs].reverse()
  const failed = runs.filter((run) => run === "failed").length
  return <span role="img" aria-label={`Last ${runs.length} run${runs.length === 1 ? "" : "s"}: ${runs.length - failed} without failure, ${failed} failed`} className="flex items-center gap-0.5">
    {ordered.map((run, index) => <span key={index} className={`size-1.5 rounded-full ${runDot[run] ?? "bg-muted-foreground/40"}`} />)}
  </span>
}

export function ApplicationCard({
  app, canManage, onDeleted, showCluster = true, clusterLevel, blockedBy, now, favorite, favoriteBusy, onFavorite, onEditLabels, activity, hideLabelKey, quietEdge = false,
}: {
  app: WorkspaceApplication; canManage?: boolean; onDeleted?: (id: string) => void
  favorite?: boolean; favoriteBusy?: boolean; onFavorite?: () => void; onEditLabels?: () => void
  showCluster?: boolean; clusterLevel?: HealthLevel; blockedBy?: ConnectionHealth; now?: number
  /** Latest runs and deploy time, from the workspace sync history. */
  activity?: ApplicationSyncActivity
  /** The label the list is grouped by; its value is already the group heading. */
  hideLabelKey?: string
  /** Every card in the group shares this state and the group header says so: drop the coloured edge. */
  quietEdge?: boolean
}) {
  const [showAllBadges, setShowAllBadges] = useState(false)
  const labels = summaryLabels(app).filter(([key]) => key !== hideLabelKey)
  const namespaces = app.namespaces.map((item) => item.namespace).join(", ") || "No namespace"
  const revision = /^[a-f0-9]{40}$|^[a-f0-9]{64}$/i.test(app.revision) ? app.revision.slice(0, 8) : app.revision
  // Grouped by cluster, the header already names the outage: the card only says that it is the cause.
  const status = applicationCardStatus(app, { blocked: Boolean(blockedBy), blockedLabel: showCluster ? blockedBy?.label : undefined })
  const reason = status.reason
  const stalled = isCheckStalled(app, now)
  const attention = needsAttention(app) || stalled
  const tab = attentionTab(app)
  const edge = quietEdge ? null : status.edge
  const clusterName = app.clusterName || app.clusterId
  return (
    <Card as="article" size="sm" className={`group/app min-w-0 ${edge === "destructive" ? "border-destructive/50 dark:border-destructive/50" : edge === "warning" ? "border-warning/50 dark:border-warning/50" : ""}`}><Card.Content className="flex h-full flex-col">
      <header className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="line-clamp-2 break-words text-base leading-snug font-semibold tracking-tight" title={app.name}>
            <Link href={`/applications/${app.id}`} className="rounded-sm underline-offset-4 hover:underline">{app.name}</Link>
          </h3>
          <p className="mt-0.5 truncate text-sm text-muted-foreground" title={namespaces}>{namespaces}</p>
        </div>
        <div className="-mr-1 flex shrink-0 items-center">
          {/* The star is clutter on every card: shown on hover or focus, or when it is already a favourite. Always shown without a hover pointer. */}
          {onFavorite && <span className={favorite ? "" : "[@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover/app:opacity-100 [@media(hover:hover)]:focus-within:opacity-100"}><FavoriteButton name={app.name} favorite={Boolean(favorite)} busy={favoriteBusy} onClick={onFavorite} /></span>}
          <ApplicationActions app={app} canManage={Boolean(canManage)} onDeleted={onDeleted} onEditLabels={onEditLabels} />
        </div>
      </header>
      {/* The cluster gets a line of its own: cut down to "DEMO ONLY - u…" it could not explain anything. */}
      {showCluster && <p className="mt-1 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground" title={`${clusterName} (${app.clusterId})`}><Dot level={clusterLevel ?? "unknown"} /><span className="min-w-0 truncate">{clusterName}</span></p>}
      <div className="mt-3 flex flex-wrap items-center gap-1.5">
        <Badge variant={badgeVariant[status.primary.tone]} size="sm" dot title={status.primary.title}>{badgeLabel(status.primary)}</Badge>
        {/* The rest are a click away, on the card itself: a hover tooltip hid them from touch and from anyone who did not think to hover. */}
        {showAllBadges && status.others.map((item) => <Badge key={item.key} variant={badgeVariant[item.tone]} size="sm" dot={item.tone === "success"} title={item.title}>{badgeLabel(item)}</Badge>)}
        {status.others.length > 0 && <button type="button" aria-expanded={showAllBadges} aria-label={showAllBadges ? "Show fewer states" : `Show ${status.others.length} more state${status.others.length === 1 ? "" : "s"}: ${status.others.map(badgeLabel).join(", ")}`} onClick={() => setShowAllBadges((current) => !current)} className="inline-flex rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <Badge variant="secondary" size="sm" className="cursor-pointer hover:bg-secondary/80">{showAllBadges ? "Show less" : `+${status.others.length}`}</Badge>
        </button>}
        <ProfileVersionBadge application={app} />
      </div>
      {labels.length > 0 && <div aria-label="Application labels" className="mt-2 flex flex-wrap gap-1">{labels.map(([key, value]) => <Badge key={key} variant="outline" size="sm" className="max-w-full truncate" title={`${key}=${value}`}>{key}{value && <>: {value}</>}</Badge>)}</div>}
      {reason && (
        <p className="mt-2.5 flex items-start gap-2 text-sm" title={app.statusIssues?.map((issue) => issue.summary).join("\n") || reason}>
          <HugeiconsIcon icon={Alert02Icon} strokeWidth={2} aria-hidden="true" className={`mt-0.5 size-4 shrink-0 ${status.signal === "destructive" ? "text-destructive" : status.signal === "warning" ? "text-warning" : "text-muted-foreground"}`} />
          <span className="line-clamp-2 min-w-0">{reason}{!blockedBy && app.statusIssues?.length > 0 && <> · <span className="text-muted-foreground">{checkSources(app)} check failed</span></>}</span>
        </p>
      )}
      {/* The divider anchors the footer when a card stretches to the tallest card in its row. */}
      <footer className="mt-auto flex items-end justify-between gap-3 pt-4"><div className="flex w-full min-w-0 items-end justify-between gap-3 border-t pt-3">
        <div className="min-w-0 space-y-1 text-xs text-muted-foreground">
          {activity && (activity.recent?.length ?? 0) > 0 && <div className="flex items-center gap-2">
            <RecentRuns runs={activity.recent!} />
            <span className="truncate" title={activity.deployedAt ? new Date(activity.deployedAt).toLocaleString() : undefined}>{activity.deployedAt ? `Deployed ${ago(activity.deployedAt, now)}` : "Never deployed"}</span>
          </div>}
          <p className="truncate" title={`${app.revision} · ${app.manifestPath || "."} · ${app.syncPolicy}`}>
            <span className="font-mono">{revision}</span> · {rendererLabel[app.renderer]} · {app.autoSyncPaused ? "paused" : app.syncPolicy}
          </p>
          <p className={stalled ? "font-medium text-warning-foreground dark:text-warning" : ""} title={app.lastCheckedAt ? new Date(app.lastCheckedAt).toLocaleString() : undefined}>
            {/* Paused applications are not polled, so an old check is expected; the paused badge carries the signal. */}
            {stalled ? "Checks stalled · last " : app.autoSyncPaused && !app.decommissioning ? "Not checked while paused · last " : "Checked "}{ago(app.lastCheckedAt, now)}
          </p>
        </div>
        {/* Blocked by its cluster, the fix is not on this application: the outage alert above the groups links to it once. */}
        {!(blockedBy && !status.needsDecision) && attention && tab !== "overview" && <Button size="sm" variant="outline" className="shrink-0" nativeButton={false} render={<Link href={`/applications/${app.id}?tab=${tab}`} />}>{tabActionLabel[tab]}</Button>}
      </div></footer>
    </Card.Content></Card>
  )
}

/** Down, waiting for a person, or to check: one word per application, shared by the chips and the group headers. */
function statusKind(app: WorkspaceApplication, status: CardStatus): "down" | "waiting" | "check" | "synced" | "other" {
  if (status.needsDecision) return "waiting"
  if (status.edge === "destructive") return "down"
  if (needsAttention(app)) return "check"
  return isApplicationHealthy(app) ? "synced" : "other"
}

function GroupHeader({ label, bucket, mode, health, open, statusOf, sharedCause }: {
  label: string; bucket: ApplicationBucket<WorkspaceApplication>; mode: GroupMode; health?: ConnectionHealth
  /** Omit for a group that cannot collapse. */
  open?: boolean
  statusOf: (app: WorkspaceApplication) => CardStatus
  /** One cause behind the group's problems (an unreachable cluster), said here once. */
  sharedCause?: string
}) {
  const apps = bucket.applications
  // Down, waiting for a person, or to check: the same words and colours as the cards inside.
  const counts = { synced: 0, down: 0, waiting: 0, check: 0, other: 0 }
  for (const app of apps) counts[statusKind(app, statusOf(app))]++
  const worst = counts.down ? "destructive" : counts.waiting || counts.check ? "warning" : null
  // Only a cluster has a health of its own; otherwise the group takes its worst application's colour.
  const level: HealthLevel = health?.level ?? (worst === "destructive" ? "critical" : worst === "warning" ? "warning" : "unknown")
  const summary = [
    counts.down && <span key="down" className="text-destructive">{counts.down} down</span>,
    counts.waiting && <span key="waiting" className="text-warning-foreground dark:text-warning">{counts.waiting} waiting for review</span>,
    counts.check && <span key="check" className="text-warning-foreground dark:text-warning">{counts.check} to check</span>,
  ].filter(Boolean)
  return (
    <span className="flex w-full min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 text-left">
      <span aria-hidden="true" className={`grid size-8 shrink-0 place-items-center rounded-lg ${tone[level].bg} ${tone[level].text}`}>
        <WorkspaceIcon name={mode === "cluster" ? "server" : mode === "namespace" ? "folder" : "app"} className="size-4" />
      </span>
      <span className="min-w-0 flex-1 basis-56">
        <span className="flex flex-wrap items-center gap-x-2">
          <span className="break-words text-sm font-semibold">{label}</span>
          {health && <span className={`text-xs font-medium ${tone[health.level].text}`}><span className="sr-only">{health.level}: </span>{health.label}</span>}
        </span>
        {/* A cluster that is down is explained by the alert above the groups. */}
        {health && health.level !== "healthy" && health.level !== "critical" ? (
          <span className="mt-0.5 block text-xs text-muted-foreground">
            {health.detail}{health.affectedApplications.length > 0 && ` ${health.affectedApplications.length} application${health.affectedApplications.length === 1 ? "" : "s"} affected.`}
          </span>
        ) : sharedCause ? <span className="mt-0.5 block text-xs text-muted-foreground">{sharedCause}</span> : null}
      </span>
      {/* On a phone the counts take their own line, aligned under the title rather than under the icon. */}
      <span className="flex w-full shrink-0 items-center gap-3 pl-11 sm:w-auto sm:pl-0">
        <span role="img" aria-label={`${counts.synced} healthy, ${counts.down} down, ${counts.waiting} waiting for review, ${counts.check} to check, ${counts.other} other`} className="flex h-1.5 w-24 gap-px overflow-hidden rounded-full bg-foreground/10">
          {([["bg-success", counts.synced], ["bg-destructive", counts.down], ["bg-warning", counts.waiting + counts.check], ["bg-muted-foreground/40", counts.other]] as const).map(([color, n]) => n > 0 && <span key={color} className={color} style={{ width: `${(n / apps.length) * 100}%` }} />)}
        </span>
        <span className="text-xs tabular-nums text-muted-foreground">{apps.length} app{apps.length === 1 ? "" : "s"}{summary.length > 0 && <> · {summary.flatMap((part, index) => index ? [", ", part] : [part])}</>}</span>
      </span>
      {open !== undefined && <span className="sr-only">{open ? "Collapse" : "Expand"} {label}</span>}
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
  activity,
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
  /** Latest runs per application id, from the workspace sync history. */
  activity?: Record<string, ApplicationSyncActivity>
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
  const [filtersOpen, setFiltersOpen] = useState(false)
  // Bulk actions in the list view: the ids of the checked rows.
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => new Set())
  const [bulkBusy, setBulkBusy] = useState("")
  const [bulkLabelOpen, setBulkLabelOpen] = useState(false)
  const [bulkLabel, setBulkLabel] = useState({ key: "", value: "" })
  const wide = useMediaQuery("(min-width: 640px)")
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
  const filterFields = useMemo(() => applicationFilterFields(available, workspaces), [available, workspaces])
  const activeConditions = countActiveConditions(filterQuery, applicationFilterFieldIds)
  /** The unreachable cluster behind this application's state, whatever the list is grouped by. */
  const blockedByCluster = (app: WorkspaceApplication) => {
    const state = clusters.get(app.clusterId)?.health
    return state?.level === "critical" && state.affectedApplications.some((item) => item.id === app.id) ? state : undefined
  }
  const statusOf = (app: WorkspaceApplication) => { const blocked = blockedByCluster(app); return applicationCardStatus(app, { blocked: Boolean(blocked), blockedLabel: blocked?.label }) }
  const kindOf = (app: WorkspaceApplication) => statusKind(app, statusOf(app))
  const kinds = new Map(available.map((app) => [app.id, kindOf(app)]))
  const counts = {
    all: available.length,
    down: available.filter((app) => kinds.get(app.id) === "down").length,
    waiting: available.filter((app) => kinds.get(app.id) === "waiting").length,
    check: available.filter((app) => kinds.get(app.id) === "check").length,
    favorites: available.filter(app => favoriteIds.includes(app.id)).length,
    attention: available.filter(needsAttention).length,
    synced: available.filter(isApplicationHealthy).length,
    paused: available.filter((app) => app.autoSyncPaused).length,
    other: available.filter((app) => kinds.get(app.id) === "other").length,
  }
  const visible = available
    .filter((app) => {
      const matchesStatus =
        filter === "all" ||
        (filter === "attention" ? needsAttention(app)
          : filter === "down" || filter === "waiting" || filter === "check" ? kinds.get(app.id) === filter
          : filter === "synced" ? isApplicationHealthy(app)
          : filter === "paused" ? app.autoSyncPaused
          : kinds.get(app.id) === "other")
      return (
        matchesStatus &&
        (!favoritesOnly || favoriteIds.includes(app.id)) &&
        (!clusterFilter || app.clusterId === clusterFilter) &&
        (!activeConditions || matchesFilterQuery(filterQuery, (field) => readApplicationField(app, field))) &&
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
  const filtering = Boolean(query.trim() || clusterFilter || activeConditions > 0 || filter !== "all" || favoritesOnly)
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
      <>{renderHeader?.()}<div className="min-w-0">
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
        const blocked = blockedByCluster(app)
        const status = applicationCardStatus(app, { blocked: Boolean(blocked), blockedLabel: blocked?.label })
        const reason = status.reason
        return (
          <div className="space-y-1.5">
            <div className="flex flex-wrap items-center gap-1.5">
              {status.badges.map((item) => <Badge key={item.key} variant={badgeVariant[item.tone]} size="sm" dot={item.tone !== "neutral"} title={item.title}>{badgeLabel(item)}</Badge>)}
              <ProfileVersionBadge application={app} />
            </div>
            {reason && <p className="line-clamp-2 text-xs text-muted-foreground" title={reason}>{reason}</p>}
          </div>
        )
      },
    },
    ...(showCluster ? [{
      id: "cluster", title: "Cluster", size: 160,
      cell: (app: WorkspaceApplication) => <span className="flex items-center gap-1.5 truncate text-xs font-medium" title={`${app.clusterName || app.clusterId} (${app.clusterId})`}><Dot level={clusters.get(app.clusterId)?.health.level ?? "unknown"} />{app.clusterName || app.clusterId}</span>,
    }] : []),
    { id: "labels", title: "Labels", size: 200, cell: app => { const text = summaryLabels(app).map(([key, value]) => `${key}=${value}`).join(" · "); return <span className="block truncate text-xs text-muted-foreground" title={text}>{text || "—"}</span> } },
    { id: "target", title: "Namespace", size: 150, cell: (app) => <span className="block truncate text-xs">{app.namespaces.map((item) => item.namespace).join(", ") || "—"}</span> },
    { id: "revision", title: "Revision", size: 120, cell: (app) => <span className="block truncate font-mono text-xs">{app.revision}</span> },
    {
      id: "checked", title: "Checked", size: 110,
      cell: (app) => <span className={`text-xs ${isCheckStalled(app, now) ? "font-medium text-warning-foreground dark:text-warning" : "text-muted-foreground"}`} title={app.lastCheckedAt ? new Date(app.lastCheckedAt).toLocaleString() : undefined}>{ago(app.lastCheckedAt, now)}</span>,
    },
    { id: "policy", title: "Sync policy", size: 110, cell: (app) => <span className="text-xs">{app.autoSyncPaused ? "paused" : app.syncPolicy}</span> },
    { id: "actions", title: "", size: 64, cell: (app) => <ApplicationActions app={app} canManage={canManageApp(app)} onDeleted={handleDeleted} onEditLabels={editLabelsFor(app)} /> },
  ]

  // An outage is said once, above the groups, with the one action that fixes it.
  const unreachable = [...clusters.values()].filter((cluster) => cluster.health.level === "critical" && visible.some((app) => app.clusterId === cluster.id && blockedByCluster(app)))

  const canDeployApp = (app: WorkspaceApplication) => canManageApp(app) || Boolean(workspaces?.some((workspace) => workspace.id === app.workspaceId && workspace.role === "deployer")) || workspaceRole === "deployer"
  // Only what is checked and still on screen: a filter change never acts on rows the reader cannot see.
  const selectedApps = view === "list" ? visible.filter((app) => selectedIds.has(app.id)) : []
  const bulk = {
    pause: selectedApps.filter((app) => canManageApp(app) && !app.autoSyncPaused && !app.decommissioning),
    // A rollback pin that needs a revision to resume is a decision per application, made on its own page.
    resume: selectedApps.filter((app) => canManageApp(app) && app.autoSyncPaused && !app.decommissioning && !app.rollbackResumeRequiresRevision),
    plan: selectedApps.filter((app) => app.decommissioning || app.configurationMissing ? canManageApp(app) : canDeployApp(app)),
    label: selectedApps.filter((app) => canManageApp(app) && !app.repositoryConfigurationId && !app.branchTest),
  }
  async function runBulk(kind: string, apps: WorkspaceApplication[], call: (app: WorkspaceApplication) => Promise<WorkspaceApplication | void>, done: string) {
    setBulkBusy(kind)
    const results = await Promise.allSettled(apps.map(call))
    setBulkBusy("")
    results.forEach((result) => { if (result.status === "fulfilled" && result.value) onApplicationUpdated?.(result.value) })
    const failed = results.flatMap((result, index) => result.status === "rejected" ? [`${apps[index].name}: ${errorMessage(result.reason)}`] : [])
    const skipped = selectedApps.length - apps.length
    const note = skipped ? ` ${skipped} skipped: not allowed or not applicable.` : ""
    if (failed.length) toast.error(`${done} ${apps.length - failed.length} of ${apps.length}. ${failed.join(" · ")}${note}`)
    else toast.success(`${done} ${apps.length} application${apps.length === 1 ? "" : "s"}.${note}`)
    if (!failed.length) setSelectedIds(new Set())
  }
  const appPath = (app: WorkspaceApplication) => `/api/v1/applications/${encodeURIComponent(app.id)}`
  const bulkBar = selectedApps.length > 0 && (
    <div role="region" aria-label="Bulk actions" className="pointer-events-none fixed inset-x-0 bottom-4 z-30 flex justify-center px-4 lg:pl-[272px]">
      <Card size="sm" className="pointer-events-auto max-w-full shadow-lg"><Card.Content className="flex flex-wrap items-center gap-2">
        <span className="me-1 text-sm font-medium tabular-nums">{selectedApps.length} selected</span>
        <Button size="sm" variant="outline" disabled={!bulk.plan.length || Boolean(bulkBusy)} loading={bulkBusy === "plan"} onClick={() => void runBulk("plan", bulk.plan, async (app) => { await apiPost(`${appPath(app)}/plans`) }, "Plan refresh started for")}>Refresh plans</Button>
        <Button size="sm" variant="outline" disabled={!bulk.label.length || Boolean(bulkBusy)} loading={bulkBusy === "label"} onClick={() => { setBulkLabel({ key: "", value: "" }); setBulkLabelOpen(true) }}>Add label</Button>
        <ActionMenu label="Auto-sync" size="sm" items={[
          { label: `Pause auto-sync (${bulk.pause.length})`, disabled: !bulk.pause.length || Boolean(bulkBusy), onSelect: () => void runBulk("pause", bulk.pause, (app) => apiPost<WorkspaceApplication>(`${appPath(app)}/pause`).then((updated) => ({ ...app, ...updated })), "Auto-sync paused for") },
          { label: `Resume auto-sync (${bulk.resume.length})`, disabled: !bulk.resume.length || Boolean(bulkBusy), confirm: {
            title: `Resume auto-sync for ${bulk.resume.length} application${bulk.resume.length === 1 ? "" : "s"}?`,
            description: "JustCD will reconcile them with Git again. Manual changes made while they were paused may be overwritten.",
            confirmLabel: "Resume auto-sync",
            onConfirm: () => runBulk("resume", bulk.resume, (app) => apiPost<WorkspaceApplication>(`${appPath(app)}/rollback-state`, { action: "resume", revision: "" }).then((updated) => ({ ...app, ...updated })), "Auto-sync resumed for"),
          } },
        ]} />
        <Button size="sm" variant="ghost" onClick={() => setSelectedIds(new Set())}>Clear</Button>
      </Card.Content></Card>
    </div>
  )

  const renderBucket = (bucket: ApplicationBucket<WorkspaceApplication>) => {
    const health = groupBy === "cluster" ? clusters.get(bucket.key)?.health : undefined
    // One unreachable cluster behind every blocked application in the group: say it in the header once,
    // and when it covers the whole group, let the header carry the colour instead of every card's edge.
    const blockedClusters = new Set(bucket.applications.map((app) => blockedByCluster(app) ? app.clusterId : null).filter(Boolean))
    const blockedCount = bucket.applications.filter((app) => blockedByCluster(app)).length
    const sharedClusterId = blockedClusters.size === 1 ? [...blockedClusters][0]! : undefined
    const sharedCause = sharedClusterId && groupBy !== "cluster" ? `${blockedCount} of ${bucket.applications.length} blocked by ${clusters.get(sharedClusterId)?.name ?? sharedClusterId}.` : undefined
    const quietEdge = bucket.applications.length > 1 && Boolean(sharedClusterId) && blockedCount === bucket.applications.length
    const body = view === "cards" ? (
      <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,300px),1fr))] gap-3">
        {bucket.applications.map((app) => (
          <ApplicationCard
            key={app.id} app={app} canManage={canManageApp(app)} onDeleted={handleDeleted} now={now}
            favorite={favoriteIds.includes(app.id)} favoriteBusy={!preferencesReady || favoriteBusy.includes(app.id)} onFavorite={() => void toggleFavorite(app)} onEditLabels={editLabelsFor(app)}
            showCluster={groupBy !== "cluster"} clusterLevel={clusters.get(app.clusterId)?.health.level}
            blockedBy={blockedByCluster(app)} activity={activity?.[app.id]} hideLabelKey={groupBy === "label" ? labelKey : undefined} quietEdge={quietEdge}
          />
        ))}
      </div>
    ) : (
      <DataGridList rows={bucket.applications} columns={listColumns(groupBy !== "cluster")} selection={{ rowId: (app) => app.id, rowLabel: (app) => app.name, selected: selectedIds, onSelectedChange: setSelectedIds }} />
    )
    if (groupBy === "none") return <div key={bucket.key}>{body}</div>
    // Every group is a tray, one group or ten: the page keeps one shape whatever the data.
    const open = openGroups[bucket.key] ?? defaultOpen(bucket)
    return (
      <CollapsibleGroup key={bucket.key} open={open} onOpenChange={(next) => setOpenGroups((current) => ({ ...current, [bucket.key]: next }))} header={<GroupHeader label={bucket.label} bucket={bucket} mode={groupBy} health={health} open={open} statusOf={statusOf} sharedCause={sharedCause} />}>
        {body}
      </CollapsibleGroup>
    )
  }

  // Everything that narrows the list, for the count on the phone's Filters button. The search has its own field.
  const narrowing = Number(filter !== "all") + Number(favoritesOnly) + Number(Boolean(clusterFilter)) + activeConditions
  const statusChips = (
    // No "All apps" chip: nothing pressed is all of them, and pressing the active chip again lets go.
    <Chip.Group multiple={false} value={filter === "all" ? [] : [filter]} onValueChange={(next) => setFilter(next[0] ?? "all")} aria-label="Filter by status" className="flex-wrap">

        {/* The same split as the group headers: what is down, what waits on a person, what to look at.
            "Needs attention" (the three together) stays reachable from links and saved views. */}
        {([["all", "All apps"], ["attention", "Needs attention"], ["down", "Down"], ["waiting", "Waiting for review"], ["check", "To check"], ["synced", "In sync"], ["paused", "Paused"], ["other", "Other"]] as const)
          // Empty buckets are noise; keep them only while they are the active filter.
          .filter(([value]) => value !== "all" && (value === "attention" ? filter === value : counts[value] > 0 || filter === value))
          .map(([value, label]) => (
            <Chip key={value} value={value}>{label} <span className="ms-1 tabular-nums">{counts[value]}</span></Chip>
          ))}

    </Chip.Group>
  )
  const favoritesChip = (
    <Chip pressed={favoritesOnly} onPressedChange={setFavoritesOnly} disabled={!preferencesReady} className="gap-1.5">
      {/* The chip draws its own tick while pressed, so the star only shows while it is off. */}
      {!favoritesOnly && <HugeiconsIcon icon={StarIcon} aria-hidden="true" className="size-3.5" />}
      Favorites{counts.favorites > 0 && <span className="tabular-nums"> {counts.favorites}</span>}
    </Chip>
  )
  const searchField = (
    <Input.Group className={wide ? "w-full min-w-48 sm:w-60 2xl:w-72" : "min-w-0 flex-1"}>
      <Input.Addon><WorkspaceIcon name="search" className="size-4" /></Input.Addon>
      <Input aria-label="Search applications" placeholder="Search applications, labels, clusters…" value={query} onChange={(event) => setQuery(event.target.value)} />
    </Input.Group>
  )
  const clusterControl = clusters.size > 1 && (
    <ApplicationClusterFilter
      clusters={[...clusters.values()].sort((a, b) => levelRank[a.health.level] - levelRank[b.health.level] || a.name.localeCompare(b.name))}
      value={clusterFilter}
      onValueChange={setClusterFilter}
    />
  )
  // On a wide screen the advanced filter is an icon beside the other view controls; the count says when it is on.
  const advancedFilter = <Filters variant="advanced" advancedMode="popover" advancedAlign={wide ? "end" : "start"} fields={filterFields} query={filterQuery} onQueryChange={setFilterQuery} size="sm"
    trigger={wide ? <Button type="button" variant="outline" size="icon" className="relative" aria-label={activeConditions ? `Advanced filter, ${activeConditions} active` : "Advanced filter"} title="Advanced filter">
      <HugeiconsIcon icon={FilterHorizontalIcon} strokeWidth={1.8} aria-hidden="true" />
      {activeConditions > 0 && <span aria-hidden="true" className="absolute -right-1 -top-1 grid size-4 place-items-center rounded-full bg-primary text-[10px] font-semibold tabular-nums text-primary-foreground">{activeConditions}</span>}
    </Button> : undefined} />
  const groupMenu = (
    <Menu>
      <Menu.Trigger render={<Button type="button" variant="outline" aria-label="Group and sort" />}>
        <HugeiconsIcon icon={SlidersHorizontalIcon} strokeWidth={1.8} aria-hidden="true" />
        <span className="hidden xl:inline">{groupLabels[groupBy]} · {sortLabels[sort]}</span>
      </Menu.Trigger>
      <Menu.Content align="end" className="min-w-56">
        <Menu.Group>
          <Menu.GroupLabel>Group by</Menu.GroupLabel>
          <Menu.RadioGroup value={groupBy} onValueChange={(value) => { setGroupBy(value as GroupMode); setOpenGroups({}); if (value === "label" && !labelKey) setLabelKey(labelKeys[0] ?? "") }}>
            {(Object.keys(groupLabels) as GroupMode[]).map((value) => <Menu.RadioItem key={value} value={value}>{groupLabels[value]}</Menu.RadioItem>)}
          </Menu.RadioGroup>
        </Menu.Group>
        {groupBy === "label" && <Menu.Group>
          <Menu.Separator />
          <Menu.GroupLabel>Label key</Menu.GroupLabel>
          <Menu.RadioGroup value={labelKey} onValueChange={(value) => { setLabelKey(String(value)); setOpenGroups({}) }}>
            {labelKeys.map((key) => <Menu.RadioItem key={key} value={key}>{key}</Menu.RadioItem>)}
          </Menu.RadioGroup>
        </Menu.Group>}
        <Menu.Separator />
        <Menu.Group>
          <Menu.GroupLabel>Sort by</Menu.GroupLabel>
          <Menu.RadioGroup value={sort} onValueChange={(value) => setSort(String(value))}>
            {Object.entries(sortLabels).map(([value, label]) => <Menu.RadioItem key={value} value={value}>{label}</Menu.RadioItem>)}
          </Menu.RadioGroup>
        </Menu.Group>
      </Menu.Content>
    </Menu>
  )
  const viewTabs = (
    <Tabs value={view} onValueChange={(value) => { if (value === "cards" || value === "list") setView(value) }}>
      <Tabs.List aria-label="Application view">
        <Tabs.Trigger value="cards">Cards</Tabs.Trigger>
        <Tabs.Trigger value="list">List</Tabs.Trigger>
      </Tabs.List>
    </Tabs>
  )

  return (
    <>
      {renderHeader?.(viewMenu)}
      <div>
      {preferencesError && <Alert tone="warning" className="mb-3"><Alert.Icon /><Alert.Title>Favorites and saved views could not be loaded.</Alert.Title><Alert.Actions><Button variant="outline" size="sm" onClick={() => setPreferencesVersion(current => current + 1)}>Retry</Button></Alert.Actions></Alert>}
      <ConnectionDialog open={saveDialog} onOpenChange={setSaveDialog} title="Save application view" description="Save your grouping, filters, sort order and layout. This view is personal and will reopen when you return." busy={viewBusy} size="sm">
        <form className="space-y-4" onSubmit={event => { event.preventDefault(); void saveView(true) }}><FormField label="View name" htmlFor="application-view-name"><Input id="application-view-name" value={viewName} maxLength={80} placeholder="Production by team" autoFocus required disabled={viewBusy} onChange={event => setViewName(event.target.value)} /></FormField>{viewError && <p role="alert" className="text-sm text-destructive">{viewError}</p>}<DialogFooter><DialogCancel disabled={viewBusy} /><Button type="submit" loading={viewBusy} disabled={!viewName.trim()}>Save view</Button></DialogFooter></form>
      </ConnectionDialog>
      <ConnectionDialog open={bulkLabelOpen} onOpenChange={setBulkLabelOpen} title={`Add a label to ${bulk.label.length} application${bulk.label.length === 1 ? "" : "s"}`} description={`Existing labels are kept; a label with the same key is replaced.${selectedApps.length > bulk.label.length ? ` ${selectedApps.length - bulk.label.length} selected application${selectedApps.length - bulk.label.length === 1 ? " is" : "s are"} skipped: labels managed in Git, or you are not an owner.` : ""}`} busy={bulkBusy === "label"} size="sm">
        <form className="space-y-4" onSubmit={(event) => {
          event.preventDefault()
          const key = bulkLabel.key.trim(), value = bulkLabel.value.trim()
          void runBulk("label", bulk.label, (app) => api<WorkspaceApplication>(`${appPath(app)}/labels`, { method: "PUT", body: JSON.stringify({ labels: { ...(app.labels ?? {}), [key]: value } }) }).then((updated) => ({ ...app, ...updated })), `Label ${key}${value ? `=${value}` : ""} added to`).then(() => setBulkLabelOpen(false))
        }}>
          <FormField label="Key" htmlFor="bulk-label-key"><Input id="bulk-label-key" value={bulkLabel.key} placeholder="team" autoFocus required disabled={bulkBusy === "label"} onChange={(event) => setBulkLabel((current) => ({ ...current, key: event.target.value }))} /></FormField>
          <FormField label="Value" htmlFor="bulk-label-value"><Input id="bulk-label-value" value={bulkLabel.value} placeholder="platform" disabled={bulkBusy === "label"} onChange={(event) => setBulkLabel((current) => ({ ...current, value: event.target.value }))} /></FormField>
          <DialogFooter><DialogCancel disabled={bulkBusy === "label"} /><Button type="submit" loading={bulkBusy === "label"} disabled={!bulkLabel.key.trim()}>Add label</Button></DialogFooter>
        </form>
      </ConnectionDialog>
      {bulkBar}
      {editingLabels && <ApplicationLabelsDialog key={editingLabels.id} application={editingLabels} onClose={() => setEditingLabels(null)} onSaved={labels => onApplicationUpdated?.({ ...editingLabels, labels })} />}
      <section aria-label="Filter applications" className="mb-4 space-y-3">
        {/* One row: find (search, then the chips that narrow), and on the right how to look at it. */}
        {wide ? <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          {searchField}
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            {statusChips}
            <span aria-hidden="true" className="mx-0.5 h-5 w-px bg-border" />
            {favoritesChip}
          </div>
          <div className="ml-auto flex items-center gap-2">{clusterControl}{advancedFilter}{groupMenu}{viewTabs}</div>
        </div> : <>
          {/* A phone gets the search and one Filters button; the chips would otherwise take three rows. */}
          <div className="flex items-center gap-2">
            {searchField}
            <Sheet open={filtersOpen} onOpenChange={setFiltersOpen}>
              <Sheet.Trigger render={<Button type="button" variant="outline" className="shrink-0" aria-label={narrowing ? `Filters, ${narrowing} active` : "Filters"} />}>
                <HugeiconsIcon icon={FilterHorizontalIcon} strokeWidth={1.8} aria-hidden="true" />
                Filters{narrowing > 0 && <Badge size="sm" variant="secondary" className="tabular-nums">{narrowing}</Badge>}
              </Sheet.Trigger>
              <Sheet.Content>
                <Sheet.Header title="Filter applications" />
                <Sheet.Body className="space-y-5">
                  <div className="space-y-2">
                    <p className="text-xs font-medium text-muted-foreground">Status</p>
                    {statusChips}
                  </div>
                  <div className="flex flex-wrap items-center gap-2">{favoritesChip}{clusterControl}{advancedFilter}</div>
                </Sheet.Body>
                <Sheet.Footer>
                  {narrowing > 0 && <Button type="button" variant="ghost" onClick={() => { setFilter("all"); setClusterFilter(""); setFilterQuery(createFilterQuery()); setFavoritesOnly(false) }}>Clear filters</Button>}
                  <Sheet.Close render={<Button type="button" />}>Show {visible.length} application{visible.length === 1 ? "" : "s"}</Sheet.Close>
                </Sheet.Footer>
              </Sheet.Content>
            </Sheet>
          </div>
          <div className="flex items-center justify-between gap-2">{groupMenu}{viewTabs}</div>
        </>}
        {(filtering || buckets.length > 1) && <p role="status" className="px-1 text-sm text-muted-foreground">Showing {visible.length} of {available.length} applications{buckets.length > 1 && groupBy !== "none" ? ` in ${buckets.length} groups` : ""}</p>}
      </section>
      {visible.length ? (
        <div className="space-y-3">
        {unreachable.length > 0 && <Alert tone="destructive" variant="plain">
          <Alert.Icon />
          <Alert.Title>{unreachable.length === 1 ? `${unreachable[0].name}: ${unreachable[0].health.label.toLowerCase()}` : `${unreachable.length} clusters are unreachable`}</Alert.Title>
          <Alert.Description>{unreachable.length === 1 ? unreachable[0].health.detail : unreachable.map((cluster) => cluster.name).join(" · ")} {unreachable.reduce((sum, cluster) => sum + cluster.health.affectedApplications.length, 0)} applications are blocked until the connection is restored.</Alert.Description>
          {unreachable[0].health.affectedApplications[0] && <Alert.Actions><Button size="sm" variant="outline" nativeButton={false} render={<Link href={`/workspaces/${unreachable[0].health.affectedApplications[0].workspaceId}/connections/clusters`} />}>{unreachable.length === 1 ? "Check cluster" : "Check clusters"}</Button></Alert.Actions>}
        </Alert>}
        {buckets.map(renderBucket)}
        </div>
      ) : (
        <div className="pb-6 text-center">
          <EmptyState title="No matching applications" description="Try another search or clear the filters to see all applications." />
          <Button variant="outline" onClick={resetFilters}>Clear filters</Button>
        </div>
      )}
    </div>
    </>
  )
}
