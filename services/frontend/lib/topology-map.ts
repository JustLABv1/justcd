import type { ResourceTopology, TopologyNode } from "./types"

export type RuntimeSignal = "healthy" | "progressing" | "degraded" | "unknown"
export type ConnectionSignal = RuntimeSignal | "syncing"

/** Ownership alone controls drill-down; shared dependencies must never become children. */
export function topologyHierarchy(graph: ResourceTopology) {
  const nodes = new Map(graph.nodes.map((node) => [node.id, node]))
  const children = new Map<string, TopologyNode[]>()
  const parents = new Map<string, string>()
  for (const edge of graph.edges) {
    if (edge.relation !== "owns" || edge.from === edge.to || !nodes.has(edge.from) || !nodes.has(edge.to)) continue
    const list = children.get(edge.from) ?? []
    if (!list.some((node) => node.id === edge.to)) list.push(nodes.get(edge.to)!)
    children.set(edge.from, list)
    if (!parents.has(edge.to)) parents.set(edge.to, edge.from)
  }
  const descendants = (id: string) => {
    const seen = new Set([id]), result: TopologyNode[] = [], queue = [id]
    for (let i = 0; i < queue.length; i++) for (const child of children.get(queue[i]) ?? []) {
      if (seen.has(child.id)) continue
      seen.add(child.id); queue.push(child.id); result.push(child)
    }
    return result
  }
  const ancestry = (id: string) => {
    const result: string[] = [], seen = new Set([id])
    let parent = parents.get(id)
    while (parent && !seen.has(parent)) {
      result.unshift(parent); seen.add(parent); parent = parents.get(parent)
    }
    return result
  }
  return { children, parents, descendants, ancestry }
}

export function topologyScope(graph: ResourceTopology, parentId: string | null, showReplicaSets = false): TopologyNode[] {
  const hierarchy = topologyHierarchy(graph)
  if (parentId && graph.nodes.some((node) => node.id === parentId)) {
    const children = hierarchy.children.get(parentId) ?? []
    // Flatten only the known Deployment → ReplicaSet → Pod ownership chain.
    // Empty ReplicaSets remain discoverable even in the simplified view.
    const expanded = children.flatMap((child) => {
      const pods = (hierarchy.children.get(child.id) ?? []).filter((item) => item.identity.kind === "Pod")
      return child.identity.kind === "ReplicaSet" && pods.length ? [...(showReplicaSets ? [child] : []), ...pods] : [child]
    })
    const ids = new Set([parentId, ...expanded.map((node) => node.id)])
    return graph.nodes.filter((node) => ids.has(node.id))
  }
  return graph.nodes.filter((node) => !hierarchy.parents.has(node.id) || node.tracked === true || node.source === "desired" || node.source === "managed" || node.identity.kind === "Ingress")
}

/** Supporting objects stay inspectable without competing with traffic and workloads. */
export function supportingTopologyResource(node: TopologyNode) {
  return ["ConfigMap", "Secret", "ServiceAccount", "Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding", "NetworkPolicy", "PersistentVolumeClaim", "PersistentVolume", "StorageClass", "HorizontalPodAutoscaler", "PodDisruptionBudget", "ResourceQuota", "LimitRange", "Endpoints", "EndpointSlice", "Namespace", "SGInstanceProfile", "SGScript", "SGObjectStorage"].includes(node.identity.kind)
}

/** Only connected dependencies belong on the canvas when explicitly requested. */
export function topologyCanvasResources(nodes: TopologyNode[], edges: ResourceTopology["edges"], showDependencies: boolean) {
  const available = new Set(nodes.map((node) => node.id))
  const core = new Set(nodes.filter((node) => !supportingTopologyResource(node)).map((node) => node.id))
  const included = new Set(core)
  if (showDependencies) {
    // Include dependency chains (e.g. ServiceAccount → RoleBinding → Role), but
    // never pull a disconnected inventory object onto the map.
    let changed = true
    while (changed) {
      changed = false
      for (const edge of edges) {
        if (!available.has(edge.from) || !available.has(edge.to) || !included.has(edge.from) && !included.has(edge.to)) continue
        for (const id of [edge.from, edge.to]) if (!included.has(id)) { included.add(id); changed = true }
      }
    }
  }
  return nodes.filter((node) => included.has(node.id))
}

