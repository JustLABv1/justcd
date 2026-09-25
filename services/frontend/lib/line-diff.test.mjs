import assert from "node:assert/strict"
import test from "node:test"
import { diffJsonLines, reviewRows, contextRows } from "./line-diff.ts"

test("review alignment retains all lines for inserts, removals and replacements", () => {
  for (const [before, after] of [[{ a: 1 }, { a: 1, b: 2 }], [{ a: 1, b: 2 }, { b: 3 }], [null, { kind: "Secret" }], [{ kind: "Pod" }, null]]) {
    const rows = reviewRows(before, after)
    assert.deepEqual(rows.filter(r => r.oldLine).map(r => r.before), JSON.stringify(before, null, 2).split("\n"))
    assert.deepEqual(rows.filter(r => r.newLine).map(r => r.after), JSON.stringify(after, null, 2).split("\n"))
    assert.ok(rows.some(r => r.changed))
  }
})

test("collapsed context retains every change and neighboring lines", () => {
  const before = Object.fromEntries(Array.from({ length: 40 }, (_, i) => [`field${i}`, i]))
  const rows = reviewRows(before, { ...before, field20: 99 })
  const visible = contextRows(rows)
  rows.forEach((row, index) => { if (row.changed) for (let i = index - 3; i <= index + 3; i++) assert.ok(visible.has(i)) })
  assert.ok(visible.size < rows.length)
})

test("equal manifests have no changed review rows", () => {
  const rows = reviewRows({ a: 1 }, { a: 1 })
  assert.equal(rows.some(r => r.changed), false)
  assert.equal(contextRows(rows).size, 0)
})

test("highlights only the changed JSON value on each side", () => {
  const diff = diffJsonLines({ spec: { replicas: 2, image: "web:v1" } }, { spec: { replicas: 3, image: "web:v1" } })
  assert.deepEqual([...diff.removed].map((index) => diff.before[index].trim()), ['"replicas": 2,'])
  assert.deepEqual([...diff.added].map((index) => diff.after[index].trim()), ['"replicas": 3,'])
})

test("an inserted property highlights only the desired side", () => {
  const diff = diffJsonLines({ data: { existing: "yes" } }, { data: { existing: "yes", feature: "on" } })
  assert.equal(diff.removed.size, 0)
  assert.deepEqual([...diff.added].map((index) => diff.after[index].trim()), ['"feature": "on"'])
})

test("identical manifests contain no highlighted lines", () => {
  const diff = diffJsonLines({ kind: "ConfigMap" }, { kind: "ConfigMap" })
  assert.equal(diff.removed.size, 0)
  assert.equal(diff.added.size, 0)
})
