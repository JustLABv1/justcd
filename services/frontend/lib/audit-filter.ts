import type { FilterNode, FilterQuery } from "@/components/reui/filters/filters-types"

import { parseLocalDateTime } from "./audit-datetime.ts"

type WireNode = { type: "group"; combinator: "and" | "or"; rules: WireNode[] } | { type: "rule"; field: string; operator: string; value?: unknown; negated?: boolean }

const noValue = new Set(["empty", "not_empty"])

function wire(node: FilterNode): WireNode | null {
  if (node.type === "group") {
    const rules = node.rules.map(wire).filter((item): item is WireNode => item !== null)
    return rules.length ? { type: "group", combinator: node.combinator, rules } : null
  }
  if (node.type !== "rule" || !node.operator) return null
  const field = node.path[0]
  const negated = node.negated ? { negated: true } : {}
  if (field === "createdAt") {
    const moment = parseLocalDateTime(node.value)
    if (!moment) return null
    return { type: "rule", field, operator: node.operator === "is_before" ? "before" : "from", value: moment.toISOString(), ...negated }
  }
  if (noValue.has(node.operator)) return { type: "rule", field, operator: node.operator, ...negated }
  const value = node.value
  if (Array.isArray(value) ? value.length === 0 : value === undefined || value === null || value === "") return null
  return { type: "rule", field, operator: node.operator, value, ...negated }
}

/** The filter as the audit API expects it, or "" when no condition is complete yet. */
export function serializeAuditFilter(query: FilterQuery): string {
  const tree = wire(query)
  return tree ? JSON.stringify(tree) : ""
}
