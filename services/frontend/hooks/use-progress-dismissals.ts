"use client"

import { useSyncExternalStore } from "react"

const key = "justcd:dismissed-progress"
const event = "justcd:progress-dismissed"
let fallback = "[]"
function snapshot() {
  try { return localStorage.getItem(key) ?? fallback } catch { return fallback }
}
function subscribe(callback: () => void) {
  window.addEventListener(event, callback)
  window.addEventListener("storage", callback)
  return () => { window.removeEventListener(event, callback); window.removeEventListener("storage", callback) }
}
function parse(value: string): string[] {
  try { const result: unknown = JSON.parse(value); return Array.isArray(result) ? result.filter((id): id is string => typeof id === "string") : [] } catch { return [] }
}
export function useProgressDismissals() {
  const value = useSyncExternalStore(subscribe, snapshot, () => "[]")
  return {
    isDismissed: (id: string) => parse(value).includes(id),
    dismiss: (id: string) => {
      fallback = JSON.stringify([...new Set([...parse(snapshot()), id])].slice(-100))
      try { localStorage.setItem(key, fallback) } catch { /* Storage may be unavailable. */ }
      window.dispatchEvent(new Event(event))
    },
  }
}
