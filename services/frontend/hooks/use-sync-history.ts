"use client"

import { useEffect, useState } from "react"
import { api } from "@/lib/api"
import type { SyncHistory } from "@/lib/sync-history"

export function useSyncHistory(
  scope: "workspaces" | "applications",
  scopeID: string,
  revision = ""
) {
  const [range, setRange] = useState<"24h" | "7d">("24h")
  const [refresh, setRefresh] = useState(0)
  const requestKey = `${scope}:${scopeID}:${range}:${refresh}:${revision ?? ""}`
  const [response, setResult] = useState<{
    key: string
    history?: SyncHistory
    error?: unknown
    loading: boolean
  }>({ key: "", loading: true })
  const result =
    response.key === requestKey
      ? response
      : { loading: true, history: undefined, error: undefined }
  useEffect(() => {
    if (!scopeID) return
    const controller = new AbortController()
    let loading = false
    async function load() {
      if (loading) return
      loading = true
      try {
        const history = await api<SyncHistory>(
          `/api/v1/${scope}/${encodeURIComponent(scopeID)}/sync-history?range=${range}`,
          { signal: controller.signal }
        )
        if (!controller.signal.aborted)
          setResult({ key: requestKey, history, loading: false })
      } catch (error) {
        if (!controller.signal.aborted)
          setResult({ key: requestKey, error, loading: false })
      } finally {
        loading = false
      }
    }
    void load()
    const timer = window.setInterval(() => void load(), 60_000)
    return () => {
      controller.abort()
      window.clearInterval(timer)
    }
  }, [scope, scopeID, range, requestKey])
  return {
    result,
    range,
    setRange,
    retry: () => setRefresh((value) => value + 1),
  }
}

export type SyncHistoryState = ReturnType<typeof useSyncHistory>
