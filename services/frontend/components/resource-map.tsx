"use client"

import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowUpRight01Icon, ArrowLeft01Icon, ArrowRight01Icon, Alert02Icon, InformationCircleIcon, Cancel01Icon, FilterHorizontalIcon, Search01Icon, FocusPointIcon, FullScreenIcon, RefreshIcon, Layers01Icon, Settings02Icon, Globe02Icon, DatabaseIcon, CubeIcon, Router02Icon, ServerStack01Icon, Files01Icon, FileTextIcon, Key01Icon, Shield01Icon, Clock01Icon, PlayIcon, Route01Icon, WorkflowSquare04Icon, Folder01Icon } from "@hugeicons/core-free-icons"
import { Alert } from "@/components/panelui/alert"
import { Badge } from "@/components/panelui/badge"
import { Menu } from "@/components/panelui/menu"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Button } from "@/components/panelui/button"
import { Inspector } from "@/components/panelui/inspector"
import { Toolbar } from "@/components/panelui/toolbar"
import { Workbench, useWorkbench } from "@/components/panelui/workbench"
import { Disclosure } from "@/components/ui/collapsible"
import { StatusBadge } from "@/components/ui-kit"
import { runtimeSignal } from "@/lib/topology-map"
import { ContainerList } from "./container-list"
import styles from "./resource-map.module.css"
import { TopologyFlow } from "./topology-flow"
import { podLabel, podState } from "@/lib/runtime-health"
import { ResourceActions } from "@/components/resource-actions"
import { Input } from "@/components/panelui/input"
import { resourceURLs } from "@/lib/topology-map"
import { identityKey, observed, configurationResource, relatedNodes, syncState, type SyncState } from "@/lib/topology-view"
import type { Application, Identity, ManagedResource, Operation, PlanRecord, ResourceTopology, TopologyNode } from "@/lib/types"

function runtimeDot(node: TopologyNode) {
  const signal = runtimeSignal(node)
  return signal === "healthy" ? "bg-success" : signal === "progressing" ? "bg-warning" : signal === "degraded" ? "bg-destructive" : "bg-muted-foreground/40"
}

