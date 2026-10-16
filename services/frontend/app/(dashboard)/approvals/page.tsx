"use client"

import Link from "next/link"
import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Spinner } from "@/components/ui/spinner"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon, Refresh01Icon } from "@hugeicons/core-free-icons"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { Input } from "@/components/ui/input"
import { Filters } from "@/components/reui/filters/filters"
import { createFilterQuery, flattenFilterConditions, flattenFilterRules } from "@/components/reui/filters/filters-query"
import type { FilterField, FilterQuery } from "@/components/reui/filters/filters-types"
import { EmptyState, PageHeading } from "@/components/ui-kit"
import { Badge } from "@/components/reui/badge"
import { ErrorDetailsButton } from "@/components/error-details"
import { ApprovalRequestPeople } from "@/components/approval-request-people"
import { BackgroundTaskStatus } from "@/components/background-task-status"
import { useBackgroundTasks } from "@/hooks/background-tasks"
import { useToast } from "@/components/toast-provider"
import { approvalOutcomeMessage, type ApprovalOutcome } from "@/lib/approval-inbox"
import { APIError, api, apiPostIdempotent, errorMessage } from "@/lib/api"
import { useWorkspaceSelection } from "@/hooks/workspace-selection"
import type { BackgroundTask, Workspace } from "@/lib/types"

type Request = {
  planId: string; planDigest: string; applicationId: string; applicationName: string; applicationGroupId?: string
  workspaceId: string; workspaceName: string; kind: string; createdAt: string; expiresAt: string
  requiredApprovals: number; approvedApprovals: number; deletions: string[]; clusterScoped: string[]; takeovers: string[]; changeCount: number
}
type Inbox = { items: Request[]; applications: { id: string; name: string }[]; page: number; limit: number; total: number; hasMore: boolean }
type Result = { results: { planId: string; status: number; success: boolean; error?: string; message?: string; warning?: boolean }[]; succeeded: number; failed: number }
type QueuedResult = { results: { planId: string; status: number; accepted: boolean; error?: string; task?: BackgroundTask<ApprovalOutcome> }[] }
type ApprovalAttempt = { status: "submitting" | "processing" | "succeeded" | "failed"; taskId?: string; error?: unknown }
const pageSize = 20
const maxSelection = 20

export default function ApprovalInboxPage() {
  const { workspace, workspaceId } = useWorkspaceSelection()
  if (!workspaceId || !workspace) return <><PageHeading title="Approvals" description="Select a workspace to review its plans." /><div className="rounded-xl border bg-card"><EmptyState title="No workspace selected" description="Choose a workspace from the sidebar to see its pending plans." /></div></>
  return <WorkspaceApprovalInbox key={workspaceId} workspace={workspace} workspaceId={workspaceId} />
}

