import assert from "node:assert/strict"
import test from "node:test"
import { dateTimePresets, formatLocalDateTime, parseLocalDateTime, timePart, withDay, withTime } from "./filter-datetime.ts"

test("parses local date-times and rejects impossible ones", () => {
  const parsed = parseLocalDateTime("2026-11-05T14:30")
  assert.equal(parsed.getFullYear(), 2026)
  assert.equal(parsed.getMonth(), 10)
  assert.equal(parsed.getHours(), 14)
  assert.equal(parsed.getMinutes(), 30)
  assert.equal(parseLocalDateTime("2026-11-05").getHours(), 0)
  for (const bad of ["2026-02-31T10:00", "2026-11-05T25:00", "2026-11-05T10:61", "yesterday", "", null, 5]) assert.equal(parseLocalDateTime(bad), null, String(bad))
})

test("round-trips through the wire format", () => {
  const value = "2026-03-08T09:05"
  assert.equal(formatLocalDateTime(parseLocalDateTime(value)), value)
})

test("changing the day keeps the time, changing the time keeps the day", () => {
  assert.equal(withDay("2026-11-05T14:30", new Date(2026, 10, 9)), "2026-11-09T14:30")
  assert.equal(withDay(undefined, new Date(2026, 10, 9)), "2026-11-09T00:00")
  assert.equal(withTime("2026-11-05T14:30", "08:15"), "2026-11-05T08:15")
  assert.equal(withTime("2026-11-05T14:30", "not a time"), "2026-11-05T00:00")
  assert.equal(timePart("2026-11-05T14:30"), "14:30")
  assert.equal(timePart("2026-11-05"), "")
})

test("presets resolve relative to now", () => {
  const now = new Date(2026, 10, 6, 16, 45)
  const at = Object.fromEntries(dateTimePresets.map((preset) => [preset.id, formatLocalDateTime(preset.at(now))]))
  assert.equal(at.hour, "2026-11-06T15:45")
  assert.equal(at.today, "2026-11-06T00:00")
  assert.equal(at.week, "2026-10-30T16:45")
})
