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
import { ErrorOutput } from "@/components/error-output"
import { RolloutPanel } from "@/components/application/rollout-panel"
import { Card } from "@/components/panelui/card"
import { Frame } from "@/components/panelui/frame"
import { Button } from "@/components/panelui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { HugeiconsIcon } from "@hugeicons/react"
import { Alert02Icon, Cancel01Icon } from "@hugeicons/core-free-icons"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/panelui/tooltip"
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
  embedded = false,
  hideStages = false,
}: {
  /** Draw a failure as rows for a frame that already exists (the attention frame). */
  embedded?: boolean
  /** The plan lifecycle flow already shows the stages. */
  hideStages?: boolean
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
    // A run that stopped without a finish time (a restart or lost worker) has no known duration.
    if (!finishedAt && operation.status !== "running" && operation.status !== "queued") return "unknown"
    const seconds = Math.max(
      0,
      Math.floor(
        ((finishedAt ? Date.parse(finishedAt) : now) - Date.parse(startedAt)) /
          1000
      )
    )
    const minutes = Math.floor(seconds / 60)
    if (minutes >= 1440) return `${Math.floor(minutes / 1440)}d ${Math.floor((minutes % 1440) / 60)}h`
    if (minutes >= 60) return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
    return `${minutes}m ${seconds % 60}s`
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
  if (!hideStages && terminal && isDismissed(`operation:${operation.id}`)) return null
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
  const actionButtons = failed ? (
    <>
            {failed && onCreatePlan && (
              <Button
                size="sm"
                disabled={!canCreatePlan}
                onClick={onCreatePlan}
              >
                Create fresh plan
              </Button>
            )}
            {failed && aiAction}
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
    </>
  ) : null
  const dismissButton = terminal && !hideStages ? (
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
  ) : null
  const actionBar = (failed || terminal) ? (
    <div className="flex flex-wrap items-center gap-2">
      {actionButtons}
      {dismissButton}
    </div>
  ) : null
  if (failed && embedded) {
    return (
      <Frame.Row role="alert" className="flex-wrap items-start gap-x-3 gap-y-3">
        <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center rounded-full bg-destructive/15 text-destructive"><HugeiconsIcon icon={Alert02Icon} strokeWidth={2} className="size-4" /></span>
        <div className="min-w-0 flex-1 basis-60 space-y-0.5">
          <p className="text-sm font-medium">{operationPhaseLabel(operation.progress?.phase, operation.status)}</p>
          <p className="text-sm break-words text-muted-foreground">
            {failedResource ? `${resourceLabel(failedResource.identity)} could not be applied.` : ""}
            {!failureDetail && <>{failedResource ? " " : ""}{failedResource?.message || operation.message}</>}
          </p>
          {failureDetail && <pre className="my-2 rounded-lg border border-destructive/30 border-l-4 border-l-destructive bg-destructive/10 px-3 py-2 font-mono text-xs leading-5 break-words whitespace-pre-wrap text-destructive select-text">{failureDetail}</pre>}
          <p className="text-sm text-muted-foreground">
            {total > 0 ? `${completed} of ${total} resources were applied and kept. Nothing was rolled back automatically.` : "No resources were applied."}
            {operation.nextRetryAt ? ` A fresh plan retry is scheduled for ${new Date(operation.nextRetryAt).toLocaleString()}.` : ""}
            {" "}Elapsed {elapsed(operation.startedAt, operation.finishedAt)} · Attempt {operation.attemptCount || 1}.
          </p>
          {total > 0 && <div className="pt-1.5" role="img" aria-label={`${completed} of ${total} resources applied`}><div className="h-1.5 overflow-hidden rounded-full bg-muted"><div className="h-full rounded-full bg-success" style={{ width: `${Math.round((completed / total) * 100)}%` }} /></div></div>}
          {cancelError != null && <p role="alert" className="pt-1 text-sm text-destructive">{errorMessage(cancelError)}</p>}
        </div>
        <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
          {onCreatePlan && <Button size="sm" disabled={!canCreatePlan} onClick={onCreatePlan}>Create fresh plan</Button>}
          {aiAction}
          <ActionMenu
            label="More"
            size="sm"
            variant="ghost"
            items={[
              ...(canRestoreState ? [{ label: `Restore state before this ${operation.type === "rollback" ? "rollback" : "sync"}`, disabled, onSelect: () => onRestore({ kind: "pre_operation", id: checkpointId }) }] : []),
              { label: "Debug details", onSelect: () => setDetailsOpen(true) },
            ]}
          />
          <ErrorDetailsDialog error={details} open={detailsOpen} onOpenChange={setDetailsOpen} />
          {scheduleActions}
          {dismissButton}
        </div>
        <div className="basis-full">
          <Disclosure summary="Timeline and resource rollout">
            <div className="space-y-4">
              {progress && <RunProgressStages progress={progress} label="Sync stages" />}
              {Boolean(operation.progress?.resources?.length) && (
                <RolloutPanel key={operation.id} operation={operation} plan={plan} elapsed={elapsed} onReviewResource={onReviewResource} />
              )}
            </div>
          </Disclosure>
        </div>
      </Frame.Row>
    )
  }
  if (hideStages) {
    const waiting = operation.status === "queued" && operation.scheduleDeferred && !operation.scheduleOverrideReason
    const heading = failed
      ? failedResource ? `${resourceLabel(failedResource.identity)} could not be applied` : "Sync failed"
      : waiting ? "Waiting for deployment schedule" : operationPhaseLabel(operation.progress?.phase, operation.status)
    const duration = elapsed(operation.startedAt, operation.finishedAt)
    const meta = [
      duration === "unknown" ? null : `${terminal ? "Took" : "Elapsed"} ${duration}`,
      `Attempt ${operation.attemptCount || 1}`,
      operation.nextRetryAt ? `Fresh plan retry ${new Date(operation.nextRetryAt).toLocaleString()}` : null,
    ].filter(Boolean).join(" · ")
    const percent = total > 0 ? Math.round((completed / total) * 100) : 0
    return (
      <section aria-label={operation.type === "rollback" ? "Rollback details" : "Sync details"} className="min-w-0 space-y-7">
        <div role="status" aria-live="polite" className="space-y-2">
          <div className="flex flex-wrap items-center gap-2.5">
            <StatusBadge status={operation.status} />
            <h3 className="min-w-0 text-base font-semibold break-words">{heading}</h3>
          </div>
          <p className="text-sm text-muted-foreground tabular-nums">{meta}</p>
          {!failed && operation.progress?.current && <p className="text-sm text-muted-foreground">Now: {resourceLabel(operation.progress.current)}</p>}
        </div>
        {failed && (
          <div className="space-y-3">
            <h4 className="text-sm font-semibold">What happened</h4>
            <ErrorOutput message={failureDetail || failedResource?.message || operation.message} />
            <p className="text-sm text-muted-foreground">
              {total > 0 ? `${completed} of ${total} resources were applied and kept. Nothing was rolled back automatically.` : "No resources were applied."}
            </p>
            {total > 0 && (
              <div role="img" aria-label={`${completed} of ${total} resources applied`} className="h-1.5 overflow-hidden rounded-full bg-muted">
                <div className="h-full rounded-full bg-success" style={{ width: `${percent}%` }} />
              </div>
            )}
          </div>
        )}
        {!failed && total > 0 && (
          <p className="text-sm text-muted-foreground tabular-nums">{operation.progress?.resources?.length ? rolloutSummary(operation.progress.resources) : `${completed} / ${total} resources`}</p>
        )}
        {operation.progress?.cancelRequested && !terminal && (
          <p role="status" className="text-sm text-muted-foreground">The current resource step will finish before stopping. Applied changes remain. Automatic reconciliation is paused.</p>
        )}
        {cancelError != null && <p role="alert" className="text-sm text-destructive">{errorMessage(cancelError)}</p>}
        {(operation.status === "running" && operation.progress?.phase !== "complete" && canCancel && operation.type !== "resource_action") || failed || scheduleActions ? (
          <div className="flex flex-wrap items-center gap-2">
            {operation.status === "running" && operation.progress?.phase !== "complete" && canCancel && operation.type !== "resource_action" && (
              <Button variant="destructive" loading={cancelling} disabled={operation.progress?.cancelRequested} onClick={() => void cancelOperation()}>
                {operation.progress?.cancelRequested ? "Stopping after current resource" : "Stop after current resource"}
              </Button>
            )}
            {scheduleActions}
            {failed && actionButtons}
          </div>
        ) : null}
        {Boolean(operation.progress?.resources?.length) && (
          <RolloutPanel key={operation.id} operation={operation} plan={plan} elapsed={elapsed} onReviewResource={onReviewResource} />
        )}
      </section>
    )
  }
  if (failed) {
    return (
      <Card aria-label={operation.type === "rollback" ? "Rollback progress" : "Sync progress"} className="mb-5 min-w-0 border-destructive/40 bg-[color-mix(in_oklab,var(--destructive)_12%,var(--muted))] dark:border-destructive/40">
        <Card.Header>
          <Card.Title as="h2" className="text-destructive">{operationPhaseLabel(operation.progress?.phase, operation.status)}</Card.Title>
          <Card.Description>Elapsed {elapsed(operation.startedAt, operation.finishedAt)} · Attempt {operation.attemptCount || 1}</Card.Description>
          <Card.Action><StatusBadge status={operation.status} /></Card.Action>
        </Card.Header>
        <Card.Content>
          <div role="alert" className="flex min-w-0 items-start gap-3 p-4">
            <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center rounded-full bg-destructive/15 text-destructive"><HugeiconsIcon icon={Alert02Icon} strokeWidth={2} className="size-4" /></span>
            <div className="min-w-0 flex-1 space-y-1.5">
            <p className="text-sm font-semibold break-words text-destructive">
              {failedResource ? `${resourceLabel(failedResource.identity)} could not be applied` : operation.message}
            </p>
            {failureDetail && <ErrorOutput message={failureDetail} />}
            {!failureDetail && failedResource && <p className="text-sm break-words text-muted-foreground">{failedResource.message || operation.message}</p>}
            <p className="text-sm text-muted-foreground">
              {total > 0 ? `${completed} of ${total} resources were applied and kept. Nothing was rolled back automatically.` : "No resources were applied."}
              {operation.nextRetryAt ? ` A fresh plan retry is scheduled for ${new Date(operation.nextRetryAt).toLocaleString()}.` : ""}
            </p>
            {total > 0 && <div className="pt-1" role="img" aria-label={`${completed} of ${total} resources applied`}><div className="h-1.5 overflow-hidden rounded-full bg-muted"><div className="h-full rounded-full bg-success" style={{ width: `${Math.round((completed / total) * 100)}%` }} /></div></div>}
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-end gap-2 px-4 py-3">
            {actionBar}
            {scheduleActions}
          </div>
          {cancelError != null && <p role="alert" className="px-4 py-3 text-sm text-destructive">{errorMessage(cancelError)}</p>}
          <div className="px-4 py-3">
            <Disclosure summary="Timeline and resource rollout">
              <div className="space-y-4">
                {progress && <RunProgressStages progress={progress} label="Sync stages" />}
                {Boolean(operation.progress?.resources?.length) && (
                  <RolloutPanel key={operation.id} operation={operation} plan={plan} elapsed={elapsed} onReviewResource={onReviewResource} />
                )}
              </div>
            </Disclosure>
          </div>
        </Card.Content>
      </Card>
    )
  }
  const Shell = hideStages ? "section" : Card
  const Body = hideStages ? "div" : Card.Content
  return (
    <Shell
      aria-label={
        operation.type === "rollback" ? "Rollback progress" : "Sync progress"
      }
      className={hideStages ? "min-w-0" : "mb-5 min-w-0"}
    >
      <Body>
      <div className="min-w-0 space-y-4">
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
        <div role="alert" className="min-w-0 space-y-2 rounded-2xl bg-destructive/10 p-4">
          <p className="text-sm font-medium break-words">
            {failedResource ? `${resourceLabel(failedResource.identity)} could not be applied` : operation.message}
          </p>
          {failureDetail && <ErrorOutput message={failureDetail} />}
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
          {progress && !hideStages && <RunProgressStages progress={progress} label="Sync stages" />}
          {Boolean(operation.progress?.resources?.length) && (
            <RolloutPanel key={operation.id} operation={operation} plan={plan} elapsed={elapsed} onReviewResource={onReviewResource} />
          )}
        </>
      )}
      </div>
      </Body>
    </Shell>
  )
}
