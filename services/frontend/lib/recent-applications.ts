/**
 * The applications this viewer opened last, newest first. A per-browser convenience for the
 * command palette: kept in localStorage, and simply empty where storage is unavailable.
 */
const key = "justcd.recentApplications"
const limit = 5

export function readRecentApplications(): string[] {
  try {
    const value: unknown = JSON.parse(window.localStorage.getItem(key) ?? "[]")
    return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string").slice(0, limit) : []
  } catch {
    return []
  }
}

export function recordRecentApplication(id: string) {
  try {
    window.localStorage.setItem(key, JSON.stringify([id, ...readRecentApplications().filter((item) => item !== id)].slice(0, limit)))
  } catch { /* storage unavailable */ }
}
