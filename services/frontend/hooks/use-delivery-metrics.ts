"use client"

import { useEffect, useState } from "react"
import { api } from "@/lib/api"

export type DeliveryPeriod = { startedAt: string; endedAt: string; deployments: number; failed: number; restoreSeconds?: number; restored: number }
export type DeliveryMetrics = {
  current: DeliveryPeriod
  previous: DeliveryPeriod
  failuresByApplication: { applicationId: string; name: string; failed: number; total: number }[]
}

/** Delivery of a workspace or one application over the last 7 or 30 days, compared with the period before. Refreshes every five minutes. */
export function useDeliveryMetrics(scope: "workspaces" | "applications", id: string | undefined, range: "7d" | "30d", revision = "") {
  const key = `${scope}:${id}:${range}:${revision}`
  const [state, setState] = useState<{ key: string; metrics?: DeliveryMetrics; error?: unknown }>({ key: "" })
  useEffect(() => {
    if (!id) return
    const controller = new AbortController()
    const load = () => api<DeliveryMetrics>(`/api/v1/${scope}/${encodeURIComponent(id)}/delivery-metrics?range=${range}`, { signal: controller.signal })
      .then((metrics) => setState({ key, metrics }))
      .catch((error) => { if (!controller.signal.aborted) setState({ key, error }) })
    void load()
    const timer = window.setInterval(() => void load(), 300_000)
    return () => { controller.abort(); window.clearInterval(timer) }
  }, [scope, id, range, key])
  const current = state.key === key ? state : { key }
  return { metrics: current.metrics, error: current.error, loading: !current.metrics && !current.error }
}

/** Failed runs as a share of finished ones (cancelled runs count as neither). Null without finished runs. */
export function failureRate(period: DeliveryPeriod) {
  const finished = period.deployments + period.failed
  return finished ? period.failed / finished : null
}
