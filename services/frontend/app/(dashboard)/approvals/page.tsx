"use client"

import Link from "next/link"
import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { Input } from "@/components/ui/input"
import { PageHeading } from "@/components/ui-kit"
import { ErrorDetailsButton } from "@/components/error-details"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { ListResponse, Project } from "@/lib/types"

type Request = {
  planId: string; planDigest: string; applicationId: string; applicationName: string; applicationGroupId?: string
  projectId: string; projectName: string; kind: string; createdAt: string; expiresAt: string
  requiredApprovals: number; approvedApprovals: number; deletions: string[]; clusterScoped: string[]; takeovers: string[]; changeCount: number
}
type Inbox = { items: Request[]; applications: { id: string; name: string }[]; page: number; limit: number; total: number; hasMore: boolean }
type Result = { results: { planId: string; status: number; success: boolean; error?: string }[]; succeeded: number; failed: number }
const pageSize = 20

export default function ApprovalInboxPage() {
  const [projects, setProjects] = useState<Project[]>([])
  const [projectId, setProjectId] = useState("")
  const [applicationId, setApplicationId] = useState("")
  const [risk, setRisk] = useState("")
  const [age, setAge] = useState("")
  const [expiry, setExpiry] = useState("")
  const [page, setPage] = useState(1)
  const [inbox, setInbox] = useState<Inbox | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [comment, setComment] = useState("")
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [results, setResults] = useState<Result | null>(null)
  const [revision, setRevision] = useState(0)
  useEffect(() => { api<ListResponse<Project>>("/api/v1/projects").then(value => setProjects(value.items)).catch(() => undefined) }, [])
  const refresh = useCallback(() => { setLoading(true); setError(null); setRevision(value => value + 1) }, [])
  const filter = (setter: (value: string) => void, value: string) => { setLoading(true); setError(null); setter(value); setPage(1); setSelected([]); setResults(null) }
  useEffect(() => {
    let active = true
    const query = new URLSearchParams({ page: String(page), limit: String(pageSize) })
    for (const [key, value] of Object.entries({ projectId, applicationId, risk, age, expiry })) if (value) query.set(key, value)
    api<Inbox>(`/api/v1/approval-inbox?${query}`).then(value => { if (active) { setInbox(value); setSelected([]) } }).catch(cause => { if (active) setError(cause) }).finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [projectId, applicationId, risk, age, expiry, page, revision])
  const applications = useMemo(() => (inbox?.applications ?? []).map(item => [item.id, item.name]), [inbox])
  const chosen = (inbox?.items ?? []).filter(item => selected.includes(item.planId))
  const riskKey = (item: Request) => `${item.kind}:${item.deletions.length > 0}:${item.clusterScoped.length > 0}:${item.takeovers.length > 0}`
  const selectedRisk = chosen[0] && riskKey(chosen[0])
  const toggle = (item: Request) => setSelected(current => current.includes(item.planId) ? current.filter(id => id !== item.planId) : current.length < 20 && (!selectedRisk || selectedRisk === riskKey(item)) ? [...current, item.planId] : current)
  async function approve(items: Request[]) {
    if (!items.length) return
    setBusy(true); setError(null); setResults(null)
    try {
      const result = await apiPost<Result>("/api/v1/approval-inbox/batch", { items: items.map(item => ({ planId: item.planId, planDigest: item.planDigest })), comment: comment.trim() })
      setResults(result); setSelected([]); refresh()
    } catch (cause) { setError(cause) }
    finally { setBusy(false) }
  }
  return <>
    <PageHeading title="Approvals" description="Plans waiting for your review across projects and application groups." actions={<Button variant="outline" onClick={refresh} disabled={loading || busy}>Refresh</Button>} />
    <section aria-label="Filter approval requests" className="mb-5 grid gap-3 rounded-2xl border bg-card p-4 sm:grid-cols-2 xl:grid-cols-5">
      <Filter label="Project" value={projectId} onChange={value => { filter(setProjectId, value); setApplicationId("") }} options={[["", "All projects"], ...projects.map(project => [project.id, project.name])]} />
      <Filter label="Application" value={applicationId} onChange={value => filter(setApplicationId, value)} options={[["", "All applications"], ...applications.map(([id, name]) => [id, name])]} />
      <Filter label="Risk" value={risk} onChange={value => filter(setRisk, value)} options={[["", "All risks"], ["deletion", "Deletion"], ["cluster", "Cluster scoped"], ["takeover", "Takeover"], ["sync", "Other changes"]]} />
      <Filter label="Age" value={age} onChange={value => filter(setAge, value)} options={[["", "Any age"], ["24h", "Last 24 hours"], ["7d", "Last 7 days"], ["older7d", "Older than 7 days"]]} />
      <Filter label="Expiry" value={expiry} onChange={value => filter(setExpiry, value)} options={[["", "Any expiry"], ["1h", "Within 1 hour"], ["24h", "Within 24 hours"], ["later", "After 24 hours"]]} />
    </section>
    {error != null && <div role="alert" className="mb-5 flex flex-wrap items-center gap-3 rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm"><span>{errorMessage(error)}</span><ErrorDetailsButton error={error} /><Button variant="outline" size="sm" onClick={refresh}>Try again</Button></div>}
    {results && <div role="status" className="mb-5 rounded-xl border bg-card p-4 text-sm"><strong>{results.succeeded} approved · {results.failed} failed</strong>{results.failed > 0 && <ul className="mt-2 space-y-1 text-xs text-destructive">{results.results.filter(item => !item.success).map(item => <li key={item.planId}>{item.planId.slice(0, 8)}: {item.error || `HTTP ${item.status}`}</li>)}</ul>}</div>}
    <div className="mb-3 flex flex-wrap items-center justify-between gap-3"><p className="text-xs text-muted-foreground">{loading ? "Loading requests…" : `${inbox?.total ?? 0} request${inbox?.total === 1 ? "" : "s"} · page ${page}`}</p><span className="text-xs text-muted-foreground">Review each plan before approving.</span></div>
    {loading ? <div role="status" aria-label="Loading approval requests" className="space-y-3">{[0, 1, 2].map(value => <div key={value} className="h-32 animate-pulse rounded-2xl border bg-card motion-reduce:animate-none" />)}</div> : error ? null : inbox?.items.length === 0 ? <div className="rounded-2xl border bg-card px-6 py-12 text-center"><h2 className="text-base font-semibold">No requests to review</h2><p className="mt-2 text-sm text-muted-foreground">Try a different filter or check back when a new plan needs your approval.</p></div> : <div className="space-y-3">{inbox?.items.map(item => <article key={item.planId} className="rounded-2xl border bg-card p-4 sm:p-5">
      <div className="flex items-start gap-3"><input type="checkbox" aria-label={`Select ${item.applicationName}`} checked={selected.includes(item.planId)} disabled={busy || (!!selectedRisk && selectedRisk !== riskKey(item)) || (selected.length >= 20 && !selected.includes(item.planId))} onChange={() => toggle(item)} className="mt-1 size-4 accent-primary" /><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><Link href={`/applications/${item.applicationId}?tab=changes`} className="font-semibold hover:text-primary">{item.applicationName}</Link>{item.applicationGroupId && <span className="rounded-full bg-muted px-2 py-0.5 text-[10px] text-muted-foreground">Group child</span>}<span className="text-xs text-muted-foreground">{item.projectName}</span></div><p className="mt-1 text-xs text-muted-foreground">{item.changeCount} changes · {item.approvedApprovals}/{item.requiredApprovals} approvals · created {new Date(item.createdAt).toLocaleString()} · expires {new Date(item.expiresAt).toLocaleString()}</p></div><span className="rounded-full border px-2 py-1 text-[11px] capitalize">{item.kind || "sync"}</span></div>
      {(item.deletions.length > 0 || item.clusterScoped.length > 0 || item.takeovers.length > 0) && <div className="mt-4 grid gap-3 sm:grid-cols-2">{item.deletions.length > 0 && <Risk title={`${item.deletions.length} deletion${item.deletions.length === 1 ? "" : "s"}`} items={item.deletions} tone="rose" />}{item.clusterScoped.length > 0 && <Risk title={`${item.clusterScoped.length} cluster scoped change${item.clusterScoped.length === 1 ? "" : "s"}`} items={item.clusterScoped} tone="amber" />}{item.takeovers.length > 0 && <Risk title={`${item.takeovers.length} takeover${item.takeovers.length === 1 ? "" : "s"}`} items={item.takeovers} tone="amber" />}</div>}
      <div className="mt-4 flex justify-end"><Link href={`/applications/${item.applicationId}?tab=changes`} className="text-xs font-medium text-primary hover:underline">Open plan →</Link></div>
    </article>)}</div>}
    {inbox && inbox.total > pageSize && <nav aria-label="Approval pages" className="mt-5 flex items-center justify-end gap-3"><Button variant="outline" size="sm" disabled={page === 1 || loading} onClick={() => { setLoading(true); setPage(value => value - 1) }}>Previous</Button><span className="text-xs text-muted-foreground">{page} of {Math.ceil(inbox.total / pageSize)}</span><Button variant="outline" size="sm" disabled={!inbox.hasMore || loading} onClick={() => { setLoading(true); setPage(value => value + 1) }}>Next</Button></nav>}
    {chosen.length > 0 && <section aria-label="Approve selected plans" className="sticky bottom-4 z-10 mt-6 flex flex-wrap items-center gap-3 rounded-2xl border bg-card p-4 shadow-lg"><p className="min-w-0 flex-1 text-sm"><strong>{chosen.length} selected</strong><span className="ml-2 text-xs text-muted-foreground">Same approval kind and risk profile. Every plan is rechecked independently.</span></p><Input aria-label="Approval comment" value={comment} maxLength={1000} onChange={event => setComment(event.target.value)} placeholder="Comment (optional)" className="w-full sm:w-64" /><ConfirmDisclosure trigger={`Review ${chosen.length} approval${chosen.length === 1 ? "" : "s"}`} triggerVariant="outline" title="Approve these exact plans?" description="Each plan will be checked again for current authorization, expiry, and changes. Successful approvals remain recorded if another plan fails." confirmLabel="Approve selected plans" disabled={busy} onConfirm={() => approve(chosen)}><ul className="max-h-48 space-y-2 overflow-y-auto text-xs">{chosen.map(item => <li key={item.planId}><strong>{item.applicationName}</strong> · {item.changeCount} changes · {item.deletions.length} deletions · {item.clusterScoped.length} cluster scoped · {item.takeovers.length} takeovers</li>)}</ul>{comment.trim() && <p className="mt-3 text-xs text-muted-foreground">Comment: {comment.trim()}</p>}</ConfirmDisclosure></section>}
  </>
}
function Filter({ label, value, onChange, options }: { label: string; value: string; onChange: (value: string) => void; options: string[][] }) { return <label className="space-y-1.5 text-xs font-medium">{label}<select value={value} onChange={event => onChange(event.target.value)} className="mt-1.5 block h-9 w-full rounded-lg border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring">{options.map(([key, text]) => <option key={key} value={key}>{text}</option>)}</select></label> }
function Risk({ title, items, tone }: { title: string; items: string[]; tone: "rose" | "amber" }) { return <div className={`rounded-xl border p-3 text-xs ${tone === "rose" ? "border-rose-500/20 bg-rose-500/5" : "border-amber-500/20 bg-amber-500/5"}`}><p className="font-semibold">{title}</p><ul className="mt-2 max-h-24 space-y-1 overflow-y-auto text-muted-foreground">{items.map((name, index) => <li key={`${name}-${index}`} className="break-all">{name}</li>)}</ul></div> }
