/** Filter values are local wall-clock times written as `yyyy-MM-ddTHH:mm`. */
const LOCAL = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2}))?$/

const pad = (value: number) => String(value).padStart(2, "0")

/** Reads a local date-time. A bare date means the start of that day. */
export function parseLocalDateTime(value: unknown): Date | null {
  const match = typeof value === "string" ? LOCAL.exec(value.trim()) : null
  if (!match) return null
  const [year, month, day, hour = "0", minute = "0"] = match.slice(1)
  const date = new Date(Number(year), Number(month) - 1, Number(day), Number(hour), Number(minute))
  const valid = date.getFullYear() === Number(year) && date.getMonth() === Number(month) - 1 && date.getDate() === Number(day) && date.getHours() === Number(hour) && date.getMinutes() === Number(minute)
  return valid ? date : null
}

export function formatLocalDateTime(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

/** The time of day part, `HH:mm`, or "" when the value has none. */
export function timePart(value: unknown): string {
  const match = typeof value === "string" ? LOCAL.exec(value.trim()) : null
  return match?.[4] !== undefined ? `${match[4]}:${match[5]}` : ""
}

/** Moves a value to another day or another time of day, keeping the other part. */
export function withDay(value: unknown, day: Date): string {
  const time = timePart(value) || "00:00"
  return `${day.getFullYear()}-${pad(day.getMonth() + 1)}-${pad(day.getDate())}T${time}`
}

export function withTime(value: unknown, time: string, fallbackDay: Date = new Date()): string {
  const base = parseLocalDateTime(value) ?? fallbackDay
  return `${formatLocalDateTime(base).slice(0, 10)}T${/^([01]\d|2[0-3]):[0-5]\d$/.test(time) ? time : "00:00"}`
}

export function describeDateTime(value: unknown): string {
  const date = parseLocalDateTime(value)
  if (!date) return ""
  return date.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })
}

export type DateTimePreset = { id: string; label: string; at: (now: Date) => Date }

/** Starting points for "is after"; each resolves to a fixed moment when picked. */
export const dateTimePresets: DateTimePreset[] = [
  { id: "hour", label: "1 hour ago", at: (now) => new Date(now.getTime() - 60 * 60 * 1000) },
  { id: "day", label: "24 hours ago", at: (now) => new Date(now.getTime() - 24 * 60 * 60 * 1000) },
  { id: "today", label: "Start of today", at: (now) => new Date(now.getFullYear(), now.getMonth(), now.getDate()) },
  { id: "week", label: "7 days ago", at: (now) => new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000) },
]
