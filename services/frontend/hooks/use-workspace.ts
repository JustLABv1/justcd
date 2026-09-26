"use client"

import { useCallback, useEffect, useState } from "react"
import { api } from "@/lib/api"
import type { Application, ListResponse, Project } from "@/lib/types"

export type WorkspaceApplication = Application & { projectName?: string }

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

export function useWorkspace() {
  const [projects, setProjects] = useState<Project[]>([])
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
        const result = await api<ListResponse<Project>>("/api/v1/projects", {
          signal: controller.signal,
        })
        const grouped = await Promise.all(
          result.items.map(async (project) => {
            const response = await api<ListResponse<Application>>(
              `/api/v1/applications?projectId=${encodeURIComponent(project.id)}`,
              { signal: controller.signal }
            )
            return response.items.map((app) => ({
              ...app,
              projectName: project.name,
            }))
          })
        )
        if (!controller.signal.aborted) {
          setProjects(result.items)
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
  }, [version])
  return { projects, applications, loading, error, refresh }
}
