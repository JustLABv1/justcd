"use client"

import Link from "next/link"
import { useParams } from "next/navigation"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { DataGridList } from "@/components/data-grid-table"
import { EmptyState, PageHeading, Panel, StatusBadge } from "@/components/ui-kit"
import { APIError, api, apiPost, errorMessage } from "@/lib/api"
import type { Application, Change, ListResponse, ManagedResource, Operation, PlanRecord, Project } from "@/lib/types"

export default function ApplicationDetailPage() {
  const { applicationID } = useParams<{ applicationID: string }>()
  const [application, setApplication] = useState<Application | null>(null)
  const [project, setProject] = useState<Project | null>(null)
  const [plans, setPlans] = useState<PlanRecord[]>([])
  const [activePlan, setActivePlan] = useState<PlanRecord | null>(null)
  const [resources, setResources] = useState<ManagedResource[]>([])
  const [operations, setOperations] = useState<Operation[]>([])
  const [approvalId, setApprovalId] = useState("")
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState("")
  const [error, setError] = useState("")

  async function loadData() {
    const app = await api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`)
    const [projectList, planList, inventory, operationList] = await Promise.all([
      api<ListResponse<Project>>("/api/v1/projects"),
      api<ListResponse<PlanRecord>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`),
      api<ListResponse<ManagedResource>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/resources`),
      api<ListResponse<Operation>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/operations`),
    ])
    setApplication(app)
    setProject(projectList.items.find((item) => item.id === app.projectId) ?? null)
    setPlans(planList.items)
    setActivePlan((current) => current && planList.items.some((item) => item.id === current.id) ? current : planList.items[0] ?? null)
    setResources(inventory.items)
    setOperations(operationList.items)
  }

  useEffect(() => {
    let active = true
    Promise.resolve().then(loadData).catch((cause) => active && setError(errorMessage(cause))).finally(() => active && setLoading(false))
    return () => { active = false }
  // loadData is intentionally tied to this resource identifier only.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [applicationID])

  async function createPlan() {
    setBusy(true); setError(""); setNotice(""); setApprovalId("")
    try {
      const plan = await apiPost<PlanRecord>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`)
      setActivePlan(plan)
      setPlans((current) => [plan, ...current.filter((item) => item.id !== plan.id)])
      setNotice(plan.plan.changes.length ? `Plan ready: ${plan.plan.changes.length} change${plan.plan.changes.length === 1 ? "" : "s"} to review.` : "The application already matches its Git revision.")
      await refreshSummary()
    } catch (cause) { setError(errorMessage(cause)) } finally { setBusy(false) }
  }

  async function refreshSummary() {
    try {
      const [inventory, operationList] = await Promise.all([
        api<ListResponse<ManagedResource>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/resources`),
        api<ListResponse<Operation>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/operations`),
      ])
      setResources(inventory.items); setOperations(operationList.items)
    } catch { /* the primary plan result remains useful when a secondary panel is unavailable */ }
  }

  async function approvePlan() {
    if (!activePlan) return
    const deletes = activePlan.plan.changes.filter((change) => change.kind === "delete")
    const privileged = activePlan.plan.changes.filter((change) => change.identity.clusterScoped)
    const summary = deletes.length ? `This approves deletion of ${deletes.length} resource${deletes.length === 1 ? "" : "s"}:\n\n${deletes.map((change) => `${change.identity.kind} ${change.identity.namespace}/${change.identity.name}`).join("\n")}\n\nThe approval applies only to this plan and expires shortly.` : `This approves ${privileged.length} cluster-scoped change${privileged.length === 1 ? "" : "s"} in this exact plan. It expires shortly.`
    if (!window.confirm(summary)) return
    setBusy(true); setError(""); setNotice("")
    try {
      const approval = await apiPost<{ id: string; expiresAt: string }>(`/api/v1/plans/${encodeURIComponent(activePlan.id)}/approvals`)
      setApprovalId(approval.id)
      setNotice(`Owner approval recorded. It expires at ${new Date(approval.expiresAt).toLocaleTimeString()}.`)
    } catch (cause) { setError(errorMessage(cause)) } finally { setBusy(false) }
  }

  async function applyPlan() {
    if (!activePlan) return
    setBusy(true); setError(""); setNotice("")
    try {
      const result = await apiPost<{ operation: Operation }>(`/api/v1/plans/${encodeURIComponent(activePlan.id)}/apply`, { approvalId })
      setNotice(result.operation.message || "Sync completed.")
      setApprovalId("")
      await loadData()
    } catch (cause) {
      if (cause instanceof APIError && cause.status === 409 && typeof cause.payload === "object" && cause.payload && "plan" in cause.payload) {
        const refreshed = (cause.payload as { plan: PlanRecord }).plan
        setActivePlan(refreshed); setPlans((current) => [refreshed, ...current.filter((item) => item.id !== refreshed.id)])
        setApprovalId("")
        setError("The live cluster changed after review. A fresh plan is shown below; review it before syncing.")
      } else setError(errorMessage(cause))
    } finally { setBusy(false); await refreshSummary() }
  }

  const changeCounts = activePlan?.plan.changes.reduce((acc, item) => ({ ...acc, [item.kind]: acc[item.kind] + 1 }), { create: 0, update: 0, delete: 0 })
  const canDeploy = project?.role === "owner" || project?.role === "deployer"
  const canApprove = project?.role === "owner"

  return <>
    <div className="mb-4"><Link href={application ? `/projects/${application.projectId}` : "/projects"} className="text-[11px] text-muted-foreground hover:text-foreground">← {project?.name ?? "Projects"}</Link></div>
    <PageHeading eyebrow={project?.name ?? "Application"} title={application?.name ?? (loading ? "Loading application…" : "Application not found")} description={application ? `${application.renderer} · ${application.manifestPath} · ${application.revision}` : ""} actions={<><Button variant="outline" onClick={() => void createPlan()} disabled={!application || !canDeploy || busy}><span aria-hidden="true">↻</span> {busy ? "Working…" : "Refresh plan"}</Button><StatusBadge status={application?.health ?? "unknown"} /></>} />
    {error && <div role="alert" className="mb-4 rounded-lg border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-destructive">{error}</div>}
    {notice && <div role="status" className="mb-4 rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-200">{notice}</div>}
    {!loading && application && <>
      <div className="mb-6 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <InfoCard label="Target" value={application.namespaces.map((binding) => binding.namespace).join(", ")} note={application.namespaces.length === 1 ? "Namespace-scoped credentials" : `${application.namespaces.length} bound namespaces`} />
        <InfoCard label="Git source" value={application.revision} note={`Rendered from ${application.renderer}`} mono />
        <InfoCard label="Last synced" value={application.lastSyncedRevision || "Not synced yet"} note={application.lastCheckedAt ? `Checked ${new Date(application.lastCheckedAt).toLocaleString()}` : "No sync operation recorded"} mono />
        <InfoCard label="Policy" value={application.syncPolicy === "auto-safe" ? "Auto-safe" : "Manual"} note={application.syncPolicy === "auto-safe" ? `Checks every ${application.pollSeconds}s; stops before deletion` : "Every sync is user initiated"} />
      </div>

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px]">
        <div className="space-y-5">
          <Panel title="Plan & diff" description="A reviewed plan is a snapshot of the desired Git commit and live cluster state." action={plans.length > 0 && <select aria-label="Select plan" className="h-8 max-w-[210px] rounded-md border bg-background px-2 text-xs" value={activePlan?.id ?? ""} onChange={(event) => { setApprovalId(""); setActivePlan(plans.find((plan) => plan.id === event.target.value) ?? null) }}>{plans.map((plan) => <option value={plan.id} key={plan.id}>{new Date(plan.createdAt).toLocaleString()} · {plan.status}</option>)}</select>}>
            {!activePlan ? <EmptyState title="No review plan yet" description="Build a plan to render Git manifests and compare them with live, app-owned resources." /> : <div className="p-4 sm:p-5">
              <div className="mb-4 flex flex-wrap items-center gap-2"><StatusBadge status={activePlan.status} /><span className="font-mono text-[10px] text-muted-foreground">{activePlan.plan.revision.slice(0, 12)}</span><span className="text-[10px] text-muted-foreground">· expires {new Date(activePlan.expiresAt).toLocaleTimeString()}</span><span className="ml-auto font-mono text-[9px] text-muted-foreground">{activePlan.plan.digest.slice(0, 16)}</span></div>
              <div className="mb-5 grid grid-cols-3 gap-2"><ChangeCount label="Create" count={changeCounts?.create ?? 0} color="text-emerald-700 bg-emerald-50 dark:text-emerald-300 dark:bg-emerald-950/40" /><ChangeCount label="Update" count={changeCounts?.update ?? 0} color="text-blue-700 bg-blue-50 dark:text-blue-300 dark:bg-blue-950/40" /><ChangeCount label="Delete" count={changeCounts?.delete ?? 0} color="text-rose-700 bg-rose-50 dark:text-rose-300 dark:bg-rose-950/40" /></div>
              {activePlan.plan.changes.length ? <div className="space-y-3">{activePlan.plan.changes.map((change, index) => <DiffCard key={`${change.identity.apiVersion}/${change.identity.kind}/${change.identity.namespace}/${change.identity.name}/${index}`} change={change} />)}</div> : <div className="rounded-lg border border-dashed px-4 py-8 text-center"><span className="text-emerald-600">✓</span><p className="mt-2 text-sm font-medium">No changes to apply</p><p className="mt-1 text-xs text-muted-foreground">Git and the managed cluster fields are in sync.</p></div>}
              {activePlan.plan.requiresApproval && <div className="mt-5 rounded-lg border border-amber-300/70 bg-amber-50/70 p-3.5 text-xs leading-5 text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><strong>Owner approval required.</strong> {changeCounts?.delete ? "Deletion is never automatic. Review the exact targets above; an owner must approve this plan before the sync button is enabled." : "Cluster-scoped changes need explicit owner approval."}</div>}
              <div className="mt-5 flex flex-wrap justify-end gap-2 border-t pt-4">
                {activePlan.plan.requiresApproval && <Button variant="outline" onClick={() => void approvePlan()} disabled={busy || !canApprove || activePlan.status !== "current"}>{approvalId ? "Approved ✓" : "Approve reviewed changes"}</Button>}
                <Button onClick={() => void applyPlan()} disabled={busy || !canDeploy || activePlan.status !== "current" || activePlan.plan.changes.length === 0 || (activePlan.plan.requiresApproval && !approvalId)}>{busy ? "Syncing…" : activePlan.plan.requiresApproval ? "Apply approved plan" : "Sync application"}</Button>
              </div>
              {!canApprove && activePlan.plan.requiresApproval && <p className="mt-2 text-right text-[10px] text-muted-foreground">Only a project owner can approve this plan.</p>}
            </div>}
          </Panel>

          <Panel title="Managed components" description="JustCD tracks only resources that it created and owns on the cluster.">
            {resources.length ? <div className="p-4"><DataGridList rows={resources} columns={[
              { id: "resource", title: "Resource", cell: (item) => <span className="font-medium">{item.identity.name}<span className="mt-0.5 block text-[10px] font-normal text-muted-foreground">{item.identity.kind} · {item.identity.apiVersion}</span></span> },
              { id: "namespace", title: "Namespace", cell: (item) => <span className="text-xs text-muted-foreground">{item.identity.namespace || "cluster scope"}</span> },
              { id: "cluster", title: "Cluster", cell: (item) => <span className="font-mono text-[10px] text-muted-foreground">{item.identity.clusterId?.slice(0, 8)}</span> },
              { id: "version", title: "Live version", cell: (item) => <span className="font-mono text-[10px] text-muted-foreground">{item.resourceVersion}</span> },
            ]} empty="A component inventory appears after the first successful sync." /></div> : <EmptyState title="No managed components yet" description="After the first successful sync, this inventory shows every resource owned by this application." />}
          </Panel>
        </div>

        <div className="space-y-5">
          <Panel title="Recent operations" description="Sync history and result state">
            {operations.length ? <div className="divide-y">{operations.slice(0, 8).map((operation) => <div key={operation.id} className="px-5 py-3.5"><div className="flex items-center justify-between gap-3"><span className="text-xs font-medium">{operation.status === "succeeded" ? "Sync completed" : operation.status === "failed" ? "Sync failed" : "Sync running"}</span><StatusBadge status={operation.status} /></div><p className="mt-1 line-clamp-2 text-[10px] leading-4 text-muted-foreground">{operation.message || "Sync operation"}</p><p className="mt-1.5 text-[9px] text-muted-foreground">{new Date(operation.startedAt).toLocaleString()}</p></div>)}</div> : <EmptyState title="No syncs yet" description="Operations will be recorded here with the actor and resulting status." />}
          </Panel>
          <Panel title="Application source" description="Configuration stored in JustCD"><div className="space-y-3 p-5 text-xs"><KeyValue label="Manifest path" value={application.manifestPath} mono /><KeyValue label="Renderer" value={application.renderer} /><KeyValue label="Cluster" value={application.clusterId.slice(0, 12)} mono /><KeyValue label="Poll interval" value={`${application.pollSeconds} seconds`} /><Link href={`/projects/${application.projectId}`} className="mt-1 inline-block text-xs font-medium text-primary hover:underline">Open project →</Link></div></Panel>
        </div>
      </div>
    </>}
  </>
}

function InfoCard({ label, value, note, mono = false }: { label: string; value: string; note: string; mono?: boolean }) {
  return <div className="min-w-0 rounded-xl border bg-card p-4"><p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</p><p className={`mt-2 truncate text-sm font-semibold ${mono ? "font-mono text-xs" : ""}`}>{value}</p><p className="mt-1 truncate text-[10px] text-muted-foreground">{note}</p></div>
}

function ChangeCount({ label, count, color }: { label: string; count: number; color: string }) {
  return <div className={`rounded-lg px-3 py-2 ${color}`}><span className="text-lg font-semibold tabular-nums">{count}</span><span className="ml-2 text-[10px] font-medium">{label}</span></div>
}

function DiffCard({ change }: { change: Change }) {
  const name = `${change.identity.kind} ${change.identity.namespace ? `${change.identity.namespace}/` : ""}${change.identity.name}`
  const color = change.kind === "delete" ? "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-900 dark:bg-rose-950/25 dark:text-rose-300" : change.kind === "create" ? "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/25 dark:text-emerald-300" : "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/25 dark:text-blue-300"
  return <article className="overflow-hidden rounded-lg border">
    <div className="flex items-center gap-2 border-b bg-muted/20 px-3 py-2"><span className={`rounded border px-1.5 py-0.5 text-[9px] font-semibold uppercase ${color}`}>{change.kind}</span><span className="truncate text-xs font-medium">{name}</span>{change.identity.clusterScoped && <span className="ml-auto rounded-full border px-2 py-0.5 text-[9px] text-amber-700">cluster-wide</span>}</div>
    <div className="grid divide-y md:grid-cols-2 md:divide-x md:divide-y-0">
      <ManifestBlock title={change.kind === "create" ? "Current" : "Live before"} value={change.before} empty={change.kind === "create" ? "Not present" : "Unavailable"} />
      <ManifestBlock title={change.kind === "delete" ? "After sync" : "Desired after"} value={change.after} empty={change.kind === "delete" ? "Resource removed" : "Unavailable"} />
    </div>
  </article>
}

function ManifestBlock({ title, value, empty }: { title: string; value: unknown; empty: string }) {
  const body = value === undefined || value === null ? empty : JSON.stringify(value, null, 2)
  return <details className="group min-w-0 px-3 py-2.5" open={body !== empty && body.length < 1600}>
    <summary className="cursor-pointer list-none text-[9px] font-semibold uppercase tracking-wider text-muted-foreground"><span className="inline-block transition-transform group-open:rotate-90">›</span> {title}</summary>
    <pre className="mt-2 max-h-64 overflow-auto rounded bg-muted/50 p-2 font-mono text-[9px] leading-4 text-foreground/80">{body}</pre>
  </details>
}

function KeyValue({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="flex items-start justify-between gap-3"><span className="text-muted-foreground">{label}</span><span className={`max-w-[65%] truncate text-right ${mono ? "font-mono text-[10px]" : "font-medium"}`}>{value}</span></div>
}
