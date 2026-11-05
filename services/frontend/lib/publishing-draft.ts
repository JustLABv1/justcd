// Persist only the explicit non-secret shape supplied by each publishing form.
// Never serialize form state wholesale: credentials, Files, review results, and
// acknowledgement flags must stay in memory.
export const DRAFT_TTL = 30 * 24 * 60 * 60 * 1000
export function draftKey(user: string, scope: string) {
  return `justcd:publishing-draft:v1:${encodeURIComponent(user)}:${encodeURIComponent(scope)}`
}
export function pickDraft<T>(shape: T, value: unknown): T {
  if (typeof shape === "string")
    return (
      typeof value === "string" && value.length <= 16384 ? value : shape
    ) as T
  if (typeof shape === "boolean")
    return (typeof value === "boolean" ? value : shape) as T
  if (typeof shape === "number")
    return (
      typeof value === "number" && Number.isFinite(value) ? value : shape
    ) as T
  if (shape && typeof shape === "object" && !Array.isArray(shape)) {
    const record =
      value && typeof value === "object" && !Array.isArray(value)
        ? (value as Record<string, unknown>)
        : {}
    return Object.fromEntries(
      Object.entries(shape).map(([key, fallback]) => [
        key,
        pickDraft(fallback, record[key]),
      ])
    ) as T
  }
  return shape
}
export function readDraft<T>(
  raw: string | null,
  shape: T,
  now = Date.now(),
  isMeaningful: (data: T) => boolean = () => true
): { data: T; updatedAt: number } | null {
  if (!raw) return null
  const envelope = JSON.parse(raw)
  if (
    envelope?.version !== 1 ||
    typeof envelope.updatedAt !== "number" ||
    now - envelope.updatedAt > DRAFT_TTL ||
    envelope.updatedAt > now + 60000
  )
    return null
  const data = pickDraft(shape, envelope.data)
  if (!isMeaningful(data)) return null
  return {
    data,
    updatedAt: envelope.updatedAt,
  }
}
