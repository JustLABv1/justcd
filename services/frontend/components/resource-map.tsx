"use client"

import { useEffect, useId, useMemo, useRef, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Layers01Icon, Settings02Icon, Globe02Icon, DatabaseIcon, CubeIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { identityKey, observed, workload, relatedNodes, syncState, topologyLayout, nodeWidth, nodeHeight, type SyncState } from "@/lib/topology-view"
import type { Application, Identity, ManagedResource, Operation, PlanRecord, ResourceTopology, TopologyNode } from "@/lib/types"

const labels: Record<SyncState, string> = { syncing: "Applying", applied: "Deployed · health separate", failed: "Failed", create: "Will be created", update: "Out of sync", delete: "Will be destroyed", synced: "Deployed", unknown: "State unknown", observed: "Observed" }
const tones: Record<SyncState, string> = { syncing: "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-800 dark:bg-blue-950/40 dark:text-blue-200", applied: "border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200", failed: "border-rose-300 bg-rose-50 text-rose-800 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-200", create: "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-800 dark:bg-blue-950/40 dark:text-blue-200", update: "border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200", delete: "border-rose-300 bg-rose-50 text-rose-800 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-200", synced: "border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200", unknown: "border-border bg-muted/50 text-muted-foreground", observed: "border-border bg-muted/50 text-muted-foreground" }
const stateDot: Record<SyncState, string> = { syncing: "bg-blue-600", applied: "bg-emerald-600", failed: "bg-rose-600", create: "bg-blue-600", update: "bg-amber-500", delete: "bg-rose-600", synced: "bg-emerald-600", unknown: "bg-muted-foreground", observed: "bg-muted-foreground" }
const iconFor = (kind: string) => ["Ingress", "Service", "HTTPRoute", "Gateway"].includes(kind) ? Globe02Icon : ["PersistentVolumeClaim", "PersistentVolume"].includes(kind) ? DatabaseIcon : ["Pod", "ReplicaSet"].includes(kind) ? CubeIcon : ["Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob"].includes(kind) ? Layers01Icon : Settings02Icon
function health(node: TopologyNode) {
  if (node.source === "sample") return `Sample · ${node.readiness || node.phase || "unknown"}`
  if (node.readiness) return node.readiness
  if (node.phase) return `Phase: ${node.phase}`
  return node.uid ? "Health not reported" : "Not observed in cluster"
}
function fallback(plan: PlanRecord | null, inventory: ManagedResource[]): ResourceTopology {
  const nodes = new Map<string, TopologyNode>()
  for (const identity of plan?.resources ?? []) nodes.set(identityKey(identity), { id: identityKey(identity), identity, source: "desired" })
  for (const item of inventory) nodes.set(identityKey(item.identity), { id: identityKey(item.identity), identity: item.identity, source: nodes.has(identityKey(item.identity)) ? "desired" : "managed", uid: item.uid, resourceVersion: item.resourceVersion })
  for (const change of plan?.plan.changes ?? []) if (!nodes.has(identityKey(change.identity))) nodes.set(identityKey(change.identity), { id: identityKey(change.identity), identity: change.identity, source: "desired" })
  return { nodes: [...nodes.values()], edges: [], warnings: ["Topology unavailable. Showing inventory without inferred relationships."] }
}

export function ResourceMap({ application, plan, inventory, operations, topology, onViewDiff, onRefresh, refreshing }: {
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
        {!shown.length ? <p className="p-12 text-center text-sm text-muted-foreground">{graph.nodes.length ? "No resources match this view." : "Build a plan to discover resources."}</p> : <div style={{ width: layout.width * zoom, height: layout.height * zoom }}><div className="relative origin-top-left" style={{ width: layout.width, height: layout.height, transform: `scale(${zoom})` }}>
          {layout.bands.map((band) => <div key={band.id} className="absolute rounded-xl border border-dashed bg-card/60" style={{ left: 16, top: band.y, width: layout.width - 32, height: band.height }}><h3 className="px-5 py-3 text-sm font-semibold text-muted-foreground">{band.label}</h3></div>)}
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
              return <g key={index} opacity={selectedId && !active ? .2 : 1}><path d={path} fill="none" stroke={active ? "var(--primary)" : "var(--muted-foreground)"} strokeOpacity={active ? 1 : .5} strokeWidth={active ? 2.5 : 1.75} markerEnd={`url(#${marker})`} />{active && <text x={(x1 + x2) / 2} y={(y1 + y2) / 2 - 8} textAnchor="middle" fontSize="12" fill="var(--primary)" stroke="var(--card)" strokeWidth="5" paintOrder="stroke">{edge.relation}</text>}</g>
            })}
          </svg>
          {shown.map((node) => {
            const point = layout.positions.get(node.id)!, state = stateFor(node)
            const descendants = children.get(node.id) ?? [], pods = descendants.filter((n) => n.identity.kind === "Pod"), replicas = descendants.filter((n) => n.identity.kind === "ReplicaSet")
            const dimmed = (query && !matches.has(node.id)) || (selectedId && !related.has(node.id))
            return <div key={node.id} style={{ left: point.x, top: point.y, width: nodeWidth, height: nodeHeight }} className={`absolute rounded-lg border bg-card shadow-sm ${observed(node) ? "border-dashed" : ""} ${node.id === selectedId ? "border-primary ring-2 ring-primary/25" : state === "syncing" ? "border-blue-500 ring-2 ring-blue-500/20" : state === "failed" || state === "delete" ? "border-rose-400 dark:border-rose-800" : state === "update" ? "border-amber-400 dark:border-amber-800" : state === "create" ? "border-blue-300 dark:border-blue-900" : state === "synced" || state === "applied" ? "border-emerald-300 dark:border-emerald-900" : "border-border"} ${dimmed ? "opacity-30" : ""}`}>
              <button type="button" aria-pressed={node.id === selectedId} onClick={() => setSelectedId(node.id === selectedId ? null : node.id)} className="block w-full rounded-lg p-3 text-left focus-visible:outline-2 focus-visible:outline-primary">
                <span className="flex items-center gap-2 text-xs text-muted-foreground"><HugeiconsIcon icon={iconFor(node.identity.kind)} className="size-4 shrink-0" aria-hidden="true" />{node.identity.kind}</span>
                <span className="mt-1 block truncate text-sm font-semibold" title={node.identity.name}>{node.identity.name}</span>
                <span className={`mt-1 inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs font-semibold ${tones[state]}`}><span aria-hidden="true" className={`size-1.5 shrink-0 rounded-full ${stateDot[state]}`} />{labels[state]}</span>
                <span className={`mt-1 block truncate text-xs ${node.source !== "sample" && node.readiness === "Ready" ? "font-medium text-emerald-700 dark:text-emerald-300" : node.readiness === "Not ready" ? "font-medium text-rose-600 dark:text-rose-300" : "text-muted-foreground"}`}>{health(node)}</span>
              </button>
              {descendants.length > 0 && <button type="button" aria-expanded={expanded.has(node.id)} onClick={() => setExpanded((old) => { const next = new Set(old); if (next.has(node.id)) next.delete(node.id); else next.add(node.id); return next })} className="absolute bottom-1 left-3 right-3 rounded text-left text-xs font-medium text-primary focus-visible:outline-2 focus-visible:outline-primary">{expanded.has(node.id) ? "−" : "+"} Pods {pods.filter((n) => n.readiness === "Ready" && n.source !== "sample").length}/{pods.filter((n) => n.source !== "sample").length} ready · {replicas.length} ReplicaSets{descendants.some((n) => n.source === "sample") ? " · samples" : ""}</button>}
            </div>
          })}
        </div></div>}
      </div>
      {selected && <aside aria-label="Resource details" className="min-w-0 space-y-5 border-t bg-card p-5 xl:h-[min(72svh,900px)] xl:overflow-auto xl:border-t-0 xl:border-l">
        <div className="flex items-start justify-between gap-2"><div className="min-w-0"><p className="text-xs text-muted-foreground">{selected.identity.kind}</p><h3 className="mt-1 break-all text-base font-semibold">{selected.identity.name}</h3></div><Button type="button" variant="ghost" aria-label="Close resource details" onClick={() => { setSelectedId(null); setFocus(false) }}>×</Button></div>
        <dl className="space-y-3 text-sm">{[["Namespace", selected.identity.namespace || "Cluster scope"], ["Sync", labels[stateFor(selected)]], ["Health", health(selected)], ["API version", selected.identity.apiVersion], ["Source", selected.source], ["Last observed", selected.observedAt ? new Date(selected.observedAt).toLocaleString() : "Not available"], ["Resource version", selected.resourceVersion || "Not available"]].map(([key, value]) => <div key={key}><dt className="text-xs text-muted-foreground">{key}</dt><dd className="mt-1 break-all">{value}</dd></div>)}</dl>
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
