"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Badge } from "@/components/panelui/badge"
import { Flow } from "@/components/panelui/flow"
import { EmptyState } from "@/components/ui-kit"
import { flowModel, type FlowRow } from "@/lib/topology-flow"
import { runtimeSignal, type RuntimeSignal } from "@/lib/topology-map"
import { podLabel, podState, type PodState } from "@/lib/runtime-health"
import type { SyncState } from "@/lib/topology-view"
import type { ResourceTopology, TopologyNode } from "@/lib/types"

type Icon = Parameters<typeof HugeiconsIcon>[0]["icon"]


type Props = {
  graph: ResourceTopology
  selectedId: string | null
  onSelect: (id: string | null) => void
  query: string
  mode: string
  focus: boolean
  related: ReadonlySet<string>
  stateFor: (node: TopologyNode) => SyncState
  syncLabels: Record<SyncState, string>
  healthFor: (node: TopologyNode) => string
  toneFor: (node: TopologyNode) => string
  iconFor: (kind: string) => Icon
  isChanged: (node: TopologyNode) => boolean
  isConflict: (node: TopologyNode) => boolean
}

const NODE_W = 256
const NODE_H = 112
const GAP_X = 80
const GAP_Y = 20
const ROW_GAP = 48
const MARGIN = 8

type Chip = { label: string; color: "success" | "warning" | "destructive" | "muted" }
const signalChip: Record<RuntimeSignal, Chip> = {
  healthy: { label: "Healthy", color: "success" },
  progressing: { label: "Progressing", color: "warning" },
  degraded: { label: "Degraded", color: "destructive" },
  unknown: { label: "Not observed", color: "muted" },
}
const podChip: Record<PodState, Chip["color"]> = { ready: "success", starting: "warning", failing: "destructive", terminating: "muted", completed: "muted", unknown: "muted", sample: "muted" }

/** Workload rollout in a few words, from the replica counts Kubernetes reports. */
function rollout(node: TopologyNode, olderRevisions: number) {
  const health = node.healthSummary
  const parts: string[] = []
  if (health?.desiredReplicas !== undefined) {
    parts.push(`${health.readyReplicas ?? 0}/${health.desiredReplicas} ready`)
    if (health.updatedReplicas !== undefined && health.updatedReplicas < health.desiredReplicas) parts.push(`${health.updatedReplicas} updated`)
  }
  if (olderRevisions > 0) parts.push(`${olderRevisions} older ${olderRevisions === 1 ? "revision" : "revisions"} at zero`)
  const rolling = health?.desiredReplicas !== undefined && health.updatedReplicas !== undefined && health.updatedReplicas < health.desiredReplicas
  return { text: parts.join(" · "), rolling }
}

