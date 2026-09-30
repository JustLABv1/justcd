"use client"

import { useEffect, useId, useMemo, useRef, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Layers01Icon, Settings02Icon, Globe02Icon, DatabaseIcon, CubeIcon, Router02Icon, ServerStack01Icon, Files01Icon, FileTextIcon, Key01Icon, Shield01Icon, Clock01Icon, PlayIcon, Route01Icon, WorkflowSquare04Icon, Folder01Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { EmptyState, StatusBadge } from "@/components/ui-kit"
import { ResourceActions } from "@/components/resource-actions"
import { Input } from "@/components/ui/input"
import { identityKey, observed, workload, relatedNodes, syncState, topologyLayout, nodeWidth, nodeHeight, type SyncState } from "@/lib/topology-view"
import type { Application, Identity, ManagedResource, Operation, PlanRecord, ResourceTopology, TopologyNode } from "@/lib/types"

const labels: Record<SyncState, string> = { syncing: "Applying", applied: "Deployed · health separate", failed: "Failed", create: "Will be created", update: "Out of sync", delete: "Will be destroyed", synced: "Deployed", unknown: "State unknown", observed: "Observed" }
const stateDot: Record<SyncState, string> = { syncing: "bg-blue-600", applied: "bg-emerald-600", failed: "bg-rose-600", create: "bg-blue-600", update: "bg-amber-500", delete: "bg-rose-600", synced: "bg-emerald-600", unknown: "bg-muted-foreground", observed: "bg-muted-foreground" }
const resourceIcons: Record<string, typeof CubeIcon> = {
  Service: Router02Icon, Pod: CubeIcon, Deployment: Layers01Icon,
  ReplicaSet: Files01Icon, StatefulSet: ServerStack01Icon, DaemonSet: WorkflowSquare04Icon,
  Ingress: Globe02Icon, Gateway: Globe02Icon, HTTPRoute: Route01Icon,
  ConfigMap: FileTextIcon, Secret: Key01Icon,
  PersistentVolumeClaim: DatabaseIcon, PersistentVolume: DatabaseIcon,
  Job: PlayIcon, CronJob: Clock01Icon, Namespace: Folder01Icon,
  ServiceAccount: Shield01Icon, Role: Shield01Icon, RoleBinding: Shield01Icon,
  ClusterRole: Shield01Icon, ClusterRoleBinding: Shield01Icon, NetworkPolicy: Shield01Icon,
}
const iconFor = (kind: string) => resourceIcons[kind] ?? Settings02Icon
function health(node: TopologyNode) {
  if (node.source === "sample") return `Sample · ${node.readiness || node.phase || "unknown"}`
  if (node.readiness) return node.readiness
  const details = node.healthSummary
  if (details?.readyReplicas !== undefined && details.desiredReplicas !== undefined) return `Ready ${details.readyReplicas}/${details.desiredReplicas} replicas`
  if (details?.numberReady !== undefined && details.desiredScheduled !== undefined) return `Ready ${details.numberReady}/${details.desiredScheduled} pods`
  if (details?.succeeded !== undefined && details.completions !== undefined) return `Completed ${details.succeeded}/${details.completions}`
  const condition = details?.conditions?.find((item) => item.status !== "True") ?? details?.conditions?.[0]
  if (condition) return `${condition.type}: ${condition.status}${condition.reason ? ` · ${condition.reason}` : ""}`
  if (details?.failureReason) return details.failureReason
  if (node.phase) return `Phase: ${node.phase}`
  return node.uid ? "Health not reported" : "Not observed in cluster"
}
function healthTone(node: TopologyNode) {
  if (node.readiness === "Ready" || node.phase === "Succeeded") return "font-medium text-emerald-700 dark:text-emerald-300"
  if (node.readiness === "Not ready" || node.phase === "Failed" || node.healthSummary?.failureReason || node.healthSummary?.conditions?.some((item) => item.status === "False")) return "font-medium text-rose-600 dark:text-rose-300"
  if (node.readiness === "Unknown") return "font-medium text-amber-700 dark:text-amber-300"
  return "text-muted-foreground"
}
function fallback(plan: PlanRecord | null, inventory: ManagedResource[]): ResourceTopology {
  const nodes = new Map<string, TopologyNode>()
  for (const identity of plan?.resources ?? []) nodes.set(identityKey(identity), { id: identityKey(identity), identity, source: "desired" })
  for (const item of inventory) nodes.set(identityKey(item.identity), { id: identityKey(item.identity), identity: item.identity, source: nodes.has(identityKey(item.identity)) ? "desired" : "managed", uid: item.uid, resourceVersion: item.resourceVersion })
  for (const change of plan?.plan.changes ?? []) if (!nodes.has(identityKey(change.identity))) nodes.set(identityKey(change.identity), { id: identityKey(change.identity), identity: change.identity, source: "desired" })
  return { nodes: [...nodes.values()], edges: [], warnings: ["Topology unavailable. Showing inventory without inferred relationships."] }
}

export function ResourceMap({ application, plan, inventory, operations, topology, onViewDiff, onRefresh, refreshing, canManageResources = false, resourceActionsDisabled = false, onResourceActionComplete }: {
  canManageResources?: boolean; resourceActionsDisabled?: boolean; onResourceActionComplete?: () => Promise<void>
  application: Application; plan: PlanRecord | null; inventory: ManagedResource[]; operations: Operation[]
  topology: ResourceTopology | null; onViewDiff: (identity: Identity) => void; onRefresh: () => void; refreshing: boolean
}) {
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  const [mode, setMode] = useState("all")
  const [focus, setFocus] = useState(false)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [zoom, setZoom] = useState(1)
  const [fullscreen, setFullscreen] = useState(false)
  const [notice, setNotice] = useState("")
  const root = useRef<HTMLElement>(null)
  const viewport = useRef<HTMLDivElement>(null)
  const drag = useRef<{ x: number; y: number; left: number; top: number } | null>(null)
  const marker = useId().replaceAll(":", "")
  const graph = useMemo(() => topology ?? fallback(plan, inventory), [topology, plan, inventory])
  const operation = operations.find((item) => item.planId === plan?.id)
  const running = operation && ["running", "queued"].includes(operation.status)
  const stateFor = (node: TopologyNode) => syncState(node, plan, operation)
  const selected = graph.nodes.find((node) => node.id === selectedId)
  const selectedManagedResource = selected && inventory.find((resource) => identityKey(resource.identity) === identityKey(selected.identity))
  const related = useMemo(() => selectedId ? relatedNodes(graph, selectedId) : new Set<string>(), [graph, selectedId])
  const children = useMemo(() => {
    const result = new Map<string, TopologyNode[]>()
    for (const anchor of graph.nodes.filter(workload)) {
      const ids = new Set([anchor.id]), queue = [anchor.id]
      for (let i = 0; i < queue.length; i++) for (const edge of graph.edges) if (edge.from === queue[i] && edge.relation === "owns" && !ids.has(edge.to)) { ids.add(edge.to); queue.push(edge.to) }
      result.set(anchor.id, graph.nodes.filter((node) => ids.has(node.id) && observed(node) && ["Pod", "ReplicaSet"].includes(node.identity.kind)))
    }
    return result
  }, [graph])
  const hidden = new Set([...children].flatMap(([id, nodes]) => expanded.has(id) || query.trim() || mode === "observed" ? [] : nodes.filter((node) => node.id !== selectedId).map((node) => node.id)))
  const matches = new Set(graph.nodes.filter((node) => `${node.identity.kind} ${node.identity.namespace} ${node.identity.name}`.toLowerCase().includes(query.toLowerCase())).map((node) => node.id))
  const shown = graph.nodes.filter((node) => !hidden.has(node.id) && (!focus || !selectedId || related.has(node.id)) && (mode === "all" || (mode === "observed" ? observed(node) : ["create", "update", "delete", "failed", "syncing"].includes(stateFor(node)))))
  const layout = topologyLayout(graph, shown)
  function center(id: string, scale = zoom) {
    const point = layout.positions.get(id), el = viewport.current
    if (point && el) el.scrollTo({ left: Math.max(0, (point.x + nodeWidth / 2) * scale - el.clientWidth / 2), top: Math.max(0, (point.y + nodeHeight / 2) * scale - el.clientHeight / 2) })
  }
  function fit() {
    const el = viewport.current
    if (!el) return
    setZoom(Math.max(0.15, Math.min(1, (el.clientWidth - 24) / layout.width, (el.clientHeight - 24) / layout.height)))
    el.scrollTo({ left: 0, top: 0 })
  }
  async function toggleFullscreen() {
    try { if (document.fullscreenElement === root.current) await document.exitFullscreen(); else await root.current?.requestFullscreen() }
    catch { setNotice("Fullscreen is unavailable in this browser.") }
  }
  useEffect(() => {
    const update = () => setFullscreen(document.fullscreenElement === root.current)
    document.addEventListener("fullscreenchange", update)
    return () => document.removeEventListener("fullscreenchange", update)
  }, [])
  useEffect(() => {
    if (!query.trim()) return
    const match = shown.find((node) => matches.has(node.id))
    if (match) center(match.id)
    // Search pans to an actual visible match without changing the selected resource.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query])
  const connections = selected ? graph.edges.filter((edge) => edge.from === selected.id || edge.to === selected.id) : []
  const changedCount = graph.nodes.filter((node) => ["create", "update", "delete", "failed"].includes(stateFor(node))).length

  return <section ref={root} aria-label="Application resource topology" className="mb-6 min-w-0 overflow-hidden rounded-xl border bg-card fullscreen:overflow-auto fullscreen:rounded-none">
    <header className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
      <div><h2 className="text-base font-semibold">Resource topology</h2><p className="mt-1 text-sm text-muted-foreground">{application.name} · {graph.nodes.length} resources · {changedCount} changes · {graph.nodes.filter(observed).length} observed</p></div>
      <div className="flex gap-2"><Button type="button" variant="outline" onClick={onRefresh} loading={refreshing} loadingText="Refreshing…">Refresh cluster</Button><Button type="button" variant="outline" onClick={() => void toggleFullscreen()}>{fullscreen ? "Exit fullscreen" : "Fullscreen"}</Button></div>
    </header>
    <div className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
      <Input aria-label="Search resources" placeholder="Find a resource…" className="w-full sm:w-56" value={query} onChange={(event) => { setQuery(event.target.value); setMode("all"); setFocus(false) }} />
      <div role="group" aria-label="Topology filter" className="flex gap-1">{["all", "changes", "observed"].map((value) => <Button type="button" key={value} variant={mode === value ? "secondary" : "ghost"} aria-pressed={mode === value} onClick={() => setMode(value)} className="capitalize">{value}</Button>)}</div>
      <Button type="button" variant="outline" disabled={!selected} aria-pressed={focus} onClick={() => setFocus(!focus)}>{focus ? "Show all resources" : "Focus selection"}</Button>
      <div className="ml-auto flex flex-wrap items-center gap-1">
        <Button type="button" variant="outline" onClick={fit}>Fit all</Button>
        <Button type="button" variant="outline" disabled={!selected || !layout.positions.has(selected.id)} onClick={() => selected && center(selected.id)}>Center selection</Button>
        <Button type="button" variant="ghost" aria-label="Zoom out" onClick={() => setZoom(Math.max(.15, zoom - .1))}>−</Button>
        <Button type="button" variant="ghost" aria-label="Reset zoom to 100 percent" onClick={() => setZoom(1)}>{Math.round(zoom * 100)}%</Button>
        <Button type="button" variant="ghost" aria-label="Zoom in" onClick={() => setZoom(Math.min(1.6, zoom + .1))}>+</Button>
      </div>
    </div>
    {query && <p role="status" className="px-5 py-2 text-sm text-muted-foreground">{matches.size} matching resources</p>}
    {(running || operation?.status === "failed") && <div role="status" className="border-b bg-muted/30 px-5 py-3 text-sm"><strong>{operation?.type === "rollback" ? "Rollback" : "Sync"} {operation?.status === "failed" ? "stopped" : operation?.status === "queued" ? "queued" : "running"}</strong> · {operation?.progress?.completed.length ?? 0}/{operation?.progress?.total ?? 0} resource steps completed{operation?.progress?.current ? ` · ${operation.progress.current.kind}/${operation.progress.current.name}` : ""}<span className="mt-1 block text-muted-foreground">Applied does not mean Healthy. {operation?.status === "failed" ? operation.message : ""}</span></div>}
    {(notice || graph.warnings.length > 0) && <p role="status" className="border-b px-5 py-3 text-sm text-amber-700 dark:text-amber-300">{[notice, ...graph.warnings].filter(Boolean).join(" · ")}</p>}
    <div className={`grid min-w-0 ${selected ? "xl:grid-cols-[minmax(0,1fr)_320px]" : ""}`}>
      <div ref={viewport} tabIndex={0} aria-label="Topology canvas. Drag empty space to pan; use arrow keys to scroll." className="relative h-[min(72svh,900px)] min-h-96 min-w-0 touch-pan-x touch-pan-y overflow-auto bg-muted/10 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary fullscreen:h-[80svh]" onPointerDown={(event) => {
        if (event.button !== 0 || event.pointerType === "touch" || (event.target as HTMLElement).closest("button")) return
        const el = event.currentTarget
        drag.current = { x: event.clientX, y: event.clientY, left: el.scrollLeft, top: el.scrollTop }
        el.setPointerCapture(event.pointerId); el.style.cursor = "grabbing"
      }} onPointerMove={(event) => { if (drag.current) { event.currentTarget.scrollLeft = drag.current.left - event.clientX + drag.current.x; event.currentTarget.scrollTop = drag.current.top - event.clientY + drag.current.y } }} onPointerUp={(event) => { drag.current = null; event.currentTarget.style.cursor = ""; if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId) }} onPointerCancel={() => { drag.current = null }}>
        {!shown.length ? <div className="grid min-h-full place-items-center"><EmptyState title={graph.nodes.length ? "No matching resources" : "No resources to show yet"} description={graph.nodes.length ? "Try another filter to see resources in this view." : "Build a plan to discover resources and their relationships."} /></div> : <div style={{ width: layout.width * zoom, height: layout.height * zoom }}><div className="relative origin-top-left" style={{ width: layout.width, height: layout.height, transform: `scale(${zoom})` }}>
          {layout.bands.map((band) => <div key={band.id} className="pointer-events-none absolute border-t border-border/60" style={{ left: 28, top: band.y, width: layout.width - 56, height: band.height }}><h3 className="py-3 text-xs font-medium text-muted-foreground">{band.label}</h3></div>)}
          <svg aria-hidden="true" className="pointer-events-none absolute inset-0" width={layout.width} height={layout.height}>
            <defs><marker id={marker} markerWidth="7" markerHeight="7" refX="6" refY="3.5" orient="auto"><path d="M0 0L7 3.5L0 7Z" fill="context-stroke" /></marker></defs>
            {graph.edges.map((edge, index) => {
              const a = layout.positions.get(edge.from), b = layout.positions.get(edge.to)
              if (!a || !b) return null
              const forward = b.x > a.x, sameColumn = a.x === b.x
              const x1 = a.x + (forward || sameColumn ? nodeWidth : 0), x2 = b.x + (forward ? 0 : nodeWidth), y1 = a.y + nodeHeight / 2, y2 = b.y + nodeHeight / 2
              const siblingFrom = graph.edges.filter((candidate) => candidate.from === edge.from)
              const siblingTo = graph.edges.filter((candidate) => candidate.to === edge.to)
              const sourceLane = siblingFrom.indexOf(edge) - (siblingFrom.length - 1) / 2
              const targetLane = siblingTo.indexOf(edge) - (siblingTo.length - 1) / 2
              const lane = Math.max(-14, Math.min(14, (sourceLane + targetLane) * 4))
              const arc = Math.abs(y2 - y1) < 32 ? -10 : 0
              const controlY1 = y1 + (y2 - y1) * .22 + arc + lane
              const controlY2 = y2 - (y2 - y1) * .22 + arc + lane
              const bend = sameColumn ? 28 : Math.min(48, Math.max(12, Math.abs(x2 - x1) * .34))
              const active = selectedId && related.has(edge.from) && related.has(edge.to)
              const direction = forward || sameColumn ? 1 : -1
              const path = sameColumn
                ? `M${x1} ${y1} C${x1 + bend} ${controlY1},${x2 + bend} ${controlY2},${x2} ${y2}`
                : `M${x1} ${y1} C${x1 + direction * bend} ${controlY1},${x2 - direction * bend} ${controlY2},${x2} ${y2}`
              return <g key={index} opacity={selectedId && !active ? .2 : 1}><path d={path} fill="none" stroke={active ? "var(--foreground)" : "var(--muted-foreground)"} strokeOpacity={active ? .65 : .3} strokeWidth={active ? 1.5 : 1} markerEnd={`url(#${marker})`} />{active && <text x={(x1 + x2) / 2} y={(y1 + y2) / 2 - 8} textAnchor="middle" fontSize="11" fill="var(--muted-foreground)" stroke="var(--card)" strokeWidth="5" paintOrder="stroke">{edge.relation}</text>}</g>
            })}
          </svg>
          {shown.map((node) => {
            const point = layout.positions.get(node.id)!, state = stateFor(node)
            const descendants = children.get(node.id) ?? [], pods = descendants.filter((n) => n.identity.kind === "Pod"), replicas = descendants.filter((n) => n.identity.kind === "ReplicaSet")
            const dimmed = (query && !matches.has(node.id)) || (selectedId && !related.has(node.id))
            return <div key={node.id} style={{ left: point.x, top: point.y, width: nodeWidth, height: nodeHeight }} className={`absolute overflow-hidden rounded-lg border bg-card ${node.id === selectedId ? "border-foreground/40 ring-2 ring-foreground/10" : "border-border hover:border-foreground/25"} ${dimmed ? "opacity-30" : ""}`}>
              <button type="button" aria-pressed={node.id === selectedId} onClick={() => setSelectedId(node.id === selectedId ? null : node.id)} className={`block w-full rounded-lg px-3.5 py-3 text-left focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-primary ${descendants.length ? "h-[98px]" : "h-full"}`}>
                <span className="flex items-center gap-3">
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted/70 text-foreground/70"><HugeiconsIcon icon={iconFor(node.identity.kind)} strokeWidth={1.6} className="size-5" aria-hidden="true" /></span>
                  <span className="min-w-0"><span className="block text-[11px] leading-4 text-muted-foreground">{node.identity.kind}</span><span className="block truncate text-[13px] font-medium leading-5" title={node.identity.name}>{node.identity.name}</span></span>
                </span>
                <span className="mt-2.5 flex items-center gap-1.5 text-[11px] leading-4 text-foreground/80"><span aria-hidden="true" className={`size-1.5 shrink-0 rounded-full ${stateDot[state]}`} />{labels[state]}</span>
                <span title={health(node)} className={`block truncate pl-3 text-[11px] leading-4 ${node.source === "sample" ? "text-muted-foreground" : healthTone(node)}`}>{health(node)}</span>
              </button>
              {descendants.length > 0 && <button type="button" aria-expanded={expanded.has(node.id)} onClick={() => setExpanded((old) => { const next = new Set(old); if (next.has(node.id)) next.delete(node.id); else next.add(node.id); return next })} className="absolute inset-x-0 bottom-0 border-t bg-muted/20 px-3.5 py-1 text-left text-[10px] font-medium text-muted-foreground hover:text-foreground focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-primary">{expanded.has(node.id) ? "−" : "+"} Pods {pods.filter((n) => n.readiness === "Ready" && n.source !== "sample").length}/{pods.filter((n) => n.source !== "sample").length} ready · {replicas.length} ReplicaSets{descendants.some((n) => n.source === "sample") ? " · samples" : ""}</button>}
            </div>
          })}
        </div></div>}
      </div>
      {selected && <aside aria-label="Resource details" className="min-w-0 space-y-5 border-t bg-card p-5 xl:h-[min(72svh,900px)] xl:overflow-auto xl:border-t-0 xl:border-l">
        <div className="flex items-start justify-between gap-2"><div className="min-w-0"><p className="text-xs text-muted-foreground">{selected.identity.kind}</p><h3 className="mt-1 break-all text-base font-semibold">{selected.identity.name}</h3></div><Button type="button" variant="ghost" aria-label="Close resource details" onClick={() => { setSelectedId(null); setFocus(false) }}>×</Button></div>
        {canManageResources && onResourceActionComplete && selectedManagedResource?.identity.namespace && !selectedManagedResource.identity.clusterScoped && <ResourceActions key={selectedManagedResource.uid} applicationID={application.id} resource={selectedManagedResource} disabled={resourceActionsDisabled} onComplete={onResourceActionComplete} />}
        <dl className="space-y-3 text-sm">{[["Namespace", selected.identity.namespace || "Cluster scope"], ["Sync", labels[stateFor(selected)]], ["Health", health(selected)], ["API version", selected.identity.apiVersion], ["Source", selected.source], ["Last observed", selected.observedAt ? new Date(selected.observedAt).toLocaleString() : "Not available"], ["Resource version", selected.resourceVersion || "Not available"]].map(([key, value]) => <div key={key}><dt className="text-xs text-muted-foreground">{key}</dt><dd className="mt-1 break-all">{value}</dd></div>)}</dl>
        {selected.healthSummary && <section aria-label="Kubernetes health details" className="space-y-3 border-t pt-4">
          <h4 className="text-sm font-semibold">Kubernetes health details</h4>
          <dl className="grid grid-cols-2 gap-2 text-xs">
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
          {selected.healthSummary.conditions?.length ? <div className="space-y-2"><h5 className="text-xs font-semibold">Conditions</h5><ul className="space-y-2">{selected.healthSummary.conditions.map((condition, index) => <li key={`${condition.type}-${index}`} className="rounded-md border p-2 text-xs"><div className="flex flex-wrap items-center gap-1.5"><strong>{condition.type}</strong><StatusBadge status={condition.status} />{condition.reason && <span className="text-muted-foreground">{condition.reason}</span>}</div>{condition.message && <p className="mt-1 whitespace-pre-wrap break-words text-muted-foreground">{condition.message}</p>}{condition.lastTransitionTime && <p className="mt-1 text-[10px] text-muted-foreground">Transitioned {new Date(condition.lastTransitionTime).toLocaleString()}</p>}</li>)}</ul></div> : <p className="text-xs text-muted-foreground">Kubernetes has not reported explicit status conditions for this resource.</p>}
        </section>}
        {operation && <p className="text-xs text-muted-foreground">Plan operation: {operation.status} · {new Date(operation.finishedAt || operation.startedAt).toLocaleString()}</p>}
        {plan?.plan.changes.some((change) => identityKey(change.identity) === identityKey(selected.identity)) && <Button type="button" variant="outline" onClick={async () => { if (document.fullscreenElement === root.current) await document.exitFullscreen(); onViewDiff(selected.identity) }}>View diff →</Button>}
        <div><h4 className="text-sm font-semibold">Relationships ({connections.length})</h4><div className="mt-2 space-y-2">{connections.map((edge, i) => {
          const other = graph.nodes.find((node) => node.id === (edge.from === selected.id ? edge.to : edge.from))
          return other ? <Button type="button" key={i} variant="outline" className="h-auto w-full justify-start whitespace-normal py-2 text-left text-xs" onClick={() => { setSelectedId(other.id); setMode("all") }}><span>{edge.from === selected.id ? `${edge.relation} →` : `← ${edge.relation} from`}<span className="mt-1 block break-all font-medium">{other.identity.kind}/{other.identity.name}</span></span></Button> : null
        })}{!connections.length && <p className="text-sm text-muted-foreground">No relationship detected in available manifests or observations.</p>}</div></div>
      </aside>}
    </div>
    <footer className="flex flex-wrap items-center gap-x-5 gap-y-2 border-t px-5 py-3 text-xs text-muted-foreground" aria-label="Topology status legend"><span className="font-medium text-foreground">Sync state</span><span className="inline-flex items-center gap-1.5"><i aria-hidden="true" className="size-2 rounded-full bg-emerald-600" />Deployed</span><span className="inline-flex items-center gap-1.5"><i aria-hidden="true" className="size-2 rounded-full bg-amber-500" />Out of sync</span><span className="inline-flex items-center gap-1.5"><i aria-hidden="true" className="size-2 rounded-full bg-blue-600" />Will be created / applying</span><span className="inline-flex items-center gap-1.5"><i aria-hidden="true" className="size-2 rounded-full bg-rose-600" />Will be destroyed / failed</span><span className="mx-1 hidden h-4 border-l sm:block" /><span><i aria-hidden="true" className="mr-1 inline-block size-2 rounded-full bg-emerald-600" />Pod Ready is health; deployed resources without reported health say so explicitly.</span></footer>
  </section>
}
