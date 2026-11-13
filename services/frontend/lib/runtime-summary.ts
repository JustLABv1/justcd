import { runtimeSignal, type RuntimeSignal } from "./topology-map"
import { applicationResourceNodes, configurationResource } from "./topology-view"
import type { ResourceTopology } from "./types"

export type RuntimeSummary = Record<RuntimeSignal, number> & {
  /** Observed resources that carry a workload health signal. */
  total: number
  worst: RuntimeSignal | null
}

/**
 * Health of the application's own observed resources. Configuration objects
 * (ConfigMaps, Services, RBAC…) have no workload health and are left out, so a
 * degraded Deployment cannot hide behind a row of healthy Secrets.
 */
export function summarizeRuntime(topology: ResourceTopology | null): RuntimeSummary {
  const summary: RuntimeSummary = { healthy: 0, progressing: 0, degraded: 0, unknown: 0, total: 0, worst: null }
  if (!topology) return summary
  for (const node of applicationResourceNodes(topology)) {
    if (node.source !== "kubernetes" || configurationResource(node.identity)) continue
    summary[runtimeSignal(node)] += 1
    summary.total += 1
  }
  summary.worst = summary.degraded ? "degraded" : summary.progressing ? "progressing" : summary.unknown ? "unknown" : summary.healthy ? "healthy" : null
  return summary
}
