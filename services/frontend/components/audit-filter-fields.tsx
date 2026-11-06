import type { FilterField, FilterOperator } from "@/components/reui/filters/filters-types"
import { AuditDateTimeEditor } from "@/components/audit-datetime-editor"
import { describeDateTime, parseLocalDateTime } from "@/lib/audit-datetime"
import { words } from "@/lib/audit"

const is: FilterOperator = { value: "is", label: "is", arity: "one", inverse: "is_not" }
const isNot: FilterOperator = { value: "is_not", label: "is not", arity: "one", inverse: "is" }
const isAnyOf: FilterOperator = { value: "is_any_of", label: "is any of", arity: "many", inverse: "is_none_of" }
const isNoneOf: FilterOperator = { value: "is_none_of", label: "is none of", arity: "many", inverse: "is_any_of" }
const choice = [is, isNot, isAnyOf, isNoneOf]

const resourceTypes = ["application", "application_group", "cluster", "credential", "git_source", "repository_configuration", "publishing_profile", "oidc_provider", "user", "workspace", "instance"]

/** The fields a user can build audit filters from. The backend accepts exactly these ids. */
export function auditFilterFields(workspaces: { id: string; name: string }[]): FilterField[] {
  return [
    { id: "action", label: "Action", type: "text", placeholder: "e.g. plan.created", description: "The recorded action name" },
    { id: "actor", label: "Actor", type: "text", placeholder: "Name or email" },
    { id: "actorKind", label: "Actor type", type: "select", options: [{ value: "people", label: "People" }, { value: "automation", label: "Automation" }], operators: [is, isNot] },
    { id: "status", label: "Result", type: "select", options: [{ value: "succeeded", label: "Succeeded" }, { value: "failed", label: "Failed" }, { value: "queued", label: "Queued" }, { value: "cancelled", label: "Cancelled" }], operators: choice },
    { id: "resourceType", label: "Resource type", type: "select", options: resourceTypes.map((value) => ({ value, label: words(value) })), operators: choice },
    { id: "resourceName", label: "Resource name", type: "text" },
    { id: "resourceId", label: "Resource ID", type: "text" },
    ...(workspaces.length ? [{ id: "workspace", label: "Workspace", type: "select" as const, options: workspaces.map((item) => ({ value: item.id, label: item.name })), operators: choice }] : []),
    { id: "cluster", label: "Cluster", type: "text", description: "Matches any cluster the event targeted" },
    { id: "namespace", label: "Namespace", type: "text", description: "Matches any namespace the event targeted" },
    {
      id: "createdAt", label: "When", type: "text", description: "Date and time of the event",
      operators: [{ value: "is_after", label: "is at or after", arity: "one", inverse: "is_before" }, { value: "is_before", label: "is before", arity: "one", inverse: "is_after" }],
      editor: AuditDateTimeEditor,
      renderValue: ({ value }) => describeDateTime(value),
      valueText: ({ value }) => describeDateTime(value),
      validate: ({ value }) => (value === undefined || value === "" || parseLocalDateTime(value) ? null : "Pick a date and time"),
    },
  ]
}

