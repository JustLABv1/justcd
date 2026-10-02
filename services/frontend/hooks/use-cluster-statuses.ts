"use client"

import { useEffect, useState } from "react"
import { api } from "@/lib/api"
import type { AgentSnapshot } from "@/lib/connection-health"
import type { Cluster, ListResponse } from "@/lib/types"

export type ClusterStatus = { cluster: Cluster; agent?: AgentSnapshot }

/** Clusters (and agent state) visible through the given workspaces, keyed by cluster id. */
export function useClusterStatuses(workspaceIds: string[]) {
  const [statuses, setStatuses] = useState<Record<string, ClusterStatus>>({})
  const key = workspaceIds.join(",")

  useEffect(() => {
    if (!key) return
    const controller = new AbortController()
    const { signal } = controller
    async function load() {
      const found: Record<string, { cluster: Cluster; workspaceId: string }> = {}
      await Promise.all(key.split(",").map(async (workspaceId) => {
        try {
          const response = await api<ListResponse<Cluster>>(`/api/v1/clusters?workspaceId=${encodeURIComponent(workspaceId)}`, { signal })
          for (const cluster of response.items) found[cluster.id] ??= { cluster, workspaceId }
        } catch { /* a workspace we cannot read just contributes no cluster details */ }
      }))
      const entries = await Promise.all(Object.values(found).map(async ({ cluster, workspaceId }): Promise<[string, ClusterStatus]> => {
        if (cluster.connectionMode !== "agent") return [cluster.id, { cluster }]
        try {
          return [cluster.id, { cluster, agent: await api<AgentSnapshot>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/agent?workspaceId=${encodeURIComponent(workspaceId)}`, { signal }) }]
        } catch { return [cluster.id, { cluster }] }
      }))
      if (!signal.aborted) setStatuses(Object.fromEntries(entries))
    }
    let running = false
    const run = () => {
      if (running || document.visibilityState === "hidden") return
      running = true
      void load().catch(() => {}).finally(() => { running = false })
    }
    run()
    const timer = window.setInterval(run, 10000)
    return () => { controller.abort(); window.clearInterval(timer) }
  }, [key])

  return statuses
}
