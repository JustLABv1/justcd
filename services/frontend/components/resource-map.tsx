"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { Input } from "@/components/ui/input"
import type { Application, Identity, ManagedResource, Operation, PlanRecord, ResourceTopology, TopologyNode } from "@/lib/types"

type Status = "syncing" | "applied" | "failed" | "create" | "update" | "delete" | "synced" | "unknown"
type Mode = "all" | "changes" | "observed"
const labels: Record<Status, string> = { syncing: "Syncing", applied: "Applied", failed: "Failed", create: "To create", update: "Out of sync", delete: "To delete", synced: "In sync", unknown: "Not checked" }
const tones: Record<Status, string> = { syncing:"bg-blue-500", applied:"bg-emerald-500", failed:"bg-rose-500", create:"bg-emerald-500", update:"bg-amber-500", delete:"bg-rose-500", synced:"bg-emerald-500", unknown:"bg-slate-400" }
const nodeWidth = 210, nodeHeight = 68, columnGap = 42, rowGap = 22, inset = 24
const observed = (node: TopologyNode) => node.source === "sample" || node.source === "kubernetes"
const shortKind = (kind: string) => ({ HorizontalPodAutoscaler: "Autoscaler", PersistentVolumeClaim: "Volume claim", ServiceAccount: "Service account", ReplicaSet: "Replica set" })[kind as "HorizontalPodAutoscaler" | "PersistentVolumeClaim" | "ServiceAccount" | "ReplicaSet"] ?? kind
const identityKey = (identity: Identity) => JSON.stringify([identity.clusterId ?? "", identity.apiVersion, identity.kind, identity.namespace, identity.name])
function column(kind: string) {
  if (["Ingress", "HTTPRoute", "Gateway"].includes(kind)) return 0
  if (kind === "Service") return 1
  if (["Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob"].includes(kind)) return 2
  if (kind === "ReplicaSet") return 3
  if (kind === "Pod") return 4
  return 5
}
function fallback(plan: PlanRecord | null, inventory: ManagedResource[]): ResourceTopology {
  const nodes = new Map<string, TopologyNode>()
  for (const identity of plan?.resources ?? []) nodes.set(identityKey(identity), { id: identityKey(identity), identity, source: "desired" })
  for (const item of inventory) nodes.set(identityKey(item.identity), { id: identityKey(item.identity), identity: item.identity, source: nodes.has(identityKey(item.identity)) ? "desired" : "managed", uid: item.uid, resourceVersion: item.resourceVersion })
  for (const change of plan?.plan.changes ?? []) if (!nodes.has(identityKey(change.identity))) nodes.set(identityKey(change.identity), { id: identityKey(change.identity), identity: change.identity, source: "desired" })
  return { planId: plan?.id, nodes: [...nodes.values()], edges: [], warnings: ["Topology API unavailable. Restart the backend to show links and observed Pods."] }
}

