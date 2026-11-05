"use client"

import { useProgressDismissals } from "@/hooks/use-progress-dismissals"
import { useEffect, useState, type ReactNode } from "react"
import { apiPost, errorMessage } from "@/lib/api"
import { ActionMenu } from "@/components/action-menu"
import {
  operationPhaseLabel,
  resourceLabel,
} from "@/components/application/helpers"
import {
  ErrorDetailsButton,
  ErrorDetailsDialog,
} from "@/components/error-details"
import { RolloutPanel } from "@/components/application/rollout-panel"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { HugeiconsIcon } from "@hugeicons/react"
import { Cancel01Icon } from "@hugeicons/core-free-icons"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { RunProgressStages } from "@/components/application/run-progress"
import { StatusBadge } from "@/components/ui-kit"
import { rolloutSummary } from "@/lib/rollout-timeline"
import { syncRunProgress } from "@/lib/run-progress"
import type { Identity, Operation, PlanRecord } from "@/lib/types"
import { useToast } from "@/components/toast-provider"

export type RollbackRequest = {
  kind: "successful_sync" | "pre_operation" | "git_revision"
  id?: string
  revision?: string
}

/** Live sync stages and outcome, with recovery actions for failures. */
export function OperationBar({
  operation,
  plan,
  checkpointId,
  canRestore,
  disabled,
  onRestore,
  canCancel,
  onUpdated,
  onReviewResource,
  scheduleActions,
  onCreatePlan,
  canCreatePlan,
  aiAction,
}: {
  aiAction?: ReactNode
  onCreatePlan?: () => void
  canCreatePlan?: boolean
  operation: Operation
  scheduleActions?: ReactNode
  plan?: PlanRecord
  checkpointId?: string
  canRestore: boolean
  disabled: boolean
  onRestore: (target: RollbackRequest) => void
  canCancel: boolean
  onUpdated: () => Promise<void>
  onReviewResource: (identity: Identity) => void
}) {
  const [detailsOpen, setDetailsOpen] = useState(false)
  const toast = useToast()
  const [cancelling, setCancelling] = useState(false)
  const [cancelError, setCancelError] = useState<unknown>(null)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (operation.finishedAt) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [operation.id, operation.finishedAt])
  const elapsed = (startedAt: string, finishedAt?: string) => {
    const seconds = Math.max(
      0,
      Math.floor(
        ((finishedAt ? Date.parse(finishedAt) : now) - Date.parse(startedAt)) /
          1000
      )
    )
    return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  }
  async function cancelOperation() {
    setCancelling(true)
    setCancelError(null)
    try {
      await apiPost(
        `/api/v1/operations/${encodeURIComponent(operation.id)}/cancel`,
        {}
      )
      toast.info("Cancellation requested.")
      await onUpdated()
    } catch (cause) {
      setCancelError(cause)
    } finally {
      setCancelling(false)
    }
  }
  const { isDismissed, dismiss } = useProgressDismissals()
  const terminal =
    operation.status !== "running" && operation.status !== "queued"
  if (terminal && isDismissed(`operation:${operation.id}`)) return null
  const failed = operation.status === "failed"
  const completed = operation.progress?.completed?.length ?? 0
  const total = operation.progress?.total ?? 0
  const failedResource = operation.progress?.resources?.find((resource) => resource.status === "failed")
  const failureDetail = failedResource?.failureDetail
  const details = {
    name: "SyncOperationError",
    message: operation.message,
    operationId: operation.id,
    applicationId: operation.applicationId,
    planId: operation.planId,
    status: operation.status,
    startedAt: operation.startedAt,
    finishedAt: operation.finishedAt,
    progress: operation.progress,
    failureDetail,
  }
  const canRestoreState = failed && canRestore && Boolean(checkpointId)
  const progress =
    operation.type === "resource_action"
      ? null
      : syncRunProgress(operation, plan)
  const actionBar = (failed || terminal) ? (
    <div className="flex flex-wrap items-center gap-2">
            {failed && aiAction}
            {failed && onCreatePlan && (
              <Button
                size="sm"
                disabled={!canCreatePlan}
                onClick={onCreatePlan}
              >
                Create fresh plan
              </Button>
            )}
            {failed && (
              <div className="flex items-center gap-2">
                {canRestoreState ? (
                  <>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={disabled}
                      onClick={() =>
                        onRestore({ kind: "pre_operation", id: checkpointId })
                      }
                    >
                      Restore state before this{" "}
                      {operation.type === "rollback" ? "rollback" : "sync"}
                    </Button>
                    <ActionMenu
                      label="More"
                      size="sm"
                      variant="ghost"
                      items={[
                        {
                          label: "Debug details",
                          onSelect: () => setDetailsOpen(true),
                        },
                      ]}
                    />
                    <ErrorDetailsDialog
                      error={details}
                      open={detailsOpen}
                      onOpenChange={setDetailsOpen}
                    />
                  </>
                ) : (
                  <ErrorDetailsButton label="Debug details" error={details} />
                )}
              </div>
            )}
            {terminal && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon"
                      className="text-muted-foreground"
                      aria-label="Dismiss completed sync progress"
                      onClick={() => dismiss(`operation:${operation.id}`)}
                    />
                  }
                >
                  <HugeiconsIcon
                    icon={Cancel01Icon}
                    strokeWidth={2}
                    aria-hidden="true"
                  />
                </TooltipTrigger>
                <TooltipContent>Dismiss progress</TooltipContent>
              </Tooltip>
            )}
          </div>
  ) : null
  return (
    <section
      aria-label={
        operation.type === "rollback" ? "Rollback progress" : "Sync progress"
      }
      className={`mb-5 min-w-0 space-y-4 rounded-xl border px-4 py-4 ${failed ? "border-destructive/30 bg-destructive/5" : "bg-card"}`}
    >
      <div
        role="status"
        aria-live="polite"
        className="flex flex-wrap items-center gap-3"
      >
        <StatusBadge status={operation.status} />
        <span className="text-sm font-medium">
          {operation.status === "queued" &&
          operation.scheduleDeferred &&
          !operation.scheduleOverrideReason
            ? "Waiting for deployment schedule"
            : operationPhaseLabel(operation.progress?.phase, operation.status)}
        </span>
        {!failed && (
          <span className="min-w-0 truncate text-sm text-muted-foreground">
            {operation.progress?.current
              ? `Now: ${resourceLabel(operation.progress.current)}`
              : operation.message}
          </span>
        )}
        <span className="ml-auto text-sm text-muted-foreground tabular-nums">
          {failed
            ? `Elapsed ${elapsed(operation.startedAt, operation.finishedAt)} · Attempt ${operation.attemptCount || 1}`
            : operation.progress?.resources?.length
              ? rolloutSummary(operation.progress.resources)
              : `${completed} / ${total} resources`}
        </span>
        {operation.status === "running" &&
          operation.progress?.phase !== "complete" &&
          canCancel &&
          operation.type !== "resource_action" && (
            <Button
              size="sm"
              variant="destructive"
              loading={cancelling}
              disabled={operation.progress?.cancelRequested}
              onClick={() => void cancelOperation()}
            >
              {operation.progress?.cancelRequested
                ? "Stopping after current resource"
                : "Stop after current resource"}
            </Button>
          )}
        {!failed && actionBar}
      </div>
      {failed && (
        <div role="alert" className="min-w-0 space-y-2 rounded-lg border border-destructive/30 bg-background/60 p-3">
          <p className="text-sm font-medium break-words">
            {failedResource ? `${resourceLabel(failedResource.identity)} could not be applied` : operation.message}
          </p>
          {failureDetail && <p className="font-mono text-xs break-words text-destructive select-text">{failureDetail}</p>}
          {!failureDetail && failedResource && <p className="text-sm break-words text-muted-foreground">{failedResource.message || operation.message}</p>}
          <p className="text-xs text-muted-foreground">
            {total > 0 ? `${completed} of ${total} resources were applied and kept. Nothing was rolled back automatically.` : "No resources were applied."}
            {operation.nextRetryAt ? ` A fresh plan retry is scheduled for ${new Date(operation.nextRetryAt).toLocaleString()}.` : ""}
          </p>
        </div>
      )}
      {failed && actionBar}
      {!failed && (
        <p className="text-xs text-muted-foreground tabular-nums">
          Elapsed {elapsed(operation.startedAt, operation.finishedAt)} · Attempt{" "}
          {operation.attemptCount || 1}
          {operation.nextRetryAt
            ? ` · Fresh plan retry scheduled for ${new Date(operation.nextRetryAt).toLocaleString()}`
            : ""}
        </p>
      )}
      {operation.progress?.cancelRequested && !terminal && (
        <p role="status" className="text-sm text-muted-foreground">
          The current resource step will finish before stopping. Applied changes
          remain. Automatic reconciliation is paused.
        </p>
      )}
      {cancelError != null && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(cancelError)}
        </p>
      )}
      {scheduleActions}
      {failed ? (
        <Disclosure summary="Timeline and resource rollout">
          <div className="space-y-4">
            {progress && <RunProgressStages progress={progress} label="Sync stages" />}
            {Boolean(operation.progress?.resources?.length) && (
              <RolloutPanel key={operation.id} operation={operation} plan={plan} elapsed={elapsed} onReviewResource={onReviewResource} />
            )}
          </div>
        </Disclosure>
      ) : (
        <>
          {progress && (
            <>
              <RunProgressStages progress={progress} label="Sync stages" />
              <p className="text-sm text-muted-foreground">{progress.summary}</p>
            </>
          )}
          {Boolean(operation.progress?.resources?.length) && (
            <RolloutPanel key={operation.id} operation={operation} plan={plan} elapsed={elapsed} onReviewResource={onReviewResource} />
          )}
        </>
      )}
    </section>
  )
}
