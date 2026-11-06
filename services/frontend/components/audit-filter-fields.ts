import type { FilterField } from "@/components/reui/filters/filters-types"
import { binaryOperators, choiceOperators as choice, dateTimeField } from "@/components/filter-field-presets"
import { words } from "@/lib/audit"

const resourceTypes = ["application", "application_group", "cluster", "credential", "git_source", "repository_configuration", "publishing_profile", "oidc_provider", "user", "workspace", "instance"]

/** The fields a user can build audit filters from. The backend accepts exactly these ids. */
export function auditFilterFields(workspaces: { id: string; name: string }[]): FilterField[] {
  return [
    { id: "action", label: "Action", type: "text", placeholder: "e.g. plan.created", description: "The recorded action name" },
    { id: "actor", label: "Actor", type: "text", placeholder: "Name or email" },
    { id: "actorKind", label: "Actor type", type: "select", options: [{ value: "people", label: "People" }, { value: "automation", label: "Automation" }], operators: binaryOperators },
    { id: "status", label: "Result", type: "select", options: [{ value: "succeeded", label: "Succeeded" }, { value: "failed", label: "Failed" }, { value: "queued", label: "Queued" }, { value: "cancelled", label: "Cancelled" }], operators: choice },
    { id: "resourceType", label: "Resource type", type: "select", options: resourceTypes.map((value) => ({ value, label: words(value) })), operators: choice },
    { id: "resourceName", label: "Resource name", type: "text" },
    { id: "resourceId", label: "Resource ID", type: "text" },
    ...(workspaces.length ? [{ id: "workspace", label: "Workspace", type: "select" as const, options: workspaces.map((item) => ({ value: item.id, label: item.name })), operators: choice }] : []),
    { id: "cluster", label: "Cluster", type: "text", description: "Matches any cluster the event targeted" },
    { id: "namespace", label: "Namespace", type: "text", description: "Matches any namespace the event targeted" },
    dateTimeField("createdAt", "When", "Date and time of the event"),
  ]
}

