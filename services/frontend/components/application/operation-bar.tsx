"use client"

import { useProgressDismissals } from "@/hooks/use-progress-dismissals"
import { useState } from "react"
import { ActionMenu } from "@/components/action-menu"
import { operationPhaseLabel, resourceLabel } from "@/components/application/helpers"
import { ErrorDetailsButton, ErrorDetailsDialog } from "@/components/error-details"
import { Button } from "@/components/ui/button"
import { HugeiconsIcon } from "@hugeicons/react"
import { Cancel01Icon } from "@hugeicons/core-free-icons"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { RunProgressStages } from "@/components/application/run-progress"
import { StatusBadge } from "@/components/ui-kit"
import { syncRunProgress } from "@/lib/run-progress"
import type { Operation, PlanRecord } from "@/lib/types"

export type RollbackRequest = { kind: "successful_sync" | "pre_operation" | "git_revision"; id?: string; revision?: string }

/** Live sync stages and outcome, with recovery actions for failures. */
export function OperationBar({ operation, plan, checkpointId, canRestore, disabled, onRestore }: {
  operation: Operation
  plan?: PlanRecord
  checkpointId?: string
  canRestore: boolean
  disabled: boolean
  onRestore: (target: RollbackRequest) => void
}) {
  const [detailsOpen, setDetailsOpen] = useState(false)
  const { isDismissed, dismiss } = useProgressDismissals()
  const terminal = operation.status !== "running" && operation.status !== "queued"
  if (terminal && isDismissed(`operation:${operation.id}`)) return null
  const failed = operation.status === "failed"
  const completed = operation.progress?.completed?.length ?? 0
  const total = operation.progress?.total ?? 0
  const details = { name: "SyncOperationError", message: operation.message, operationId: operation.id, applicationId: operation.applicationId, planId: operation.planId, status: operation.status, startedAt: operation.startedAt, finishedAt: operation.finishedAt, progress: operation.progress }
  const canRestoreState = failed && canRestore && Boolean(checkpointId)
  const progress = operation.type === "resource_action" ? null : syncRunProgress(operation, plan)
  return <section aria-label={operation.type === "rollback" ? "Rollback progress" : "Sync progress"} className={`mb-5 min-w-0 space-y-4 rounded-xl border px-4 py-4 ${failed ? "border-destructive/30 bg-destructive/5" : "bg-card"}`}>
    <div role="status" aria-live="polite" className="flex flex-wrap items-center gap-3">
      <StatusBadge status={operation.status} />
      <span className="text-sm font-medium">{operationPhaseLabel(operation.progress?.phase, operation.status)}</span>
      <span className="min-w-0 truncate text-sm text-muted-foreground">{operation.progress?.current ? `Now: ${resourceLabel(operation.progress.current)}` : operation.message}</span>
      <span className="ml-auto text-sm tabular-nums text-muted-foreground">{completed} / {total} resources</span>
      {(failed || terminal) && <div className="flex shrink-0 items-center gap-2">
        {failed && <div className="flex items-center gap-2">
          {canRestoreState
            ? <>
              <Button size="sm" variant="outline" disabled={disabled} onClick={() => onRestore({ kind: "pre_operation", id: checkpointId })}>Restore state before this {operation.type === "rollback" ? "rollback" : "sync"}</Button>
              <ActionMenu label="More" size="sm" variant="ghost" items={[{ label: "Debug details", onSelect: () => setDetailsOpen(true) }]} />
              <ErrorDetailsDialog error={details} open={detailsOpen} onOpenChange={setDetailsOpen} />
            </>
            : <ErrorDetailsButton label="Debug details" error={details} />}
      </div>}
      {terminal && <Tooltip><TooltipTrigger render={<Button variant="ghost" size="icon" className="text-muted-foreground" aria-label="Dismiss completed sync progress" onClick={() => dismiss(`operation:${operation.id}`)} />}><HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} aria-hidden="true" /></TooltipTrigger><TooltipContent>Dismiss progress</TooltipContent></Tooltip>}
      </div>}
    </div>
    {progress && <><RunProgressStages progress={progress} label="Sync stages" /><p className="text-sm text-muted-foreground">{progress.summary}</p></>}
  </section>
}
