"use client"

import { useCallback, useEffect, useState } from "react"
import { api } from "@/lib/api"
import type { Application, ListResponse, Workspace } from "@/lib/types"
import { useWorkspaceSelection } from "@/hooks/workspace-selection"

export type WorkspaceApplication = Application & { workspaceName?: string }

export function needsAttention(app: Application) {
  return [
    "out_of_sync",
    "deletion_pending",
    "degraded",
    "failed",
    "error",
  ].includes(app.health) || [
    "Progressing",
    "Degraded",
    "Suspended",
    "Missing",
    "Unknown",
    "Partial",
  ].includes(app.healthCondition?.status ?? "")
}

export function isApplicationHealthy(app: Application) {
  return app.health === "synced" && app.healthCondition?.status === "Healthy"
}

export function useWorkspace({ includeAllApplications = false }: { includeAllApplications?: boolean } = {}) {
  const { workspaceId } = useWorkspaceSelection()
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [applications, setApplications] = useState<WorkspaceApplication[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown | null>(null)
  const [version, setVersion] = useState(0)
  const refresh = useCallback(() => {
    setLoading(true)
    setVersion((value) => value + 1)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    async function load() {
      try {
        const result = await api<ListResponse<Workspace>>("/api/v1/workspaces", {
          signal: controller.signal,
        })
        const selected = includeAllApplications
          ? result.items
          : result.items.filter((workspace) => workspace.id === workspaceId)
        const grouped = await Promise.all(
          selected.map(async (workspace) => {
            const response = await api<ListResponse<Application>>(
              `/api/v1/applications?workspaceId=${encodeURIComponent(workspace.id)}`,
              { signal: controller.signal }
            )
            return response.items.map((app) => ({
              ...app,
              workspaceName: workspace.name,
            }))
          })
        )
        if (!controller.signal.aborted) {
          setWorkspaces(result.items)
          setApplications(grouped.flat())
          setError(null)
        }
      } catch (cause) {
        if (!controller.signal.aborted) setError(cause)
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }
    void load()
    return () => controller.abort()
  }, [version, workspaceId, includeAllApplications])
  return { workspaces, applications, loading, error, refresh }
}
