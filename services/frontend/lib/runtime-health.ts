import type { TopologyNode } from "./types"

export type PodState = "ready" | "starting" | "failing" | "terminating" | "completed" | "unknown" | "sample"
export function podState(node: TopologyNode): PodState {
  if (node.source === "sample") return "sample"
  if (node.healthSummary?.terminating) return "terminating"
  if (node.phase === "Failed" || node.healthSummary?.failureReason || node.health?.status === "Degraded") return "failing"
  if (node.phase === "Succeeded") return "completed"
  if (node.phase === "Running" && node.readiness === "Ready") return "ready"
  if (node.phase === "Pending" || node.phase === "Running") return "starting"
  return "unknown"
}

export function podLabel(node: TopologyNode) {
  const state = podState(node)
  if (state === "sample" || state === "terminating") return state
  return node.healthSummary?.failureReason || node.healthSummary?.waitingReason || state
}

export function podSummary(pods: TopologyNode[]) {
  const counts = new Map<PodState, number>()
  for (const pod of pods) { const state = podState(pod); counts.set(state, (counts.get(state) ?? 0) + 1) }
  return (["ready", "starting", "failing", "terminating", "completed", "unknown", "sample"] as const)
    .filter((state) => counts.has(state)).map((state) => `${counts.get(state)} ${state}`).join(" · ")
}

export function runtimeFreshness(observedAt: string | undefined, now: number) {
  const timestamp = observedAt ? Date.parse(observedAt) : NaN
  if (!Number.isFinite(timestamp)) return { stale: true, label: "Runtime not observed yet" }
  const seconds = Math.max(0, Math.floor((now - timestamp) / 1000))
  const age = seconds < 60 ? `${seconds}s` : seconds < 3600 ? `${Math.floor(seconds / 60)}m` : seconds < 86400 ? `${Math.floor(seconds / 3600)}h` : `${Math.floor(seconds / 86400)}d`
  return { stale: seconds > 60, label: `Runtime observed ${age} ago` }
}
