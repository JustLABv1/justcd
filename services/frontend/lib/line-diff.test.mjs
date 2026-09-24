import assert from "node:assert/strict"
import test from "node:test"
import { diffJsonLines } from "./line-diff.ts"

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