function WorkspaceApprovalInbox({ workspace, workspaceId }: { workspace: Workspace; workspaceId: string }) {
  const toast = useToast()
  const [filterQuery, setFilterQuery] = useState<FilterQuery>(() => createFilterQuery())
  const [page, setPage] = useState(1)
  const [inbox, setInbox] = useState<Inbox | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [comment, setComment] = useState("")
  const [loading, setLoading] = useState(true)
  const [actionBusy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [results, setResults] = useState<Result | null>(null)
  const [revision, setRevision] = useState(0)
  const [attempts, setAttempts] = useState<Record<string, ApprovalAttempt>>({})
  const refreshQuietly = useCallback(() => setRevision(value => value + 1), [])
  const refresh = useCallback(() => { setLoading(true); setError(null); setRevision(value => value + 1) }, [])
  const backgroundTasks = useBackgroundTasks({ workspaceId }, refreshQuietly)
  const pendingApprovals = Object.values(attempts).filter(attempt => attempt.status === "submitting" || attempt.status === "processing").length
  const busy = actionBusy || backgroundTasks.pending || pendingApprovals > 0
  const conditions = flattenFilterConditions(filterQuery)
  const filterValue = (field: string) => String(conditions.find((condition) => condition.field === field)?.values[0] ?? "")
  const applicationId = filterValue("applicationId")
  const risk = filterValue("risk")
  const age = filterValue("age")
  const expiry = filterValue("expiry")
  const changeFilters = (next: FilterQuery) => {
    const nextConditions = flattenFilterConditions(next)
    const changed = ["applicationId", "risk", "age", "expiry"].some((field) =>
      String(nextConditions.find((condition) => condition.field === field)?.values[0] ?? "") !== filterValue(field)
    )
    setFilterQuery(next)
    if (changed) { setLoading(true); setError(null); setPage(1); setSelected([]); setResults(null) }
  }
  useEffect(() => {
    let active = true
    const query = new URLSearchParams({ page: String(page), limit: String(pageSize) })
    for (const [key, value] of Object.entries({ workspaceId, applicationId, risk, age, expiry })) if (value) query.set(key, value)
    let pending = false
    const controller = new AbortController()
    const load = async () => {
      if (pending) return
      pending = true
      try {
        const value = await api<Inbox>(`/api/v1/approval-inbox?${query}`, { signal: controller.signal })
        if (active) {
          setInbox(value)
          const lastPage = Math.max(1, Math.ceil(value.total / pageSize))
          if (page > lastPage) setPage(lastPage)
          setError(null)
          setSelected(current => current.filter(id => value.items.some(item => item.planId === id)))
        }
      } catch (cause) { if (active) setError(cause) }
      finally { pending = false; if (active) setLoading(false) }
    }
    void load()
    const timer = window.setInterval(() => void load(), 5000)
    const onFocus = () => void load()
    window.addEventListener("focus", onFocus)
    return () => { active = false; controller.abort(); window.clearInterval(timer); window.removeEventListener("focus", onFocus) }
  }, [workspaceId, applicationId, risk, age, expiry, page, revision])
  const applications = useMemo(() => (inbox?.applications ?? []).map(item => [item.id, item.name]), [inbox])
  const fields: FilterField[] = [
    { id: "applicationId", label: "Application", type: "select", options: applications.map(([value, label]) => ({ value, label })), operators: [{ value: "is", label: "is" }] },
    { id: "risk", label: "Risk", type: "select", options: [{ value: "deletion", label: "Deletion" }, { value: "cluster", label: "Cluster scoped" }, { value: "takeover", label: "Takeover" }, { value: "sync", label: "Other changes" }], operators: [{ value: "is", label: "is" }] },
    { id: "age", label: "Age", type: "select", options: [{ value: "24h", label: "Last 24 hours" }, { value: "7d", label: "Last 7 days" }, { value: "older7d", label: "Older than 7 days" }], operators: [{ value: "is", label: "is" }] },
    { id: "expiry", label: "Expiry", type: "select", options: [{ value: "1h", label: "Within 1 hour" }, { value: "24h", label: "Within 24 hours" }, { value: "later", label: "After 24 hours" }], operators: [{ value: "is", label: "is" }] },
  ]
  const pendingItems = (inbox?.items ?? []).filter(item => attempts[item.planId]?.status !== "succeeded")
  const pendingTotal = Math.max(0, (inbox?.total ?? 0) - ((inbox?.items.length ?? 0) - pendingItems.length))
  const chosen = pendingItems.filter(item => selected.includes(item.planId))
  const riskKey = (item: Request) => `${item.kind}:${item.deletions.length > 0}:${item.clusterScoped.length > 0}:${item.takeovers.length > 0}`
  const selectedRisk = chosen[0] && riskKey(chosen[0])
  const toggle = (item: Request) => setSelected(current => current.includes(item.planId) ? current.filter(id => id !== item.planId) : current.length < maxSelection && (!selectedRisk || selectedRisk === riskKey(item)) ? [...current, item.planId] : current)
  async function approve(items: Request[]) {
    if (!items.length) return
    setBusy(true); setError(null); setResults(null)
    setAttempts(current => ({ ...current, ...Object.fromEntries(items.map(item => [item.planId, { status: "submitting" as const }])) }))
    try {
      const queued = await apiPostIdempotent<QueuedResult>("/api/v1/approval-inbox/batch", { items: items.map(item => ({ planId: item.planId, planDigest: item.planDigest })), comment: comment.trim() })
      setSelected([])
      setAttempts(current => ({ ...current, ...Object.fromEntries(queued.results.map(item => [item.planId, { status: "processing" as const, taskId: item.task?.id }])) }))
      void finishBatch(queued)
    } catch (cause) { if (!(cause instanceof Error && cause.name === "AbortError")) {
      setError(cause)
      setAttempts(current => ({ ...current, ...Object.fromEntries(items.map(item => [item.planId, { status: "failed" as const, error: cause }])) }))
      toast.error("Could not queue approvals.", cause)
    } }
    finally { setBusy(false) }
  }
  async function finishBatch(queued: QueuedResult) {
    const recordResult = (result: Result["results"][number], cause?: unknown) => {
      setAttempts(current => ({ ...current, [result.planId]: { ...current[result.planId], status: result.success ? "succeeded" : "failed", error: cause ?? result.error } }))
      setResults(current => {
        const results = [...(current?.results ?? []).filter(item => item.planId !== result.planId), result]
        const succeeded = results.filter(item => item.success).length
        return { results, succeeded, failed: results.length - succeeded }
      })
      refreshQuietly()
      return result
    }
    try {
      const completed = await Promise.all(queued.results.map(async (item) => {
        if (!item.accepted || !item.task) return recordResult({ planId: item.planId, status: item.status, success: false, error: item.error || "Approval was not queued." })
        try {
          const outcome = await backgroundTasks.wait(item.task)
          return recordResult({ planId: item.planId, status: 201, success: true, message: approvalOutcomeMessage(outcome), warning: !!outcome?.autoApplyError })
        } catch (cause) {
          if (cause instanceof Error && cause.name === "AbortError") throw cause
          return recordResult({ planId: item.planId, status: cause instanceof APIError ? cause.status : 0, success: false, error: errorMessage(cause) }, cause)
        }
      }))
      const succeeded = completed.filter(item => item.success).length
      setSelected([])
      if (completed.length === 1 && completed[0].warning) toast.warning(completed[0].message!)
      else if (completed.length === 1 && succeeded === 1) toast.success(completed[0].message || "Approval recorded.")
      else if (succeeded === completed.length) toast.success(`${succeeded} approvals recorded.`)
      else toast.warning(`${succeeded} approved · ${completed.length - succeeded} failed. Review the failed requests.`)
    } catch (cause) { if (!(cause instanceof Error && cause.name === "AbortError")) setError(cause) }
  }
  return <>
    <PageHeading title="Approvals" description={workspace ? `Plans waiting for review in ${workspace.name}.` : "Select a workspace to review its plans."} actions={<Button variant="outline" onClick={refresh} disabled={loading || busy}><HugeiconsIcon icon={Refresh01Icon} strokeWidth={1.8} aria-hidden="true" />Refresh</Button>} />
    <BackgroundTaskStatus tasks={backgroundTasks.tasks.filter(task => task.kind === "approval")} error={backgroundTasks.error} onRefresh={() => void backgroundTasks.refresh()} showApplication />
    <section aria-label="Filter approval requests" className="mb-5 rounded-xl border bg-card p-4">
      <Filters fields={fields} query={filterQuery} onQueryChange={changeFilters} onBeforeQueryChange={(next) => { const paths = flattenFilterRules(next).map((rule) => rule.path.join(".")); return new Set(paths).size === paths.length }} size="sm" />
    </section>
    {error != null && <div role="alert" className="mb-5 flex flex-wrap items-center gap-3 rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm"><span>{errorMessage(error)}</span><ErrorDetailsButton error={error} /><Button variant="outline" size="sm" onClick={refresh}>Try again</Button></div>}
    <div className="mb-3 flex flex-wrap items-center justify-between gap-3"><p className="text-sm text-muted-foreground">{loading ? "Loading requests…" : `${pendingTotal} request${pendingTotal === 1 ? "" : "s"} · page ${page}`}</p><span className="text-sm text-muted-foreground">Select up to {maxSelection} requests with the same approval kind and risk profile to approve them together. Review each plan first.</span></div>
    {loading ? <div role="status" aria-label="Loading approval requests" className="space-y-3">{[0, 1, 2].map(value => <div key={value} className="h-32 animate-pulse rounded-xl border bg-card motion-reduce:animate-none" />)}</div> : error && !inbox ? null : pendingItems.length === 0 ? <div className="rounded-xl border bg-card"><EmptyState title="No requests to review" description="Try a different filter or check back when a new plan needs your approval." /></div> : <div className="space-y-3">{pendingItems.map(item => <article key={item.planId} aria-label={`Approval request for ${item.applicationName}`} className="rounded-xl border bg-card p-4 sm:p-5">
      <div className="flex items-start gap-3">{(() => {
        const mixedRisk = !!selectedRisk && selectedRisk !== riskKey(item)
        const limitReached = selected.length >= maxSelection && !selected.includes(item.planId)
        const reason = mixedRisk ? "Only requests with the same approval kind and risk profile can be approved together." : limitReached ? `You can select at most ${maxSelection} requests at a time.` : ""
        const checkbox = <Checkbox aria-label={`Select approval request for ${item.applicationName}`} checked={selected.includes(item.planId)} disabled={busy || !!reason} onCheckedChange={() => toggle(item)} />
        return reason ? <Tooltip><TooltipTrigger render={<span className="mt-1 inline-flex" tabIndex={0} />}>{checkbox}</TooltipTrigger><TooltipContent>{reason}</TooltipContent></Tooltip> : <span className="mt-1 inline-flex">{checkbox}</span>
      })()}<div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><h2 className="text-sm font-semibold">{item.applicationName}</h2>{item.applicationGroupId && <Badge size="xs" radius="full" variant="secondary">Group child</Badge>}<span className="text-xs text-muted-foreground">{item.workspaceName}</span></div><p className="mt-1 text-sm text-muted-foreground">{item.changeCount} changes · {item.approvedApprovals}/{item.requiredApprovals} approvals · created {new Date(item.createdAt).toLocaleString()} · expires {new Date(item.expiresAt).toLocaleString()}</p></div><div className="flex shrink-0 flex-wrap items-center justify-end gap-2"><Badge size="sm" radius="full" variant={item.kind === "deletion" ? "destructive-light" : item.kind === "takeover" || item.kind === "rollback" ? "warning-light" : "primary-light"} className="capitalize">{item.kind || "sync"}</Badge><Button render={<Link href={`/applications/${item.applicationId}?tab=changes`} aria-label={`Open plan for ${item.applicationName}`} />} nativeButton={false} size="xs" variant="outline">Open plan<HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={1.8} aria-hidden="true" /></Button></div></div>
      {(item.deletions.length > 0 || item.clusterScoped.length > 0 || item.takeovers.length > 0) && <div className="mt-4 grid gap-3 sm:grid-cols-2">{item.deletions.length > 0 && <Risk title={`${item.deletions.length} deletion${item.deletions.length === 1 ? "" : "s"}`} items={item.deletions} tone="destructive" />}{item.clusterScoped.length > 0 && <Risk title={`${item.clusterScoped.length} cluster scoped change${item.clusterScoped.length === 1 ? "" : "s"}`} items={item.clusterScoped} tone="warning" />}{item.takeovers.length > 0 && <Risk title={`${item.takeovers.length} takeover${item.takeovers.length === 1 ? "" : "s"}`} items={item.takeovers} tone="warning" />}</div>}
      <ApprovalRequestPeople planId={item.planId} approved={item.approvedApprovals} required={item.requiredApprovals} />
      {attempts[item.planId] && <div role={attempts[item.planId].status === "failed" ? "alert" : "status"} className="mt-3 flex flex-wrap items-center gap-2 border-t pt-3 text-sm">
        {attempts[item.planId].status === "failed" ? <>
          <span className="text-destructive">Approval failed: {typeof attempts[item.planId].error === "string" ? String(attempts[item.planId].error) : errorMessage(attempts[item.planId].error)}</span>
          <ErrorDetailsButton error={attempts[item.planId].error} />
        </> : <><Spinner className="size-4" aria-hidden="true" /><span>{attempts[item.planId].status === "submitting" ? "Submitting approval…" : backgroundTasks.tasks.find(task => task.id === attempts[item.planId].taskId)?.status === "running" ? "Approving plan…" : "Approval queued…"}</span><span className="text-muted-foreground">This request will leave your inbox once your approval is recorded.</span></>}
      </div>}
    </article>)}</div>}
    {inbox && (inbox.total > pageSize || page > 1) && <nav aria-label="Approval pages" className="mt-5 flex items-center justify-end gap-3"><Button variant="outline" size="sm" disabled={page === 1 || loading} onClick={() => { setLoading(true); setPage(value => value - 1) }}>Previous</Button><span className="text-xs text-muted-foreground">{page} of {Math.ceil(inbox.total / pageSize)}</span><Button variant="outline" size="sm" disabled={!inbox.hasMore || loading} onClick={() => { setLoading(true); setPage(value => value + 1) }}>Next</Button></nav>}
    {(pendingApprovals > 0 || (results && chosen.length === 0)) && <section aria-label="Approval progress" role="status" className="sticky bottom-[max(1rem,var(--toast-clearance,0px))] z-10 mt-6 space-y-2 rounded-xl border bg-card p-4 shadow-lg">
      <div className="flex flex-wrap items-center gap-3">
        {pendingApprovals > 0 && <Spinner className="size-4" aria-hidden="true" />}
        <p className="min-w-0 flex-1 text-sm"><strong>{pendingApprovals > 0 ? `Approving ${pendingApprovals} plan${pendingApprovals === 1 ? "" : "s"}…` : "Approval processing complete"}</strong>{results && <span className="ml-2">{results.succeeded} approved · {results.failed} failed</span>}</p>
        {pendingApprovals === 0 && <Button variant="ghost" size="sm" onClick={() => setResults(null)}>Dismiss</Button>}
      </div>
      {results?.results.filter(item => item.success).slice(-1).map(item => <p key={item.planId} className="text-sm text-muted-foreground">{item.message}</p>)}
      {!!results?.failed && <ul className="max-h-24 space-y-1 overflow-auto text-xs text-destructive">{results.results.filter(item => !item.success).map(item => <li key={item.planId}>{item.error || `HTTP ${item.status}`}</li>)}</ul>}
    </section>}
    {chosen.length > 0 && pendingApprovals === 0 && <section aria-label="Approve selected plans" className="sticky bottom-4 z-10 mt-6 flex flex-wrap items-center gap-3 rounded-xl border bg-card p-4 shadow-lg"><p className="min-w-0 flex-1 text-sm"><strong>{chosen.length} selected</strong><span className="ml-2 text-sm text-muted-foreground">Same approval kind and risk profile. Every plan is rechecked independently.</span></p><Input aria-label="Approval comment" value={comment} maxLength={1000} onChange={event => setComment(event.target.value)} placeholder="Comment (optional)" className="w-full sm:w-64" /><ConfirmDisclosure trigger={`Review ${chosen.length} approval${chosen.length === 1 ? "" : "s"}`} triggerVariant="default" confirmVariant="default" title="Approve these exact plans?" description="Each plan will be checked again for current authorization, expiry, and changes. Successful approvals remain recorded if another plan fails." confirmLabel="Approve selected plans" disabled={busy} onConfirm={() => approve(chosen)}><ul className="max-h-48 space-y-2 overflow-y-auto text-xs">{chosen.map(item => <li key={item.planId}><strong>{item.applicationName}</strong> · {item.changeCount} changes · {item.deletions.length} deletions · {item.clusterScoped.length} cluster scoped · {item.takeovers.length} takeovers</li>)}</ul>{comment.trim() && <p className="mt-3 text-sm text-muted-foreground">Comment: {comment.trim()}</p>}</ConfirmDisclosure></section>}
  </>
}
function Risk({ title, items, tone }: { title: string; items: string[]; tone: "destructive" | "warning" }) { return <div className={`rounded-xl border p-3 text-xs ${tone === "destructive" ? "border-destructive/25 bg-destructive/5" : "border-warning/25 bg-warning/10"}`}><p className={`font-semibold ${tone === "destructive" ? "text-destructive" : "text-warning-foreground"}`}>{title}</p><ul className="mt-2 max-h-24 space-y-1 overflow-y-auto text-foreground/80">{items.map((name, index) => <li key={`${name}-${index}`} className="break-all">{name}</li>)}</ul></div> }
