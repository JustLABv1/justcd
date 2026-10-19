"use client"

import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent } from "react"
import { ReactFlow, ReactFlowProvider, Handle, Position, BaseEdge, EdgeText, getBezierPath, getViewportForBounds, useReactFlow, useNodesInitialized, useViewport, type Node, type NodeProps, type Edge, type EdgeProps, type NodeChange, type Viewport, type XYPosition } from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowLeft01Icon, ArrowRight01Icon, CenterFocusIcon, Maximize01Icon, MinusSignIcon, PlusSignIcon, CubeIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { Switch } from "@/components/ui/switch"
import { EmptyState } from "@/components/ui-kit"
import { aggregateRuntime, connectionSignal, mapNodeHeightFor, mapNodeWidth, resourceURLs, podSummary, topologyUpstream, topologyContextLayout, topologyVisibleEdges, topologyHierarchy, topologyMapLayout, topologyScope, type ConnectionSignal, type RuntimeSignal } from "@/lib/topology-map"
import type { SyncState } from "@/lib/topology-view"
import type { ResourceTopology, TopologyNode } from "@/lib/types"
import styles from "./topology-canvas.module.css"

type MapNodeData = {
  resource: TopologyNode; icon: typeof CubeIcon; runtime: RuntimeSignal; runtimeLabel: string
  podStatus: string | null; context: boolean; backLabel: string | null; sync: SyncState; syncLabel: string; childCount: number; childLabel: string; dimmed: boolean; canEnter: boolean
  onEnter: (id: string, animate: boolean) => void; onInspect: (id: string) => void
}
type MapNode = Node<MapNodeData, "resource">
type MapEdge = Edge<{ signal: ConnectionSignal; relation: string; active: boolean; dependency: boolean }, "relationship">
const signalLabels: Record<RuntimeSignal, string> = { healthy: "Healthy", progressing: "Not ready / progressing", degraded: "Needs attention", unknown: "Health unknown or not applicable" }

function ResourceNode({ data, selected }: NodeProps<Node<MapNodeData, "resource">>) {
  const { resource, runtime, childCount } = data
  const urls = resourceURLs(resource)
  return <div className={`${styles.node} ${selected ? styles.selected : ""} ${data.dimmed ? styles.dimmed : ""}`} data-runtime={runtime} data-sync={data.sync} data-network={Boolean(resource.routingHealth)} data-context={data.context}>
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
  const [path, labelX, labelY] = getBezierPath(props)
  const { signal = "unknown", relation = "", active = false, dependency = false } = props.data ?? {}
  return <g className={`${styles.relationship} ${active ? styles.activeEdge : ""}`} data-signal={signal} data-dependency={dependency}>
    <title>{relation} · {signal === "syncing" ? "Applying deployment" : signalLabels[signal]}</title>
    <BaseEdge id={props.id} path={path} className={styles.edgePath} interactionWidth={20} />
    {(signal === "syncing" || signal === "progressing") && <path d={path} className={styles.flow} fill="none" aria-hidden="true" />}
    <g className={styles.edgeLabel}><EdgeText x={labelX} y={labelY} label={relation} labelStyle={{ fill: "var(--foreground)", fontSize: 11 }} labelBgStyle={{ fill: "var(--card)" }} labelBgPadding={[7, 4]} /></g>
  </g>
}

