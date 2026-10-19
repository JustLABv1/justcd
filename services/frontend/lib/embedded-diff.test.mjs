import assert from "node:assert/strict"
import test from "node:test"
import {
  embeddedFiles,
  otherManifestChanges,
  changedTextRanges,
} from "./embedded-diff.ts"
import { reviewTextRows, contextRows } from "./line-diff.ts"

const config = (data, extra = {}) => ({ kind: "ConfigMap", data, ...extra })

test("embedded file diff isolates an INI edit and collapses unchanged configuration", () => {
  const before =
    Array.from({ length: 80 }, (_, i) => `setting_${i} = old`).join("\n") + "\n"
  const after = before.replace("setting_40 = old", "setting_40 = new")
  const [file] = embeddedFiles(
    config({ "grafana.ini": before }),
    config({ "grafana.ini": after })
  )
  assert.equal(file.language, "ini")
  assert.equal(file.status, "changed")
  const rows = reviewTextRows(file.before, file.after)
  assert.equal(rows.filter((row) => row.changed).length, 1)
  assert.equal(contextRows(rows).size, 7)
  assert.deepEqual(
    rows.filter((row) => row.oldLine).map((row) => row.before),
    before.split("\n")
  )
  assert.equal(
    otherManifestChanges(
      config({ "grafana.ini": before }),
      config({ "grafana.ini": after }),
      [file]
    ),
    false
  )
})

test("files retain added, removed, empty and unchanged content without decoding secrets", () => {
  const files = embeddedFiles(
    config({ "removed.conf": "", "same.json": "{}" }),
    config({
      "added.yaml": "",
      "same.json": "{}",
      plain: "value",
      multiline: "a\nb",
    })
  )
  assert.equal(files.find((f) => f.key === "removed.conf").status, "removed")
  assert.equal(files.find((f) => f.key === "added.yaml").status, "added")
  assert.equal(files.at(-1).status, "unchanged")
  assert.equal(
    files.some((f) => f.key === "plain"),
    false
  )
  assert.equal(files.find((f) => f.key === "multiline").language, "text")
  assert.deepEqual(reviewTextRows(undefined, ""), [
    {
      before: undefined,
      after: "",
      oldLine: undefined,
      newLine: 1,
      changed: true,
    },
  ])
  assert.deepEqual(
    embeddedFiles({ kind: "Secret", data: { "config.ini": "a\nb" } }, null),
    []
  )
})

test("other fields and redactions remain separate from the decoded file view", () => {
  const before = config({ "grafana.ini": "[smtp]\nenabled=false", flag: "old" })
  const after = config(
    { "grafana.ini": "[smtp]\nenabled=true", flag: "new" },
    { metadata: { labels: { team: "new" } } }
  )
  assert.equal(
    otherManifestChanges(before, after, embeddedFiles(before, after)),
    true
  )
})

test("valid ConfigMap keys cannot resolve inherited object properties", () => {
  const [file] = embeddedFiles(
    config({}),
    config({ constructor: "line one\nline two" })
  )
  assert.equal(file.key, "constructor")
  assert.equal(file.status, "added")
  assert.equal(file.language, "text")
})

test("decoded content preserves literal escapes, commas, CRLF and final newlines", () => {
  for (const [before, after] of [
    ["a,", "a"],
    ["a\\nb\n", "a\nb\n"],
    ["a\r\nb", "a\nb"],
    ["a\n", "a"],
  ]) {
    const rows = reviewTextRows(before, after)
    assert.ok(rows.some((row) => row.changed))
    assert.deepEqual(
      rows.filter((row) => row.oldLine).map((row) => row.before),
      before.split("\n")
    )
    assert.deepEqual(
      rows.filter((row) => row.newLine).map((row) => row.after),
      after.split("\n")
    )
  }
})

test("word highlights isolate separated edits and preserve common punctuation", () => {
  const before = "host = old.example.com port = 2525",
    after = "host = new.example.com port = 587"
  const ranges = changedTextRanges(before, after)
  assert.deepEqual(
    ranges.before.map((r) => before.slice(r.start, r.end)),
    ["old", "2525"]
  )
  assert.deepEqual(
    ranges.after.map((r) => after.slice(r.start, r.end)),
    ["new", "587"]
  )
  assert.deepEqual(changedTextRanges("a", "a"), { before: [], after: [] })
  assert.deepEqual(changedTextRanges("", "a").after, [{ start: 0, end: 1 }])
  const huge = "x".repeat(30000)
  assert.deepEqual(changedTextRanges(huge + "old", huge + "new").before, [
    { start: 30000, end: 30003 },
  ])
})