/** One card per resource, left to right: traffic → service → workload → pods, joined by the topology's own relations. */
export function TopologyFlow(props: Props) {
  const { graph, selectedId, onSelect, query, mode, focus, related, stateFor, syncLabels, healthFor, toneFor, iconFor, isChanged, isConflict } = props
  const model = useMemo(() => flowModel(graph), [graph])
  const stage = useRef<HTMLDivElement>(null)
  const [box, setBox] = useState<{ width: number; height: number } | null>(null)
  useEffect(() => {
    const element = stage.current
    if (!element) return
    // The workbench's columns animate open, so wait until the stage has stopped changing size.
    let timer: ReturnType<typeof setTimeout> | undefined
    const settle = () => { clearTimeout(timer); timer = setTimeout(() => setBox((current) => current ?? { width: element.clientWidth, height: element.clientHeight }), 250) }
    const observer = new ResizeObserver(settle)
    observer.observe(element)
    settle()
    return () => { observer.disconnect(); clearTimeout(timer) }
  })
  const needle = query.trim().toLowerCase()
  const matches = (node: TopologyNode) =>
    (!needle || `${node.identity.kind} ${node.identity.namespace} ${node.identity.name}`.toLowerCase().includes(needle)) &&
    (mode === "all" || (mode === "observed" ? ["kubernetes", "sample"].includes(node.source) : mode === "conflicts" ? isConflict(node) : isChanged(node)))
  const nodeIndex = useMemo(() => new Map(graph.nodes.map((node) => [node.id, node])), [graph])
  const filtering = Boolean(needle) || mode !== "all"
  const rowVisible = (row: FlowRow) => {
    if (focus && selectedId) return row.members.has(selectedId) || [...row.members].some((id) => related.has(id))
    return !filtering || [...row.members].some((id) => { const node = nodeIndex.get(id); return node && matches(node) })
  }
  const rows = model.rows.filter(rowVisible)
  const other = focus && selectedId ? [] : model.other.filter((node) => !filtering || matches(node))
  const isBad = (node: TopologyNode) => isConflict(node) || toneFor(node).includes("text-destructive")

  type Placed = { node: TopologyNode; x: number; y: number; older?: number }
  const edgeColor = (target: TopologyNode) => {
    if (target.identity.kind === "Pod") { const state = podState(target); return state === "failing" ? "destructive" as const : state === "starting" ? "warning" as const : state === "ready" ? "success" as const : undefined }
    const signal = runtimeSignal(target)
    return isBad(target) || signal === "degraded" ? "destructive" as const : signal === "progressing" ? "warning" as const : signal === "healthy" ? "success" as const : undefined
  }

  const card = ({ node, x, y, older = 0 }: Placed) => {
    const pod = node.identity.kind === "Pod"
    const workload = ["Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob"].includes(node.identity.kind)
    const state = pod ? podState(node) : null
    const bad = isBad(node)
    const sync = stateFor(node)
    const flow = workload ? rollout(node, older) : null
    const chip: Chip = isConflict(node) ? { label: "Owned elsewhere", color: "warning" } : pod && state ? { label: podLabel(node), color: podChip[state] } : flow?.rolling ? { label: "Rolling out", color: "warning" } : signalChip[runtimeSignal(node)]
    const detail = pod ? [(node.healthSummary?.restartCount ?? 0) > 0 ? `${node.healthSummary?.restartCount} restarts` : "", node.healthSummary ? `${node.healthSummary.readyContainers ?? 0}/${node.healthSummary.totalContainers ?? 0} containers ready` : ""].filter(Boolean).join(" · ") : flow?.text || healthFor(node)
    const extra = !pod && flow?.text ? healthFor(node) : ""
    const edge = bad || state === "failing" ? "border-destructive/50" : ""
    return <Flow.Node key={node.id} id={node.id} position={{ x, y }} selected={selectedId === node.id} onPress={() => onSelect(node.id)} aria-label={`${node.identity.kind} ${node.identity.name}: ${chip.label}`} className={`overflow-hidden ${edge}`} style={{ width: NODE_W, height: NODE_H }}>
      <Flow.NodeHeader className="py-2">
        <span className="flex min-w-0 items-center gap-2 text-sm text-muted-foreground"><HugeiconsIcon icon={iconFor(node.identity.kind)} strokeWidth={1.6} className="size-4 shrink-0" aria-hidden="true" /><span className="truncate">{node.identity.kind}</span></span>
        <span className="flex shrink-0 items-center gap-1">{sync !== "unknown" && sync !== "observed" && <Flow.NodeBadge color="muted">{syncLabels[sync]}</Flow.NodeBadge>}<Flow.NodeBadge color={chip.color} className={state === "starting" ? "animate-pulse" : ""}>{chip.label}</Flow.NodeBadge></span>
      </Flow.NodeHeader>
      <Flow.NodeContent className="px-4 py-2.5">
        <Flow.NodeTitle className="truncate" title={node.identity.name}>{node.identity.name}</Flow.NodeTitle>
        <Flow.NodeDescription className={`line-clamp-2 ${bad && !flow?.text ? "text-destructive" : ""}`}>{detail}{extra && <span className={bad ? "block truncate text-destructive" : "block truncate"}>{extra}</span>}</Flow.NodeDescription>
      </Flow.NodeContent>
    </Flow.Node>
  }

  const layout = rows.reduce<{ items: { placed: Placed[]; edges: { from: string; to: string; color: ReturnType<typeof edgeColor>; animated: boolean }[] }[]; top: number }>((acc, row) => {
    const { workload } = row
    const pods = workload.identity.kind === "Pod" ? [] : row.pods
    const older = row.groups.filter((group) => group.pods.length === 0).length
    const columns = [row.traffic, row.services, [workload], pods].filter((column) => column.length > 0)
    const placed: Placed[] = []
    columns.forEach((column, c) => column.forEach((node, r) => placed.push({ node, x: MARGIN + c * (NODE_W + GAP_X), y: acc.top + r * (NODE_H + GAP_Y), older: node.id === workload.id ? older : 0 })))
    const tallest = Math.max(...columns.map((column) => column.length))
    let bottom = acc.top + tallest * (NODE_H + GAP_Y) - GAP_Y
    if (row.config.length) { bottom += GAP_Y * 2; row.config.forEach((node, i) => placed.push({ node, x: MARGIN + i * (NODE_W + GAP_X), y: bottom })); bottom += NODE_H }
    const edges: { from: string; to: string; color: ReturnType<typeof edgeColor>; animated: boolean }[] = []
    const add = (from: string, to: TopologyNode) => edges.push({ from, to: to.id, color: edgeColor(to), animated: runtimeSignal(to) === "progressing" || (to.identity.kind === "Pod" && podState(to) === "starting") })
    for (const edge of graph.edges) if (edge.relation === "routes to" && row.traffic.some((node) => node.id === edge.from) && row.services.some((node) => node.id === edge.to)) add(edge.from, nodeIndex.get(edge.to)!)
    for (const service of row.services) add(service.id, workload)
    for (const pod of pods) add(workload.id, pod)
    return { items: [...acc.items, { placed, edges }], top: bottom + ROW_GAP }
  }, { items: [], top: MARGIN })
  const otherCols = 4
  const otherPlaced: Placed[] = other.map((node, i) => ({ node, x: MARGIN + (i % otherCols) * (NODE_W + GAP_X), y: layout.top + Math.floor(i / otherCols) * (NODE_H + GAP_Y) }))
  const placedAll = [...layout.items.flatMap((item) => item.placed), ...otherPlaced]

  if (!rows.length && !other.length) return <div className="min-w-0 p-6"><EmptyState title={graph.nodes.length ? "No matching resources" : "No resources to show yet"} description={graph.nodes.length ? "Try another filter or search term." : "Build a plan to discover resources, or refresh the cluster once the application is deployed."} /></div>

  const workloadsNeedingAttention = rows.filter((row) => [row.workload, ...row.pods].some((node) => runtimeSignal(node) === "degraded" || isConflict(node))).length
  const podsReady = rows.flatMap((row) => row.pods).filter((pod) => podState(pod) === "ready" || podState(pod) === "completed").length
  const podTotal = rows.reduce((total, row) => total + row.pods.length, 0)
  const toChange = graph.nodes.filter(isChanged).length
  const totalWidth = Math.max(...placedAll.map((item) => item.x + NODE_W)) + MARGIN
  const totalHeight = Math.max(...placedAll.map((item) => item.y + NODE_H)) + MARGIN
  const zoom = box ? Math.min(1, (box.width - 48) / totalWidth, (box.height - 48) / totalHeight) : 1
  const viewport = box ? { zoom, x: Math.max(24, (box.width - totalWidth * zoom) / 2), y: 24 } : undefined
  const layoutKey = [...rows.map((row) => row.id), other.length].join("|")
  return <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3">
    {rows.length > 1 && <div role="status" className="flex flex-wrap items-center gap-2">
      <Badge variant="secondary">{rows.length} {rows.length === 1 ? "workload" : "workloads"}</Badge>
      <Badge variant="outline" className="bg-background">{podsReady}/{podTotal} pods ready</Badge>
      {workloadsNeedingAttention > 0 ? <Badge variant="destructive" dot>{workloadsNeedingAttention} need attention</Badge> : <Badge variant="success" dot>None need attention</Badge>}
      {toChange > 0 && <Badge variant="warning" dot>{toChange} to change</Badge>}
    </div>}
    <div ref={stage} className="min-h-80 flex-1">{viewport && <Flow key={layoutKey} label="Resource topology" defaultViewport={viewport} minZoom={0.2} className="h-full rounded-none border-0 bg-transparent">
      <Flow.Background variant="dots" />
      {placedAll.map(card)}
      {layout.items.flatMap((item) => item.edges).map((edge) => <Flow.Edge key={`${edge.from}>${edge.to}`} from={edge.from} to={edge.to} variant="smoothstep" arrow animated={edge.animated} color={edge.color} width={1.5} />)}
      <Flow.Controls position="bottom-left" />
    </Flow>}</div>
  </div>
}
