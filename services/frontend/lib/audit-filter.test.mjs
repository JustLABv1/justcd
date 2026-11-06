import assert from "node:assert/strict"
import test from "node:test"
import { serializeAuditFilter } from "./audit-filter.ts"

const rule = (path, operator, value, extra = {}) => ({ id: path + operator, type: "rule", path: [path], operator, value, ...extra })
const group = (combinator, rules) => ({ id: combinator, type: "group", combinator, rules })

test("serialises nested AND/OR groups and skips incomplete conditions", () => {
  const query = group("and", [
    rule("action", "contains", "plan"),
    group("or", [rule("actor", "is", "justin"), rule("status", "is_any_of", ["failed", "queued"]), rule("namespace", "")]),
    rule("resourceName", "contains", ""),
    rule("cluster", "not_empty", undefined),
  ])
  assert.deepEqual(JSON.parse(serializeAuditFilter(query)), {
    type: "group", combinator: "and", rules: [
      { type: "rule", field: "action", operator: "contains", value: "plan" },
      { type: "group", combinator: "or", rules: [{ type: "rule", field: "actor", operator: "is", value: "justin" }, { type: "rule", field: "status", operator: "is_any_of", value: ["failed", "queued"] }] },
      { type: "rule", field: "cluster", operator: "not_empty" },
    ],
  })
})

test("an empty or unfinished query sends no filter", () => {
  assert.equal(serializeAuditFilter(group("and", [])), "")
  assert.equal(serializeAuditFilter(group("and", [rule("action", "")])), "")
  assert.equal(serializeAuditFilter(group("or", [group("and", [rule("actor", "is", "")])])), "")
})

test("date-times are sent as exact instants: before is exclusive, after is inclusive", () => {
  const before = JSON.parse(serializeAuditFilter(group("and", [rule("createdAt", "is_before", "2026-11-05T14:30")])))
  const after = JSON.parse(serializeAuditFilter(group("and", [rule("createdAt", "is_after", "2026-11-05T14:30")])))
  assert.equal(before.rules[0].operator, "before")
  assert.equal(before.rules[0].value, new Date(2026, 10, 5, 14, 30).toISOString())
  assert.equal(after.rules[0].operator, "from")
  assert.equal(after.rules[0].value, new Date(2026, 10, 5, 14, 30).toISOString())
  const dayOnly = JSON.parse(serializeAuditFilter(group("and", [rule("createdAt", "is_after", "2026-11-05")])))
  assert.equal(dayOnly.rules[0].value, new Date(2026, 10, 5).toISOString())
  assert.equal(serializeAuditFilter(group("and", [rule("createdAt", "is_before", "05/11/2026")])), "")
})

test("carries the negate flag", () => {
  const parsed = JSON.parse(serializeAuditFilter(group("and", [rule("action", "contains", "plan", { negated: true })])))
  assert.equal(parsed.rules[0].negated, true)
})
