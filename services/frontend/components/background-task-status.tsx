"use client"

import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { Badge } from "@/components/reui/badge"
import { ErrorDetailsButton } from "@/components/error-details"
import { errorMessage, taskError } from "@/lib/api"
import { taskPending } from "@/hooks/background-tasks"
import type { BackgroundTask } from "@/lib/types"
import { RunProgressStages } from "@/components/application/run-progress"
import { planRunProgress } from "@/lib/run-progress"

export function BackgroundTaskStatus({
  tasks,
  error,
  onRefresh,
  showApplication = false,
}: {
  tasks: BackgroundTask[]
  error: unknown
  onRefresh: () => void
  showApplication?: boolean
}) {
  const active = tasks.filter(taskPending)
  const displayed = active.length ? active : tasks.slice(0, 1)
  if (!displayed.length && !error) return null
  return (
    <section
      aria-label="Background work"
      className="mb-5 space-y-3 rounded-xl border bg-card p-4"
    >
      {displayed.map((task) => (
        <div key={task.id} className="min-w-0 space-y-4">
          <div
            role={task.status === "failed" ? "alert" : "status"}
            className="flex flex-wrap items-center gap-3 text-sm"
          >
            {taskPending(task) && (
              <Spinner
                aria-hidden="true"
                className="size-4 motion-reduce:animate-none"
              />
            )}
            <div className="min-w-0 flex-1">
              <p className="font-medium">
                {task.kind === "plan" ? "Plan generation" : "Approval"}{" "}
                <Badge
                  size="xs"
                  variant={
                    task.status === "failed"
                      ? "destructive-light"
                      : task.status === "succeeded"
                        ? "success-light"
                        : "secondary"
                  }
                >
                  {task.status === "succeeded"
                    ? "Complete"
                    : task.status === "failed"
                      ? "Failed"
                      : task.status === "running"
                        ? "Running"
                        : "Queued"}
                </Badge>
              </p>
              <p
                className={
                  task.status === "failed"
                    ? "mt-1 text-destructive"
                    : "mt-1 text-muted-foreground"
                }
              >
                {taskPending(task)
                  ? "You can leave this page. Progress will be available when you return."
                  : task.status === "failed"
                    ? taskError(task).message
                    : task.kind === "plan"
                      ? "The plan is ready for review."
                      : "Approval recorded."}
              </p>
            </div>
            {task.status === "failed" && (
              <ErrorDetailsButton error={taskError(task)} />
            )}
            {showApplication && (
              <Button
                variant="outline"
                size="sm"
                render={
                  <Link
                    href={`/applications/${task.applicationId}?tab=changes`}
                  />
                }
                nativeButton={false}
              >
                Open application
              </Button>
            )}
          </div>
          {task.kind === "plan" && (
            <RunProgressStages
              progress={planRunProgress(task)}
              label="Plan generation stages"
            />
          )}
        </div>
      ))}
      {error != null && (
        <div role="alert" className="flex flex-wrap items-center gap-3 text-sm">
          <p className="min-w-0 flex-1 text-muted-foreground">
            Could not refresh task status: {errorMessage(error)}. Background
            work may still be running.
          </p>
          <Button variant="outline" size="sm" onClick={onRefresh}>
            Refresh status
          </Button>
        </div>
      )}
    </section>
  )
}
