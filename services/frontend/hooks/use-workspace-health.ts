"use client"

import { useEffect, useState } from "react"
import { api } from "@/lib/api"
import type { AgentSnapshot } from "@/lib/connection-health"
import type { Cluster, GitSource, ListResponse } from "@/lib/types"

export type PendingApproval = {
  planId: string; applicationId: string; applicationName: string; kind: string
  createdAt: string; expiresAt: string; requiredApprovals: number; approvedApprovals: number
  deletions: string[]; clusterScoped: string[]; takeovers: string[]; changeCount: number
}

type Health = {
  clusters: Cluster[]
  gitSources: GitSource[]
  agents: Record<string, AgentSnapshot>
  approvals: PendingApproval[]
  approvalTotal: number
  /** Sections whose request failed; the dashboard must say so instead of implying health. */
  failed: ("clusters" | "gitSources" | "approvals")[]
  loaded: boolean
}

const empty: Health = { clusters: [], gitSources: [], agents: {}, approvals: [], approvalTotal: 0, failed: [], loaded: false }

export function useWorkspaceHealth(workspaceId: string | null | undefined) {
  const [state, setState] = useState<{ id: string | null; health: Health }>({ id: null, health: empty })

  useEffect(() => {
    if (!workspaceId) return
    const id = workspaceId
    const controller = new AbortController()
    const { signal } = controller
    const q = encodeURIComponent(workspaceId)
    async function load() {
      const [clusters, gitSources, inbox] = await Promise.allSettled([
        api<ListResponse<Cluster>>(`/api/v1/clusters?workspaceId=${q}`, { signal }),
        api<ListResponse<GitSource>>(`/api/v1/git-sources?workspaceId=${q}`, { signal }),
        api<{ items: PendingApproval[]; total: number }>(`/api/v1/approval-inbox?workspaceId=${q}&page=1&limit=5`, { signal }),
      ])
      const clusterItems = clusters.status === "fulfilled" ? clusters.value.items : []
      const agentEntries = await Promise.all(
        clusterItems.filter((cluster) => cluster.connectionMode === "agent").map(async (cluster) => {
          try {
            return [cluster.id, await api<AgentSnapshot>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/agent?workspaceId=${q}`, { signal })] as const
          } catch { return null }
        })
      )
      if (signal.aborted) return
      const failed: Health["failed"] = []
      if (clusters.status === "rejected") failed.push("clusters")
      if (gitSources.status === "rejected") failed.push("gitSources")
      if (inbox.status === "rejected") failed.push("approvals")
      setState({
        id,
        health: {
          clusters: clusterItems,
          gitSources: gitSources.status === "fulfilled" ? gitSources.value.items : [],
          agents: Object.fromEntries(agentEntries.filter((entry) => entry !== null)),
          approvals: inbox.status === "fulfilled" ? inbox.value.items : [],
          approvalTotal: inbox.status === "fulfilled" ? inbox.value.total : 0,
          failed,
          loaded: true,
        },
      })
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
  }, [workspaceId])

  return state.id === workspaceId ? state.health : empty
}
