"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { AppDialog } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { DataGridList, type GridColumn } from "@/components/data-grid-table"
import { LoadErrorNotice } from "@/components/settings/shared"
import { api } from "@/lib/api"
import type { Cluster } from "@/lib/types"

type Activity = {
  id: string
  profile: string
  namespace: string
  method: string
  resource: string
  state: "queued" | "running" | "succeeded" | "failed" | "expired" | "unknown"
  httpStatus?: number
  errorCode?: string
  operationId?: string
  applicationId?: string
  createdAt: string
  startedAt?: string
  finishedAt?: string
  deadline: string
}
type AgentStatus = {
  mode: "agent" | "direct"
  online?: boolean
  agent?: {
    version: string
    revoked: boolean
    clusterUid: string
    lastSeenAt?: string
    profiles: { name: string; namespaces: string[]; clusterScope: boolean }[]
  }
  summary?: {
    queued: number
    running: number
    lastSuccessAt?: string
    lastFailureAt?: string
  }
  activity?: Activity[]
}
const when = (value?: string) =>
  value ? new Date(value).toLocaleString() : "—"
const stateLabel = (state: Activity["state"]) =>
  state === "unknown"
    ? "Unknown outcome"
    : state[0].toUpperCase() + state.slice(1)
function duration(task: Activity) {
  if (!task.startedAt) return "—"
  return `${Math.max(0, Math.round(((task.finishedAt ? Date.parse(task.finishedAt) : Date.now()) - Date.parse(task.startedAt)) / 1000))}s`
}
const errors: Record<string, string> = {
  "cluster.agent_scope_denied": "The local agent profile denied this request.",
  "cluster.agent_unknown_outcome":
    "Execution could not be confirmed. Refresh the plan and inspect live state before retrying.",
  "cluster.agent_task_expired": "The task expired before execution.",
  "cluster.agent_request_failed":
    "The agent could not complete the Kubernetes request.",
  "kubernetes.access_denied":
    "Kubernetes authentication or RBAC denied the request.",
  "kubernetes.request_failed": "Kubernetes returned an unsuccessful response.",
}

