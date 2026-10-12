"use client"

import Link from "next/link"
import { useEffect, useState, type ReactNode } from "react"
import { ActionMenu } from "@/components/action-menu"
import { operationPhaseLabel, resourceLabel } from "@/components/application/helpers"
import type { RollbackRequest } from "@/components/application/operation-bar"
import { ErrorDetailsButton, ErrorDetailsDialog } from "@/components/error-details"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { filterRollbackTargets } from "@/lib/activity-history"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { EmptyState, FormField, Panel, StatusBadge } from "@/components/ui-kit"
import { api, errorMessage } from "@/lib/api"
import type { Application, Operation, RollbackTarget } from "@/lib/types"

function SectionLoading({ label }: { label: string }) {
  return <div role="status" aria-label={label} className="space-y-4 p-5 sm:p-6"><span className="sr-only">{label}…</span><div aria-hidden="true" className="space-y-3"><Skeleton className="h-5 w-2/3 max-w-72 motion-reduce:animate-none" /><Skeleton className="h-4 w-full max-w-lg motion-reduce:animate-none" /><Skeleton className="h-4 w-4/5 max-w-md motion-reduce:animate-none" /></div></div>
}

function KeyValue({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="flex items-start justify-between gap-3"><dt className="text-muted-foreground">{label}</dt><dd className={`max-w-[65%] truncate text-right ${mono ? "font-mono text-xs" : "font-medium"}`}>{value}</dd></div>
}

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

export function ActivityTab({ application, operations, operationsLoading, rollbackTargets, rollbackLoading, kustomization, kustomizationError, canDeploy, canApprove, busy, hasPendingOperation, pendingAction, onRetryReconciliation, onCreateRollbackPlan }: {
  application: Application
  operations: Operation[]
  operationsLoading: boolean
  rollbackTargets: RollbackTarget[]
  rollbackLoading: boolean
  kustomization: { namespace: string; commit: string } | null
  kustomizationError: unknown | null
  canDeploy: boolean
  canApprove: boolean
  busy: boolean
  hasPendingOperation: boolean
  pendingAction: string
  onRetryReconciliation: (operationId?: string) => void
  /** Resolves true when the rollback plan was created. */
  onCreateRollbackPlan: (target: RollbackRequest) => Promise<boolean>
}) {
  const [rollbackRevision, setRollbackRevision] = useState("")
  const [page, setPage] = useState(0)
  const [retry, setRetry] = useState(0)
  const [history, setHistory] = useState<{ key: string; items: Operation[]; hasMore: boolean } | null>(null)
  const [historyError, setHistoryError] = useState<unknown>(null)
  const [operationSearch, setOperationSearch] = useState("")
  const [operationStatus, setOperationStatus] = useState("")
  const [operationType, setOperationType] = useState("")
  const [rollbackSearch, setRollbackSearch] = useState("")
  const [rollbackKind, setRollbackKind] = useState("")
  const [rollbackPage, setRollbackPage] = useState(0)
  const pageSize = 5
  const historyKey = JSON.stringify([application.id, page, operationSearch.trim(), operationStatus, operationType])
  const filteredTargets = filterRollbackTargets(rollbackTargets, rollbackSearch, rollbackKind)
  const targetPage = Math.min(rollbackPage, Math.max(0, Math.ceil(filteredTargets.length / pageSize) - 1))
  const visibleTargets = filteredTargets.slice(targetPage * pageSize, (targetPage + 1) * pageSize)
  useEffect(() => {
    const controller = new AbortController()
    const query = new URLSearchParams({ limit: String(pageSize), offset: String(page * pageSize) })
    if (operationSearch.trim()) query.set("q", operationSearch.trim())
    if (operationStatus) query.set("status", operationStatus)
    if (operationType) query.set("type", operationType)
    const timer = setTimeout(() => {
      api<{ items: Operation[]; hasMore: boolean }>(`/api/v1/applications/${encodeURIComponent(application.id)}/operations?${query}`, { signal: controller.signal })
        .then((result) => { if (!controller.signal.aborted) { setHistory({ key: historyKey, ...result }); setHistoryError(null) } })
        .catch((cause) => { if (!controller.signal.aborted) setHistoryError(cause) })
    }, operationSearch.trim() ? 250 : 0)
    return () => { clearTimeout(timer); controller.abort() }
  }, [application.id, page, operationSearch, operationStatus, operationType, historyKey, operations, retry])
  const historyLoading = operationsLoading || history?.key !== historyKey
  const operationFiltersActive = Boolean(operationSearch.trim() || operationStatus || operationType)
  const rollbackFiltersActive = Boolean(rollbackSearch.trim() || rollbackKind)
  const blocked = busy || hasPendingOperation
  const linkButton = (href: string, label: string): ReactNode => <Button variant="link" size="sm" className="h-auto justify-start p-0" render={<Link href={href} />}>{label}</Button>

  return <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px]">
    <div className="min-w-0 space-y-5">
      <Panel title="Recent operations" description="Sync history and result state">
        <div className="grid gap-3 border-b p-5 sm:grid-cols-[minmax(0,1fr)_160px_160px]">
          <Input aria-label="Search operations" placeholder="Search messages or IDs…" maxLength={256} value={operationSearch} onChange={(event) => { setOperationSearch(event.target.value); setPage(0); setHistoryError(null) }} />
          <FormSelect ariaLabel="Filter operations by status" value={operationStatus} emptyOption="All statuses" items={[{ value: "succeeded", label: "Succeeded" }, { value: "failed", label: "Failed" }, { value: "queued", label: "Queued" }, { value: "running", label: "Running" }, { value: "cancelled", label: "Cancelled" }]} onValueChange={(value) => { setOperationStatus(value); setPage(0); setHistoryError(null) }} />
          <FormSelect ariaLabel="Filter operations by type" value={operationType} emptyOption="All operation types" items={[{ value: "sync", label: "Sync" }, { value: "rollback", label: "Rollback" }, { value: "resource_action", label: "Resource action" }]} onValueChange={(value) => { setOperationType(value); setPage(0); setHistoryError(null) }} />
          {operationFiltersActive && <Button variant="ghost" size="sm" className="justify-self-start" onClick={() => { setOperationSearch(""); setOperationStatus(""); setOperationType(""); setPage(0); setHistoryError(null) }}>Clear operation filters</Button>}
        </div>
        {(application.retryTerminalReason || application.retryNextAt) && <div className="flex flex-wrap items-center gap-3 border-b bg-muted/20 px-5 py-3"><div className="min-w-0 flex-1"><p className="text-sm font-medium">Reconciliation needs attention</p><p className="mt-1 text-sm text-muted-foreground">{application.retryNextAt ? `Next automatic attempt: ${new Date(application.retryNextAt).toLocaleString()}` : application.retryTerminalReason?.replaceAll("_", " ")}{application.retryLastErrorCode ? ` · ${application.retryLastErrorCode}` : ""}</p></div>{canDeploy && !application.decommissioning && <Button size="sm" variant="outline" loading={pendingAction === "retry-reconciliation"} disabled={blocked} onClick={() => onRetryReconciliation()}>Retry now</Button>}</div>}
        {historyError ? <div role="alert" className="space-y-2 p-5"><p className="text-sm">Could not load operations: {errorMessage(historyError)}</p><Button variant="outline" size="sm" onClick={() => { setHistoryError(null); setRetry((value) => value + 1) }}>Retry</Button></div> : historyLoading ? <SectionLoading label="Loading recent operations" /> : history?.items.length ? <ul className="divide-y">{history.items.map((operation) => {
          const checkpoint = operation.rollbackCheckpointId ?? rollbackTargets.find((target) => target.kind === "pre_operation" && target.operationId === operation.id)?.id
          return <li key={operation.id} className="px-5 py-3.5">
            <div className="flex items-center justify-between gap-3"><span className="text-sm font-medium">{operationTitle(operation)}</span><StatusBadge status={operation.status} /></div>
            <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{operation.message || (operation.type === "resource_action" ? "Resource action" : operation.type === "rollback" ? "Rollback operation" : "Sync operation")}</p>
            {operation.status === "failed" && <p className="mt-1 text-sm text-muted-foreground">Attempt {operation.attemptCount}{operation.nextRetryAt ? ` · next retry ${new Date(operation.nextRetryAt).toLocaleString()}` : operation.terminalReason ? ` · ${operation.terminalReason.replaceAll("_", " ")}` : ""}{operation.errorCode ? ` · ${operation.errorCode}` : ""}</p>}
            {operation.progress && operation.type !== "resource_action" && <p className="mt-1 text-sm text-muted-foreground">{operationPhaseLabel(operation.progress.phase, operation.status)} · {operation.progress.completed?.length ?? 0}/{operation.progress.total} resources{operation.progress.current ? ` · ${resourceLabel(operation.progress.current)}` : ""}</p>}
            {operation.status === "failed" && <div className="mt-2 flex flex-wrap items-center gap-2">
              <FailedOperationActions operation={operation} disabled={blocked} canRetry={canDeploy && !application.decommissioning && operation.type === "sync"} restoreCheckpoint={canApprove ? checkpoint : undefined} onRetry={() => onRetryReconciliation(operation.id)} onRestore={() => void onCreateRollbackPlan({ kind: "pre_operation", id: checkpoint })} />
            </div>}
            <p className="mt-1.5 text-sm text-muted-foreground">{new Date(operation.startedAt).toLocaleString()}</p>
          </li>
        })}</ul> : <EmptyState title={operationFiltersActive ? "No matching operations" : "No syncs yet"} description={operationFiltersActive ? "Try a different search or clear the filters." : "Operations will be recorded here with the actor and resulting status."} />}
        <nav aria-label="Activity pagination" className="flex flex-wrap items-center justify-between gap-3 border-t px-5 py-3">
          <p role="status" aria-live="polite" className="text-sm text-muted-foreground">Page {page + 1}{!historyLoading && history?.items.length ? ` · Operations ${page * pageSize + 1}–${page * pageSize + history.items.length}` : ""}</p>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" disabled={page === 0} onClick={() => { setHistoryError(null); setPage((value) => value - 1) }}>Previous</Button>
            <Button variant="outline" size="sm" disabled={historyLoading || Boolean(historyError) || !history?.hasMore} onClick={() => setPage((value) => value + 1)}>Next</Button>
          </div>
        </nav>
      </Panel>
      <Panel title="Rollback targets" description="Review a new plan to restore a previous deployment. Opening a target never changes the cluster.">
        <div className="space-y-4 p-5">
          <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_220px]">
            <Input aria-label="Search rollback targets" placeholder="Search revision or operation ID…" value={rollbackSearch} onChange={(event) => { setRollbackSearch(event.target.value); setRollbackPage(0) }} />
            <FormSelect ariaLabel="Filter rollback targets by type" value={rollbackKind} emptyOption="All target types" items={[{ value: "successful_sync", label: "Successful deployment" }, { value: "pre_operation", label: "Before failed operation" }]} onValueChange={(value) => { setRollbackKind(value); setRollbackPage(0) }} />
            {rollbackFiltersActive && <Button variant="ghost" size="sm" className="justify-self-start" onClick={() => { setRollbackSearch(""); setRollbackKind(""); setRollbackPage(0) }}>Clear rollback filters</Button>}
          </div>
          {rollbackLoading ? <SectionLoading label="Loading rollback targets" /> : visibleTargets.length > 0 ? <ul className="divide-y rounded-lg border">{visibleTargets.map((target) => {
            const title = target.kind === "successful_sync" ? "Successful deployment" : "Before failed operation"
            return <li key={target.id} className="flex flex-wrap items-center gap-3 px-3 py-3"><div className="min-w-0 flex-1"><p className="text-sm font-medium">{title}</p><p className="mt-1 text-sm text-muted-foreground">{new Date(target.createdAt).toLocaleString()} · {target.resourceCount} resources{target.revision ? ` · ${target.revision.slice(0, 12)}` : " · no successful revision"}</p>{target.operationId && <p className="mt-0.5 font-mono text-sm text-muted-foreground">Operation {target.operationId.slice(0, 12)}</p>}</div>{canApprove && <Button size="sm" variant="outline" aria-label={`Review rollback to ${title.toLowerCase()} from ${new Date(target.createdAt).toLocaleString()}`} loading={pendingAction === "rollback-plan"} disabled={blocked} onClick={() => void onCreateRollbackPlan({ kind: target.kind, id: target.id })}>Review rollback</Button>}</li>
          })}</ul> : <EmptyState title={rollbackFiltersActive ? "No matching rollback targets" : "No rollback history yet"} description={rollbackFiltersActive ? "Try a different search or clear the filters." : "A target appears after a successful deployment or a failed/interrupted operation with a saved checkpoint."} />}
          <nav aria-label="Rollback target pagination" className="flex flex-wrap items-center justify-between gap-3">
            <p role="status" aria-live="polite" className="text-sm text-muted-foreground">Page {targetPage + 1} of {Math.max(1, Math.ceil(filteredTargets.length / pageSize))} · {filteredTargets.length} targets</p>
            <div className="flex items-center gap-2">
              <Button variant="outline" size="sm" disabled={rollbackLoading || targetPage === 0} onClick={() => setRollbackPage(targetPage - 1)}>Previous</Button>
              <Button variant="outline" size="sm" disabled={rollbackLoading || (targetPage + 1) * pageSize >= filteredTargets.length} onClick={() => setRollbackPage(targetPage + 1)}>Next</Button>
            </div>
          </nav>
          <div className="border-t pt-4">
            <FormField label="Git revision" htmlFor="rollback-git-revision"><Input id="rollback-git-revision" value={rollbackRevision} onChange={(event) => setRollbackRevision(event.target.value)} placeholder="branch, tag, or commit SHA" disabled={!canApprove || blocked} /></FormField>
            <div className="mt-2 flex flex-wrap items-center justify-between gap-3"><p className="max-w-xl text-sm text-muted-foreground">Enter a branch, tag, or commit SHA to enable review. JustCD resolves and renders it, then shows a create/update/delete diff for owner approval.</p><Button size="sm" variant="outline" loading={pendingAction === "rollback-plan"} loadingText="Rendering target…" disabled={!canApprove || blocked || !rollbackRevision.trim()} onClick={async () => { if (await onCreateRollbackPlan({ kind: "git_revision", revision: rollbackRevision.trim() })) setRollbackRevision("") }}>Review Git rollback</Button></div>
          </div>
        </div>
      </Panel>
    </div>
    <Panel title="Application source" description="Configuration stored in JustCD">
      <dl className="space-y-3 p-5 text-sm">
        <KeyValue label="Manifest path" value={application.manifestPath} mono />
        <KeyValue label="Renderer" value={application.renderer} />
        {application.renderer === "kustomize" && <>
          <KeyValue label="Cluster overlay path" value={application.targetManifestPath || "Shared path"} mono />
          {Object.keys(application.namespaceManifestPaths ?? {}).length > 0 && <KeyValue label="Namespace overlays" value={Object.entries(application.namespaceManifestPaths).map(([namespace, path]) => `${namespace}: ${path}`).join(", ")} mono />}
        </>}
        <KeyValue label="Cluster" value={application.clusterId.slice(0, 12)} mono />
        <KeyValue label="Poll interval (seconds)" value={String(application.pollSeconds)} />
        {application.renderer === "kustomize" && <>
          <KeyValue label="Namespace from Git" value={kustomization ? kustomization.namespace || "Not set in kustomization" : kustomizationError ? errorMessage(kustomizationError) : "Checking kustomization…"} />
        </>}
      </dl>
      {application.renderer === "kustomize" && kustomizationError != null && <div className="px-5 pb-3"><ErrorDetailsButton error={kustomizationError} /></div>}
      <div className="flex flex-col items-start gap-1 border-t px-5 py-3">
        {application.applicationGroupId && linkButton(`/application-groups/${application.applicationGroupId}`, "Open deployment group")}
        {canApprove && linkButton(`/applications/${application.id}/edit`, "Edit application")}
        {linkButton(`/workspaces/${application.workspaceId}`, "Open workspace")}
      </div>
    </Panel>
  </div>
}
