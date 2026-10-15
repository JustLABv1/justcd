export type DeploymentEvidence = {
  schemaVersion: number
  planId: string
  commit: string
  planDigest: string
  planCreatedAt: string
  targets: {
    clusterId: string
    namespace: string
    clusterScope: boolean | null
  }[]
  requiredApprovals: number
  approvals: {
    id: string
    actorId: string
    planDigest: string
    createdAt: string
    usedAt: string | null
  }[]
  policy: {
    mode: string | null
    configDigest: string | null
    results: {
      rule: string
      resource: {
        kind: string
        name: string
        namespace: string
        clusterId: string
      }
    }[]
  }
  operationId: string | null
  operationType: string | null
  result: string | null
  startedAt: string | null
  finishedAt: string | null
  errorCode: string | null
}

export type AuditEvent = {
  schemaVersion: number
  id: number
  actorId?: string
  actorName: string
  action: string
  workspaceId: string
  resourceType: string
  resourceId: string
  resourceName: string
  details: Record<string, unknown> | null
  deploymentRecord: DeploymentEvidence | null
  createdAt: string
}
export type AuditResponse = { items: AuditEvent[]; hasMore: boolean }
export type EventFilter = "all" | "changes" | "deployments" | "access"
export function words(value: string) {
  return value
    .replaceAll("_", " ")
    .replace(/([a-z])([A-Z])/g, "$1 $2")
    .replace(/\b(id|ids|utc|api)\b/gi, (word) => word.toUpperCase())
    .replace(/^\w/, (letter) => letter.toUpperCase())
}
export function actionLabel(action: string) {
  return words(action.replaceAll(".", " ")).replace(/^\w/, (letter) =>
    letter.toUpperCase()
  )
}
export function actorLabel(event: AuditEvent) {
  if (event.actorId === "justcd-system") return "JustCD automation"
  return event.actorName || (event.actorId ? "Unknown user" : "System")
}
export function eventStatus(action: string) {
  if (action.endsWith("failed"))
    return { label: "Failed", variant: "destructive-light" as const }
  if (action.endsWith("succeeded"))
    return { label: "Succeeded", variant: "success-light" as const }
  if (action.endsWith("queued"))
    return { label: "Queued", variant: "warning-light" as const }
  if (action.endsWith("cancelled"))
    return { label: "Cancelled", variant: "secondary" as const }
  return null
}
export function dateLabel(value: string) {
  return new Date(value).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  })
}
export function valueText(value: unknown): string {
  if (value === null || value === undefined) return "Not recorded"
  if (typeof value === "boolean") return value ? "Yes" : "No"
  if (typeof value === "object") return JSON.stringify(value, null, 2)
  return String(value)
}
