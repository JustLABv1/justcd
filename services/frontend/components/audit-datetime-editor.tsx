"use client"

import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import { Input } from "@/components/ui/input"
import type { FilterEditorProps } from "@/components/reui/filters/filters-types"
import { dateTimePresets, describeDateTime, formatLocalDateTime, parseLocalDateTime, timePart, withDay, withTime } from "@/lib/audit-datetime"

/**
 * Value editor for a date-time filter condition: pick a day, set a time, or start from a preset.
 * The draft is a local `yyyy-MM-ddTHH:mm` string; the filter serializer turns it into an instant.
 */
export function AuditDateTimeEditor({ value, onValueChange, commit, cancel, autoFocusProps }: FilterEditorProps<string>) {
  const selected = parseLocalDateTime(value)
  const complete = selected !== null
  return <div className="w-72 space-y-3 p-3" onKeyDown={(event) => { if (event.key === "Enter" && complete && event.target instanceof HTMLInputElement) { event.preventDefault(); commit(value) } }}>
    <div role="group" aria-label="Quick picks" className="flex flex-wrap gap-1.5">
      {dateTimePresets.map((preset) => <Button key={preset.id} type="button" size="xs" variant="outline" onClick={() => onValueChange(formatLocalDateTime(preset.at(new Date())))}>{preset.label}</Button>)}
    </div>
    <Calendar mode="single" selected={selected ?? undefined} defaultMonth={selected ?? undefined} onSelect={(day) => { if (day) onValueChange(withDay(value, day)) }} className="mx-auto p-0 [--cell-size:--spacing(9)]" />
    <div className="flex items-center gap-2">
      <label htmlFor="audit-filter-time" className="text-sm text-muted-foreground">Time</label>
      <Input id="audit-filter-time" type="time" step={60} value={timePart(value)} onChange={(event) => onValueChange(withTime(value, event.target.value))} className="w-32" {...autoFocusProps} />
      {complete && <span className="min-w-0 flex-1 truncate text-right text-xs text-muted-foreground" title={describeDateTime(value)}>{describeDateTime(value)}</span>}
    </div>
    <div className="flex justify-end gap-2 border-t pt-3">
      <Button type="button" size="sm" variant="ghost" onClick={cancel}>Cancel</Button>
      <Button type="button" size="sm" disabled={!complete} onClick={() => commit(value)}>Apply</Button>
    </div>
  </div>
}
