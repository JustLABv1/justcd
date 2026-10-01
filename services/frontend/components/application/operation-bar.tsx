"use client"

import { useState } from "react"
import { ActionMenu } from "@/components/action-menu"
import { operationPhaseLabel, resourceLabel } from "@/components/application/helpers"
import { ErrorDetailsButton, ErrorDetailsDialog } from "@/components/error-details"
import { Button } from "@/components/ui/button"
import { StatusBadge } from "@/components/ui-kit"
import type { Operation } from "@/lib/types"

export type RollbackRequest = { kind: "successful_sync" | "pre_operation" | "git_revision"; id?: string; revision?: string }

/** Compact progress / failure line for the running or most recent failed operation. */
export function OperationBar({ operation, checkpointId, canRestore, disabled, onRestore }: {
  operation: Operation
  checkpointId?: string
  canRestore: boolean
  disabled: boolean
  onRestore: (target: RollbackRequest) => void
}) {
  const [detailsOpen, setDetailsOpen] = useState(false)
  const failed = operation.status === "failed"
  const completed = operation.progress?.completed?.length ?? 0
  const total = operation.progress?.total ?? 0
  const details = { name: "SyncOperationError", message: operation.message, operationId: operation.id, applicationId: operation.applicationId, planId: operation.planId, status: operation.status, startedAt: operation.startedAt, finishedAt: operation.finishedAt, progress: operation.progress }
  const canRestoreState = failed && canRestore && Boolean(checkpointId)
  return <div role="status" aria-live="polite" className={`mb-5 rounded-xl border px-4 py-3 ${failed ? "border-destructive/30 bg-destructive/5" : "bg-card"}`}>
    <div className="flex flex-wrap items-center gap-3">
      <StatusBadge status={operation.status} />
      <span className="text-sm font-medium">{operationPhaseLabel(operation.progress?.phase, operation.status)}</span>
      <span className="min-w-0 truncate text-sm text-muted-foreground">{operation.progress?.current ? `Now: ${resourceLabel(operation.progress.current)}` : operation.message}</span>
      <span className="ml-auto text-sm tabular-nums text-muted-foreground">{completed} / {total} resources</span>
      {failed && <div className="flex items-center gap-2">
        {canRestoreState
          ? <>
            <Button size="sm" variant="outline" disabled={disabled} onClick={() => onRestore({ kind: "pre_operation", id: checkpointId })}>Restore state before this {operation.type === "rollback" ? "rollback" : "sync"}</Button>
            <ActionMenu label="More" size="sm" variant="ghost" items={[{ label: "Debug details", onSelect: () => setDetailsOpen(true) }]} />
            <ErrorDetailsDialog error={details} open={detailsOpen} onOpenChange={setDetailsOpen} />
          </>
          : <ErrorDetailsButton label="Debug details" error={details} />}
      </div>}
    </div>
    {total > 0 && <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-muted"><div className={`h-full rounded-full ${failed ? "bg-destructive" : "bg-primary"}`} style={{ width: `${Math.min(100, Math.round((completed / total) * 100))}%` }} /></div>}
  </div>
}
