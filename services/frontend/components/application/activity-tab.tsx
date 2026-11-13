"use client"

import { Table } from "@/components/panelui/table"
import { Pagination } from "@/components/panelui/pagination"
import { Section } from "@/components/section"

import { useEffect, useState } from "react"
import { ActionMenu, RowActions } from "@/components/action-menu"
import { operationContext, operationPhaseLabel, resourceLabel } from "@/components/application/helpers"
import type { RollbackRequest } from "@/components/application/operation-bar"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/app-dialog"
import { ErrorDetailsButton, ErrorDetailsDialog } from "@/components/error-details"
import { Button } from "@/components/panelui/button"
import { filterRollbackTargets } from "@/lib/activity-history"
import { Input } from "@/components/panelui/input"
import { SectionLoading } from "@/components/section-loading"
import { Alert } from "@/components/panelui/alert"
import { Chip } from "@/components/panelui/chip"
import { Combobox } from "@/components/panelui/combobox"
import { ago } from "@/lib/relative-time"
import { EmptyState, FormField, StatusBadge } from "@/components/ui-kit"
import { api, errorMessage } from "@/lib/api"
import type { Application, Operation, PlanRecord, RollbackTarget, WorkspaceMember } from "@/lib/types"

function operationTitle(operation: Operation) {
  const { type, status } = operation
  if (type === "resource_action") return `Resource action ${status}`
  const noun = type === "rollback" ? "Rollback" : "Sync"
  if (status === "succeeded") return type === "rollback" ? "Rollback completed" : "Sync completed"
  if (status === "failed") return `${noun} stopped`
  if (status === "cancelled") return `${noun} cancelled`
  if (status === "queued") return `${noun} queued`
  return `${noun} running`
}

function FailedOperationActions({ operation, canRetry, restoreCheckpoint, disabled, onRetry, onRestore }: {
  operation: Operation
  canRetry: boolean
  restoreCheckpoint?: string
  disabled: boolean
  onRetry: () => void
  onRestore: () => void
}) {
  const [detailsOpen, setDetailsOpen] = useState(false)
  const details = { name: "SyncOperationError", message: operation.message, operationId: operation.id, applicationId: operation.applicationId, planId: operation.planId, status: operation.status, attemptCount: operation.attemptCount, errorCode: operation.errorCode, nextRetryAt: operation.nextRetryAt, terminalReason: operation.terminalReason, startedAt: operation.startedAt, finishedAt: operation.finishedAt, progress: operation.progress }
  const restoreLabel = `Restore state before this ${operation.type === "rollback" ? "rollback" : "sync"}`
  const actions = [
    ...(canRetry ? [{ label: "Retry with a fresh plan", onSelect: onRetry }] : []),
    ...(restoreCheckpoint ? [{ label: restoreLabel, onSelect: onRestore }] : []),
  ]
  if (actions.length === 0) return <ErrorDetailsButton label="Debug details" error={details} />
  const [primary, ...rest] = actions
  return <>
    <Button size="sm" variant="outline" disabled={disabled} onClick={primary.onSelect}>{primary.label}</Button>
    <ActionMenu label="More" size="sm" variant="ghost" items={[...rest.map((item) => ({ ...item, disabled })), { label: "Debug details", onSelect: () => setDetailsOpen(true) }]} />
    <ErrorDetailsDialog error={details} open={detailsOpen} onOpenChange={setDetailsOpen} />
  </>
}

const statusFilters = [["succeeded", "Succeeded"], ["failed", "Failed"], ["running", "Running"], ["queued", "Queued"], ["cancelled", "Cancelled"]] as const
const typeFilters = [["sync", "Sync"], ["rollback", "Rollback"], ["resource_action", "Resource action"]] as const
const targetFilters = [["successful_sync", "Successful deployment"], ["pre_operation", "Before failed operation"]] as const

/** A single-choice chip row whose active chip can be pressed again to clear the filter. */
function FilterChips({ label, value, options, onChange }: { label: string; value: string; options: readonly (readonly [string, string])[]; onChange: (value: string) => void }) {
  return <Chip.Group multiple={false} aria-label={label} value={value ? [value] : []} onValueChange={(next) => onChange(next[0] ?? "")}>
    {options.map(([option, text]) => <Chip key={option} value={option} size="sm">{text}</Chip>)}
  </Chip.Group>
}

