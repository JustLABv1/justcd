import type { Identity, Operation, PlanRecord, ResourceTopology, TopologyNode } from "./types"

export const identityKey = (id: Identity) => JSON.stringify([id.clusterId ?? "", id.apiVersion, id.kind, id.namespace, id.name])
export const observed = (node: TopologyNode) => node.source === "sample" || node.source === "kubernetes"
export const workload = (node: TopologyNode) => ["Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob"].includes(node.identity.kind)
export type SyncState = "syncing" | "applied" | "failed" | "create" | "update" | "delete" | "synced" | "unknown" | "observed"
export function syncState(node: TopologyNode, plan: PlanRecord | null, operation?: Operation): SyncState {
  const key = identityKey(node.identity)
  if (operation?.planId === plan?.id) {
    if (operation?.progress?.current && identityKey(operation.progress.current) === key) {
      if (operation.status === "running") return "syncing"
      if (operation.status === "failed") return "failed"
    }
    if (operation?.progress?.completed?.some((id) => identityKey(id) === key)) return "applied"
  }
  if (observed(node)) return "observed"
  if (plan?.status === "applied" && node.uid) return "synced"
  const change = plan?.plan.changes.find((change) => identityKey(change.identity) === key)
  if (change && ["current", "failed"].includes(plan?.status ?? "")) return change.kind
  if (plan?.status === "current" && node.uid) return "synced"
  return "unknown"
}

/** Directed ancestry/descendency, not an undirected flood through shared dependencies. */
export function relatedNodes(graph: ResourceTopology, selected: string): Set<string> {
  const result = new Set([selected])
  for (const direction of ["from", "to"] as const) {
    const seen = new Set([selected]), queue = [selected]
    for (let i = 0; i < queue.length; i++) for (const edge of graph.edges) {
      if (edge[direction] !== queue[i]) continue
      const next = edge[direction === "from" ? "to" : "from"]
      if (!seen.has(next)) { seen.add(next); result.add(next); queue.push(next) }
    }
  }
  return result
}

export const nodeWidth = 280, nodeHeight = 124
export function topologyLayout(graph: ResourceTopology, nodes: TopologyNode[]) {
  const anchors = graph.nodes.filter(workload).sort((a, b) => a.id.localeCompare(b.id))
  const owners = new Map<string, Set<string>>()
  for (const anchor of anchors) for (const id of relatedNodes(graph, anchor.id)) {
    const values = owners.get(id) ?? new Set<string>(); values.add(anchor.id); owners.set(id, values)
  }
  const groups = new Map<string, TopologyNode[]>()
  for (const node of nodes) {
    const candidates = owners.get(node.id)
    const group = workload(node) ? node.id : candidates?.size === 1 ? [...candidates][0] : candidates?.size ? "shared" : "unconnected"
    groups.set(group, [...(groups.get(group) ?? []), node])
  }
  // Collapsed workloads without visible dependencies share a compact band.
  const standalone: TopologyNode[] = []
  for (const [id, members] of groups) {
    if (members.length === 1 && workload(members[0])) {
      standalone.push(members[0]); groups.delete(id)
    }
  }
  if (standalone.length) groups.set("workloads", standalone)
  const rank = (node: TopologyNode) => ["Ingress", "HTTPRoute", "Gateway"].includes(node.identity.kind) ? 0 : node.identity.kind === "Service" ? 1 : workload(node) ? 2 : ["ReplicaSet", "Pod"].includes(node.identity.kind) ? 3 : 4
  const positions = new Map<string, { x: number; y: number }>()
  const bands: { id: string; label: string; y: number; height: number }[] = []
  let y = 20, width = 340
  for (const [id, members] of [...groups].sort(([a], [b]) => (a === "unconnected" ? 2 : a === "shared" ? 1 : 0) - (b === "unconnected" ? 2 : b === "shared" ? 1 : 0) || a.localeCompare(b))) {
    const ranks = [...new Set(members.map(rank))].sort((a, b) => a - b)
    const counts = new Map<number, number>()
    for (const node of [...members].sort((a, b) => a.identity.kind.localeCompare(b.identity.kind) || a.identity.name.localeCompare(b.identity.name))) {
      const compact = id === "workloads"
      const index = members.indexOf(node)
      const col = compact ? index % 3 : ranks.indexOf(rank(node)), row = compact ? Math.floor(index / 3) : counts.get(col) ?? 0
      positions.set(node.id, { x: 28 + col * 352, y: y + 44 + row * 148 }); counts.set(col, row + 1)
    }
    const height = 44 + Math.max(...counts.values()) * 148
    const anchor = graph.nodes.find((node) => node.id === id)
    bands.push({ id, label: id === "shared" ? "Shared resources" : id === "workloads" ? "Workloads" : id === "unconnected" ? "Other resources" : `${anchor?.identity.name} · ${anchor?.identity.namespace || "cluster"}`, y, height })
    width = Math.max(width, 56 + (id === "workloads" ? Math.min(3, members.length) : ranks.length) * 352 - 72); y += height + 16
  }
  return { positions, bands, width, height: Math.max(320, y) }
}