const nodeTypes = { resource: ResourceNode }
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
  const [dependencies, setDependencies] = useState(false)
  const [replicaSets, setReplicaSets] = useState(false)
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
  // Inspector relationships and pod search can explicitly select a collapsed child.
  const selectedResource = graph.nodes.find((node) => node.id === selectedId)
  if (selectedResource && !scoped.some((node) => node.id === selectedId)) scoped.push(selectedResource)
  const shown = scoped.filter((node) => (!focus || !selectedId || related.has(node.id)) && (mode === "all" || (mode === "observed" ? ["kubernetes", "sample"].includes(node.source) : ["create", "update", "delete", "failed", "syncing"].includes(stateFor(node)))))
  const layout = topologyMapLayout(shown.filter((node) => !contextIds.has(node.id)), globalView ? null : parentId, graph.edges)
  if (parentId) for (const [id, position] of topologyContextLayout(graph, parentId, context, layout.get(parentId)?.y ?? 40)) layout.set(id, position)
  const runtime = useMemo(() => new Map(graph.nodes.map((node) => [node.id, aggregateRuntime(node, hierarchy.descendants(node.id))])), [graph, hierarchy])
  const matches = (node: TopologyNode) => `${node.identity.kind} ${node.identity.namespace} ${node.identity.name}`.toLowerCase().includes(query.trim().toLowerCase())
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
      id: resource.id, type: "resource", position: positions[`${scopeKey(parentId)}:${resource.id}`] ?? layout.get(resource.id)!,
      selected: selectedId === resource.id, width: mapNodeWidth, height: mapNodeHeightFor(resource),
      measured: { width: mapNodeWidth, height: mapNodeHeightFor(resource) },
      // Shared Button owns keyboard activation; the wrapper is only the draggable canvas object.
      focusable: false, draggable: true,
      data: { resource, podStatus: resource.identity.kind === "Pod" ? resource.healthSummary?.failureReason ?? resource.readiness ?? resource.phase ?? null : podSummary(pods), context: isContext, backLabel: resource.id === parentId && !globalView ? "Back to previous view" : null, icon: iconFor(resource.identity.kind), runtime: runtime.get(resource.id)!, runtimeLabel: describeNode(resource), sync: stateFor(resource), syncLabel: syncLabels[stateFor(resource)], childCount: pods.length || children.length,
        childLabel: pods.length ? `${pods.length} pods · ${children.length} direct controllers or children` : [...new Set(children.map((node) => node.identity.kind))].map((kind) => `${children.filter((node) => node.identity.kind === kind).length} ${kind}`).join(" · "), canEnter: resource.id === parentId && !globalView || !isContext && children.length > 0 && resource.id !== parentId,
        // React Flow stores this callback in node data; it only runs on user activation.
        // eslint-disable-next-line react-hooks/refs
        dimmed: Boolean(query.trim() && !matches(resource)) || Boolean(selectedId && !related.has(resource.id)), onEnter: handleEnter, onInspect: onSelect },
    }
  })
  const visible = new Set(shown.map((node) => node.id))
  const edges: MapEdge[] = topologyVisibleEdges(graph, visible).filter((edge) => dependencies || ["owns", "selects", "routes to"].includes(edge.relation)).map((edge) => {
    const from = graph.nodes.find((node) => node.id === edge.from)!, to = graph.nodes.find((node) => node.id === edge.to)!
    const dependency = !["owns", "selects", "routes to"].includes(edge.relation)
    const sync = [from, to].some((node) => stateFor(node) === "syncing")
    const failed = [from, to].some((node) => stateFor(node) === "failed")
    const traffic = edge.relation === "routes to" ? to.routingHealth : edge.relation === "selects" ? from.routingHealth : undefined
    const backends = from.routingHealth?.backends?.filter((backend) => backend.serviceName === to.identity.name && backend.namespace === to.identity.namespace)
    const backendStatus = backends?.find((backend) => backend.status === "degraded")?.status ?? backends?.find((backend) => backend.status === "unknown")?.status ?? backends?.find((backend) => backend.status === "progressing")?.status ?? backends?.[0]?.status
    const trafficStatus = edge.relation === "routes to" && from.routingHealth?.reason === "RouteNotAdmitted" ? "degraded" : backendStatus ?? traffic?.status
    const signal = dependency ? "unknown" : trafficStatus ? connectionSignal(trafficStatus, trafficStatus, sync, failed) : connectionSignal(edge.relation === "owns" ? runtime.get(to.id)! : runtime.get(from.id)!, runtime.get(to.id)!, sync, failed)
    const active = edge.from === selectedId || edge.to === selectedId
    const forward = (nodes.find((node) => node.id === edge.from)?.position.x ?? 0) <= (nodes.find((node) => node.id === edge.to)?.position.x ?? 0)
    const relation = edge.viaReplicaSet ? "owns via ReplicaSet" : edge.relation
    return { id: JSON.stringify([edge.from, edge.to, edge.relation]), source: edge.from, target: edge.to, sourceHandle: forward ? "source-right" : "source-left", targetHandle: forward ? "target-left" : "target-right", type: "relationship", focusable: true, selectable: false,
      ariaLabel: `${from.identity.name} ${relation} ${to.identity.name}: ${signal === "syncing" ? "Applying deployment" : signalLabels[signal]}`,
      data: { signal, relation, active, dependency }, style: { opacity: selectedId && !active ? 0.35 : 1 } }
  })

  // Fit only when navigating/filtering, never on live observations or node status changes.
  const viewKey = JSON.stringify([parentId, query.trim(), mode, focus, replicaSets, focus ? selectedId : null])
  useEffect(() => {
    if (!initialized || appliedView.current === viewKey && !pendingView.current) return
    const frame = requestAnimationFrame(() => {
      const pending = pendingView.current
      pendingView.current = null
      appliedView.current = viewKey
      if (pending?.viewport) void flow.setViewport(pending.viewport, { duration: pending.duration, ease })
      else {
        const targets = query.trim() ? nodes.filter((node) => matches(node.data.resource)) : nodes
        const canvas = root.current?.querySelector(".react-flow")
        if (targets.length && canvas) {
          const x = Math.min(...targets.map((node) => node.position.x)), y = Math.min(...targets.map((node) => node.position.y))
          const width = Math.max(...targets.map((node) => node.position.x + mapNodeWidth)) - x
          const height = Math.max(...targets.map((node) => node.position.y + mapNodeHeightFor(node.data.resource))) - y
          // Use the new layout directly; React Flow's measured nodes can still belong
          // to the outgoing scene during this commit.
          const camera = getViewportForBounds({ x, y, width, height }, canvas.clientWidth, canvas.clientHeight, 0.15, 1.1, 0.25)
          void flow.setViewport(camera, { duration: pending?.duration ?? 0, ease })
        }
      }
    })
    return () => cancelAnimationFrame(frame)
    // Data refreshes intentionally retain the user's camera and dragged positions.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [viewKey, initialized])

  useEffect(() => () => { ++navigation.current }, [])

  const onNodesChange = useCallback((changes: NodeChange<MapNode>[]) => {
    const moved = changes.filter((change) => change.type === "position" && change.position)
    if (!moved.length) return
    setPositions((previous) => {
      const next = { ...previous }
      for (const change of moved) if (change.type === "position" && change.position) next[`${scopeKey(parentId)}:${change.id}`] = change.position
      return next
    })
  }, [parentId])

  function centerSelection(event: MouseEvent) {
    const node = selectedId ? flow.getNode(selectedId) : undefined
    if (node) void flow.setCenter(node.position.x + mapNodeWidth / 2, node.position.y + mapNodeHeightFor(node.data.resource) / 2, { zoom: Math.max(0.8, flow.getZoom()), duration: event.detail && !reducedMotion() ? 240 : 0, ease })
  }

  return <div ref={root} className={`${styles.canvas} ${paused ? styles.paused : ""} relative min-w-0`}>
    <div className="flex min-h-12 flex-wrap items-center justify-between gap-2 border-b bg-card/80 px-4 py-2">
      <nav aria-label="Topology location" className="flex min-w-0 flex-wrap items-center gap-1">
        <Button variant="ghost" size="sm" disabled={!parentId} onClick={(event) => back(0, event.detail !== 0)}>{applicationName}</Button>
        {validPath.map((id, index) => <span key={id} className="inline-flex min-w-0 items-center gap-1"><HugeiconsIcon icon={ArrowRight01Icon} className="size-3 text-muted-foreground" aria-hidden="true" /><Button variant="ghost" size="sm" className="max-w-40 truncate" aria-current={id === parentId ? "location" : undefined} disabled={id === parentId} onClick={(event) => back(index + 1, event.detail !== 0)}>{graph.nodes.find((node) => node.id === id)?.identity.name}</Button></span>)}
      </nav>
      <div className="flex flex-wrap items-center gap-4">
        <label className="inline-flex items-center gap-2 text-xs text-muted-foreground"><Switch checked={dependencies} onCheckedChange={setDependencies} aria-label="Show configuration dependencies" />Dependencies</label>
        {parentId && !globalView && (hierarchy.children.get(parentId) ?? []).some((node) => node.identity.kind === "ReplicaSet") && <label className="inline-flex items-center gap-2 text-xs text-muted-foreground"><Switch checked={replicaSets} onCheckedChange={setReplicaSets} aria-label="Show ReplicaSets" />ReplicaSets</label>}
      <Button variant="ghost" size="xs" aria-pressed={paused} onClick={() => setPaused(!paused)}>{paused ? "Resume animation" : "Pause animation"}</Button></div>
    </div>
    {globalView && parentId && <p className="border-b px-4 py-2 text-xs text-muted-foreground">Search and filters show resources across the application. Clear them to return to this group.</p>}
    <div className="relative h-[min(68svh,760px)] min-h-[420px]">
      <ReactFlow<MapNode, MapEdge> nodes={nodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} onNodesChange={onNodesChange}
        nodesConnectable={false} edgesReconnectable={false} nodesFocusable={false} deleteKeyCode={null} selectionKeyCode={null}
        minZoom={0.15} maxZoom={2} zoomOnDoubleClick={false} panOnDrag panOnScroll zoomOnScroll={false} zoomOnPinch
        onInit={(instance) => { void instance.fitView({ padding: 0.25, maxZoom: 1.1 }) }}
        tabIndex={0} onKeyDown={(event) => {
          if (event.target !== event.currentTarget) return
          const offset: Record<string, XYPosition> = { ArrowLeft: { x: 64, y: 0 }, ArrowRight: { x: -64, y: 0 }, ArrowUp: { x: 0, y: 64 }, ArrowDown: { x: 0, y: -64 } }
          const delta = offset[event.key]
          if (delta) { event.preventDefault(); const camera = flow.getViewport(); void flow.setViewport({ ...camera, x: camera.x + delta.x, y: camera.y + delta.y }) }
          else if (event.key === "Home") { event.preventDefault(); void flow.fitView({ padding: 0.25, maxZoom: 1.1 }) }
          else if (event.key === "Escape" && parentId) { event.preventDefault(); back(validPath.length - 1, false) }
        }}
        aria-label="Interactive application topology. Drag resources to arrange them. Drag the background or use arrow keys to pan. Pinch or use the controls to zoom. Home fits the view; Escape returns to the parent group."
        className={moving ? styles.entering : undefined}>
        {!shown.length && <div className="pointer-events-none absolute inset-0 z-10 grid place-items-center p-6"><EmptyState title={graph.nodes.length ? "No matching resources" : "No resources to show yet"} description={graph.nodes.length ? "Try another filter or return to the application." : "Build a plan to discover resources and their relationships."} /></div>}
      </ReactFlow>
      <div className="absolute right-3 bottom-3 z-10 flex max-w-[calc(100%-24px)] flex-wrap items-center gap-0.5 rounded-lg border bg-card/95 p-1 shadow-sm" role="group" aria-label="Canvas view controls">
        {parentId && <Tooltip><TooltipTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Back to parent group" onClick={(event) => back(validPath.length - 1, event.detail !== 0)} />}><HugeiconsIcon icon={ArrowLeft01Icon} aria-hidden="true" /></TooltipTrigger><TooltipContent>Back to parent</TooltipContent></Tooltip>}
        <Tooltip><TooltipTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Fit all resources" onClick={(event) => { void flow.fitView({ padding: 0.25, maxZoom: 1.1, duration: event.detail && !reducedMotion() ? 240 : 0, ease }) }} />}><HugeiconsIcon icon={Maximize01Icon} aria-hidden="true" /></TooltipTrigger><TooltipContent>Fit view</TooltipContent></Tooltip>
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
      <span>{dependencies ? "Dashed = configuration dependency" : "Configuration links hidden · Enable Dependencies to show"}</span>
    </div>
  </div>
}

export function TopologyCanvas(props: Props) {
  return <ReactFlowProvider><Canvas {...props} /></ReactFlowProvider>
}