export function ResourceMap({ application, plan, inventory, operations, topology, onViewDiff, onRefresh, refreshing }: {
  application: Application; plan: PlanRecord | null; inventory: ManagedResource[]; operations: Operation[]
  topology: ResourceTopology | null; onViewDiff: (identity: Identity) => void; onRefresh: () => void; refreshing: boolean
}) {
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  const [mode, setMode] = useState<Mode>("all")
  const [focus, setFocus] = useState(false)
  const [zoom, setZoom] = useState(0.85)
  const viewport = useRef<HTMLDivElement>(null)
  const graph = topology ?? fallback(plan, inventory)
  const latestOperation = operations.find((item) => item.planId === plan?.id)
  const running = latestOperation?.status === "running" || latestOperation?.status === "queued" ? latestOperation : null
  const completed = new Set(running?.progress?.completed?.map(identityKey) ?? [])
  const current = running?.progress?.current ? identityKey(running.progress.current) : null
  const failed = latestOperation?.status === "failed" && latestOperation.progress?.current ? identityKey(latestOperation.progress.current) : null
  const changes = new Map(plan?.plan.changes.map((change) => [identityKey(change.identity), change]) ?? [])
  const stateFor = (node: TopologyNode): Status => {
    if (observed(node)) return "unknown"
    const key = identityKey(node.identity)
    if (current === key) return "syncing"
    if (failed === key) return "failed"
    if (completed.has(key)) return "applied"
    const change = changes.get(key)
    if (plan?.status === "applied" && node.uid) return "synced"
    if (change && (plan?.status === "current" || plan?.status === "failed")) return change.kind
    if (plan?.status === "current" && node.uid) return "synced"
    return "unknown"
  }
  const shown = useMemo(() => {
    const base = graph.nodes.filter((node) => mode === "all" || (mode === "observed" ? observed(node) : ["create", "update", "delete", "failed", "syncing"].includes(stateFor(node))))
    if (mode === "all" && !focus) return base
    const ids = new Set(base.map((node) => node.id))
    if (focus && selectedId) { ids.clear(); ids.add(selectedId) }
    for (const edge of graph.edges) if (ids.has(edge.from) || ids.has(edge.to)) { ids.add(edge.from); ids.add(edge.to) }
    return graph.nodes.filter((node) => ids.has(node.id))
  // State is derived from the current plan and operations.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [topology, plan, inventory, operations, mode, focus, selectedId])
  const layout = useMemo(() => {
    const counts = [0, 0, 0, 0, 0, 0]
    const positions = new Map<string, { x: number; y: number }>()
    const ordered = [...shown].sort((a, b) => column(a.identity.kind) - column(b.identity.kind) || a.identity.namespace.localeCompare(b.identity.namespace) || a.identity.name.localeCompare(b.identity.name))
    for (const node of ordered) {
      const col = column(node.identity.kind)
      positions.set(node.id, { x: inset + col * (nodeWidth + columnGap), y: inset + counts[col] * (nodeHeight + rowGap) })
      counts[col]++
    }
    return { positions, width: inset * 2 + 6 * nodeWidth + 5 * columnGap, height: Math.max(270, inset * 2 + Math.max(...counts) * (nodeHeight + rowGap) - rowGap) }
  }, [shown])
  const selected = graph.nodes.find((node) => node.id === selectedId)
  const connections = selected ? graph.edges.filter((edge) => edge.from === selected.id || edge.to === selected.id) : []
  const matches = new Set(graph.nodes.filter((node) => `${node.identity.kind} ${node.identity.namespace} ${node.identity.name}`.toLowerCase().includes(query.toLowerCase())).map((node) => node.id))
  const changedCount = graph.nodes.filter((node) => ["create", "update", "delete", "failed"].includes(stateFor(node))).length
  const observedCount = graph.nodes.filter(observed).length
  const sampleCount = graph.nodes.filter((node) => node.source === "sample").length
  const fit = () => setZoom(Math.max(0.45, Math.min(1, (viewport.current?.clientWidth ?? 1100) / layout.width)))
  useEffect(() => {
    if (!query.trim()) return
    const match = [...layout.positions].find(([id]) => matches.has(id))
    if (match) viewport.current?.scrollTo({ left: Math.max(0, match[1].x * zoom - 24), top: Math.max(0, match[1].y * zoom - 24), behavior: "smooth" })
  // Search should move to the first matching node in the current view.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, layout, zoom])

  return <section aria-label="Application resource topology" className="mb-6 min-w-0 overflow-hidden rounded-xl border bg-card">
    <div className="flex flex-wrap items-start justify-between gap-3 border-b px-5 py-4">
      <div><h2 className="text-sm font-semibold">Resource topology</h2><p className="mt-1 text-xs text-muted-foreground">Traffic, workloads, and dependencies for {application.name}</p></div>
      <div className="flex flex-wrap items-center gap-2 text-[11px]"><span className="rounded-full border px-2.5 py-1 tabular-nums">{graph.nodes.length} resources</span><span className="rounded-full border px-2.5 py-1 tabular-nums">{graph.edges.length} links</span><span className="rounded-full border border-amber-200 bg-amber-50 px-2.5 py-1 text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-300">{changedCount} changes</span><span className="rounded-full border px-2.5 py-1 tabular-nums">{sampleCount ? `${sampleCount} sample Pods/replicas` : `${observedCount} observed`}</span><button type="button" onClick={onRefresh} disabled={refreshing} className="rounded-md border px-2.5 py-1 font-medium hover:bg-muted/50 disabled:opacity-50">{refreshing ? "Refreshing…" : sampleCount ? "Refresh map" : "Refresh cluster"}</button></div>
    </div>
    <div className="flex flex-wrap items-center gap-2 border-b px-5 py-3">
      <Input aria-label="Search resources" value={query} onChange={(event) => { setQuery(event.target.value); if (event.target.value.trim()) { setMode("all"); setFocus(false) } }} placeholder="Find a resource…" className="h-8 min-w-0 flex-1 text-xs sm:max-w-60" />
      {query.trim() && <span role="status" className="text-[11px] text-muted-foreground">{matches.size} {matches.size === 1 ? "match" : "matches"}</span>}
      <div role="group" aria-label="Topology view" className="flex rounded-md border p-0.5">{(["all", "changes", "observed"] as const).map((item) => <button type="button" key={item} aria-pressed={mode === item} onClick={() => setMode(item)} className={`rounded px-2.5 py-1.5 text-[11px] capitalize focus-visible:outline-2 focus-visible:outline-primary ${mode === item ? "bg-muted font-semibold" : "text-muted-foreground hover:text-foreground"}`}>{item}</button>)}</div>
      <button type="button" aria-pressed={focus} disabled={!selected} onClick={() => setFocus(!focus)} className="h-8 rounded-md border px-2.5 text-[11px] disabled:opacity-40">{focus ? "Show all links" : "Focus links"}</button>
      <div role="group" aria-label="Zoom" className="ml-auto flex rounded-md border"><button type="button" aria-label="Zoom out" onClick={() => setZoom(Math.max(0.45, +(zoom - 0.1).toFixed(2)))} className="h-8 w-8 border-r text-sm">−</button><button type="button" onClick={fit} className="h-8 min-w-12 border-r px-1 text-[11px] tabular-nums" title="Fit to width">{Math.round(zoom * 100)}%</button><button type="button" aria-label="Zoom in" onClick={() => setZoom(Math.min(1.4, +(zoom + 0.1).toFixed(2)))} className="h-8 w-8 text-sm">+</button></div>
    </div>
    {running && <div role="status" className="mx-5 mt-4 flex flex-wrap justify-between gap-2 rounded-md border border-blue-200 bg-blue-50 px-3 py-2 text-xs text-blue-800 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-200"><span>Sync in progress</span><span className="tabular-nums">{running.progress?.completed?.length ?? 0} / {running.progress?.total ?? plan?.plan.changes.length ?? 0} applied</span></div>}
    {graph.warnings.length > 0 && <div role="status" className="mx-5 mt-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-[11px] text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-300">{graph.warnings.join(" · ")}</div>}
    {graph.nodes.length ? <>
      <div ref={viewport} className="h-[520px] min-w-0 overflow-auto bg-[radial-gradient(circle_at_center,var(--border)_0.7px,transparent_0.7px)] bg-size-[18px_18px]" aria-label="Scrollable topology canvas">
        <div style={{ width: layout.width * zoom, height: layout.height * zoom, position: "relative" }}><div style={{ width: layout.width, height: layout.height, transform: `scale(${zoom})`, transformOrigin: "top left", position: "relative" }}>
          <svg aria-hidden="true" className="pointer-events-none absolute inset-0" width={layout.width} height={layout.height}><defs><marker id="topology-arrow" markerWidth="7" markerHeight="7" refX="6" refY="3.5" orient="auto"><path d="M 0 0 L 7 3.5 L 0 7 z" fill="currentColor" className="text-border" /></marker></defs>{graph.edges.map((edge, index) => {
            const a = layout.positions.get(edge.from), b = layout.positions.get(edge.to)
            if (!a || !b) return null
            const forward = b.x > a.x
            const x1 = forward ? a.x + nodeWidth : a.x, x2 = forward ? b.x : b.x + nodeWidth
            const y1 = a.y + nodeHeight / 2, y2 = b.y + nodeHeight / 2
            const bend = Math.max(50, Math.abs(x2 - x1) * 0.48)
            const path = `M ${x1} ${y1} C ${x1 + (forward ? bend : -bend)} ${y1}, ${x2 - (forward ? bend : -bend)} ${y2}, ${x2} ${y2}`
            const active = selectedId && (edge.from === selectedId || edge.to === selectedId)
            return <path key={`${edge.from}-${edge.to}-${index}`} d={path} fill="none" stroke={active ? "var(--primary)" : "var(--border)"} strokeWidth={active ? 2.5 : 1.5} opacity={selectedId && !active ? 0.45 : 1} markerEnd="url(#topology-arrow)" />
          })}</svg>
          {shown.map((node) => {
            const position = layout.positions.get(node.id)!
            const state = stateFor(node), isObserved = observed(node)
            const dimmed = query.length > 0 && !matches.has(node.id)
            const related = selectedId && graph.edges.some((edge) => (edge.from === selectedId && edge.to === node.id) || (edge.to === selectedId && edge.from === node.id))
            return <button key={node.id} type="button" aria-pressed={selectedId === node.id} onClick={() => setSelectedId(node.id === selectedId ? null : node.id)} style={{ position: "absolute", left: position.x, top: position.y, width: nodeWidth, height: nodeHeight }} className={`min-w-0 rounded-lg border bg-card px-3 py-2 text-left shadow-sm transition-[border-color,opacity,box-shadow] hover:border-primary/60 hover:shadow-md focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary ${isObserved ? "border-dashed" : ""} ${selectedId === node.id ? "z-10 border-primary ring-2 ring-primary/20" : related ? "border-primary/50" : "border-border"} ${dimmed ? "opacity-35" : ""}`}>
              <div className="flex min-w-0 items-center justify-between gap-2"><span className="truncate text-[10px] font-semibold uppercase tracking-wide text-muted-foreground" title={node.identity.kind}>{shortKind(node.identity.kind)}</span><span className={`size-2 shrink-0 rounded-full ${isObserved ? (node.readiness === "Ready" ? "bg-emerald-500" : node.readiness === "Not ready" ? "bg-rose-500" : "bg-slate-400") : tones[state]}`} /></div>
              <p className="mt-1 truncate text-xs font-semibold" title={node.identity.name}>{node.identity.name}</p>
              <p className="mt-0.5 truncate text-[10px] text-muted-foreground">{isObserved ? `${node.source === "sample" ? "Sample · " : ""}${node.readiness || node.phase || "Observed"}` : labels[state]}</p>
            </button>
          })}
        </div></div>
      </div>
      {selected && <div className="border-t bg-muted/10 px-5 py-4"><div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><p className="text-sm font-semibold">{selected.identity.kind} / {selected.identity.name}</p><p className="mt-1 break-all text-[11px] text-muted-foreground">{selected.identity.namespace || "Cluster scope"} · {selected.identity.apiVersion} · {selected.source === "sample" ? "Sample observation" : selected.source === "kubernetes" ? "Observed in cluster" : "Helm/plan resource"}{selected.resourceVersion ? ` · version ${selected.resourceVersion}` : ""}</p></div><div className="flex items-center gap-2">{!observed(selected) && <span className="text-[11px] text-muted-foreground">{labels[stateFor(selected)]}</span>}{changes.has(identityKey(selected.identity)) && <button type="button" onClick={() => onViewDiff(selected.identity)} className="rounded-md border px-2.5 py-1.5 text-[11px] font-medium text-primary hover:bg-muted/50">View diff →</button>}</div></div>
        {connections.length > 0 && <div className="mt-3 flex flex-wrap items-center gap-1.5 text-[11px]"><span className="mr-1 text-muted-foreground">Connected:</span>{connections.map((edge, index) => { const other = graph.nodes.find((node) => node.id === (edge.from === selected.id ? edge.to : edge.from)); return other ? <button type="button" key={index} onClick={() => setSelectedId(other.id)} className="rounded-md border bg-background px-2 py-1 hover:border-primary/50">{edge.from === selected.id ? edge.relation : `${edge.relation} by`} · {other.identity.kind}/{other.identity.name}</button> : null })}</div>}
      </div>}
    </> : <div className="px-5 py-12 text-center"><p className="text-sm font-medium">No resources to map yet</p><p className="mt-1 text-xs text-muted-foreground">Build a plan to render this application’s resources.</p></div>}
    <div className="border-t px-5 py-2.5 text-[10px] leading-4 text-muted-foreground">Links between solid nodes come from desired manifests; dashed ReplicaSets and Pods {sampleCount ? "are explicitly seeded samples in this demo" : "come from read-only Kubernetes discovery"}. Sync state and Pod readiness are separate signals. Scroll, search, or select a node to inspect its links.</div>
  </section>
}
