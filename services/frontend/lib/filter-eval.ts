import type { FilterNode, FilterQuery, FilterRule } from "@/components/reui/filters/filters-types"
import { parseLocalDateTime } from "./filter-datetime.ts"

/**
 * Reads what a record holds for a field: every value it has (several for namespaces or labels),
 * none for an empty field, or `undefined` when the field is not one of this list's filters.
 */
export type FieldReader = (field: string) => string[] | undefined

const inverse: Record<string, string> = { not_contains: "contains", is_not: "is", is_none_of: "is_any_of", empty: "not_empty" }

function operandsOf(rule: FilterRule): string[] {
  const raw = rule.value
  const list = Array.isArray(raw) ? raw : raw === undefined || raw === null ? [] : [raw]
  return list.map((item) => String(item)).filter((item) => item.trim() !== "")
}

function matchesText(value: string, operator: string, operands: string[]): boolean {
  const text = value.toLowerCase()
  const wanted = operands.map((item) => item.toLowerCase())
  switch (operator) {
    case "contains": return text.includes(wanted[0])
    case "starts_with": return text.startsWith(wanted[0])
    case "ends_with": return text.endsWith(wanted[0])
    case "is": return text === wanted[0]
    case "is_any_of": return wanted.includes(text)
    default: return false
  }
}

/** `null` means the rule says nothing yet (no operator, no value, unknown field) and is skipped. */
function ruleResult(rule: FilterRule, read: FieldReader): boolean | null {
  if (!rule.operator) return null
  const values = read(rule.path[0])
  if (!values) return null
  const present = values.filter((value) => value.trim() !== "")
  let result: boolean
  if (rule.operator === "empty" || rule.operator === "not_empty") {
    result = rule.operator === "empty" ? present.length === 0 : present.length > 0
  } else {
    const operands = operandsOf(rule)
    if (!operands.length) return null
    if (rule.operator === "is_before" || rule.operator === "is_after") {
      const boundary = parseLocalDateTime(operands[0])
      if (!boundary) return null
      const times = present.map((value) => Date.parse(value)).filter((time) => !Number.isNaN(time))
      // "is after" is inclusive, matching the audit filter's "is at or after".
      result = times.some((time) => rule.operator === "is_before" ? time < boundary.getTime() : time >= boundary.getTime())
    } else if (inverse[rule.operator]) {
      // Negative operators mean no value matches, the only sensible reading for multi-valued fields.
      result = !present.some((value) => matchesText(value, inverse[rule.operator], operands))
    } else {
      result = present.some((value) => matchesText(value, rule.operator, operands))
    }
  }
  return rule.negated ? !result : result
}

function nodeResult(node: FilterNode, read: FieldReader): boolean | null {
  if (node.type === "rule") return ruleResult(node, read)
  const results = node.rules.map((child) => nodeResult(child, read)).filter((item): item is boolean => item !== null)
  if (!results.length) return null
  return node.combinator === "or" ? results.some(Boolean) : results.every(Boolean)
}

/** Whether a record satisfies the filter. Unfinished conditions are ignored, so an empty filter matches everything. */
export function matchesFilterQuery(query: FilterQuery, read: FieldReader): boolean {
  return nodeResult(query, read) ?? true
}

/** How many complete conditions on the given fields the filter holds, at any depth. */
export function countActiveConditions(query: FilterQuery, fields: readonly string[]): number {
  const known = new Set(fields)
  const count = (node: FilterNode): number => {
    if (node.type === "group") return node.rules.reduce((total, child) => total + count(child), 0)
    const complete = node.operator !== "" && known.has(node.path[0]) && (node.operator === "empty" || node.operator === "not_empty" || operandsOf(node).length > 0)
    return complete ? 1 : 0
  }
  return count(query)
}