/** Incoming traffic and ownership ancestors provide real upstream context. */
export function topologyUpstream(graph: ResourceTopology, parentId: string): TopologyNode[] {
  const ids = new Set(topologyHierarchy(graph).ancestry(parentId)), queue = [parentId, ...ids]
  for (let index = 0; index < queue.length; index++) {
    for (const edge of graph.edges) {
      if (edge.to !== queue[index] || !["routes to", "selects"].includes(edge.relation) || edge.from === parentId || ids.has(edge.from)) continue
      ids.add(edge.from); queue.push(edge.from)
    }
  }
  return graph.nodes.filter((node) => ids.has(node.id))
}

/** Project hidden ReplicaSets without changing the actual resource relationships. */
export function topologyVisibleEdges(graph: ResourceTopology, visible: Set<string>) {
  const edges = graph.edges.filter((edge) => visible.has(edge.from) && visible.has(edge.to)).map((edge) => ({ ...edge, viaReplicaSet: false }))
  const hierarchy = topologyHierarchy(graph)
  for (const parent of graph.nodes.filter((node) => visible.has(node.id))) {
    for (const controller of hierarchy.children.get(parent.id) ?? []) {
      if (controller.identity.kind !== "ReplicaSet" || visible.has(controller.id)) continue
      for (const pod of hierarchy.children.get(controller.id) ?? []) {
        if (pod.identity.kind === "Pod" && visible.has(pod.id)) edges.push({ from: parent.id, to: pod.id, relation: "owns", viaReplicaSet: true })
      }
    }
  }
  return edges.filter((edge) => {
    // Expanded generated Routes already carry this traffic; the Ingress →
    // Service shortcut would draw the same connection across two layers.
    if (edge.relation === "routes to") return !edges.some((ownership) => ownership.from === edge.from && ownership.relation === "owns" && graph.nodes.some((node) => node.id === ownership.to && node.identity.kind === "Route") && edges.some((route) => route.from === ownership.to && route.to === edge.to && route.relation === "routes to"))
    if (edge.relation !== "selects") return true
    const ancestors = hierarchy.ancestry(edge.to)
    return !edges.some((other) => other.from === edge.from && other.relation === "selects" && ancestors.includes(other.to))
  })
}

/** Place upstream resources in successive lanes rather than crossing a single stack. */
export function topologyContextLayout(graph: ResourceTopology, parentId: string, context: TopologyNode[], centerY: number) {
  const depth = new Map([[parentId, 0]]), queue = [parentId]
  for (let index = 0; index < queue.length; index++) {
    for (const edge of graph.edges) {
      if (edge.to !== queue[index] || !["owns", "selects", "routes to"].includes(edge.relation) || depth.has(edge.from)) continue
      depth.set(edge.from, depth.get(edge.to)! + 1); queue.push(edge.from)
    }
  }
  const positions = new Map<string, { x: number; y: number }>()
  const lanes = new Map<number, TopologyNode[]>()
  for (const node of context) {
    const lane = depth.get(node.id) ?? 1
    lanes.set(lane, [...(lanes.get(lane) ?? []), node])
  }
  for (const [lane, members] of lanes) {
    members.sort((a, b) => a.id.localeCompare(b.id))
    const row = Math.max(mapNodeHeight, ...members.map(mapNodeHeightFor)) + 40
    members.forEach((node, index) => positions.set(node.id, { x: 40 - lane * 340, y: centerY + (index - (members.length - 1) / 2) * row }))
  }
  return positions
}

export function podSummary(pods: TopologyNode[]): string | null {
  if (!pods.length) return null
  const sample = pods.some((pod) => pod.source === "sample")
  const reasons = new Map<string, number>()
  for (const pod of pods) {
    const reason = pod.healthSummary?.failureReason
    if (reason) reasons.set(reason, (reasons.get(reason) ?? 0) + 1)
  }
  const ready = pods.filter((pod) => pod.phase === "Running" && pod.readiness === "Ready" && !pod.healthSummary?.terminating).length
  const [firstReason] = [...reasons]
  const failures = firstReason ? `${firstReason[1]} ${firstReason[0]}${reasons.size > 1 ? ` + ${reasons.size - 1} other issues` : ""}` : ""
  const other = pods.filter((pod) => runtimeSignal(pod) === "degraded" && !pod.healthSummary?.failureReason).length
  const suffix = [failures, other ? `${other} failed` : ""].filter(Boolean).join(" · ")
  return `${sample ? "Sample · " : ""}${ready}/${pods.length} pods ready${suffix ? ` · ${suffix}` : ""}`
}

