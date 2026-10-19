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
  return graph.nodes.filter((node) => !hierarchy.parents.has(node.id) || node.tracked === true || node.source === "desired" || node.source === "managed" || node.identity.kind === "Ingress" || node.identity.kind === "Route" && node.identity.apiVersion.startsWith("route.openshift.io/"))
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
  if (node.routingHealth) return runtimeSignal(node)
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
  const rank = (node: TopologyNode) => ["Ingress", "Route", "HTTPRoute", "Gateway"].includes(node.identity.kind) ? 0 : node.identity.kind === "Service" ? 1 : ["Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "ReplicaSet", "Pod"].includes(node.identity.kind) ? 2 : 3
  let x = 40
  for (const lane of [0, 1, 2, 3]) {
    const incomingY = (id: string) => {
      const ys = edges.filter((edge) => edge.to === id && ["routes to", "selects"].includes(edge.relation)).flatMap((edge) => positions.has(edge.from) ? [positions.get(edge.from)!.y] : [])
      return ys.length ? ys.reduce((a, b) => a + b, 0) / ys.length : Number.MAX_SAFE_INTEGER
    }
    const members = sorted.filter((node) => rank(node) === lane).sort((a, b) => incomingY(a.id) - incomingY(b.id))
    if (!members.length) continue
    const columns = Math.min(3, Math.ceil(members.length / 5))
    const rowHeight = Math.max(mapNodeHeight, ...members.map(mapNodeHeightFor)) + 40
    members.forEach((node, index) => positions.set(node.id, { x: x + index % columns * 280, y: 40 + Math.floor(index / columns) * rowHeight }))
    x += columns * 280 + 60
  }
  return positions
}
