"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowLeft01Icon, ArrowRight01Icon, Cancel01Icon, FocusPointIcon, FullScreenIcon, RefreshIcon, Layers01Icon, Settings02Icon, Globe02Icon, DatabaseIcon, CubeIcon, Router02Icon, ServerStack01Icon, Files01Icon, FileTextIcon, Key01Icon, Shield01Icon, Clock01Icon, PlayIcon, Route01Icon, WorkflowSquare04Icon, Folder01Icon } from "@hugeicons/core-free-icons"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { StatusBadge } from "@/components/ui-kit"
import styles from "./resource-map.module.css"
import { TopologyCanvas } from "./topology-canvas"
import { TopologyOverview } from "./topology-overview"
import { podLabel, podState } from "@/lib/runtime-health"
import { ResourceActions } from "@/components/resource-actions"
import { DataGridList } from "@/components/data-grid-table"
import { Input } from "@/components/ui/input"
import { resourceURLs } from "@/lib/topology-map"
import { applicationResourceNodes, identityKey, observed, configurationResource, relatedNodes, syncState, type SyncState } from "@/lib/topology-view"
import type { Application, Identity, ManagedResource, Operation, PlanRecord, ResourceTopology, TopologyNode } from "@/lib/types"

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

export function ResourceMap({ view, onViewChange, application, plan, inventory, operations, topology, onViewDiff, onRefresh, refreshing, canManageResources = false, resourceActionsDisabled = false, onResourceActionComplete, stalePlan = null, conflicts = [], onReviewConflicts }: {
  stalePlan?: PlanRecord | null; conflicts?: Identity[]; onReviewConflicts?: () => void
  view: "overview" | "graph" | "list"; onViewChange: (view: "overview" | "graph" | "list") => void
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
  const root = useRef<HTMLElement>(null)
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
  const appCount = applicationResourceNodes(graph).length
  const childCount = graph.nodes.length - appCount
  const summary = [`${appCount} resources`, childCount > 0 && `${childCount} runtime children`, changedCount > 0 && `${changedCount} changes`, observedCount > 0 && `${observedCount} observed`, conflictCount > 0 && `${conflictCount} owned elsewhere`].filter(Boolean).join(" · ")
  const filters = [["all", "All", 0], ["changes", "Changes", changedCount], ["observed", "Observed", observedCount], ["conflicts", "Conflicts", conflictCount]] as const

  return <section ref={root} aria-label="Application resource topology" className="mb-6 min-w-0 overflow-hidden rounded-xl border bg-card fullscreen:overflow-auto fullscreen:rounded-none">
    <header className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
      <div><h2 className="text-base font-semibold">Resources</h2><p className="mt-1 text-sm text-muted-foreground">{application.name} · {summary}</p></div>
      <div className="flex items-center gap-2">
        <Button type="button" variant="outline" onClick={onRefresh} loading={refreshing} loadingText="Refreshing…"><HugeiconsIcon icon={RefreshIcon} strokeWidth={1.8} aria-hidden="true" />Refresh cluster</Button>
        <Tooltip><TooltipTrigger render={<Button type="button" variant={focus ? "secondary" : "outline"} size="icon" aria-label={focus ? "Show all resources" : "Focus selection"} aria-pressed={focus} disabled={!selected} onClick={() => setFocus(!focus)} />}><HugeiconsIcon icon={FocusPointIcon} strokeWidth={1.8} aria-hidden="true" /></TooltipTrigger><TooltipContent>{selected ? focus ? "Show all resources" : "Focus selection" : "Select a resource to focus"}</TooltipContent></Tooltip>
        <Tooltip><TooltipTrigger render={<Button type="button" variant="outline" size="icon" aria-label={fullscreen ? "Exit fullscreen" : "Fullscreen"} onClick={() => void toggleFullscreen()} />}><HugeiconsIcon icon={FullScreenIcon} strokeWidth={1.8} aria-hidden="true" /></TooltipTrigger><TooltipContent>{fullscreen ? "Exit fullscreen" : "Fullscreen"}</TooltipContent></Tooltip>
      </div>
    </header>
    <div className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
      <ToggleGroup aria-label="Resource view" value={[view]} onValueChange={(next) => { if (next[0] === "overview" || next[0] === "graph" || next[0] === "list") onViewChange(next[0]) }}>
        <ToggleGroupItem value="overview">Overview</ToggleGroupItem>
        <ToggleGroupItem value="graph">Graph</ToggleGroupItem>
        <ToggleGroupItem value="list">List</ToggleGroupItem>
      </ToggleGroup>
      <Input aria-label="Search resources" placeholder="Find a resource…" className="w-full sm:w-56" value={query} onChange={(event) => { setQuery(event.target.value); setMode("all"); setFocus(false) }} />
      <ToggleGroup aria-label="Topology filter" value={[mode]} onValueChange={(next) => { if (next[0]) setMode(next[0]) }}>
        {filters.filter(([value, , count]) => value === "all" || count > 0 || value === mode).map(([value, label, count]) => <ToggleGroupItem key={value} value={value}>{label}{count > 0 && <span className="ml-1.5 text-muted-foreground">{count}</span>}</ToggleGroupItem>)}
      </ToggleGroup>
      {focus && <Button type="button" variant="outline" size="sm" onClick={() => setFocus(false)}>Show all resources</Button>}
    </div>
    {query && <p role="status" className="px-5 py-2 text-sm text-muted-foreground">{matches.size} matching resources</p>}
    {(running || operation?.status === "failed") && <div role="status" className="border-b bg-muted/30 px-5 py-3 text-sm"><strong>{operation?.type === "rollback" ? "Rollback" : "Sync"} {operation?.status === "failed" ? "stopped" : operation?.status === "queued" ? "queued" : "running"}</strong> · {operation?.progress?.completed?.length ?? 0}/{operation?.progress?.total ?? 0} resource steps completed{operation?.progress?.current ? ` · ${operation.progress.current.kind}/${operation.progress.current.name}` : ""}<span className="mt-1 block text-muted-foreground">Applied does not mean Healthy. {operation?.status === "failed" ? operation.message : ""}</span></div>}
    {warnings.length > 0 && <p role="status" className="border-b px-5 py-3 text-sm text-warning-foreground dark:text-warning">{warnings.join(" · ")}</p>}
    {attention.length > 0 && <div role="region" aria-label="Needs attention" className="flex flex-wrap items-center gap-2 border-b px-4 py-2.5"><span className="text-sm font-medium">Needs attention</span>{attention.slice(0, 6).map((node) => <Button key={node.id} type="button" size="xs" variant="outline" onClick={() => { setSelectedId(node.id); setMode("all"); setQuery("") }}>{node.identity.name}<span className="text-muted-foreground">{isConflict(node) ? "owned elsewhere" : stateFor(node) === "failed" ? "failed" : healthFor(node)}</span></Button>)}{attention.length > 6 && <span className="text-xs text-muted-foreground">+{attention.length - 6} more</span>}{conflictKeys.size > 0 && onReviewConflicts && <Button type="button" size="xs" variant="ghost" onClick={onReviewConflicts}>Review conflicts</Button>}</div>}
    {nothingObserved && graph.nodes.length > 0 && <p role="status" className="border-b px-5 py-2.5 text-sm text-muted-foreground">Not observed in the cluster yet. Showing desired state from Git; deploy or adopt existing resources to see live health.</p>}
    <div className={`grid min-w-0 ${selected ? "xl:grid-cols-[minmax(0,1fr)_360px]" : ""}`}>
      {view === "list" ? <div className="min-w-0 p-4"><DataGridList rows={graph.nodes.filter((node) => (!query.trim() || matches.has(node.id)) && (mode === "all" || mode === "observed" && observed(node) || mode === "changes" && ["create", "update", "delete", "failed"].includes(stateFor(node))) && (!focus || !selected || related.has(node.id)))} columns={[
        { id: "resource", title: "Resource", cell: (node) => <Button variant="link" className="h-auto max-w-full justify-start whitespace-normal p-0 text-left" onClick={() => setSelectedId(node.id)} aria-current={selectedId === node.id ? "true" : undefined}>{node.identity.name}</Button> },
        { id: "kind", title: "Kind", cell: (node) => <span className="text-xs">{node.identity.kind}</span> },
        { id: "namespace", title: "Namespace", cell: (node) => <span className="text-xs text-muted-foreground">{node.identity.namespace || "Cluster scope"}</span> },
        { id: "health", title: "Runtime health", cell: (node) => <span className={`text-xs ${toneFor(node)}`}>{healthFor(node)}</span> },
        { id: "sync", title: "Sync state", cell: (node) => <span className="text-xs">{labels[stateFor(node)]}</span> },
        { id: "management", title: "Management", cell: (node) => <span className="text-xs text-muted-foreground">{inventory.some((resource) => identityKey(resource.identity) === identityKey(node.identity)) ? "Managed by JustCD" : observed(node) ? "Observed · managed by Kubernetes" : "Desired · not managed yet"}</span> },
      ]} empty="No matching resources. Refresh the cluster or try another filter." /></div> : view === "overview" ? <TopologyOverview graph={graph} selectedId={selectedId} onSelect={setSelectedId} query={query} mode={mode} isChanged={isChanged} isConflict={isConflict} stateFor={stateFor} syncLabels={labels} healthFor={healthFor} toneFor={toneFor} iconFor={iconFor} onShowInGraph={(text) => { setQuery(text); setMode("all"); onViewChange("graph") }} /> : <TopologyCanvas graph={graph} applicationName={application.name} selectedId={selectedId} onSelect={setSelectedId} query={query} mode={mode} focus={focus} related={related} stateFor={stateFor} syncLabels={labels} describeNode={healthFor} iconFor={iconFor} isChanged={isChanged} isConflict={isConflict} />}
      {selected && <aside aria-label="Resource details" className={`${styles.inspector} min-w-0 space-y-5 border-t p-5 xl:h-[min(72svh,900px)] xl:overflow-auto xl:border-t-0 xl:border-l`}>
        <div className="flex items-start justify-between gap-2"><div className="flex min-w-0 items-center gap-3"><span className="grid size-10 shrink-0 place-items-center rounded-xl border bg-muted/40 text-muted-foreground"><HugeiconsIcon icon={iconFor(selected.identity.kind)} strokeWidth={1.6} className="size-5" aria-hidden="true" /></span><div className="min-w-0"><p className="text-sm font-medium uppercase tracking-widest text-muted-foreground">{selected.identity.kind}</p><h3 className="mt-1 break-all text-base font-semibold">{selected.identity.name}</h3></div></div><Button type="button" variant="ghost" size="icon" aria-label="Close resource details" onClick={() => { setSelectedId(null); setFocus(false) }}><HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} aria-hidden="true" /></Button></div>

        <div className="space-y-2 rounded-xl border bg-muted/20 p-3"><p className={`text-sm font-medium ${toneFor(selected)}`}>{healthFor(selected)}</p><div className="flex flex-wrap gap-2"><StatusBadge status={labels[stateFor(selected)]} />{application.autoSyncPaused && <StatusBadge status="Auto-sync paused" />}</div></div>
        {(plan ?? stalePlan)?.plan.changes.some((change) => identityKey(change.identity) === identityKey(selected.identity)) && <Button type="button" variant="outline" onClick={async () => { if (document.fullscreenElement === root.current) await document.exitFullscreen(); onViewDiff(selected.identity) }}>View diff<HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} data-icon="inline-end" aria-hidden="true" /></Button>}
        {operation && <p className="text-sm text-muted-foreground">Plan operation: {operation.status} · {new Date(operation.finishedAt || operation.startedAt).toLocaleString()}</p>}
        {canManageResources && onResourceActionComplete && selectedManagedResource?.identity.namespace && !selectedManagedResource.identity.clusterScoped && <ResourceActions key={selectedManagedResource.uid} applicationID={application.id} resource={selectedManagedResource} disabled={resourceActionsDisabled} onComplete={onResourceActionComplete} />}
        {["Ingress", "Route"].includes(selected.identity.kind) && <section aria-label="Resource URLs" className="space-y-2 border-t pt-4"><h4 className="text-sm font-semibold">URLs</h4>{resourceURLs(selected).length ? resourceURLs(selected).map((url) => <Button key={url} variant="link" nativeButton={false} render={<a href={url} target="_blank" rel="noopener noreferrer" />} className="h-auto max-w-full justify-start whitespace-normal break-all p-0 text-left text-xs">{url}<span className="shrink-0" aria-hidden="true">↗</span><span className="sr-only"> (opens in a new tab)</span></Button>) : <p className="text-xs text-muted-foreground">No concrete hostname or address is available yet. Wildcard hosts cannot be opened directly.</p>}<p className="text-xs text-muted-foreground">Configured addresses; availability depends on routing and cluster connectivity.</p></section>}
        {selected.routingHealth && <section aria-label="Backend readiness" className="space-y-3 border-t pt-4"><h4 className="text-sm font-semibold">Backend readiness</h4><p className={`text-sm ${toneFor(selected)}`}>{selected.routingHealth.label}</p><p className="text-xs text-muted-foreground">{selected.routingHealth.readyPods}/{selected.routingHealth.totalPods} matching pods ready. Based on Kubernetes observations; DNS, TLS, network policies, and external HTTP reachability have not been tested.</p>{selected.routingHealth.backends?.map((backend, index) => <div key={`${backend.serviceName}-${backend.port}-${index}`} className="space-y-1 rounded-lg border p-3 text-xs"><p className="break-all font-medium">Service/{backend.serviceName}{backend.port ? ` · port ${backend.port}` : ""}</p><p className="text-muted-foreground">{backend.message}</p></div>)}</section>}
        <div><h4 className="text-sm font-semibold">Relationships ({connections.length})</h4><div className="mt-2 space-y-2">{connections.map((edge, i) => {
          const other = graph.nodes.find((node) => node.id === (edge.from === selected.id ? edge.to : edge.from))
          return other ? <Button type="button" key={i} variant="outline" className="h-auto w-full justify-start gap-3 whitespace-normal rounded-lg py-2.5 text-left text-xs" onClick={() => { setSelectedId(other.id); setMode("all") }}><HugeiconsIcon icon={iconFor(other.identity.kind)} strokeWidth={1.6} className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" /><span><span className="inline-flex items-center gap-1">{edge.from !== selected.id && <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} aria-hidden="true" className="size-3" />}{edge.from === selected.id ? edge.relation : `${edge.relation} from`}{edge.from === selected.id && <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} aria-hidden="true" className="size-3" />}</span><span className="mt-1 block break-all font-medium">{other.identity.kind}/{other.identity.name}</span></span></Button> : null
        })}{!connections.length && <p className="text-sm text-muted-foreground">No relationship detected in available manifests or observations.</p>}</div></div>
        {selected.identity.kind === "Pod" && selected.healthSummary && <dl className="grid grid-cols-2 gap-3 border-t pt-4 text-xs"><div><dt className="text-muted-foreground">Container readiness</dt><dd>{selected.healthSummary.readyContainers ?? 0}/{selected.healthSummary.totalContainers ?? 0}</dd></div><div><dt className="text-muted-foreground">Restarts</dt><dd>{selected.healthSummary.restartCount ?? 0}</dd></div></dl>}
        {selected.healthSummary && <Disclosure className="border-t pt-4" summary="Kubernetes health details" defaultOpen={Boolean(selected.healthSummary.failureReason)}><div className="space-y-3">
          <dl className="grid grid-cols-2 gap-2 text-xs [&>div]:rounded-lg [&>div]:border [&>div]:bg-muted/20 [&>div]:p-3 [&_dd]:text-base [&_dd]:tabular-nums">
            {selected.healthSummary.readyReplicas !== undefined && <div><dt className="text-muted-foreground">Ready replicas</dt><dd className="mt-1 font-medium">{selected.healthSummary.readyReplicas}{selected.healthSummary.desiredReplicas !== undefined ? ` / ${selected.healthSummary.desiredReplicas}` : ""}</dd></div>}
            {selected.healthSummary.updatedReplicas !== undefined && <div><dt className="text-muted-foreground">Updated replicas</dt><dd className="mt-1 font-medium">{selected.healthSummary.updatedReplicas}{selected.healthSummary.desiredReplicas !== undefined ? ` / ${selected.healthSummary.desiredReplicas}` : ""}</dd></div>}
            {selected.healthSummary.availableReplicas !== undefined && <div><dt className="text-muted-foreground">Available replicas</dt><dd className="mt-1 font-medium">{selected.healthSummary.availableReplicas}{selected.healthSummary.desiredReplicas !== undefined ? ` / ${selected.healthSummary.desiredReplicas}` : ""}</dd></div>}
            {selected.healthSummary.numberReady !== undefined && <div><dt className="text-muted-foreground">Ready pods</dt><dd className="mt-1 font-medium">{selected.healthSummary.numberReady}{selected.healthSummary.desiredScheduled !== undefined ? ` / ${selected.healthSummary.desiredScheduled}` : ""}</dd></div>}
            {selected.healthSummary.updatedScheduled !== undefined && <div><dt className="text-muted-foreground">Updated pods</dt><dd className="mt-1 font-medium">{selected.healthSummary.updatedScheduled}{selected.healthSummary.desiredScheduled !== undefined ? ` / ${selected.healthSummary.desiredScheduled}` : ""}</dd></div>}
            {selected.healthSummary.succeeded !== undefined && <div><dt className="text-muted-foreground">Job completions</dt><dd className="mt-1 font-medium">{selected.healthSummary.succeeded}{selected.healthSummary.completions !== undefined ? ` / ${selected.healthSummary.completions}` : ""}</dd></div>}
            {selected.healthSummary.active !== undefined && <div><dt className="text-muted-foreground">Active pods</dt><dd className="mt-1 font-medium">{selected.healthSummary.active}</dd></div>}
            {selected.healthSummary.failed !== undefined && <div><dt className="text-muted-foreground">Failed pods</dt><dd className="mt-1 font-medium">{selected.healthSummary.failed}</dd></div>}
          </dl>
          {selected.healthSummary.failureReason && <p className="rounded-md border border-destructive/30 bg-destructive/5 p-2 text-xs text-destructive"><strong>{selected.healthSummary.failureReason}</strong>{selected.healthSummary.failureMessage ? ` · ${selected.healthSummary.failureMessage}` : ""}</p>}
          {selected.healthSummary.conditions?.length ? <div className="space-y-2"><h5 className="text-xs font-semibold">Conditions</h5><ul className="space-y-2">{selected.healthSummary.conditions.map((condition, index) => <li key={`${condition.type}-${index}`} className="rounded-lg border bg-muted/15 p-2.5 text-sm"><Disclosure summary={<span className="flex flex-wrap items-center gap-1.5"><strong>{condition.type}</strong><StatusBadge status={condition.status} />{condition.reason && <span className="font-normal text-muted-foreground">{condition.reason}</span>}</span>}>{condition.message && <p className="whitespace-pre-wrap break-words text-muted-foreground">{condition.message}</p>}{condition.lastTransitionTime && <p className="mt-1 text-sm text-muted-foreground">Transitioned {new Date(condition.lastTransitionTime).toLocaleString()}</p>}</Disclosure></li>)}</ul></div> : <p className="text-sm text-muted-foreground">Kubernetes has not reported explicit status conditions for this resource.</p>}
        </div></Disclosure>}
        <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">{[["Namespace", selected.identity.namespace || "Cluster scope"], ["API version", selected.identity.apiVersion], ["Source", selected.source], ["Last observed", selected.observedAt ? new Date(selected.observedAt).toLocaleString() : "Not available"], ["Resource version", selected.resourceVersion || "Not available"]].map(([key, value]) => <div key={key}><dt className="text-xs text-muted-foreground">{key}</dt><dd className="mt-1 break-words font-medium">{value}</dd></div>)}</dl>
      </aside>}
    </div>
    <footer className="flex flex-wrap items-center gap-x-5 gap-y-2 border-t px-5 py-3 text-xs text-muted-foreground" aria-label="Topology status legend"><span className="font-medium text-foreground">Sync state</span><span className="inline-flex items-center gap-1.5"><i aria-hidden="true" className="size-2 rounded-full bg-success" />Deployed</span><span className="inline-flex items-center gap-1.5"><i aria-hidden="true" className="size-2 rounded-full bg-warning" />Out of sync</span><span className="inline-flex items-center gap-1.5"><i aria-hidden="true" className="size-2 rounded-full bg-info" />Will be created / applying</span><span className="inline-flex items-center gap-1.5"><i aria-hidden="true" className="size-2 rounded-full bg-destructive" />Will be destroyed / failed</span><span className="mx-1 hidden h-4 border-l sm:block" /><span><i aria-hidden="true" className="mr-1 inline-block size-2 rounded-full bg-success" />Runtime readiness is separate from sync. Configuration objects do not require workload health checks.</span></footer>
  </section>
}
