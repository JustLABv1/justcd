"use client"

import { useEffect, useMemo, useRef, useState, type MouseEvent } from "react"
import { ReactFlow, ReactFlowProvider, Handle, Position, BaseEdge, EdgeText, getBezierPath, getViewportForBounds, useReactFlow, useNodesInitialized, useViewport, type Node, type NodeProps, type Edge, type EdgeProps, type NodeChange, type Viewport, type XYPosition } from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowLeft01Icon, ArrowRight01Icon, CenterFocusIcon, Maximize01Icon, MinusSignIcon, PlusSignIcon, CubeIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { Switch } from "@/components/ui/switch"
import { EmptyState } from "@/components/ui-kit"
import { aggregateRuntime, connectionSignal, mapNodeHeightFor, mapNodeWidth, resourceURLs, supportingTopologyResource, podSummary, topologyUpstream, topologyVisibleEdges, topologyHierarchy, topologyScope, type ConnectionSignal, type RuntimeSignal } from "@/lib/topology-map"
import { packedTopologyScene, type TopologyGroup } from "@/lib/topology-map"
import { routeTopologyDependencies, type RoutedConnection } from "@/lib/topology-routing"
import type { SyncState } from "@/lib/topology-view"
import type { ResourceTopology, TopologyNode } from "@/lib/types"
import styles from "./topology-canvas.module.css"

type MapNodeData = {
  resource: TopologyNode; icon: typeof CubeIcon; runtime: RuntimeSignal; runtimeLabel: string
  compact: boolean; podStatus: string | null; context: boolean; backLabel: string | null; sync: SyncState; syncLabel: string; childCount: number; childLabel: string; dimmed: boolean; canEnter: boolean
  onEnter: (id: string, animate: boolean) => void; onInspect: (id: string) => void
}
type ResourceMapNode = Node<MapNodeData, "resource">
type MapNode = ResourceMapNode | Node<{ group: TopologyGroup }, "cluster">
type MapEdge = Edge<{ signal: ConnectionSignal; relation: string; active: boolean; dependency: boolean; route?: RoutedConnection | null }, "relationship">
const signalLabels: Record<RuntimeSignal, string> = { healthy: "Healthy", progressing: "Not ready / progressing", degraded: "Needs attention", unknown: "Health unknown or not applicable" }

/** Interpolate model positions, so nodes and obstacle-routed edges move together.
 * The scene settles after a layout change; health refreshes do not restart it. */
