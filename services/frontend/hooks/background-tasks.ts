"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { api, submitTask, waitForTask } from "@/lib/api"
import type { BackgroundTask, ListResponse } from "@/lib/types"

export function taskPending(task: BackgroundTask) {
  return task.status === "queued" || task.status === "running"
}

export function useBackgroundTasks(
  scope: { applicationId?: string; workspaceId?: string },
  onSettled: (task: BackgroundTask) => void
) {
  const { applicationId, workspaceId } = scope
  const scopeKey = applicationId
    ? `application:${applicationId}`
    : `workspace:${workspaceId}`
  const [tasks, setTasks] = useState<BackgroundTask[]>([])
  const [error, setError] = useState<unknown>(null)
  const previous = useRef<BackgroundTask[]>([])
  const executing = useRef(new Set<string>())
  const callback = useRef(onSettled)
  const controller = useRef<{ scope: string; value: AbortController } | null>(
    null
  )
  useEffect(() => {
    callback.current = onSettled
  }, [onSettled])

  const refresh = useCallback(async () => {
    const signal =
      controller.current?.scope === scopeKey
        ? controller.current.value.signal
        : undefined
    if (!signal || signal.aborted || (!applicationId && !workspaceId)) return
    const query = new URLSearchParams(
      applicationId ? { applicationId } : { workspaceId: workspaceId! }
    )
    try {
      const value = await api<ListResponse<BackgroundTask>>(
        `/api/v1/tasks?${query}`,
        { signal }
      )
      const latestFinished = value.items.find((task) => !taskPending(task))?.id
      await Promise.all(
        value.items.map(async (task, index) => {
          const prior = previous.current.find((item) => item.id === task.id)
          if (taskPending(task)) return
          if (prior?.result !== undefined && prior.status === task.status)
            value.items[index] = { ...task, result: prior.result }
          else if (task.id === latestFinished || (prior && taskPending(prior)))
            value.items[index] = await api<BackgroundTask>(
              `/api/v1/tasks/${encodeURIComponent(task.id)}`,
              { signal }
            )
        })
      )
      if (signal.aborted) return
      for (const task of value.items) {
        if (
          !taskPending(task) &&
          previous.current.some(
            (item) => item.id === task.id && taskPending(item)
          ) &&
          !executing.current.has(task.id)
        )
          callback.current(task)
      }
      previous.current = value.items
      setTasks(value.items)
      setError(null)
    } catch (cause) {
      if (!signal.aborted) setError(cause)
    }
  }, [applicationId, workspaceId, scopeKey])

  useEffect(() => {
    const current = new AbortController()
    controller.current = { scope: scopeKey, value: current }
    previous.current = []
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      await refresh()
      if (!current.signal.aborted)
        timer = setTimeout(
          poll,
          previous.current.some(taskPending) ? 1500 : 5000
        )
    }
    void poll()
    return () => {
      current.abort()
      clearTimeout(timer)
    }
  }, [refresh, scopeKey])

  const wait = useCallback(
    async <T>(task: BackgroundTask<T>): Promise<T> => {
      const signal =
        controller.current?.scope === scopeKey
          ? controller.current.value.signal
          : undefined
      if (!signal) throw new Error("Task status is not ready yet.")
      executing.current.add(task.id)
      setTasks((current) => [
        task,
        ...current.filter((item) => item.id !== task.id),
      ])
      try {
        return await waitForTask(task, signal)
      } finally {
        await refresh()
        executing.current.delete(task.id)
      }
    },
    [refresh, scopeKey]
  )

  const run = useCallback(
    async <T>(path: string, value?: unknown): Promise<T> => {
      const signal =
        controller.current?.scope === scopeKey
          ? controller.current.value.signal
          : undefined
      if (!signal) throw new Error("Task status is not ready yet.")
      return wait(await submitTask<T>(path, value, signal))
    },
    [wait, scopeKey]
  )

  const queue = useCallback(
    async <T>(path: string, value?: unknown) => {
      const signal =
        controller.current?.scope === scopeKey
          ? controller.current.value.signal
          : undefined
      if (!signal) throw new Error("Task status is not ready yet.")
      const task = await submitTask<T>(path, value, signal)
      setTasks((current) => [
        task,
        ...current.filter((item) => item.id !== task.id),
      ])
      return task
    },
    [scopeKey]
  )

  const visibleTasks = applicationId
    ? tasks.filter((task) => task.applicationId === applicationId)
    : tasks
  return {
    tasks: visibleTasks,
    error,
    refresh,
    run,
    wait,
    queue,
    pending: visibleTasks.some(taskPending),
  }
}
