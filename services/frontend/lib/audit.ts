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

export type AuditTarget = { clusterId: string; clusterName: string; namespace: string }

export type AuditEvent = {
  schemaVersion: number
  id: number
  workspaceName?: string
  /** Where the change landed, with names resolved when the event was read. */
  where?: AuditTarget[]
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
/** What the signed-in user may read; the server enforces it, the page only reflects it. */
export type AuditAccess = { scope: "instance" | "workspace"; role?: "owner" | "deployer" | "viewer"; groups: string[]; canExport: boolean }
export type AuditResponse = { items: AuditEvent[]; hasMore: boolean; access?: AuditAccess }
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
export type ActorKind = "all" | "people" | "automation"
export function isAutomation(event: AuditEvent) {
  return event.actorId === "justcd-system"
}
export function actorLabel(event: AuditEvent) {
  if (isAutomation(event)) return event.actorName || "JustCD automation"
  return event.actorName || (event.actorId ? "Unknown user" : "System")
}
/** What kicked off an automated event, when the action name says so. */
export function automationTrigger(event: AuditEvent) {
  if (!isAutomation(event)) return ""
  if (event.action.startsWith("auto_sync.")) return "Auto-sync"
  if (event.action.startsWith("plan.")) return "Reconciliation"
  return ""
}
/** The place an event happened in: workspace › cluster › namespace. */
export function wherePath(event: AuditEvent, showWorkspace: boolean) {
  const targets = event.where ?? []
  const first = targets[0]
  const path = [showWorkspace ? event.workspaceName : "", first?.clusterName, first?.namespace].filter((part): part is string => Boolean(part))
  return { path, more: Math.max(0, targets.length - 1) }
}
export function eventStatus(action: string) {
  if (action.endsWith("failed"))
    return { label: "Failed", variant: "destructive" as const }
  if (action.endsWith("succeeded"))
    return { label: "Succeeded", variant: "success" as const }
  if (action.endsWith("queued"))
    return { label: "Queued", variant: "warning" as const }
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