export function ActivityTab({ application, operations, operationsLoading, plans, members, rollbackTargets, rollbackLoading, canDeploy, canApprove, busy, hasPendingOperation, pendingAction, onRetryReconciliation, onCreateRollbackPlan, onViewPlan }: {
  application: Application
  operations: Operation[]
  operationsLoading: boolean
  plans: PlanRecord[]
  members: WorkspaceMember[]
  rollbackTargets: RollbackTarget[]
  rollbackLoading: boolean
  canDeploy: boolean
  canApprove: boolean
  busy: boolean
  hasPendingOperation: boolean
  pendingAction: string
  onRetryReconciliation: (operationId?: string) => void
  /** Resolves true when the rollback plan was created. */
  onCreateRollbackPlan: (target: RollbackRequest) => Promise<boolean>
  /** Opens the plan an operation applied, in the Changes tab. */
  onViewPlan: (planId: string) => void
}) {
  const [rollbackRevision, setRollbackRevision] = useState("")
  const [revisionOpen, setRevisionOpen] = useState(false)
  const [rollingBack, setRollingBack] = useState<string | null>(null)
  const [page, setPage] = useState(0)
  const [retry, setRetry] = useState(0)
  const [history, setHistory] = useState<{ key: string; items: Operation[]; hasMore: boolean; total?: number } | null>(null)
  const [historyError, setHistoryError] = useState<unknown>(null)
  const [operationSearch, setOperationSearch] = useState("")
  const [operationStatus, setOperationStatus] = useState("")
  const [operationType, setOperationType] = useState("")
  const [rollbackSearch, setRollbackSearch] = useState("")
  const [rollbackKind, setRollbackKind] = useState("")
  const [rollbackPage, setRollbackPage] = useState(0)
  const pageSize = 10
  const historyKey = JSON.stringify([application.id, page, operationSearch.trim(), operationStatus, operationType])
  const filteredTargets = filterRollbackTargets(rollbackTargets, rollbackSearch, rollbackKind)
  const targetPage = Math.min(rollbackPage, Math.max(0, Math.ceil(filteredTargets.length / pageSize) - 1))
  const visibleTargets = filteredTargets.slice(targetPage * pageSize, (targetPage + 1) * pageSize)
  // Revisions JustCD has seen, offered while typing a custom rollback target; any branch, tag or SHA is still accepted.
  const revisionSuggestions = [...new Set([application.revision, ...rollbackTargets.map((target) => target.revision ?? ""), ...plans.map((plan) => plan.plan.revision)].filter(Boolean))].slice(0, 20)
  useEffect(() => {
    const controller = new AbortController()
    const query = new URLSearchParams({ limit: String(pageSize), offset: String(page * pageSize) })
    if (operationSearch.trim()) query.set("q", operationSearch.trim())
    if (operationStatus) query.set("status", operationStatus)
    if (operationType) query.set("type", operationType)
    const timer = setTimeout(() => {
      api<{ items: Operation[]; hasMore: boolean; total?: number }>(`/api/v1/applications/${encodeURIComponent(application.id)}/operations?${query}`, { signal: controller.signal })
        .then((result) => { if (!controller.signal.aborted) { setHistory({ key: historyKey, ...result }); setHistoryError(null) } })
        .catch((cause) => { if (!controller.signal.aborted) setHistoryError(cause) })
    }, operationSearch.trim() ? 250 : 0)
    return () => { clearTimeout(timer); controller.abort() }
  }, [application.id, page, operationSearch, operationStatus, operationType, historyKey, operations, retry])
  const historyLoading = operationsLoading || history?.key !== historyKey
  // A server that does not report a total still pages: assume one more page while there is more.
  const historyTotal = history ? history.total ?? page * pageSize + history.items.length + (history.hasMore ? 1 : 0) : 0
  const operationFiltersActive = Boolean(operationSearch.trim() || operationStatus || operationType)
  const rollbackFiltersActive = Boolean(rollbackSearch.trim() || rollbackKind)
  const blocked = busy || hasPendingOperation
  // Older runs may point at plans that are no longer loaded; drop the column rather than show a page of dashes.
  const showRevision = Boolean(history?.items.some((operation) => operationContext(operation, plans, members).revision))

  async function reviewRollback(id: string, target: RollbackRequest) {
    setRollingBack(id)
    try { return await onCreateRollbackPlan(target) } finally { setRollingBack(null) }
  }

  return <div className="min-w-0 space-y-5">
    <Section surface title="Recent operations" description="Syncs, rollbacks and resource actions, newest first">
      <div className="space-y-3">
        <Input aria-label="Search operations" placeholder="Search messages or IDs…" maxLength={256} value={operationSearch} onChange={(event) => { setOperationSearch(event.target.value); setPage(0); setHistoryError(null) }} />
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <FilterChips label="Filter operations by status" value={operationStatus} options={statusFilters} onChange={(value) => { setOperationStatus(value); setPage(0); setHistoryError(null) }} />
          <span aria-hidden="true" className="hidden h-5 w-px bg-border sm:block" />
          <FilterChips label="Filter operations by type" value={operationType} options={typeFilters} onChange={(value) => { setOperationType(value); setPage(0); setHistoryError(null) }} />
          {operationFiltersActive && <Button variant="ghost" size="sm" onClick={() => { setOperationSearch(""); setOperationStatus(""); setOperationType(""); setPage(0); setHistoryError(null) }}>Clear filters</Button>}
        </div>
      </div>
      {historyError ? <Alert tone="destructive" variant="inline" role="alert"><Alert.Icon /><Alert.Title>Operations could not be loaded</Alert.Title><Alert.Description>{errorMessage(historyError)}</Alert.Description><Alert.Actions><Button variant="outline" size="sm" onClick={() => { setHistoryError(null); setRetry((value) => value + 1) }}>Retry</Button></Alert.Actions></Alert>
        : historyLoading ? <SectionLoading label="Loading recent operations" />
          : history?.items.length ? <Table aria-label="Recent operations">
            <Table.Header><Table.Row><Table.Head>Operation</Table.Head><Table.Head>Status</Table.Head>{showRevision && <Table.Head>Revision</Table.Head>}<Table.Head>Started by</Table.Head><Table.Head>Started</Table.Head><Table.Head>Duration</Table.Head><Table.Head><span className="sr-only">Actions</span></Table.Head></Table.Row></Table.Header>
            <Table.Body>{history.items.map((operation) => {
              const checkpoint = operation.rollbackCheckpointId ?? rollbackTargets.find((target) => target.kind === "pre_operation" && target.operationId === operation.id)?.id
              const context = operationContext(operation, plans, members)
              return <Table.Row key={operation.id}>
                <Table.Cell className="min-w-64 whitespace-normal">
                  <p className="text-sm font-medium">{operationTitle(operation)}</p>
                  <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{operation.message || (operation.type === "resource_action" ? "Resource action" : operation.type === "rollback" ? "Rollback operation" : "Sync operation")}</p>
                  {operation.status === "failed" && <p className="mt-1 text-sm text-muted-foreground">Attempt {operation.attemptCount}{operation.nextRetryAt ? ` · next retry ${new Date(operation.nextRetryAt).toLocaleString()}` : operation.terminalReason ? ` · ${operation.terminalReason.replaceAll("_", " ")}` : ""}{operation.errorCode ? ` · ${operation.errorCode}` : ""}</p>}
                  {operation.progress && operation.type !== "resource_action" && <p className="mt-1 text-sm text-muted-foreground">{operationPhaseLabel(operation.progress.phase, operation.status)} · {operation.progress.completed?.length ?? 0}/{operation.progress.total} resources{operation.progress.current ? ` · ${resourceLabel(operation.progress.current)}` : ""}</p>}
                  {operation.status === "failed" && <div className="mt-2 flex flex-wrap items-center gap-2">
                    <FailedOperationActions operation={operation} disabled={blocked} canRetry={canDeploy && !application.decommissioning && operation.type === "sync"} restoreCheckpoint={canApprove ? checkpoint : undefined} onRetry={() => onRetryReconciliation(operation.id)} onRestore={() => void onCreateRollbackPlan({ kind: "pre_operation", id: checkpoint })} />
                  </div>}
                </Table.Cell>
                <Table.Cell><StatusBadge status={operation.status} /></Table.Cell>
                {showRevision && <Table.Cell className="font-mono text-xs" title={context.revision}>{context.revision ? context.revision.slice(0, 7) : <span className="text-muted-foreground">–</span>}</Table.Cell>}
                <Table.Cell className="max-w-48 truncate text-sm">{context.source}</Table.Cell>
                <Table.Cell className="whitespace-nowrap text-xs text-muted-foreground"><time dateTime={operation.startedAt} title={new Date(operation.startedAt).toLocaleString()}>{ago(operation.startedAt)}</time></Table.Cell>
                <Table.Cell className="whitespace-nowrap text-xs text-muted-foreground tabular-nums">{context.duration ?? "–"}</Table.Cell>
                <Table.Cell className="text-right">{operation.planId && plans.some((plan) => plan.id === operation.planId) && <RowActions label={operationTitle(operation)} items={[{ label: "View plan", onSelect: () => onViewPlan(operation.planId!) }]} />}</Table.Cell>
              </Table.Row>
            })}</Table.Body>
          </Table> : <EmptyState title={operationFiltersActive ? "No matching operations" : "No syncs yet"} description={operationFiltersActive ? "Try a different search or clear the filters." : "Operations will be recorded here with the actor and resulting status."} />}
      {history && (historyTotal > pageSize || page > 0) && (
        <Pagination aria-label="Activity pagination" count={Math.max(1, Math.ceil(historyTotal / pageSize))} page={page + 1} disabled={historyLoading} onPageChange={(next) => { setHistoryError(null); setPage(next - 1) }}>
          <Pagination.Status page={page + 1} pageSize={pageSize} total={historyTotal} />
        </Pagination>
      )}
    </Section>
    <Section surface title="Rollback targets" description="Review a new plan to restore a previous deployment. Opening a target never changes the cluster." action={canApprove && <Button size="sm" variant="outline" disabled={blocked} onClick={() => setRevisionOpen(true)}>Custom revision…</Button>}>
      <div className="space-y-3">
        <Input aria-label="Search rollback targets" placeholder="Search revision or operation ID…" value={rollbackSearch} onChange={(event) => { setRollbackSearch(event.target.value); setRollbackPage(0) }} />
        <div className="flex flex-wrap items-center gap-2">
          <FilterChips label="Filter rollback targets by type" value={rollbackKind} options={targetFilters} onChange={(value) => { setRollbackKind(value); setRollbackPage(0) }} />
          {rollbackFiltersActive && <Button variant="ghost" size="sm" onClick={() => { setRollbackSearch(""); setRollbackKind(""); setRollbackPage(0) }}>Clear filters</Button>}
        </div>
      </div>
      {rollbackLoading ? <SectionLoading label="Loading rollback targets" /> : visibleTargets.length > 0 ? <Table aria-label="Rollback targets"><Table.Header><Table.Row><Table.Head>Target</Table.Head><Table.Head>Revision</Table.Head><Table.Head>Resources</Table.Head><Table.Head><span className="sr-only">Review</span></Table.Head></Table.Row></Table.Header><Table.Body>{visibleTargets.map((target) => {
        const title = target.kind === "successful_sync" ? "Successful deployment" : "Before failed operation"
        return <Table.Row key={target.id}>
          <Table.Cell className="min-w-48 whitespace-normal"><p className="text-sm font-medium">{title}</p><p className="mt-1 text-xs text-muted-foreground"><time dateTime={target.createdAt} title={new Date(target.createdAt).toLocaleString()}>{ago(target.createdAt)}</time></p>{target.operationId && <p className="mt-1 font-mono text-xs text-muted-foreground">Operation {target.operationId.slice(0, 12)}</p>}</Table.Cell>
          <Table.Cell className="font-mono text-xs">{target.revision ? target.revision.slice(0, 12) : "No successful revision"}</Table.Cell>
          <Table.Cell className="tabular-nums">{target.resourceCount}</Table.Cell>
          <Table.Cell className="text-right">{canApprove && <Button size="sm" variant="outline" aria-label={`Review rollback to ${title.toLowerCase()} from ${new Date(target.createdAt).toLocaleString()}`} loading={rollingBack === target.id} disabled={blocked || (rollingBack !== null && rollingBack !== target.id)} onClick={() => void reviewRollback(target.id, { kind: target.kind, id: target.id })}>Review rollback</Button>}</Table.Cell>
        </Table.Row>
      })}</Table.Body></Table> : <EmptyState title={rollbackFiltersActive ? "No matching rollback targets" : "No rollback history yet"} description={rollbackFiltersActive ? "Try a different search or clear the filters." : "A target appears after a successful deployment or a failed/interrupted operation with a saved checkpoint."} />}
      {(filteredTargets.length > pageSize || targetPage > 0) && <Pagination aria-label="Rollback target pagination" count={Math.max(1, Math.ceil(filteredTargets.length / pageSize))} page={targetPage + 1} disabled={rollbackLoading} onPageChange={(next) => setRollbackPage(next - 1)}><Pagination.Status page={targetPage + 1} pageSize={pageSize} total={filteredTargets.length} /></Pagination>}
      <AppDialog open={revisionOpen} onOpenChange={setRevisionOpen} busy={pendingAction === "rollback-plan"} title="Roll back to a custom revision" description="JustCD resolves the revision, renders it, and shows a create/update/delete diff for owner approval. Nothing changes until you apply the plan.">
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={async (event) => { event.preventDefault(); if (!rollbackRevision.trim()) return; if (await reviewRollback("custom", { kind: "git_revision", revision: rollbackRevision.trim() })) { setRollbackRevision(""); setRevisionOpen(false) } }}>
          <FormField label="Git revision" htmlFor="rollback-git-revision">
            <Combobox id="rollback-git-revision" accessibilityLabel="Git revision" value={rollbackRevision} onValueChange={(value) => setRollbackRevision(value ?? "")} inputValue={rollbackRevision} onInputValueChange={setRollbackRevision} allowCustomValue placeholder="branch, tag, or commit SHA" disabled={!canApprove || blocked} emptyMessage="Any branch, tag or commit SHA is accepted">
              {revisionSuggestions.map((revision) => <Combobox.Item key={revision} value={revision} label={revision} />)}
            </Combobox>
          </FormField>
          <DialogFooter><DialogCancel disabled={pendingAction === "rollback-plan"} /><Button type="submit" loading={rollingBack === "custom"} disabled={!canApprove || blocked || !rollbackRevision.trim()}>Review rollback</Button></DialogFooter>
        </form>
      </AppDialog>
    </Section>
  </div>
}