function useFloatingLayout(target: ReadonlyMap<string, XYPosition>, paused: boolean) {
  const [displayed, setDisplayed] = useState(target)
  const current = useRef(target)
  useEffect(() => {
    const from = current.current
    if (from.size === target.size && [...target].every(([id, p]) => from.get(id)?.x === p.x && from.get(id)?.y === p.y)) return
    const duration = paused || window.matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 280
    const start = performance.now()
    let frame: number
    const tick = (now: number) => {
      const progress = duration ? Math.min(1, (now - start) / duration) : 1
      const t = 1 - Math.pow(1 - progress, 3)
      const next = new Map([...target].map(([id, to]) => {
        const previous = from.get(id) ?? to
        return [id, { x: previous.x + (to.x - previous.x) * t, y: previous.y + (to.y - previous.y) * t }] as const
      }))
      current.current = next
      setDisplayed(next)
      if (progress < 1) frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [target, paused])
  return displayed
}

function ResourceNode({ data, selected }: NodeProps<Node<MapNodeData, "resource">>) {
  const { resource, runtime, childCount } = data
  const urls = resourceURLs(resource)
  return <div className={`${styles.node} ${selected ? styles.selected : ""} ${data.dimmed ? styles.dimmed : ""}`} data-runtime={runtime} data-sync={data.sync} data-network={Boolean(resource.routingHealth)} data-context={data.context} data-compact={data.compact}>
    <Handle id="source-top" type="source" position={Position.Top} isConnectable={false} className={styles.topHandle} />
    <Handle id="target-top" type="target" position={Position.Top} isConnectable={false} className={styles.topHandle} />
    <Handle id="source-bottom" type="source" position={Position.Bottom} isConnectable={false} className={styles.bottomHandle} />
    <Handle id="target-bottom" type="target" position={Position.Bottom} isConnectable={false} className={styles.bottomHandle} />
    <Handle id="target-left" type="target" position={Position.Left} isConnectable={false} className={styles.targetHandle} />
    <Handle id="source-right" type="source" position={Position.Right} isConnectable={false} className={styles.sourceHandle} />
    <Handle id="target-right" type="target" position={Position.Right} isConnectable={false} className={styles.sourceHandle} />
    <Handle id="source-left" type="source" position={Position.Left} isConnectable={false} className={styles.targetHandle} />
    <Tooltip>
      <TooltipTrigger render={<Button variant="ghost" className={`${styles.nodeButton} nopan`} aria-label={`${data.backLabel ? "Back via" : data.canEnter ? "Explore" : "Inspect"} ${resource.identity.kind} ${resource.identity.name}: ${signalLabels[runtime]}${childCount ? `, ${childCount} contained resources` : ""}`} onClick={(event) => data.canEnter ? data.onEnter(resource.id, event.detail !== 0) : data.onInspect(resource.id)} />}>
        <span className={styles.orb}>
          <HugeiconsIcon icon={data.icon} strokeWidth={1.5} className="size-7" aria-hidden="true" />
          <span className={styles.healthDot} aria-hidden="true" />
          {childCount > 0 && <span className={styles.childCount} aria-hidden="true">{childCount}</span>}
        </span>
        <span className={styles.nodeName} title={resource.identity.name}>{resource.identity.name}</span>
        <span className={styles.nodeKind}>{resource.identity.kind}</span>
        {data.backLabel && <span className={styles.contextLabel}>← {data.backLabel}</span>}
        {data.context && <span className={styles.contextLabel}>Upstream</span>}
        {resource.identity.kind === "Ingress" && childCount > 0 && !data.backLabel && <span className={styles.contextLabel}>{data.childLabel} · Explore</span>}
        {data.podStatus && <span className={styles.podStatus}>{data.podStatus}</span>}
        {resource.routingHealth && <span className={styles.routingStatus}>{resource.routingHealth.label}</span>}
        <span className={styles.nodeStatus} data-sync={data.sync}><span aria-hidden="true" />{data.syncLabel}</span>
      </TooltipTrigger>
      <TooltipContent className="max-w-80"><p className="break-all font-medium">{resource.identity.name}</p><p>{data.runtimeLabel}</p><p>{signalLabels[runtime]}{childCount ? ` · ${data.childLabel}` : ""}</p><p>{resource.identity.namespace || "Cluster scope"}</p></TooltipContent>
    </Tooltip>
    <div className={`${styles.actions} nodrag nopan`}>
      {childCount > 0 && <Button variant="ghost" size="xs" aria-label={`Inspect ${resource.identity.kind} ${resource.identity.name}`} onClick={() => data.onInspect(resource.id)}>Details<HugeiconsIcon icon={ArrowRight01Icon} aria-hidden="true" /></Button>}
      {urls.length === 1 && <Button variant="ghost" size="xs" nativeButton={false} render={<a href={urls[0]} target="_blank" rel="noopener noreferrer" />} aria-label={`Open ${urls[0]} (opens in a new tab)`}>Open page ↗</Button>}
      {urls.length > 1 && <Button variant="ghost" size="xs" aria-label={`View ${urls.length} URLs for ${resource.identity.name}`} onClick={() => data.onInspect(resource.id)}>{urls.length} URLs ↗</Button>}
    </div>
  </div>
}

function RelationshipEdge(props: EdgeProps<MapEdge>) {
  const fallback = getBezierPath(props)
  const route = props.data?.route
  const [path, labelX, labelY] = route ? [route.path, route.labelX, route.labelY] : fallback
  if (route === null) return null
  const { signal = "unknown", relation = "", active = false, dependency = false } = props.data ?? {}
  return <g className={`${styles.relationship} ${active ? styles.activeEdge : ""}`} data-signal={signal} data-dependency={dependency}>
    <title>{relation} · {signal === "syncing" ? "Applying deployment" : signalLabels[signal]}</title>
    <BaseEdge id={props.id} path={path} className={styles.edgePath} interactionWidth={20} />
    {(signal === "syncing" || signal === "progressing") && <path d={path} className={styles.flow} fill="none" aria-hidden="true" />}
    <g className={styles.edgeLabel}><EdgeText x={labelX} y={labelY} label={relation} labelStyle={{ fill: "var(--foreground)", fontSize: 11 }} labelBgStyle={{ fill: "var(--card)" }} labelBgPadding={[7, 4]} /></g>
  </g>
}

function ResourceGroup({ data }: NodeProps<Node<{ group: TopologyGroup }, "cluster">>) {
  return <div className={styles.resourceGroup}><div className={styles.groupHeading}><span title={data.group.title}>{data.group.title}</span><span>{data.group.resourceIds.length}</span></div></div>
}

const nodeTypes = { resource: ResourceNode, cluster: ResourceGroup }
const edgeTypes = { relationship: RelationshipEdge }
const ease = (t: number) => 1 - Math.pow(1 - t, 3)
const scopeKey = (parentId: string | null) => parentId ?? "__application__"

type Props = {
  graph: ResourceTopology; applicationName: string; selectedId: string | null; onSelect: (id: string | null) => void
  query: string; mode: string; focus: boolean; related: Set<string>
  stateFor: (node: TopologyNode) => SyncState; syncLabels: Record<SyncState, string>
  describeNode: (node: TopologyNode) => string; iconFor: (kind: string) => typeof CubeIcon
}

function Canvas({ graph, applicationName, selectedId, onSelect, query, mode, focus, related, stateFor, syncLabels, describeNode, iconFor }: Props) {
  const [path, setPath] = useState<string[]>([])
  const [positions, setPositions] = useState<Record<string, XYPosition>>({})
  const [moving, setMoving] = useState(false)
  const [paused, setPaused] = useState(false)
  const [dependencies, setDependencies] = useState(true)
  const [replicaSets, setReplicaSets] = useState(false)
  const [frame, setFrame] = useState({ width: 0, height: 0 })
  const savedViews = useRef(new Map<string, Viewport>())
  const navigation = useRef(0)
  const pendingView = useRef<{ viewport?: Viewport; duration: number } | null>(null)
  const appliedView = useRef<string | null>(null)
  const root = useRef<HTMLDivElement>(null)
  const flow = useReactFlow<MapNode, MapEdge>()
  const initialized = useNodesInitialized()
  const viewport = useViewport()
  const hierarchy = useMemo(() => topologyHierarchy(graph), [graph])
  // A deleted parent returns us to the deepest surviving ancestor.
  const validPath = path.filter((id) => graph.nodes.some((node) => node.id === id))
  const parentId = validPath.at(-1) ?? null
  const globalView = Boolean(query.trim()) || mode !== "all"
  const context = !globalView && parentId ? topologyUpstream(graph, parentId) : []
  const contextIds = new Set(context.map((node) => node.id))
  const scoped = globalView ? [...graph.nodes] : [...topologyScope(graph, parentId, replicaSets), ...context]
  const matches = (node: TopologyNode) => `${node.identity.kind} ${node.identity.namespace} ${node.identity.name}`.toLowerCase().includes(query.trim().toLowerCase())
  const inFilter = (node: TopologyNode) => (!focus || !selectedId || related.has(node.id)) && (mode === "all" || (mode === "observed" ? ["kubernetes", "sample"].includes(node.source) : ["create", "update", "delete", "failed", "syncing"].includes(stateFor(node))))
  // Inventory is always visible in the overview. Drill-down adds only actual
  // dependencies of the current scope, without pulling in unrelated workloads.
  const scopedIds = new Set(scoped.map(node => node.id))
  const supportCandidates = graph.nodes.filter(supportingTopologyResource)
  const linkedIds = new Set(scopedIds)
  if (parentId && !globalView) {
    let changed = true
    while (changed) {
      changed = false
      for (const edge of graph.edges) for (const [from, to] of [[edge.from, edge.to], [edge.to, edge.from]]) {
        if (linkedIds.has(from) && !linkedIds.has(to) && supportCandidates.some(node => node.id === to)) { linkedIds.add(to); changed = true }
      }
    }
  }
  const available = [...new Map([...scoped, ...supportCandidates.filter(node => !parentId || globalView || linkedIds.has(node.id))].map(node => [node.id, node])).values()]
  const shown = available.filter(inFilter)
  const selectedResource = graph.nodes.find(node => node.id === selectedId)
  if (selectedResource && !shown.some(node => node.id === selectedId)) shown.push(selectedResource)
  const visible = new Set(shown.map(node => node.id))
  const sceneEdges = topologyVisibleEdges(graph, visible).filter(edge => dependencies || ["owns", "selects", "routes to"].includes(edge.relation) || edge.from === selectedId || edge.to === selectedId)
  const overview = packedTopologyScene(graph, shown.map(node => node.id), { width: frame.width || 1200, height: frame.height || 600 })
  const layout = overview.positions
  const floatingLayout = useFloatingLayout(layout, paused)
  const runtime = useMemo(() => new Map(graph.nodes.map((node) => [node.id, aggregateRuntime(node, hierarchy.descendants(node.id))])), [graph, hierarchy])
  const reducedMotion = () => window.matchMedia("(prefers-reduced-motion: reduce)").matches

  const enter = async (id: string, animate: boolean) => {
    if (!(hierarchy.children.get(id)?.length)) return
    const ticket = ++navigation.current
    const duration = animate && !reducedMotion() ? 240 : 0
    savedViews.current.set(scopeKey(parentId), flow.getViewport())
    const node = flow.getNode(id)
    if (node && duration) {
      setMoving(true)
      await flow.setCenter(node.position.x + mapNodeWidth / 2, node.position.y + 44, { zoom: Math.min(1.8, flow.getZoom() * 1.35), duration, ease })
    }
    if (ticket !== navigation.current) return
    pendingView.current = { viewport: savedViews.current.get(id), duration }
    onSelect(null)
    setPath([...validPath, id])
    setMoving(false)
  }
  const back = (index: number, animate: boolean) => {
    ++navigation.current
    savedViews.current.set(scopeKey(parentId), flow.getViewport())
    const next = validPath.slice(0, index)
    pendingView.current = { viewport: savedViews.current.get(scopeKey(next.at(-1) ?? null)), duration: animate && !reducedMotion() ? 240 : 0 }
    onSelect(null)
    setPath(next); setMoving(false)
  }

  const handleEnter = (id: string, animate: boolean) => {
    if (id === parentId) back(validPath.length - 1, animate)
    else if (contextIds.has(id)) onSelect(id)
    else void enter(id, animate)
  }

  const nodes: MapNode[] = shown.map((resource) => {
    const children = hierarchy.children.get(resource.id) ?? []
    const pods = hierarchy.descendants(resource.id).filter((node) => node.identity.kind === "Pod")
    const isContext = contextIds.has(resource.id)
    return {
      id: resource.id, type: "resource", position: positions[`${scopeKey(parentId)}:${resource.id}`] ?? { ...(floatingLayout.get(resource.id) ?? layout.get(resource.id))! },
      selected: selectedId === resource.id, width: mapNodeWidth, height: mapNodeHeightFor(resource),
      measured: { width: mapNodeWidth, height: mapNodeHeightFor(resource) },
      // Shared Button owns keyboard activation; the wrapper is only the draggable canvas object.
      focusable: false, draggable: true,
      data: { resource, compact: supportingTopologyResource(resource), podStatus: resource.identity.kind === "Pod" ? resource.healthSummary?.failureReason ?? resource.readiness ?? resource.phase ?? null : podSummary(pods), context: isContext, backLabel: resource.id === parentId && !globalView ? "Back to previous view" : null, icon: iconFor(resource.identity.kind), runtime: runtime.get(resource.id)!, runtimeLabel: describeNode(resource), sync: stateFor(resource), syncLabel: syncLabels[stateFor(resource)], childCount: pods.length || children.length,
        childLabel: pods.length ? `${pods.length} pods · ${children.length} direct controllers or children` : [...new Set(children.map((node) => node.identity.kind))].map((kind) => `${children.filter((node) => node.identity.kind === kind).length} ${kind}`).join(" · "), canEnter: resource.id === parentId && !globalView || !isContext && children.length > 0 && resource.id !== parentId,
        // React Flow stores the handler; it is invoked only on user activation.
        // eslint-disable-next-line react-hooks/refs
        dimmed: Boolean(query.trim() && !matches(resource)) || Boolean(selectedId && !related.has(resource.id)), onEnter: handleEnter, onInspect: onSelect },
    }
  })
  for (const group of overview?.groups ?? []) nodes.unshift({ id: `__${group.id}`, type: "cluster", position: { x: group.x, y: group.y }, width: group.width, height: group.height, measured: { width: group.width, height: group.height }, style: { width: group.width, height: group.height }, draggable: false, selectable: false, focusable: false, zIndex: -1, data: { group } })
  const dependencyRoutes = routeTopologyDependencies(nodes.filter((node): node is ResourceMapNode => node.type === "resource").map(node => ({ id: node.id, x: node.position.x, y: node.position.y, width: node.width!, height: node.height! + (node.data.backLabel || node.data.context ? 20 : 0), compact: node.data.compact })), sceneEdges.filter(edge => !["owns", "selects", "routes to"].includes(edge.relation)).map(edge => ({ id: JSON.stringify([edge.from, edge.to, edge.relation]), from: edge.from, to: edge.to })))
  const edges: MapEdge[] = sceneEdges.map((edge) => {
    const from = graph.nodes.find((node) => node.id === edge.from)!, to = graph.nodes.find((node) => node.id === edge.to)!
    const dependency = !["owns", "selects", "routes to"].includes(edge.relation)
    const sync = [from, to].some((node) => stateFor(node) === "syncing")
    const failed = [from, to].some((node) => stateFor(node) === "failed")
    const traffic = edge.relation === "routes to" ? to.routingHealth : edge.relation === "selects" ? from.routingHealth : undefined
    const backends = from.routingHealth?.backends?.filter((backend) => backend.serviceName === to.identity.name && backend.namespace === to.identity.namespace)
    const backendStatus = backends?.find((backend) => backend.status === "degraded")?.status ?? backends?.find((backend) => backend.status === "unknown")?.status ?? backends?.find((backend) => backend.status === "progressing")?.status ?? backends?.[0]?.status
    const collapsedRoutes = (hierarchy.children.get(from.id) ?? []).filter((child) => child.identity.kind === "Route" && !visible.has(child.id) && graph.edges.some((link) => link.from === child.id && link.to === to.id && link.relation === "routes to"))
    const collapsedStatus = collapsedRoutes.some((child) => runtime.get(child.id) === "degraded") ? "degraded" : collapsedRoutes.some((child) => runtime.get(child.id) === "progressing") ? "progressing" : undefined
    const trafficStatus = collapsedStatus ?? (edge.relation === "routes to" && from.routingHealth?.reason === "RouteNotAdmitted" ? "degraded" : backendStatus ?? traffic?.status)
    const signal = dependency ? "unknown" : trafficStatus ? connectionSignal(trafficStatus, trafficStatus, sync, failed) : connectionSignal(edge.relation === "owns" ? runtime.get(to.id)! : runtime.get(from.id)!, runtime.get(to.id)!, sync, failed)
    const active = edge.from === selectedId || edge.to === selectedId
    const forward = (nodes.find((node) => node.id === edge.from)?.position.x ?? 0) <= (nodes.find((node) => node.id === edge.to)?.position.x ?? 0)
    const sourcePosition = nodes.find(node => node.id === edge.from)!.position, targetPosition = nodes.find(node => node.id === edge.to)!.position
    const vertical = dependency && Math.abs(sourcePosition.y - targetPosition.y) > Math.abs(sourcePosition.x - targetPosition.x) * 0.7
    const downward = sourcePosition.y < targetPosition.y
    const route = dependency ? dependencyRoutes.get(JSON.stringify([edge.from, edge.to, edge.relation])) : undefined
    const relation = edge.viaReplicaSet ? "owns via ReplicaSet" : edge.relation
    return { id: JSON.stringify([edge.from, edge.to, edge.relation]), source: edge.from, target: edge.to, sourceHandle: route?.sourceHandle ?? (vertical ? downward ? "source-bottom" : "source-top" : forward ? "source-right" : "source-left"), targetHandle: route?.targetHandle ?? (vertical ? downward ? "target-top" : "target-bottom" : forward ? "target-left" : "target-right"), type: "relationship", focusable: true, selectable: false,
      ariaLabel: `${from.identity.name} ${relation} ${to.identity.name}: ${signal === "syncing" ? "Applying deployment" : signalLabels[signal]}`,
      data: { signal, relation, active, dependency, route }, style: { opacity: selectedId && !active ? 0.35 : 1 } }
  })

  // Status-only observations retain the camera. Structural layout changes,
  // navigation, and resizing fit the new geometry.
  const viewKey = JSON.stringify([[...layout].map(([id, position]) => [id, position.x, position.y]), frame.width, frame.height, parentId, query.trim(), mode, focus, replicaSets, dependencies, focus ? selectedId : null])
  useEffect(() => {
    if (!frame.width || !initialized || appliedView.current === viewKey && !pendingView.current) return
    const animationFrame = requestAnimationFrame(() => {
      const pending = pendingView.current
      pendingView.current = null
      appliedView.current = viewKey
      if (pending?.viewport) void flow.setViewport(pending.viewport, { duration: pending.duration, ease })
      else {
        const targets = query.trim() ? nodes.filter((node) => node.type === "resource" && matches(node.data.resource)) : nodes
        const canvas = root.current?.querySelector(".react-flow")
        if (targets.length && canvas) {
          const x = Math.min(...targets.map((node) => node.position.x)), y = Math.min(...targets.map((node) => node.position.y))
          const width = Math.max(...targets.map((node) => node.position.x + (node.width ?? mapNodeWidth))) - x
          const height = Math.max(...targets.map((node) => node.position.y + (node.height ?? 192))) - y
          // Use the new layout directly; React Flow's measured nodes can still belong
          // to the outgoing scene during this commit.
          const camera = getViewportForBounds({ x, y, width, height }, canvas.clientWidth, canvas.clientHeight, 0.7, 1.1, 0.06)
          void flow.setViewport(camera, { duration: pending?.duration ?? 0, ease })
        }
      }
    })
    return () => cancelAnimationFrame(animationFrame)
    // Data refreshes intentionally retain the user's camera and dragged positions.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [viewKey, initialized])

  useEffect(() => {
    const canvas = root.current?.querySelector(".react-flow")
    if (!canvas) return
    const observer = new ResizeObserver(([entry]) => {
      const width = Math.round(entry.contentRect.width), height = Math.round(entry.contentRect.height)
      setFrame(previous => previous.width === width && previous.height === height ? previous : { width, height })
    })
    observer.observe(canvas)
    return () => observer.disconnect()
  }, [])

  useEffect(() => () => { ++navigation.current }, [])

  const onNodesChange = (changes: NodeChange<MapNode>[]) => {
    const moved = changes.filter((change) => change.type === "position" && change.position)
    if (!moved.length) return
    setPositions((previous) => {
      const next = { ...previous }
      for (const change of moved) if (change.type === "position" && change.position) next[`${scopeKey(parentId)}:${change.id}`] = change.position
      return next
    })
  }

  function centerSelection(event: MouseEvent) {
    const node = selectedId ? flow.getNode(selectedId) : undefined
    if (node?.type === "resource") void flow.setCenter(node.position.x + mapNodeWidth / 2, node.position.y + mapNodeHeightFor(node.data.resource) / 2, { zoom: Math.max(0.8, flow.getZoom()), duration: event.detail && !reducedMotion() ? 240 : 0, ease })
  }

  return <div ref={root} className={`${styles.canvas} ${paused ? styles.paused : ""} relative min-w-0`}>
    <div className="flex min-h-12 flex-wrap items-center justify-between gap-2 border-b bg-card/80 px-4 py-2">
      <nav aria-label="Topology location" className="flex min-w-0 flex-wrap items-center gap-1">
        <Button variant="ghost" size="sm" disabled={!parentId} onClick={(event) => back(0, event.detail !== 0)}>{applicationName}</Button>
        {validPath.map((id, index) => <span key={id} className="inline-flex min-w-0 items-center gap-1"><HugeiconsIcon icon={ArrowRight01Icon} className="size-3 text-muted-foreground" aria-hidden="true" /><Button variant="ghost" size="sm" className="max-w-40 truncate" aria-current={id === parentId ? "location" : undefined} disabled={id === parentId} onClick={(event) => back(index + 1, event.detail !== 0)}>{graph.nodes.find((node) => node.id === id)?.identity.name}</Button></span>)}
      </nav>
      <div className="flex flex-wrap items-center gap-4">
        <label className="inline-flex items-center gap-2 text-xs text-muted-foreground"><Switch checked={dependencies} onCheckedChange={setDependencies} aria-label="Show dependency connections" />Dependencies</label>
        {parentId && !globalView && (hierarchy.children.get(parentId) ?? []).some((node) => node.identity.kind === "ReplicaSet") && <label className="inline-flex items-center gap-2 text-xs text-muted-foreground"><Switch checked={replicaSets} onCheckedChange={setReplicaSets} aria-label="Show ReplicaSets" />ReplicaSets</label>}
      <Button variant="ghost" size="xs" aria-pressed={paused} onClick={() => setPaused(!paused)}>{paused ? "Resume animation" : "Pause animation"}</Button></div>
    </div>
    {globalView && parentId && <p className="border-b px-4 py-2 text-xs text-muted-foreground">Search and filters show resources across the application. Clear them to return to this group.</p>}
    <div className="relative h-[min(68svh,760px)] min-h-[420px]">
      <ReactFlow<MapNode, MapEdge> nodes={nodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} onNodesChange={onNodesChange}
        nodesConnectable={false} edgesReconnectable={false} nodesFocusable={false} deleteKeyCode={null} selectionKeyCode={null}
        minZoom={0.15} maxZoom={2} zoomOnDoubleClick={false} panOnDrag panOnScroll zoomOnScroll={false} zoomOnPinch
        tabIndex={0} onKeyDown={(event) => {
          if (event.target !== event.currentTarget) return
          const offset: Record<string, XYPosition> = { ArrowLeft: { x: 64, y: 0 }, ArrowRight: { x: -64, y: 0 }, ArrowUp: { x: 0, y: 64 }, ArrowDown: { x: 0, y: -64 } }
          const delta = offset[event.key]
          if (delta) { event.preventDefault(); const camera = flow.getViewport(); void flow.setViewport({ ...camera, x: camera.x + delta.x, y: camera.y + delta.y }) }
          else if (event.key === "Home") { event.preventDefault(); void flow.fitView({ padding: 0.15, maxZoom: 1.1 }) }
          else if (event.key === "Escape" && parentId) { event.preventDefault(); back(validPath.length - 1, false) }
        }}
        aria-label="Interactive application topology. Drag resources to arrange them. Drag the background or use arrow keys to pan. Pinch or use the controls to zoom. Home fits the view; Escape returns to the parent group."
        className={moving ? styles.entering : undefined}>
        {!shown.length && <div className="pointer-events-none absolute inset-0 z-10 grid place-items-center p-6"><EmptyState title={graph.nodes.length ? "No matching resources" : "No resources to show yet"} description={graph.nodes.length ? "Try another filter or return to the application." : "Build a plan to discover resources and their relationships."} /></div>}
      </ReactFlow>
      <div className="absolute right-3 bottom-3 z-10 flex max-w-[calc(100%-24px)] flex-wrap items-center gap-0.5 rounded-lg border bg-card/95 p-1 shadow-sm" role="group" aria-label="Canvas view controls">
        {parentId && <Tooltip><TooltipTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Back to parent group" onClick={(event) => back(validPath.length - 1, event.detail !== 0)} />}><HugeiconsIcon icon={ArrowLeft01Icon} aria-hidden="true" /></TooltipTrigger><TooltipContent>Back to parent</TooltipContent></Tooltip>}
        <Tooltip><TooltipTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Fit all resources" onClick={(event) => { void flow.fitView({ padding: 0.15, maxZoom: 1.1, duration: event.detail && !reducedMotion() ? 240 : 0, ease }) }} />}><HugeiconsIcon icon={Maximize01Icon} aria-hidden="true" /></TooltipTrigger><TooltipContent>Fit view</TooltipContent></Tooltip>
        <Tooltip><TooltipTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Center selection" disabled={!selectedId || !visible.has(selectedId)} onClick={centerSelection} />}><HugeiconsIcon icon={CenterFocusIcon} aria-hidden="true" /></TooltipTrigger><TooltipContent>Center selection</TooltipContent></Tooltip>
        <Button variant="ghost" size="icon-sm" aria-label="Zoom out" disabled={viewport.zoom <= 0.15} onClick={() => { void flow.zoomOut() }}><HugeiconsIcon icon={MinusSignIcon} aria-hidden="true" /></Button>
        <Button variant="ghost" size="sm" className="tabular-nums" aria-label="Reset zoom to 100 percent" onClick={() => { void flow.zoomTo(1) }}>{Math.round(viewport.zoom * 100)}%</Button>
        <Button variant="ghost" size="icon-sm" aria-label="Zoom in" disabled={viewport.zoom >= 2} onClick={() => { void flow.zoomIn() }}><HugeiconsIcon icon={PlusSignIcon} aria-hidden="true" /></Button>
      </div>
    </div>
    <div className="flex flex-wrap justify-between gap-2 border-t px-4 py-2 text-xs text-muted-foreground"><p>Drag to pan or arrange · Pinch to zoom · Click a parent to explore</p><p role="status" aria-live="polite">{shown.length} resources · {parentId ? graph.nodes.find((node) => node.id === parentId)?.identity.name : "Application overview"}</p></div>
    <div aria-label="Connection status legend" className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t px-4 py-3 text-xs text-muted-foreground">
      <span className="font-medium text-foreground">Connections</span>
      {([["healthy", "Backend ready"], ["syncing", "Applying"], ["progressing", "Not ready / progressing"], ["degraded", "Needs attention"], ["unknown", "Idle / unknown"]] as const).map(([signal, label]) => <span key={signal} className="inline-flex items-center gap-1.5"><i className={styles.legendLine} data-signal={signal} aria-hidden="true" />{label}</span>)}
      <span>{dependencies ? "Dashed = configuration dependency" : "Select a resource to trace its dependencies · All resources stay visible"}</span>
    </div>
  </div>
}

export function TopologyCanvas(props: Props) {
  return <ReactFlowProvider><Canvas {...props} /></ReactFlowProvider>
}