const labels: Record<SyncState, string> = { syncing: "Applying", applied: "Deployed · health separate", failed: "Failed", create: "Will be created", update: "Out of sync", delete: "Will be destroyed", synced: "Deployed", unknown: "State unknown", observed: "Observed" }
const resourceIcons: Record<string, typeof CubeIcon> = {
  Service: Router02Icon, Pod: CubeIcon, Deployment: Layers01Icon,
  ReplicaSet: Files01Icon, StatefulSet: ServerStack01Icon, DaemonSet: WorkflowSquare04Icon,
  Ingress: Globe02Icon, Route: Route01Icon, Gateway: Globe02Icon, HTTPRoute: Route01Icon,
  ConfigMap: FileTextIcon, Secret: Key01Icon,
  PersistentVolumeClaim: DatabaseIcon, PersistentVolume: DatabaseIcon,
  Job: PlayIcon, CronJob: Clock01Icon, Namespace: Folder01Icon,
  ServiceAccount: Shield01Icon, Role: Shield01Icon, RoleBinding: Shield01Icon,
  ClusterRole: Shield01Icon, ClusterRoleBinding: Shield01Icon, NetworkPolicy: Shield01Icon,
}
const iconFor = (kind: string) => resourceIcons[kind] ?? Settings02Icon
function health(node: TopologyNode) {
  if (node.routingHealth) return node.routingHealth.message
  if (node.identity.kind === "Pod" && observed(node)) return podLabel(node)
  if (node.source === "sample") return `Sample · ${node.readiness || node.phase || "unknown"}`
  if (node.health) return node.health.message || node.health.status
  const details = node.healthSummary
  const ready = details?.conditions?.find((item) => item.type === "Ready")
  if (ready?.observedGeneration !== undefined && ready.observedGeneration < (details?.generation ?? 0)) return "Readiness pending current generation"
  if (node.readiness) return node.readiness
  if (details?.readyReplicas !== undefined && details.desiredReplicas !== undefined) return `Ready ${details.readyReplicas}/${details.desiredReplicas} replicas`
  if (details?.numberReady !== undefined && details.desiredScheduled !== undefined) return `Ready ${details.numberReady}/${details.desiredScheduled} pods`
  if (details?.succeeded !== undefined && details.completions !== undefined) return `Completed ${details.succeeded}/${details.completions}`
  if (node.uid && configurationResource(node.identity)) return "No workload health check"
  const condition = ready ?? details?.conditions?.find((item) => ["Failed", "ReplicaFailure"].includes(item.type) && item.status === "True")
  if (!condition && details?.conditions?.some((item) => item.type === "Failed" && item.status === "False")) return "No failure reported"
  if (condition) return `${condition.type}: ${condition.status}${condition.reason ? ` · ${condition.reason}` : ""}`
  if (details?.failureReason) return details.failureReason
  if (node.phase) return `Phase: ${node.phase}`
  return node.uid ? "Health not reported" : "Not observed in cluster"
}
function healthTone(node: TopologyNode) {
  if (node.routingHealth) return node.routingHealth.status === "healthy" ? "text-success-foreground dark:text-success" : node.routingHealth.status === "degraded" ? "text-destructive" : node.routingHealth.status === "progressing" ? "text-warning-foreground dark:text-warning" : "text-muted-foreground"
  if (node.identity.kind === "Pod" && observed(node)) {
    const state = podState(node)
    return state === "ready" || state === "completed" ? "text-success-foreground dark:text-success" : state === "failing" ? "text-destructive" : state === "starting" ? "text-warning-foreground dark:text-warning" : "text-muted-foreground"
  }
  if (node.source === "sample") return "text-muted-foreground"
  if (node.health) return node.health.status === "Healthy" ? "font-medium text-success-foreground dark:text-success" : node.health.status === "Degraded" || node.health.status === "Missing" ? "font-medium text-destructive" : node.health.status === "Progressing" ? "font-medium text-warning-foreground dark:text-warning" : "text-muted-foreground"
  if (node.readiness === "Ready" || node.phase === "Succeeded") return "font-medium text-success-foreground dark:text-success"
  if (node.readiness === "Not ready" || node.phase === "Failed" || node.healthSummary?.failureReason || node.healthSummary?.conditions?.some((item) => ["Failed", "ReplicaFailure"].includes(item.type) && item.status === "True")) return "font-medium text-destructive"
  if (node.readiness === "Unknown") return "font-medium text-warning-foreground dark:text-warning"
  return "text-muted-foreground"
}
function fallback(plan: PlanRecord | null, inventory: ManagedResource[]): ResourceTopology {
  const nodes = new Map<string, TopologyNode>()
  for (const identity of plan?.resources ?? []) nodes.set(identityKey(identity), { id: identityKey(identity), identity, source: "desired" })
  for (const item of inventory) nodes.set(identityKey(item.identity), { id: identityKey(item.identity), identity: item.identity, source: nodes.has(identityKey(item.identity)) ? "desired" : "managed", uid: item.uid, resourceVersion: item.resourceVersion })
  for (const change of plan?.plan.changes ?? []) if (!nodes.has(identityKey(change.identity))) nodes.set(identityKey(change.identity), { id: identityKey(change.identity), identity: change.identity, source: "desired" })
  return { nodes: [...nodes.values()], edges: [], warnings: ["Topology unavailable. Showing inventory without inferred relationships."] }
}

/** On a narrow workbench there is no room for a side rail, so the tools float over the top of the canvas. */
function MapBar({ children }: { children: ReactNode }) {
  const { compact } = useWorkbench()
  return compact ? <Workbench.Bar>{children}</Workbench.Bar> : null
}

const MIN_DETAILS = 320
const MAX_DETAILS = 720

