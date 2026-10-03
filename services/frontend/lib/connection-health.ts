import type { Application, Cluster, GitSource } from "./types"

export type HealthLevel = "healthy" | "warning" | "critical" | "unknown"

export type AgentSnapshot = {
  mode: "agent" | "direct"
  online?: boolean
  agent?: { revoked: boolean; lastSeenAt?: string; version?: string }
}

export type ConnectionHealth = {
  level: HealthLevel
  label: string
  detail: string
  affectedApplications: Application[]
}

const worst = (a: HealthLevel, b: HealthLevel): HealthLevel => {
  const rank: Record<HealthLevel, number> = { healthy: 0, unknown: 1, warning: 2, critical: 3 }
  return rank[a] >= rank[b] ? a : b
}

/** Issues that point at the connection itself, not at the application's own content. */
function connectionIssues(apps: Application[], source: "cluster" | "git") {
  return apps.filter((app) => !app.decommissioning && app.statusIssues?.some((issue) => issue.source === source && issue.code !== "kubernetes.ownership_conflict"))
}

export function clusterHealth(cluster: Pick<Cluster, "id" | "connectionMode">, agent: AgentSnapshot | undefined, apps: Application[], now = Date.now()): ConnectionHealth {
  const affected = connectionIssues(apps.filter((app) => app.clusterId === cluster.id), "cluster")
  let level: HealthLevel = "healthy"
  let label = "Connected"
  let detail = "No cluster errors reported by applications."

  if (cluster.connectionMode === "agent") {
    if (!agent) {
      level = "unknown"; label = "Agent status unknown"; detail = "The agent status could not be loaded."
    } else if (agent.agent?.revoked) {
      level = "critical"; label = "Agent revoked"; detail = "Enroll the agent again to restore this cluster."
    } else if (!agent.agent?.lastSeenAt) {
      level = "warning"; label = "Waiting for agent"; detail = "The agent has not connected yet."
    } else if (!agent.online) {
      const minutes = Math.max(1, Math.round((now - Date.parse(agent.agent.lastSeenAt)) / 60000))
      level = "critical"; label = "Agent offline"; detail = `Last seen ${minutes < 120 ? `${minutes} min` : `${Math.round(minutes / 60)} h`} ago.`
    } else {
      label = "Agent online"; detail = "The agent is polling JustCD."
    }
  }
  if (affected.length) {
    const unreachable = affected.some((app) => app.statusIssues.some((issue) => issue.source === "cluster" && issue.code !== "kubernetes.permission_denied"))
    level = worst(level, unreachable ? "critical" : "warning")
    if (level !== "healthy" && (label === "Connected" || label === "Agent online")) {
      label = unreachable ? "Cluster unreachable" : "Permissions missing"
      detail = affected[0].statusIssues.find((issue) => issue.source === "cluster")?.summary ?? detail
    }
  }
  return { level, label, detail, affectedApplications: affected }
}

export function gitSourceHealth(source: GitSource, apps: Application[]): ConnectionHealth {
  const affected = connectionIssues(apps.filter((app) => app.sourceId === source.id), "git")
  if (!affected.length) return { level: "healthy", label: "Reachable", detail: "No Git errors reported by applications.", affectedApplications: [] }
  return {
    level: "critical",
    label: "Git unreachable",
    detail: affected[0].statusIssues.find((issue) => issue.source === "git")?.summary ?? "Applications cannot read this repository.",
    affectedApplications: affected,
  }
}

/** An application is stalled when its checks stopped well beyond its polling interval. */
export function isCheckStalled(app: Application, now = Date.now()) {
  if (app.decommissioning || app.autoSyncPaused) return false
  const grace = Math.max((app.pollSeconds || 60) * 3, 180) * 1000
  const last = Date.parse(app.lastCheckedAt || "") || Date.parse(app.createdAt) || 0
  return now - last > grace
}

export function overallLevel(levels: HealthLevel[]): HealthLevel {
  return levels.reduce(worst, "healthy" as HealthLevel)
}
