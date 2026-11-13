export function ago(value?: string, now = Date.now()) {
  const time = value ? Date.parse(value) : NaN
  if (!Number.isFinite(time)) return "never"
  const seconds = Math.max(0, Math.round((now - time) / 1000))
  if (seconds < 60) return "just now"
  if (seconds < 3600) return `${Math.floor(seconds / 60)} min ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} h ago`
  return `${Math.floor(seconds / 86400)} d ago`
}

/** How long a finished run took, as a short phrase ("42s", "3m 10s", "1h 5m"). Null when the run has no known finish. */
export function runDuration(startedAt?: string, finishedAt?: string) {
  const start = startedAt ? Date.parse(startedAt) : NaN
  const end = finishedAt ? Date.parse(finishedAt) : NaN
  if (!Number.isFinite(start) || !Number.isFinite(end)) return null
  const seconds = Math.max(0, Math.round((end - start) / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
}