/** The right edge on a wide workbench: the tool rail beside the map, then a handle that resizes the resource details, then the details. */
function MapSide({ children, rail, width, onWidth, onResizing }: { children: ReactNode; rail: ReactNode; width: number; onWidth: (width: number) => void; onResizing: (resizing: boolean) => void }) {
  const { compact } = useWorkbench()
  const [active, setActive] = useState(false)
  if (compact) return <>{children}</>
  const clamp = (value: number) => Math.round(Math.min(Math.max(value, MIN_DETAILS), MAX_DETAILS))
  const drag = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return
    event.preventDefault()
    const target = event.currentTarget
    target.setPointerCapture(event.pointerId)
    const startX = event.clientX
    const startWidth = width
    setActive(true)
    onResizing(true)
    const move = (next: PointerEvent) => onWidth(clamp(startWidth + startX - next.clientX))
    const end = () => {
      setActive(false)
      onResizing(false)
      target.removeEventListener("pointermove", move)
      target.removeEventListener("pointerup", end)
      target.removeEventListener("pointercancel", end)
    }
    target.addEventListener("pointermove", move)
    target.addEventListener("pointerup", end)
    target.addEventListener("pointercancel", end)
  }
  const keys = (event: React.KeyboardEvent<HTMLDivElement>) => {
    const by = event.key === "ArrowLeft" ? 24 : event.key === "ArrowRight" ? -24 : 0
    if (!by) return
    event.preventDefault()
    onWidth(clamp(width + by))
  }
  return <div className="flex h-full min-w-0 flex-1 items-stretch justify-end">
    <div className="flex shrink-0 items-start">{rail}</div>
    {children ? <>
      <div role="separator" tabIndex={0} aria-orientation="vertical" aria-label="Resource details width" aria-valuenow={width} aria-valuemin={MIN_DETAILS} aria-valuemax={MAX_DETAILS} data-active={active ? "" : undefined} onPointerDown={drag} onKeyDown={keys} onDoubleClick={() => onWidth(392)} className="group/handle flex w-3 shrink-0 cursor-col-resize touch-none items-center justify-center rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <span className="h-10 w-1 rounded-full bg-foreground/20 transition-[background-color,height] group-hover/handle:h-16 group-hover/handle:bg-foreground/40 group-focus-visible/handle:h-16 group-focus-visible/handle:bg-ring group-data-active/handle:h-16 group-data-active/handle:bg-foreground/50" />
      </div>
      <div className="flex min-w-0 flex-1 pe-2 [&>*]:h-full">{children}</div>
    </> : null}
  </div>
}

/** Opens the inspector sheet on a narrow workbench when something is selected. */
function ResourceDetailsClose({ onClose }: { onClose: () => void }) {
  const { compact } = useWorkbench()
  if (compact) return null
  return <Button type="button" variant="ghost" size="icon" aria-label="Close resource details" onClick={onClose}><HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} aria-hidden="true" /></Button>
}

function OpenInspector({ open }: { open: boolean }) {
  const { setInspectorOpen } = useWorkbench()
  useEffect(() => { setInspectorOpen(open) }, [open, setInspectorOpen])
  return null
}