export function runtimeSignal(node: TopologyNode): RuntimeSignal {
  if (node.source === "sample") return "unknown"
  if (node.routingHealth) return node.routingHealth.status
  const status = node.health?.status, details = node.healthSummary
  if (status === "Degraded" || status === "Missing" || node.phase === "Failed" || details?.failureReason || details?.conditions?.some((condition) => ["Failed", "ReplicaFailure"].includes(condition.type) && condition.status === "True")) return "degraded"
  if (details?.terminating || status === "Progressing" || node.phase === "Pending") return "progressing"
  if (status === "Suspended" || status === "Unknown" || status === "Partial") return "unknown"
  if (node.identity.kind === "Pod") {
    if (node.phase === "Succeeded") return "healthy"
    if (node.phase === "Running") return node.readiness === "Ready" ? "healthy" : "progressing"
  }
  if (status === "Healthy" || node.readiness === "Ready" || node.phase === "Succeeded") return "healthy"
  if (node.readiness === "Not ready") return "progressing"
  return "unknown"
}

/** Collapsed parents retain the worst observed descendant status. */
export function aggregateRuntime(node: TopologyNode, descendants: TopologyNode[]): RuntimeSignal {
  const signals = [node, ...descendants].map((item) => runtimeSignal(item))
  if (signals.includes("degraded")) return "degraded"
  if (signals.includes("progressing")) return "progressing"
  // An unknown parent cannot become healthy just because one child is healthy.
  return runtimeSignal(node)
}

export function connectionSignal(from: RuntimeSignal, to: RuntimeSignal, syncing: boolean, failed = false): ConnectionSignal {
  if (failed || from === "degraded" || to === "degraded") return "degraded"
  if (syncing) return "syncing"
  if (from === "progressing" || to === "progressing") return "progressing"
  return from === "healthy" && to === "healthy" ? "healthy" : "unknown"
}

export const mapNodeWidth = 224, mapNodeHeight = 170

