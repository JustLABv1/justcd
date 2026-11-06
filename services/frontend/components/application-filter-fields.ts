import { binaryOperators, choiceOperators, dateTimeField } from "@/components/filter-field-presets"
import type { FilterField } from "@/components/reui/filters/filters-types"
import { isApplicationHealthy, needsAttention, type WorkspaceApplication } from "@/hooks/use-workspace"
import { words } from "@/lib/audit"

const syncPolicies: Record<string, string> = { manual: "Manual", "auto-safe": "Auto (safe changes)", auto: "Auto" }
const renderers: Record<string, string> = { yaml: "Plain YAML / JSON", helm: "Helm", kustomize: "Kustomize" }

/** The fields users can filter applications by. */
export function applicationFilterFields(applications: WorkspaceApplication[], workspaces?: { id: string; name: string }[]): FilterField[] {
  const health = [...new Set(applications.map((app) => app.health).filter(Boolean))].sort()
  return [
    { id: "name", label: "Name", type: "text", placeholder: "Application name" },
    ...(workspaces?.length ? [{ id: "workspace", label: "Workspace", type: "select" as const, options: workspaces.map((item) => ({ value: item.id, label: item.name })), operators: choiceOperators }] : []),
    { id: "cluster", label: "Cluster", type: "text", placeholder: "Cluster name" },
    { id: "namespace", label: "Namespace", type: "text", description: "Matches any namespace the application deploys to" },
    { id: "status", label: "Status", type: "select", options: [{ value: "attention", label: "Needs attention" }, { value: "healthy", label: "Healthy" }, { value: "other", label: "Progressing or unknown" }], operators: choiceOperators },
    { id: "health", label: "Sync state", type: "select", options: health.map((value) => ({ value, label: words(value) })), operators: choiceOperators },
    { id: "reconciliation", label: "Reconciliation", type: "select", options: [{ value: "paused", label: "Paused" }, { value: "active", label: "Active" }], operators: binaryOperators },
    { id: "syncPolicy", label: "Sync policy", type: "select", options: Object.entries(syncPolicies).map(([value, label]) => ({ value, label })), operators: choiceOperators },
    { id: "renderer", label: "Renderer", type: "select", options: Object.entries(renderers).map(([value, label]) => ({ value, label })), operators: choiceOperators },
    { id: "revision", label: "Branch or revision", type: "text" },
    { id: "path", label: "Manifest path", type: "text" },
    { id: "label", label: "Label", type: "text", placeholder: "key=value", description: "Matches any label, written as key=value" },
    { id: "labelKey", label: "Label key", type: "text", description: "Matches any label name" },
    dateTimeField("lastChecked", "Last checked", "When JustCD last checked Git and the cluster"),
    dateTimeField("created", "Created", "When the application was created"),
  ]
}

export const applicationFilterFieldIds = ["name", "workspace", "cluster", "namespace", "status", "health", "reconciliation", "syncPolicy", "renderer", "revision", "path", "label", "labelKey", "lastChecked", "created"]

/** What an application holds for a filter field, in the shape the evaluator expects. */
export function readApplicationField(app: WorkspaceApplication, field: string): string[] | undefined {
  switch (field) {
    case "name": return [app.name]
    case "workspace": return [app.workspaceId]
    case "cluster": return [app.clusterName ?? app.clusterId]
    case "namespace": return app.namespaces.map((item) => item.namespace)
    case "status": return [needsAttention(app) ? "attention" : isApplicationHealthy(app) ? "healthy" : "other"]
    case "health": return [app.health]
    case "reconciliation": return [app.autoSyncPaused ? "paused" : "active"]
    case "syncPolicy": return [app.syncPolicy]
    case "renderer": return [app.renderer]
    case "revision": return [app.revision]
    case "path": return [app.manifestPath]
    case "label": return Object.entries(app.labels ?? {}).map(([key, value]) => `${key}=${value}`)
    case "labelKey": return Object.keys(app.labels ?? {})
    case "lastChecked": return app.lastCheckedAt ? [app.lastCheckedAt] : []
    case "created": return [app.createdAt]
    default: return undefined
  }
}
