export function ago(value?: string, now = Date.now()) {
  const time = value ? Date.parse(value) : NaN
  if (!Number.isFinite(time)) return "never"
  const seconds = Math.max(0, Math.round((now - time) / 1000))
  if (seconds < 60) return "just now"
  if (seconds < 3600) return `${Math.floor(seconds / 60)} min ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} h ago`
  return `${Math.floor(seconds / 86400)} d ago`
}