export function mapNodeHeightFor(node: TopologyNode) {
  // Reserve enough lines even for maximum-length Kubernetes names. Labels wrap
  // naturally; using a conservative character width avoids font measurement races.
  return mapNodeHeight + (["Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "Job", "CronJob"].includes(node.identity.kind) ? 64 : node.identity.kind === "Pod" ? 18 : 0) + Math.max(0, Math.ceil(node.identity.name.length / 20) - 1) * 16 + (node.routingHealth ? 18 : 0)
}

export function resourceURLs(node: TopologyNode): string[] {
  return [...new Set((node.urls ?? []).flatMap((value) => {
    try {
      const url = new URL(value)
      return ["http:", "https:"].includes(url.protocol) && url.hostname && !url.hostname.includes("*") && !url.username && !url.password ? [url.href] : []
    } catch { return [] }
  }))]
}

/** Stable lanes rather than a force simulation: refreshes never shuffle resources. */
export function topologyMapLayout(nodes: TopologyNode[], parentId: string | null, edges: ResourceTopology["edges"] = []) {
  const positions = new Map<string, { x: number; y: number }>()
  const sorted = [...nodes].sort((a, b) => a.identity.namespace.localeCompare(b.identity.namespace) || a.identity.kind.localeCompare(b.identity.kind) || a.identity.name.localeCompare(b.identity.name) || a.id.localeCompare(b.id))
  if (parentId) {
    const children = sorted.filter((node) => node.id !== parentId)
    const depth = new Map([[parentId, 0]]), queue = [parentId], ids = new Set(nodes.map((node) => node.id))
    for (let index = 0; index < queue.length; index++) for (const edge of edges) {
      if (edge.from !== queue[index] || edge.relation !== "owns" || !ids.has(edge.to) || depth.has(edge.to)) continue
      depth.set(edge.to, depth.get(edge.from)! + 1); queue.push(edge.to)
    }
    const lanes = [...new Set(children.map((node) => depth.get(node.id) ?? 1))].sort((a, b) => a - b)
    const rowHeight = Math.max(mapNodeHeight, ...nodes.map(mapNodeHeightFor)) + 40
    const rows = Math.max(1, ...lanes.map((lane) => Math.min(5, children.filter((node) => (depth.get(node.id) ?? 1) === lane).length)))
    positions.set(parentId, { x: 40, y: (rows - 1) * rowHeight / 2 + 40 })
    let x = 380
    for (const lane of lanes) {
      const members = children.filter((node) => (depth.get(node.id) ?? 1) === lane)
      members.forEach((node, index) => positions.set(node.id, { x: x + Math.floor(index / rows) * 280, y: 40 + index % rows * rowHeight }))
      x += Math.ceil(members.length / rows) * 280 + 60
    }
    return positions
  }
  return topologyOverviewLayout(sorted, edges)
}

/** Layered graph layout: connected flows form bands; ordering minimizes crossings. */
function topologyOverviewLayout(sorted: TopologyNode[], edges: ResourceTopology["edges"]) {
  const positions = new Map<string, { x: number; y: number }>()
  const ids = new Set(sorted.map((node) => node.id))
  const links = topologyVisibleEdges({ nodes: sorted, edges, warnings: [] }, ids)
  const traffic = links.filter((edge) => ["owns", "routes to", "selects"].includes(edge.relation))
  const neighbours = new Map(sorted.map((node) => [node.id, new Set<string>()]))
  for (const edge of links) { neighbours.get(edge.from)!.add(edge.to); neighbours.get(edge.to)!.add(edge.from) }
  const components: TopologyNode[][] = [], seen = new Set<string>()
  const byId = new Map(sorted.map((node) => [node.id, node]))
  for (const node of sorted) {
    if (seen.has(node.id)) continue
    const queue = [node.id], members: TopologyNode[] = []
    seen.add(node.id)
    for (let index = 0; index < queue.length; index++) {
      members.push(byId.get(queue[index])!)
      for (const id of neighbours.get(queue[index])!) if (!seen.has(id)) { seen.add(id); queue.push(id) }
    }
    components.push(members)
  }
  // Traffic paths first, then other independent workload bands. No health-based
  // sorting: a live status refresh must not move resources.
  const hasEntry = (members: TopologyNode[]) => members.some((node) => ["Ingress", "Route", "Gateway", "HTTPRoute"].includes(node.identity.kind))
  components.sort((a, b) => Number(hasEntry(b)) - Number(hasEntry(a)))
  const hasOwnedRoute = sorted.some((node) => node.identity.kind === "Route" && traffic.some((edge) => edge.to === node.id && edge.relation === "owns"))
  const baseRank = (node: TopologyNode) => supportingTopologyResource(node) ? 5 : node.identity.kind === "Service" ? (hasOwnedRoute ? 2 : 1) : ["Ingress", "Route", "Gateway", "HTTPRoute"].includes(node.identity.kind) ? 0 : hasOwnedRoute ? 3 : 2
  const allRanks = new Map(sorted.map((node) => [node.id, baseRank(node)]))
  // Kahn ordering gives owned Routes their own layer after the Ingress, with
  // bounded deterministic behaviour even for malformed ownership cycles.
  const incoming = new Map(sorted.map((node) => [node.id, traffic.filter((edge) => edge.to === node.id).length]))
  const queue = sorted.filter((node) => incoming.get(node.id) === 0).map((node) => node.id)
  for (let index = 0; index < queue.length; index++) for (const edge of traffic) {
    if (edge.from !== queue[index]) continue
    allRanks.set(edge.to, Math.max(allRanks.get(edge.to)!, allRanks.get(edge.from)! + 1))
    incoming.set(edge.to, incoming.get(edge.to)! - 1)
    if (incoming.get(edge.to) === 0) queue.push(edge.to)
  }
  const ranks = [...new Set(allRanks.values())].sort((a, b) => a - b)
  const columns = new Map(ranks.map((rank, index) => [rank, 40 + index * 300]))
  let top = 40
  for (const members of components) {
    const lanes = ranks.map((rank) => members.filter((node) => allRanks.get(node.id) === rank).sort((a, b) => sorted.indexOf(a) - sorted.indexOf(b)))
    const memberIds = new Set(members.map((node) => node.id)), componentEdges = links.filter((edge) => memberIds.has(edge.from) && memberIds.has(edge.to))
    const order = () => new Map(lanes.flatMap((lane) => lane.map((node, index) => [node.id, index / Math.max(1, lane.length - 1)] as const)))
    // Alternating barycentre sweeps order nodes by their neighbours, rather
    // than by resource name. Ties preserve the deterministic initial order.
    for (let sweep = 0; sweep < 8; sweep++) {
      const indices = ranks.map((_, index) => index)
      if (sweep % 2) indices.reverse()
      for (const index of indices) {
        const current = order()
        const score = (id: string) => {
          const peers = componentEdges.flatMap((edge) => edge.to === id && sweep % 2 === 0 ? [edge.from] : edge.from === id && sweep % 2 === 1 ? [edge.to] : [])
          return peers.length ? peers.reduce((sum, peer) => sum + current.get(peer)!, 0) / peers.length : current.get(id)!
        }
        lanes[index].sort((a, b) => score(a.id) - score(b.id))
      }
    }
    const row = Math.max(mapNodeHeight, ...members.map(mapNodeHeightFor)) + 32
    const rows = Math.max(...lanes.map((lane) => lane.length)), height = rows * row
    lanes.forEach((lane, index) => lane.forEach((node, offset) => positions.set(node.id, { x: columns.get(ranks[index])!, y: top + (offset + (rows - lane.length) / 2) * row })))
    // Align sparse lanes with their connected resources while keeping every
    // node inside its band and leaving full label height between neighbours.
    for (let sweep = 0; sweep < 4; sweep++) {
      const indices = ranks.map((_, index) => index)
      if (sweep % 2) indices.reverse()
      for (const index of indices) {
        const lane = lanes[index]
        const desired = lane.map((node) => {
          const peers = componentEdges.flatMap((edge) => edge.to === node.id && sweep % 2 === 0 ? [edge.from] : edge.from === node.id && sweep % 2 === 1 ? [edge.to] : [])
          return peers.length ? peers.reduce((sum, id) => sum + positions.get(id)!.y, 0) / peers.length : positions.get(node.id)!.y
        })
        for (let offset = 0; offset < lane.length; offset++) desired[offset] = Math.max(top + offset * row, Math.min(desired[offset], top + height - (lane.length - offset) * row), offset ? desired[offset - 1] + row : top)
        for (let offset = lane.length - 1; offset >= 0; offset--) {
          const y = Math.min(desired[offset], top + height - (lane.length - offset) * row, offset < lane.length - 1 ? positions.get(lane[offset + 1].id)!.y - row : Infinity)
          positions.set(lane[offset].id, { x: columns.get(ranks[index])!, y })
        }
      }
    }
    top += height + 56
  }
  return positions
}


// Keep only the current scene per graph snapshot. Camera updates reuse its
// layout; filters and new observations replace it without retaining old scenes.
const sceneLayouts = new WeakMap<ResourceTopology, { key: string; positions: ReadonlyMap<string, { x: number; y: number }> }>()

/** Keep layout mutations behind the model boundary used by the canvas. */
export function topologySceneLayout(graph: ResourceTopology, parentId: string | null, globalView: boolean, visibleIds: string[], dependencies: boolean) {
  const key = JSON.stringify([parentId, globalView, visibleIds, dependencies])
  const cached = sceneLayouts.get(graph)
  if (cached?.key === key) return cached.positions
  const ids = new Set(visibleIds)
  const resources = graph.nodes.filter((node) => ids.has(node.id))
  const upstream = !globalView && parentId ? topologyUpstream(graph, parentId) : []
  const upstreamIds = new Set(upstream.map((node) => node.id))
  const links = topologyVisibleEdges(graph, ids).filter((edge) => dependencies || ["owns", "selects", "routes to"].includes(edge.relation))
  const result = topologyMapLayout(resources.filter((node) => !upstreamIds.has(node.id)), globalView ? null : parentId, links)
  if (parentId && !globalView) for (const [id, position] of topologyContextLayout(graph, parentId, upstream, result.get(parentId)?.y ?? 40)) result.set(id, position)
  sceneLayouts.set(graph, { key, positions: result })
  return result
}
