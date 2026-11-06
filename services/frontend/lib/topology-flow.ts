import { topologyHierarchy } from "./topology-map.ts"
import type { ResourceTopology, TopologyNode } from "./types"

const workloadKinds = new Set(["Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "ReplicaSet", "Pod"])
const routerKinds = new Set(["Ingress", "HTTPRoute", "Route", "Gateway", "TCPRoute", "GRPCRoute"])

export type FlowPodGroup = { id: string; replicaSet: TopologyNode | null; pods: TopologyNode[] }
export type FlowRow = {
  id: string
  workload: TopologyNode
  traffic: TopologyNode[]
  services: TopologyNode[]
  groups: FlowPodGroup[]
  pods: TopologyNode[]
  config: TopologyNode[]
  /** Every node id shown in the row, for filtering and selection. */
  members: Set<string>
}
export type FlowModel = { rows: FlowRow[]; other: TopologyNode[] }

const byName = (a: TopologyNode, b: TopologyNode) => a.identity.kind.localeCompare(b.identity.kind) || a.identity.name.localeCompare(b.identity.name)

/**
 * Arranges a topology into one left-to-right pipeline per workload:
 * traffic → services → workload (replica sets and pods) with its config underneath.
 * Everything that is not attached to a workload ends up in `other`.
 */
export function flowModel(graph: ResourceTopology): FlowModel {
  const nodes = new Map(graph.nodes.map((node) => [node.id, node]))
  const hierarchy = topologyHierarchy(graph)
  const workloads = graph.nodes.filter((node) => {
    if (!workloadKinds.has(node.identity.kind)) return false
    const parent = nodes.get(hierarchy.parents.get(node.id) ?? "")
    return !(parent && workloadKinds.has(parent.identity.kind))
  }).sort(byName)

  const placed = new Set<string>()
  const rows: FlowRow[] = workloads.map((workload) => {
    const descendants = hierarchy.descendants(workload.id)
    const pods = (workload.identity.kind === "Pod" ? [workload] : descendants.filter((node) => node.identity.kind === "Pod")).sort(byName)
    const owned = new Set([workload.id, ...descendants.map((node) => node.id)])

    const groups = new Map<string, FlowPodGroup>()
    for (const pod of pods) {
      const parent = nodes.get(hierarchy.parents.get(pod.id) ?? "")
      const rs = parent?.identity.kind === "ReplicaSet" ? parent : null
      const key = rs?.id ?? "__direct"
      const group = groups.get(key) ?? { id: key, replicaSet: rs, pods: [] }
      group.pods.push(pod)
      groups.set(key, group)
    }
    // Replica sets without pods (scaled down revisions) stay discoverable but quiet.
    for (const rs of descendants.filter((node) => node.identity.kind === "ReplicaSet")) if (!groups.has(rs.id)) groups.set(rs.id, { id: rs.id, replicaSet: rs, pods: [] })

    const serviceIds = new Set<string>()
    for (const edge of graph.edges) {
      if (edge.relation !== "selects") continue
      if (owned.has(edge.to) && nodes.get(edge.from)?.identity.kind === "Service") serviceIds.add(edge.from)
    }
    const services = [...serviceIds].map((id) => nodes.get(id)!).sort(byName)

    const trafficIds = new Set<string>()
    for (const edge of graph.edges) {
      if (edge.relation !== "routes to" || !serviceIds.has(edge.to)) continue
      const from = nodes.get(edge.from)
      if (from && routerKinds.has(from.identity.kind)) trafficIds.add(from.id)
    }
    // An Ingress that owns a Route would otherwise be listed twice for the same path.
    const duplicates = new Set<string>()
    for (const id of [...trafficIds]) { const parent = hierarchy.parents.get(id); if (parent && trafficIds.has(parent)) { trafficIds.delete(id); duplicates.add(id) } }
    const traffic = [...trafficIds].map((id) => nodes.get(id)!).sort(byName)

    const configIds = new Set<string>()
    for (const edge of graph.edges) if (edge.relation === "uses" && owned.has(edge.from) && nodes.has(edge.to) && !owned.has(edge.to)) configIds.add(edge.to)
    const config = [...configIds].map((id) => nodes.get(id)!).sort(byName)

    const members = new Set([...owned, ...serviceIds, ...trafficIds, ...duplicates, ...configIds])
    members.forEach((id) => placed.add(id))
    return { id: workload.id, workload, traffic, services, groups: [...groups.values()].sort((a, b) => b.pods.length - a.pods.length || (a.replicaSet?.identity.name ?? "").localeCompare(b.replicaSet?.identity.name ?? "")), pods, config, members }
  })
  return { rows, other: graph.nodes.filter((node) => !placed.has(node.id)).sort(byName) }
}
