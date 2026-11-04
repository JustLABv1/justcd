"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { api } from "@/lib/api"
import { draftKey, pickDraft, readDraft } from "@/lib/publishing-draft"

export function usePublishingDraft<T extends object>(
  scope: string | null,
  value: T,
  restore: (data: T, signal: AbortSignal) => void | Promise<void>
) {
  const [userId, setUserId] = useState("")
  const [loadedKey, setLoadedKey] = useState<string | null>(null)
  const [savedAt, setSavedAt] = useState<number | null>(null)
  const [restored, setRestored] = useState(false)
  const [error, setError] = useState("")
  const latest = useRef({ value, restore })
  const stopped = useRef(false)
  useEffect(() => {
    latest.current = { value, restore }
  }, [value, restore])
  useEffect(() => {
    let active = true
    api<{ user: { id: string } }>("/api/v1/auth/session")
      .then(({ user }) => {
        if (active) setUserId(user.id)
      })
      .catch(() => {
        if (active)
          setError(
            "Draft autosave is unavailable until your session reconnects."
          )
      })
    return () => {
      active = false
    }
  }, [])
  const key = userId && scope ? draftKey(userId, scope) : null
  useEffect(() => {
    if (!key) return
    let active = true
    const controller = new AbortController()
    stopped.current = false
    Promise.resolve().then(async () => {
      try {
        const draft = readDraft(localStorage.getItem(key), latest.current.value)
        if (draft && active)
          await latest.current.restore(draft.data, controller.signal)
        if (active) {
          setRestored(!!draft)
          setSavedAt(draft?.updatedAt ?? null)
          setError("")
          setLoadedKey(key)
        }
      } catch {
        if (active) {
          stopped.current = true
          setError(
            "The draft could not be restored. Reload to retry, or discard it to start again."
          )
          setLoadedKey(key)
        }
      }
    })
    return () => {
      active = false
      controller.abort()
    }
  }, [key])
  const serialized = JSON.stringify(value)
  useEffect(() => {
    if (!key || loadedKey !== key || stopped.current) return
    let active = true
    Promise.resolve().then(() => {
      if (!active || stopped.current) return
      try {
        const updatedAt = Date.now()
        localStorage.setItem(
          key,
          JSON.stringify({
            version: 1,
            updatedAt,
            data: pickDraft(latest.current.value, JSON.parse(serialized)),
          })
        )
        setSavedAt(updatedAt)
        setError("")
      } catch {
        setError(
          "Draft autosave is unavailable in this browser. Keep this page open until publishing finishes."
        )
      }
    })
    return () => {
      active = false
    }
  }, [key, loadedKey, serialized])
  useEffect(() => {
    if (!key || loadedKey !== key) return
    const flush = () => {
      if (stopped.current) return
      try {
        localStorage.setItem(
          key,
          JSON.stringify({
            version: 1,
            updatedAt: Date.now(),
            data: latest.current.value,
          })
        )
      } catch {
        /* The visible save effect reports storage failures. */
      }
    }
    const hide = () => {
      if (document.visibilityState === "hidden") flush()
    }
    window.addEventListener("pagehide", flush)
    document.addEventListener("visibilitychange", hide)
    return () => {
      window.removeEventListener("pagehide", flush)
      document.removeEventListener("visibilitychange", hide)
    }
  }, [key, loadedKey])
  const clear = useCallback(() => {
    stopped.current = true
    if (key) {
      try {
        localStorage.removeItem(key)
      } catch {
        setError("The draft could not be removed from browser storage.")
        return false
      }
    }
    setSavedAt(null)
    setRestored(false)
    return true
  }, [key])
  return { ready: !!key && loadedKey === key, savedAt, restored, error, clear }
}