export function ClusterAgentActivity({
  cluster,
  workspaceId,
}: {
  cluster: Cluster
  workspaceId: string
}) {
  const [status, setStatus] = useState<AgentStatus | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [open, setOpen] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    let active = true
    let timer: ReturnType<typeof setTimeout> | undefined
    const poll = async () => {
      try {
        const next = await api<AgentStatus>(
          `/api/v1/clusters/${encodeURIComponent(cluster.id)}/agent?workspaceId=${encodeURIComponent(workspaceId)}`
        )
        if (active) {
          setStatus(next)
          setError(null)
        }
      } catch (cause) {
        if (active) setError(cause)
      }
      if (active) {
        setLoading(false)
        timer = setTimeout(() => void poll(), 10000)
      }
    }
    void poll()
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [cluster.id, workspaceId, refreshKey])
  const activityColumns: GridColumn<Activity>[] = [
    {
      id: "time",
      title: "Started / duration",
      cell: (task) => (
        <div>
          <p>{when(task.startedAt ?? task.createdAt)}</p>
          <p className="text-xs text-muted-foreground">{duration(task)}</p>
        </div>
      ),
    },
    {
      id: "action",
      title: "Kubernetes action",
      cell: (task) => (
        <div>
          <p>
            {task.method} {task.resource}
          </p>
          <p className="text-xs text-muted-foreground">
            {task.namespace || "Cluster scope / discovery"} · {task.profile}
          </p>
        </div>
      ),
    },
    {
      id: "state",
      title: "Result",
      cell: (task) => (
        <div className="space-y-1">
          <Badge
            variant={
              task.state === "succeeded"
                ? "success-light"
                : ["failed", "unknown"].includes(task.state)
                  ? "destructive-light"
                  : "warning-light"
            }
          >
            {stateLabel(task.state)}
          </Badge>
          {task.httpStatus ? (
            <p className="text-xs">HTTP {task.httpStatus}</p>
          ) : null}
          {task.errorCode && (
            <p className="text-xs break-words text-destructive">
              {errors[task.errorCode] ?? "Request failed."}
              <span className="mt-1 block font-mono">{task.errorCode}</span>
            </p>
          )}
        </div>
      ),
    },
    {
      id: "operation",
      title: "Deployment",
      cell: (task) =>
        task.applicationId ? (
          <Button
            size="sm"
            variant="outline"
            nativeButton={false}
            render={
              <Link
                href={`/applications/${encodeURIComponent(task.applicationId)}?tab=activity`}
              />
            }
          >
            Deployment activity
          </Button>
        ) : (
          "—"
        ),
    },
  ]
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3 border-t pt-3">
        <div className="min-w-0 space-y-1 text-xs text-muted-foreground">
          {loading ? (
            <Skeleton className="h-5 w-48" />
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <Badge
                radius="full"
                variant={
                  error || status?.agent?.revoked
                    ? "destructive-light"
                    : status?.online
                      ? "success-light"
                      : "warning-light"
                }
              >
                {error
                  ? "Agent status unavailable"
                  : status?.agent?.revoked
                    ? "Agent revoked"
                    : status?.online
                      ? "Agent online"
                      : "Agent offline"}
              </Badge>
              {status?.agent?.version && <span>v{status.agent.version}</span>}
              {status?.summary && (
                <span>
                  {status.summary.running} running · {status.summary.queued}{" "}
                  queued
                </span>
              )}
            </div>
          )}
          <p>Last heartbeat: {when(status?.agent?.lastSeenAt)}</p>
          {!!status?.agent?.profiles.length && (
            <p>
              {status.agent.profiles
                .map(
                  (p) =>
                    `${p.name}: ${p.clusterScope ? "cluster scope" : p.namespaces.join(", ") || "no namespaces"}`
                )
                .join(" · ")}
            </p>
          )}
          {status?.summary?.lastFailureAt && (
            <p>Last request failure: {when(status.summary.lastFailureAt)}</p>
          )}
        </div>
        <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
          Agent activity<span className="sr-only"> for {cluster.name}</span>
        </Button>
      </div>
      <AppDialog
        variant="info"
        size="xl"
        open={open}
        onOpenChange={setOpen}
        title={`Agent activity · ${cluster.name}`}
        description="Latest 100 tasks from the last seven days, scoped to this workspace. Unknown outcomes require checking live state before retrying."
      >
        {error ? (
          <LoadErrorNotice
            error={error}
            title="Could not load agent activity"
            onRetry={() => setRefreshKey((key) => key + 1)}
          />
        ) : (
          <div className="space-y-4">
            <dl className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <dt className="text-muted-foreground">
                  Last successful request
                </dt>
                <dd>{when(status?.summary?.lastSuccessAt)}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Last failed request</dt>
                <dd>{when(status?.summary?.lastFailureAt)}</dd>
              </div>
            </dl>
            <div className="hidden sm:block">
              <DataGridList
                rows={status?.activity ?? []}
                columns={activityColumns}
                emptyTitle="No recent activity"
                emptyDescription="Tasks appear here when JustCD requests Kubernetes discovery, tests, or deployment work."
              />
            </div>
            <ul className="divide-y rounded-xl border sm:hidden">
              {(status?.activity ?? []).map((task) => (
                <li key={task.id} className="space-y-3 p-3 text-sm">
                  <div className="space-y-1">
                    <p className="font-medium">
                      {task.method} {task.resource}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {task.namespace || "Cluster scope / discovery"} ·{" "}
                      {task.profile}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {when(task.startedAt ?? task.createdAt)} ·{" "}
                      {duration(task)}
                    </p>
                  </div>
                  {activityColumns[2].cell(task)}
                  {task.applicationId && activityColumns[3].cell(task)}
                </li>
              ))}
              {!status?.activity?.length && (
                <li className="p-4 text-sm text-muted-foreground">
                  No recent activity. Tasks appear when JustCD requests
                  Kubernetes discovery, tests, or deployment work.
                </li>
              )}
            </ul>
          </div>
        )}
      </AppDialog>
    </>
  )
}
