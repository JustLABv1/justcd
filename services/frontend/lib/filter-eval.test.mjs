import assert from "node:assert/strict"
import test from "node:test"
import { countActiveConditions, matchesFilterQuery } from "./filter-eval.ts"

const rule = (field, operator, value, extra = {}) => ({ id: field + operator, type: "rule", path: [field], operator, value, ...extra })
const group = (combinator, rules) => ({ id: combinator, type: "group", combinator, rules })
const app = (extra = {}) => ({ name: ["billing-api"], cluster: ["prod-eu"], namespace: ["billing", "billing-canary"], label: ["team=pay", "tier=1"], renderer: ["helm"], checked: ["2026-11-06T10:00:00.000Z"], ...extra })
const read = (record) => (field) => record[field]
const matches = (query, record = app()) => matchesFilterQuery(query, read(record))

test("text operators are case-insensitive", () => {
  assert.ok(matches(group("and", [rule("name", "contains", "BILL")])))
  assert.ok(matches(group("and", [rule("name", "starts_with", "billing")])))
  assert.ok(matches(group("and", [rule("name", "ends_with", "-API")])))
  assert.ok(matches(group("and", [rule("name", "is", "Billing-API")])))
  assert.ok(!matches(group("and", [rule("name", "is", "billing")])))
  assert.ok(matches(group("and", [rule("name", "not_contains", "payments")])))
  assert.ok(!matches(group("and", [rule("name", "not_contains", "bill")])))
})

test("choice operators", () => {
  assert.ok(matches(group("and", [rule("renderer", "is_any_of", ["yaml", "helm"])])))
  assert.ok(!matches(group("and", [rule("renderer", "is_none_of", ["yaml", "helm"])])))
  assert.ok(matches(group("and", [rule("renderer", "is_not", "yaml")])))
})

test("multi-valued fields: positive operators need one match, negative ones need none", () => {
  assert.ok(matches(group("and", [rule("namespace", "is", "billing-canary")])))
  assert.ok(matches(group("and", [rule("label", "contains", "team=")])))
  assert.ok(!matches(group("and", [rule("namespace", "is_not", "billing")])), "one namespace is billing, so 'is not' must fail")
  assert.ok(matches(group("and", [rule("namespace", "not_contains", "staging")])))
})

test("empty and not empty", () => {
  assert.ok(matches(group("and", [rule("label", "empty")]), app({ label: [] })))
  assert.ok(!matches(group("and", [rule("label", "empty")])))
  assert.ok(matches(group("and", [rule("label", "not_empty")])))
  assert.ok(matches(group("and", [rule("cluster", "empty")]), app({ cluster: [""] })))
})

test("AND, OR and nesting", () => {
  const query = group("and", [rule("name", "contains", "billing"), group("or", [rule("cluster", "is", "nope"), rule("renderer", "is", "helm")])])
  assert.ok(matches(query))
  assert.ok(!matches(group("and", [rule("name", "contains", "billing"), group("or", [rule("cluster", "is", "nope"), rule("renderer", "is", "yaml")])])))
  assert.ok(matches(group("or", [rule("name", "is", "x"), rule("cluster", "is", "prod-eu")])))
})

test("the negate flag inverts a rule", () => {
  assert.ok(!matches(group("and", [rule("name", "contains", "bill", { negated: true })])))
  assert.ok(matches(group("and", [rule("name", "contains", "zzz", { negated: true })])))
})

test("date-times compare as instants: after is inclusive, before is exclusive", () => {
  const at = (iso) => new Date(iso).getTime()
  const local = (date) => `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}T${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`
  const boundary = local(new Date("2026-11-06T10:00:00.000Z"))
  assert.ok(matches(group("and", [rule("checked", "is_after", boundary)])))
  assert.ok(!matches(group("and", [rule("checked", "is_before", boundary)])))
  assert.ok(matches(group("and", [rule("checked", "is_before", local(new Date(at("2026-11-06T10:00:00.000Z") + 3600_000)))])))
  assert.ok(!matches(group("and", [rule("checked", "is_after", boundary)]), app({ checked: [] })), "a record without the date cannot be after anything")
})

test("unfinished or unknown conditions are ignored", () => {
  assert.ok(matches(group("and", [rule("name", ""), rule("name", "contains", ""), rule("mystery", "is", "x"), rule("name", "contains", "billing")])))
  assert.ok(matches(group("and", [])))
  assert.ok(!matches(group("or", [rule("name", "is", "nope"), rule("name", "contains", "")])), "an unfinished rule must not make an OR pass")
  assert.ok(matches(group("or", [rule("name", "is", "billing-api"), rule("name", "contains", "")])), "nor hide a real match")
})

test("counts only complete conditions on known fields", () => {
  const query = group("and", [rule("name", "contains", "x"), rule("name", "contains", ""), rule("label", "empty"), group("or", [rule("mystery", "is", "y"), rule("renderer", "is", "helm")])])
  assert.equal(countActiveConditions(query, ["name", "label", "renderer"]), 3)
})
