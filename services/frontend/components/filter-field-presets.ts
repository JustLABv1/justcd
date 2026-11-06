import { FilterDateTimeEditor } from "@/components/filter-datetime-editor"
import type { FilterField, FilterOperator } from "@/components/reui/filters/filters-types"
import { describeDateTime, parseLocalDateTime } from "@/lib/filter-datetime"

const is: FilterOperator = { value: "is", label: "is", arity: "one", inverse: "is_not" }
const isNot: FilterOperator = { value: "is_not", label: "is not", arity: "one", inverse: "is" }
const isAnyOf: FilterOperator = { value: "is_any_of", label: "is any of", arity: "many", inverse: "is_none_of" }
const isNoneOf: FilterOperator = { value: "is_none_of", label: "is none of", arity: "many", inverse: "is_any_of" }

/** Operators for a field with a fixed set of values. */
export const choiceOperators: FilterOperator[] = [is, isNot, isAnyOf, isNoneOf]
/** Operators for a two-way field such as paused/active. */
export const binaryOperators: FilterOperator[] = [is, isNot]

/** A "when" field: operators are is at or after / is before, and values come from the date-time picker. */
export function dateTimeField(id: string, label: string, description: string): FilterField {
  return {
    id, label, type: "text", description,
    operators: [{ value: "is_after", label: "is at or after", arity: "one", inverse: "is_before" }, { value: "is_before", label: "is before", arity: "one", inverse: "is_after" }],
    editor: FilterDateTimeEditor,
    renderValue: ({ value }) => describeDateTime(value),
    valueText: ({ value }) => describeDateTime(value),
    validate: ({ value }) => (value === undefined || value === "" || parseLocalDateTime(value) ? null : "Pick a date and time"),
  }
}