export function ResourceMap({ application, plan, inventory, operations, topology, onViewDiff, onRefresh, refreshing, canManageResources = false, resourceActionsDisabled = false, onResourceActionComplete, stalePlan = null, conflicts = [], onReviewConflicts }: {
  stalePlan?: PlanRecord | null; conflicts?: Identity[]; onReviewConflicts?: () => void
  canManageResources?: boolean; resourceActionsDisabled?: boolean; onResourceActionComplete?: () => Promise<void>
  application: Application; plan: PlanRecord | null; inventory: ManagedResource[]; operations: Operation[]
  topology: ResourceTopology | null; onViewDiff: (identity: Identity) => void; onRefresh: () => void; refreshing: boolean
}) {
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  const [mode, setMode] = useState("all")
  const [focus, setFocus] = useState(false)
  const [fullscreen, setFullscreen] = useState(false)
  const [notice, setNotice] = useState("")
  const [detailsWidth, setDetailsWidth] = useState(392)
  const [resizing, setResizing] = useState(false)
  const [showAttention, setShowAttention] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const graph = useMemo(() => topology ?? fallback(plan, inventory), [topology, plan, inventory])
  const healthFor = (node: TopologyNode) => health(node)
  const toneFor = (node: TopologyNode) => healthTone(node)
  const operation = operations.find((item) => item.planId === plan?.id)
  const running = operation && ["running", "queued"].includes(operation.status)
  const stateFor = (node: TopologyNode) => syncState(node, plan, operation)
  const planned = useMemo(() => new Set(((plan ?? stalePlan)?.plan.changes ?? []).map((change) => identityKey(change.identity))), [plan, stalePlan])
  const conflictKeys = useMemo(() => new Set(conflicts.map(identityKey)), [conflicts])
  const isConflict = (node: TopologyNode) => conflictKeys.has(identityKey(node.identity))
  const isChanged = (node: TopologyNode) => ["create", "update", "delete", "failed", "syncing"].includes(stateFor(node)) || planned.has(identityKey(node.identity))
  const warnings = [notice, ...graph.warnings].filter((item): item is string => Boolean(item) && !item.startsWith("No managed workloads"))
  const nothingObserved = !graph.nodes.some(observed)
  const notices = [...warnings, ...(nothingObserved && graph.nodes.length > 0 ? ["Not observed in the cluster yet. Showing desired state from Git; deploy or adopt existing resources to see live health."] : [])]
  const attention = graph.nodes.filter((node) => isConflict(node) || stateFor(node) === "failed" || toneFor(node).includes("text-destructive"))
  const selected = graph.nodes.find((node) => node.id === selectedId)
  const selectedManagedResource = selected && inventory.find((resource) => identityKey(resource.identity) === identityKey(selected.identity))
  const related = useMemo(() => selectedId ? relatedNodes(graph, selectedId) : new Set<string>(), [graph, selectedId])
  const matches = new Set(graph.nodes.filter((node) => `${node.identity.kind} ${node.identity.namespace} ${node.identity.name}`.toLowerCase().includes(query.trim().toLowerCase())).map((node) => node.id))
  async function toggleFullscreen() {
    try { if (document.fullscreenElement === root.current) await document.exitFullscreen(); else await root.current?.requestFullscreen() }
    catch { setNotice("Fullscreen is unavailable in this browser.") }
  }
  useEffect(() => {
    const update = () => setFullscreen(document.fullscreenElement === root.current)
    document.addEventListener("fullscreenchange", update)
    return () => document.removeEventListener("fullscreenchange", update)
  }, [])
  const connections = selected ? graph.edges.filter((edge) => edge.from === selected.id || edge.to === selected.id) : []
  const changedCount = graph.nodes.filter(isChanged).length
  const observedCount = graph.nodes.filter(observed).length
  const conflictCount = graph.nodes.filter(isConflict).length
  const filters = [["all", "All", 0], ["changes", "Changes", changedCount], ["observed", "Observed", observedCount], ["conflicts", "Conflicts", conflictCount]] as const

  const details = selected ? (
<aside aria-label="Resource details" className={`${styles.inspector} min-w-0 space-y-5`}>
        <div className="flex items-start justify-between gap-2"><div className="flex min-w-0 items-center gap-3"><span className="grid size-10 shrink-0 place-items-center rounded-xl border bg-muted/40 text-muted-foreground"><HugeiconsIcon icon={iconFor(selected.identity.kind)} strokeWidth={1.6} className="size-5" aria-hidden="true" /></span><div className="min-w-0"><p className="text-sm font-medium uppercase tracking-widest text-muted-foreground">{selected.identity.kind}</p><h3 className="mt-1 break-all text-base font-semibold">{selected.identity.name}</h3></div></div><ResourceDetailsClose onClose={() => { setSelectedId(null); setFocus(false) }} /></div>

        <div className="space-y-3">
          <div className="flex items-start gap-2"><i aria-hidden="true" className={`mt-1.5 size-2 shrink-0 rounded-full ${runtimeDot(selected)}`} /><div className="min-w-0"><p className="text-xs text-muted-foreground">Runtime</p><p className={`text-sm font-medium break-words ${toneFor(selected)}`}>{healthFor(selected)}</p></div></div>
          <dl className="flex flex-wrap items-center gap-x-6 gap-y-2 pt-1 text-xs"><div className="flex items-center gap-2"><dt className="text-muted-foreground">Sync</dt><dd><StatusBadge status={labels[stateFor(selected)]} /></dd></div>{application.autoSyncPaused && <div className="flex items-center gap-2"><dt className="text-muted-foreground">Auto-sync</dt><dd><StatusBadge status="Paused" /></dd></div>}</dl>
        </div>
        {(plan ?? stalePlan)?.plan.changes.some((change) => identityKey(change.identity) === identityKey(selected.identity)) && <Button type="button" variant="outline" onClick={async () => { if (document.fullscreenElement === root.current) await document.exitFullscreen(); onViewDiff(selected.identity) }}>View diff<HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} data-icon="inline-end" aria-hidden="true" /></Button>}
        {operation && <p className="text-sm text-muted-foreground">Plan operation: {operation.status} · {new Date(operation.finishedAt || operation.startedAt).toLocaleString()}</p>}
        {canManageResources && onResourceActionComplete && selectedManagedResource?.identity.namespace && !selectedManagedResource.identity.clusterScoped && <ResourceActions key={selectedManagedResource.uid} applicationID={application.id} resource={selectedManagedResource} disabled={resourceActionsDisabled} onComplete={onResourceActionComplete} />}
        {["Ingress", "Route"].includes(selected.identity.kind) && <section aria-label="Resource URLs" className="space-y-2 pt-1"><h4 className="text-sm font-semibold">URLs</h4>{resourceURLs(selected).length ? resourceURLs(selected).map((url) => <Button key={url} variant="outline" nativeButton={false} render={<a href={url} target="_blank" rel="noopener noreferrer" />} className="h-auto max-w-full justify-start whitespace-normal break-all text-left text-xs">{url}<HugeiconsIcon icon={ArrowUpRight01Icon} className="size-4 shrink-0" aria-hidden="true" /><span className="sr-only"> (opens in a new tab)</span></Button>) : <p className="text-xs text-muted-foreground">No concrete hostname or address is available yet. Wildcard hosts cannot be opened directly.</p>}<p className="text-xs text-muted-foreground">Configured addresses; availability depends on routing and cluster connectivity.</p></section>}
        {selected.routingHealth && <section aria-label="Backend readiness" className="space-y-3 pt-1"><h4 className="text-sm font-semibold">Backend readiness</h4><p className={`text-sm ${toneFor(selected)}`}>{selected.routingHealth.label}</p><p className="text-xs text-muted-foreground">{selected.routingHealth.readyPods}/{selected.routingHealth.totalPods} matching pods ready. Based on Kubernetes observations; DNS, TLS, network policies, and external HTTP reachability have not been tested.</p>{selected.routingHealth.backends?.map((backend, index) => <div key={`${backend.serviceName}-${backend.port}-${index}`} className="space-y-1 text-xs"><p className="break-all font-medium">Service/{backend.serviceName}{backend.port ? ` · port ${backend.port}` : ""}</p><p className="text-muted-foreground">{backend.message}</p></div>)}</section>}
        {selected.healthSummary?.containers && <ContainerList containers={selected.healthSummary.containers} title={selected.identity.kind === "Pod" ? "Containers" : "Images"} />}
        <div><h4 className="text-sm font-semibold">Relationships ({connections.length})</h4><div className="mt-2 space-y-2">{connections.map((edge, i) => {
          const other = graph.nodes.find((node) => node.id === (edge.from === selected.id ? edge.to : edge.from))
          return other ? <Button type="button" key={i} variant="outline" className="h-auto w-full justify-start gap-3 whitespace-normal rounded-lg py-2.5 text-left text-xs" onClick={() => { setSelectedId(other.id); setMode("all") }}><HugeiconsIcon icon={iconFor(other.identity.kind)} strokeWidth={1.6} className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" /><span><span className="inline-flex items-center gap-1">{edge.from !== selected.id && <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} aria-hidden="true" className="size-3" />}{edge.from === selected.id ? edge.relation : `${edge.relation} from`}{edge.from === selected.id && <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} aria-hidden="true" className="size-3" />}</span><span className="mt-1 block break-all font-medium">{other.identity.kind}/{other.identity.name}</span></span></Button> : null
        })}{!connections.length && <p className="text-sm text-muted-foreground">No relationship detected in available manifests or observations.</p>}</div></div>
        {selected.identity.kind === "Pod" && selected.healthSummary && <dl className="grid grid-cols-2 gap-3 pt-1 text-xs"><div><dt className="text-muted-foreground">Container readiness</dt><dd>{selected.healthSummary.readyContainers ?? 0}/{selected.healthSummary.totalContainers ?? 0}</dd></div><div><dt className="text-muted-foreground">Restarts</dt><dd>{selected.healthSummary.restartCount ?? 0}</dd></div></dl>}
        {selected.healthSummary && <Disclosure className="pt-1" summary="Kubernetes health details" defaultOpen={Boolean(selected.healthSummary.failureReason)}><div className="space-y-3">
          <dl className="grid grid-cols-2 gap-3 text-xs [&_dd]:text-base [&_dd]:tabular-nums">
            {selected.healthSummary.readyReplicas !== undefined && <div><dt className="text-muted-foreground">Ready replicas</dt><dd className="mt-1 font-medium">{selected.healthSummary.readyReplicas}{selected.healthSummary.desiredReplicas !== undefined ? ` / ${selected.healthSummary.desiredReplicas}` : ""}</dd></div>}
            {selected.healthSummary.updatedReplicas !== undefined && <div><dt className="text-muted-foreground">Updated replicas</dt><dd className="mt-1 font-medium">{selected.healthSummary.updatedReplicas}{selected.healthSummary.desiredReplicas !== undefined ? ` / ${selected.healthSummary.desiredReplicas}` : ""}</dd></div>}
            {selected.healthSummary.availableReplicas !== undefined && <div><dt className="text-muted-foreground">Available replicas</dt><dd className="mt-1 font-medium">{selected.healthSummary.availableReplicas}{selected.healthSummary.desiredReplicas !== undefined ? ` / ${selected.healthSummary.desiredReplicas}` : ""}</dd></div>}
            {selected.healthSummary.numberReady !== undefined && <div><dt className="text-muted-foreground">Ready pods</dt><dd className="mt-1 font-medium">{selected.healthSummary.numberReady}{selected.healthSummary.desiredScheduled !== undefined ? ` / ${selected.healthSummary.desiredScheduled}` : ""}</dd></div>}
            {selected.healthSummary.updatedScheduled !== undefined && <div><dt className="text-muted-foreground">Updated pods</dt><dd className="mt-1 font-medium">{selected.healthSummary.updatedScheduled}{selected.healthSummary.desiredScheduled !== undefined ? ` / ${selected.healthSummary.desiredScheduled}` : ""}</dd></div>}
            {selected.healthSummary.succeeded !== undefined && <div><dt className="text-muted-foreground">Job completions</dt><dd className="mt-1 font-medium">{selected.healthSummary.succeeded}{selected.healthSummary.completions !== undefined ? ` / ${selected.healthSummary.completions}` : ""}</dd></div>}
            {selected.healthSummary.active !== undefined && <div><dt className="text-muted-foreground">Active pods</dt><dd className="mt-1 font-medium">{selected.healthSummary.active}</dd></div>}
            {selected.healthSummary.failed !== undefined && <div><dt className="text-muted-foreground">Failed pods</dt><dd className="mt-1 font-medium">{selected.healthSummary.failed}</dd></div>}
          </dl>
          {selected.healthSummary.failureReason && <p className="text-xs text-destructive"><strong>{selected.healthSummary.failureReason}</strong>{selected.healthSummary.failureMessage ? ` · ${selected.healthSummary.failureMessage}` : ""}</p>}
          {selected.healthSummary.conditions?.length ? <div className="space-y-2"><h5 className="text-xs font-semibold">Conditions</h5><ul className="space-y-2">{selected.healthSummary.conditions.map((condition, index) => <li key={`${condition.type}-${index}`} className="text-sm"><Disclosure summary={<span className="flex flex-wrap items-center gap-1.5"><strong>{condition.type}</strong><StatusBadge status={condition.status} />{condition.reason && <span className="font-normal text-muted-foreground">{condition.reason}</span>}</span>}>{condition.message && <p className="whitespace-pre-wrap break-words text-muted-foreground">{condition.message}</p>}{condition.lastTransitionTime && <p className="mt-1 text-sm text-muted-foreground">Transitioned {new Date(condition.lastTransitionTime).toLocaleString()}</p>}</Disclosure></li>)}</ul></div> : <p className="text-sm text-muted-foreground">Kubernetes has not reported explicit status conditions for this resource.</p>}
        </div></Disclosure>}
        <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">{[["Namespace", selected.identity.namespace || "Cluster scope"], ["API version", selected.identity.apiVersion], ["Source", selected.source], ["Last observed", selected.observedAt ? new Date(selected.observedAt).toLocaleString() : "Not available"], ["Resource version", selected.resourceVersion || "Not available"]].map(([key, value]) => <div key={key}><dt className="text-xs text-muted-foreground">{key}</dt><dd className="mt-1 break-words font-medium">{value}</dd></div>)}</dl>
      </aside>
  ) : null

  const attentionPanel = showAttention ? (
    <aside aria-label="Needs attention" className="space-y-4 p-4">
      <div className="flex items-start justify-between gap-2"><div className="flex items-center gap-3"><span className="grid size-10 shrink-0 place-items-center rounded-xl bg-destructive/10 text-destructive"><HugeiconsIcon icon={Alert02Icon} strokeWidth={1.6} className="size-5" aria-hidden="true" /></span><div><p className="text-sm font-medium uppercase tracking-widest text-muted-foreground">Needs attention</p><h3 className="mt-1 text-base font-semibold">{attention.length} {attention.length === 1 ? "resource" : "resources"}</h3></div></div><ResourceDetailsClose onClose={() => setShowAttention(false)} /></div>
      <ul className="space-y-2">{attention.map((node) => <li key={node.id}><Button type="button" variant="outline" className="h-auto w-full justify-start gap-3 whitespace-normal rounded-lg py-2.5 text-left text-xs" onClick={() => { setSelectedId(node.id); setShowAttention(false); setMode("all"); setQuery("") }}><i aria-hidden="true" className={`size-2 shrink-0 rounded-full ${runtimeDot(node)}`} /><span className="min-w-0"><span className="block break-all font-medium">{node.identity.kind}/{node.identity.name}</span><span className={`mt-0.5 block ${isConflict(node) ? "text-warning" : toneFor(node)}`}>{isConflict(node) ? "Owned by another application" : stateFor(node) === "failed" ? "Failed" : healthFor(node)}</span></span></Button></li>)}</ul>
      {conflictKeys.size > 0 && onReviewConflicts && <Button type="button" variant="outline" onClick={onReviewConflicts}>Review conflicts</Button>}
    </aside>
  ) : null

  const tools = (orientation: "horizontal" | "vertical") => {
    const vertical = orientation === "vertical"
    return <Toolbar aria-label="Resource map tools" orientation={orientation} className={vertical ? "" : "max-w-full flex-wrap justify-center gap-2"}>
      <Popover>
        <PopoverTrigger render={<Button type="button" variant={query ? "secondary" : "ghost"} size="icon-sm" aria-label="Search resources" title="Search resources" />}><HugeiconsIcon icon={Search01Icon} strokeWidth={1.8} aria-hidden="true" /></PopoverTrigger>
        <PopoverContent side={vertical ? "left" : "bottom"} align="start" className="w-64"><Input aria-label="Search resources" placeholder="Find a resource…" autoFocus value={query} onChange={(event) => { setQuery(event.target.value); setMode("all"); setFocus(false) }} /></PopoverContent>
      </Popover>
      <Menu>
        <Menu.Trigger render={<Button type="button" variant={mode !== "all" ? "secondary" : "ghost"} size="icon-sm" aria-label="Filter resources" title="Filter resources" />}><HugeiconsIcon icon={FilterHorizontalIcon} strokeWidth={1.8} aria-hidden="true" /></Menu.Trigger>
        <Menu.Content side={vertical ? "left" : "bottom"} align="start">
          <Menu.RadioGroup value={mode} onValueChange={(next) => setMode(String(next))}>
            {filters.filter(([value, , count]) => value === "all" || count > 0 || value === mode).map(([value, label, count]) => <Menu.RadioItem key={value} value={value}>{label}{count > 0 && <Menu.Badge>{count}</Menu.Badge>}</Menu.RadioItem>)}
          </Menu.RadioGroup>
        </Menu.Content>
      </Menu>
      {attention.length > 0 && <Button type="button" variant={showAttention ? "secondary" : "ghost"} size="icon-sm" aria-label={`Needs attention (${attention.length})`} title={`Needs attention (${attention.length})`} aria-pressed={showAttention} className="relative text-destructive hover:text-destructive" onClick={() => { setShowAttention(!showAttention); setSelectedId(null); setFocus(false) }}><HugeiconsIcon icon={Alert02Icon} strokeWidth={1.8} aria-hidden="true" /><span aria-hidden="true" className="absolute -right-0.5 -top-0.5 grid min-w-3.5 place-items-center rounded-full bg-destructive px-1 text-[10px] font-semibold leading-3.5 text-white">{attention.length}</span></Button>}
      <Toolbar.Separator />
      <Toolbar.Button label="Refresh cluster" onClick={onRefresh} disabled={refreshing} render={<Button type="button" variant="ghost" size="icon-sm" loading={refreshing} />}><HugeiconsIcon icon={RefreshIcon} strokeWidth={1.8} aria-hidden="true" /></Toolbar.Button>
      <Toolbar.Button label={focus ? "Show all resources" : "Focus selection"} aria-pressed={focus} disabled={!selected} onClick={() => setFocus(!focus)} render={<Button type="button" variant={focus ? "secondary" : "ghost"} size="icon-sm" />}><HugeiconsIcon icon={FocusPointIcon} strokeWidth={1.8} aria-hidden="true" /></Toolbar.Button>
      <Toolbar.Button label={fullscreen ? "Exit fullscreen" : "Fullscreen"} onClick={() => void toggleFullscreen()} render={<Button type="button" variant="ghost" size="icon-sm" />}><HugeiconsIcon icon={FullScreenIcon} strokeWidth={1.8} aria-hidden="true" /></Toolbar.Button>
      <Toolbar.Separator />
      <Popover>
        <PopoverTrigger render={<Button type="button" variant="ghost" size="icon-sm" aria-label="Legend" title="Legend" />}><HugeiconsIcon icon={InformationCircleIcon} strokeWidth={1.8} aria-hidden="true" /></PopoverTrigger>
        <PopoverContent side={vertical ? "left" : "bottom"} align="end" className="w-80 gap-3" aria-label="Topology status legend">
          <div className="space-y-1.5"><p className="text-xs font-medium text-foreground">Sync state</p><div className="flex flex-wrap gap-1.5">
            <Badge variant="success" size="sm" dot>Deployed</Badge><Badge variant="warning" size="sm" dot>Out of sync</Badge><Badge variant="info" size="sm" dot>Will be created / applying</Badge><Badge variant="destructive" size="sm" dot>Will be destroyed / failed</Badge>
          </div></div>
          <div className="space-y-1.5"><p className="text-xs font-medium text-foreground">Runtime</p><div className="flex flex-wrap gap-1.5"><Badge variant="success" size="sm" dot>Ready</Badge></div><p className="text-xs text-muted-foreground">Runtime readiness is separate from sync. Configuration objects do not require workload health checks.</p></div>
        </PopoverContent>
      </Popover>
    </Toolbar>
  }

  return <section aria-label="Application resource topology" className="mb-6 min-w-0">
    <div ref={root} className={`min-w-0 ${fullscreen ? "h-screen bg-background p-3" : ""}`}>
    <Workbench className={fullscreen ? "h-full" : "h-[min(78svh,860px)]"} focusShortcut={false} inspectorWidth={(selected || showAttention ? detailsWidth + 20 : 0) + 56} style={resizing ? { transition: "none" } : undefined}>
      <MapBar>{tools("horizontal")}</MapBar>
      <Workbench.Canvas className="!bg-none !items-stretch !justify-stretch !px-0 !pb-0 !pt-4">
        <div className="flex h-full min-h-0 w-full flex-col gap-3 px-4 pb-4">
          <div className="flex min-w-0 flex-col gap-3 empty:hidden">
    {query && <p role="status" className="text-sm text-muted-foreground">{matches.size} matching resources</p>}
    {(running || operation?.status === "failed") && <Alert tone={operation?.status === "failed" ? "destructive" : "info"} role="status" variant="plain"><Alert.Icon /><Alert.Title>{operation?.type === "rollback" ? "Rollback" : "Sync"} {operation?.status === "failed" ? "stopped" : operation?.status === "queued" ? "queued" : "running"} · {operation?.progress?.completed?.length ?? 0}/{operation?.progress?.total ?? 0} resource steps completed{operation?.progress?.current ? ` · ${operation.progress.current.kind}/${operation.progress.current.name}` : ""}</Alert.Title><Alert.Description>Applied does not mean Healthy. {operation?.status === "failed" ? operation.message : ""}</Alert.Description></Alert>}
    {notices.length === 1 && <Alert tone={warnings.length > 0 ? "warning" : "info"} role="status" variant="plain"><Alert.Icon /><Alert.Title>{notices[0]}</Alert.Title></Alert>}
    {notices.length > 1 && <Alert tone={warnings.length > 0 ? "warning" : "info"} role="status" variant="plain"><Alert.Icon /><Alert.Title>Some of this map is not live yet</Alert.Title><Alert.Details label={`${notices.length} notes`} defaultOpen>{notices.map((notice) => <Alert.Item key={notice}>{notice}</Alert.Item>)}</Alert.Details></Alert>}
          </div>
          <TopologyFlow graph={graph} selectedId={selectedId} onSelect={setSelectedId} query={query} mode={mode} focus={focus} related={related} stateFor={stateFor} syncLabels={labels} healthFor={healthFor} toneFor={toneFor} iconFor={iconFor} isChanged={isChanged} isConflict={isConflict} />
        </div>
      </Workbench.Canvas>
      <Workbench.Inspector title="Resource details" className="!justify-end">
        <MapSide rail={tools("vertical")} width={detailsWidth} onWidth={setDetailsWidth} onResizing={setResizing}>{selected || showAttention ? <Inspector.Panel className="h-full w-full [&_[style*='min-width']]:!min-w-0">{selected ? details : attentionPanel}</Inspector.Panel> : null}</MapSide>
      </Workbench.Inspector>
      <OpenInspector open={Boolean(selected) || showAttention} />
    </Workbench>
    </div>
  </section>
}
