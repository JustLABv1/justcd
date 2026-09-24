"use client"

import Link from "next/link"
import { useParams } from "next/navigation"
import { useEffect, useMemo, useState } from "react"
import { Tabs } from "@base-ui/react/tabs"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { DataGridList } from "@/components/data-grid-table"
import { FormSelect } from "@/components/ui/form-select"
import { ResourceMap } from "@/components/resource-map"
import { Textarea } from "@/components/ui/textarea"
import { EmptyState, PageHeading, Panel, StatusBadge } from "@/components/ui-kit"
import { APIError, api, apiDelete, apiPost, errorMessage } from "@/lib/api"
import { diffJsonLines } from "@/lib/line-diff"
import type { Application, Change, FieldExclusion, Identity, IgnoreRule, ListResponse, ManagedResource, Operation, PlanRecord, Project, ResourceTopology } from "@/lib/types"

function diffId(identity: Identity) {
  return `diff-${[identity.clusterId ?? "", identity.apiVersion, identity.kind, identity.namespace, identity.name].map(encodeURIComponent).join("-")}`
}

function sameIdentity(a: Identity, b: Identity) { return diffId(a) === diffId(b) }

function safeIgnorePath(pointer: string) {
  const parts = pointer.split("/").slice(1).map((part) => part.replaceAll("~1", "/").replaceAll("~0", "~"))
  if (parts.some((part) => !part || /^\d+$/.test(part))) return false
  if (parts[0] === "apiVersion" || parts[0] === "kind") return false
  if (parts[0] === "metadata") {
    if (parts.length === 1 || ["name", "namespace", "uid", "resourceVersion", "generation", "managedFields"].includes(parts[1])) return false
    if (["labels", "annotations"].includes(parts[1]) && parts.length === 2) return false
    if (parts[1] === "labels" && parts[2] === "justcd.io/application-id") return false
  }
  return true
}

export default function ApplicationDetailPage() {
  const { applicationID } = useParams<{ applicationID: string }>()
  const [application, setApplication] = useState<Application | null>(null)
  const [project, setProject] = useState<Project | null>(null)
  const [plans, setPlans] = useState<PlanRecord[]>([])
  const [activePlan, setActivePlan] = useState<PlanRecord | null>(null)
  const [resources, setResources] = useState<ManagedResource[]>([])
  const [topology, setTopology] = useState<ResourceTopology | null>(null)
  const [topologyRefreshing, setTopologyRefreshing] = useState(false)
  const [operations, setOperations] = useState<Operation[]>([])
  const [ignoreRules, setIgnoreRules] = useState<IgnoreRule[]>([])
  const [selectionDraft, setSelectionDraft] = useState<{ planId: string; resources: Identity[]; fields: FieldExclusion[] } | null>(null)
  const [ignoreReasons, setIgnoreReasons] = useState<Record<string, string>>({})
  const [approvalId, setApprovalId] = useState("")
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState("")
  const [error, setError] = useState("")
  const [activeTab, setActiveTab] = useState("overview")

  const selectionDraftForPlan = activePlan && selectionDraft?.planId === activePlan.id ? selectionDraft : null
  const selectionResources = selectionDraftForPlan?.resources ?? activePlan?.plan.selection?.resources ?? []
  const selectionFields = selectionDraftForPlan?.fields ?? activePlan?.plan.selection?.fields ?? []
  function setSelectionResources(next: Identity[] | ((current: Identity[]) => Identity[])) {
    if (!activePlan) return
    setSelectionDraft((draft) => {
      const base = draft?.planId === activePlan.id ? draft : { planId: activePlan.id, resources: activePlan.plan.selection?.resources ?? [], fields: activePlan.plan.selection?.fields ?? [] }
      return { ...base, resources: typeof next === "function" ? next(base.resources) : next }
    })
  }
  function setSelectionFields(next: FieldExclusion[] | ((current: FieldExclusion[]) => FieldExclusion[])) {
    if (!activePlan) return
    setSelectionDraft((draft) => {
      const base = draft?.planId === activePlan.id ? draft : { planId: activePlan.id, resources: activePlan.plan.selection?.resources ?? [], fields: activePlan.plan.selection?.fields ?? [] }
      return { ...base, fields: typeof next === "function" ? next(base.fields) : next }
    })
  }

  useEffect(() => {
    const readTab = () => {
      const value = new URLSearchParams(window.location.search).get("tab")
      setActiveTab(["overview", "topology", "changes", "components", "activity"].includes(value ?? "") ? value! : "overview")
    }
    readTab()
    window.addEventListener("popstate", readTab)
    return () => window.removeEventListener("popstate", readTab)
  }, [])

  function selectTab(value: string) {
    setActiveTab(value)
    const url = new URL(window.location.href)
    if (value === "overview") url.searchParams.delete("tab")
    else url.searchParams.set("tab", value)
    window.history.pushState(null, "", url)
  }

  async function loadData() {
    const app = await api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`)
    const [projectList, planList, inventory, operationList, topologyResult, ignoreRuleList] = await Promise.all([
      api<ListResponse<Project>>("/api/v1/projects"),
      api<ListResponse<PlanRecord>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`),
      api<ListResponse<ManagedResource>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/resources`),
      api<ListResponse<Operation>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/operations`),
      api<ResourceTopology>(`/api/v1/applications/${encodeURIComponent(applicationID)}/topology`).catch(() => null),
      api<ListResponse<IgnoreRule>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-rules`),
    ])
    setApplication(app)
    setProject(projectList.items.find((item) => item.id === app.projectId) ?? null)
    setPlans(planList.items)
    setActivePlan((current) => current ? planList.items.find((item) => item.id === current.id) ?? planList.items[0] ?? null : planList.items[0] ?? null)
    setResources(inventory.items)
    setTopology(topologyResult)
    setOperations(operationList.items)
    setIgnoreRules(ignoreRuleList.items)
  }

  useEffect(() => {
    let active = true
    Promise.resolve().then(loadData).catch((cause) => active && setError(errorMessage(cause))).finally(() => active && setLoading(false))
    return () => { active = false }
  // loadData is intentionally tied to this resource identifier only.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [applicationID])

  const hasPendingOperation = operations.some((operation) => operation.status === "queued" || operation.status === "running")
  useEffect(() => {
    if (!busy && !hasPendingOperation) return
    const timer = window.setInterval(() => {
      void Promise.all([
        api<ListResponse<Operation>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/operations`),
        api<ListResponse<ManagedResource>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/resources`),
        api<ListResponse<PlanRecord>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`),
        api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`),
      ]).then(([nextOperations, nextResources, nextPlans, nextApplication]) => {
        setOperations(nextOperations.items)
        setResources(nextResources.items)
        setPlans(nextPlans.items)
        setApplication(nextApplication)
        setActivePlan(current => current ? nextPlans.items.find((plan) => plan.id === current.id) ?? nextPlans.items[0] ?? null : nextPlans.items[0] ?? null)
      }).catch(() => {})
    }, 1500)
    return () => window.clearInterval(timer)
  }, [applicationID, busy, hasPendingOperation])

  async function createPlan() {
    setBusy(true); setError(""); setNotice(""); setApprovalId("")
    try {
      const plan = await apiPost<PlanRecord>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`)
      setActivePlan(plan)
      setPlans((current) => [plan, ...current.filter((item) => item.id !== plan.id)])
      setNotice(plan.plan.changes.length ? `Plan ready: ${plan.plan.changes.length} change${plan.plan.changes.length === 1 ? "" : "s"} to review.` : "The application already matches its Git revision.")
      selectTab("changes")
      await refreshSummary()
    } catch (cause) { if (!acceptRefreshedPlan(cause)) setError(errorMessage(cause)) } finally { setBusy(false) }
  }

  function acceptRefreshedPlan(cause: unknown) {
    if (!(cause instanceof APIError) || cause.status !== 409 || typeof cause.payload !== "object" || !cause.payload || !("plan" in cause.payload)) return false
    const refreshed = (cause.payload as { plan: PlanRecord }).plan
    setActivePlan(refreshed)
    setPlans((current) => [refreshed, ...current.filter((item) => item.id !== refreshed.id)])
    setApprovalId("")
    setError("The plan changed since it was reviewed. A fresh snapshot is shown; review it before continuing.")
    return true
  }

  function toggleResourceExclusion(identity: Identity) {
    setSelectionResources((current) => current.some((item) => sameIdentity(item, identity)) ? current.filter((item) => !sameIdentity(item, identity)) : [...current, identity])
    setApprovalId("")
  }

  function toggleFieldExclusion(identity: Identity, path: string) {
    setSelectionFields((current) => current.some((item) => sameIdentity(item.identity, identity) && item.path === path)
      ? current.filter((item) => !(sameIdentity(item.identity, identity) && item.path === path))
      : [...current, { identity, path }])
    setApprovalId("")
  }

  const selectionDirty = (() => {
    if (!activePlan) return false
    const originalResources = activePlan.plan.selection?.resources ?? []
    const originalFields = activePlan.plan.selection?.fields ?? []
    const resources = (items: Identity[]) => items.map(diffId).sort().join("\n")
    const fields = (items: FieldExclusion[]) => items.map((item) => `${diffId(item.identity)}\n${item.path}`).sort().join("\n")
    return resources(selectionResources) !== resources(originalResources) || fields(selectionFields) !== fields(originalFields)
  })()

  async function savePlanSelection() {
    if (!activePlan || !selectionDirty) return
    setBusy(true); setError(""); setNotice(""); setApprovalId("")
    try {
      const plan = await apiPost<PlanRecord>(`/api/v1/plans/${encodeURIComponent(activePlan.id)}/selections`, { resources: selectionResources, fields: selectionFields })
      setActivePlan(plan)
      setPlans((current) => [plan, ...current.filter((item) => item.id !== plan.id)])
      setNotice(`A new immutable plan is ready with ${plan.plan.changes.length} change${plan.plan.changes.length === 1 ? "" : "s"} to apply.`)
    } catch (cause) { if (!acceptRefreshedPlan(cause)) setError(errorMessage(cause)) }
    finally { setBusy(false) }
  }

  async function saveIgnoreRule(identity: Identity, path: string, reason: string) {
    const cleanReason = reason.trim()
    if (cleanReason.length < 5) { setError("Add a short reason before saving a permanent ignore rule."); return }
    setBusy(true); setError(""); setNotice("")
    try {
      await apiPost<IgnoreRule>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-rules`, { identity, path, reason: cleanReason })
      const plan = await apiPost<PlanRecord>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`)
      await loadData()
      setActivePlan(plan)
      setNotice(`Permanent ignore rule saved. Existing plans were invalidated; the new plan includes the updated rule set.`)
    } catch (cause) { setError(errorMessage(cause)) }
    finally { setBusy(false) }
  }

  async function removeIgnoreRule(rule: IgnoreRule) {
    setBusy(true); setError(""); setNotice("")
    try {
      await apiDelete<void>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-rules/${encodeURIComponent(rule.id)}`)
      const plan = await apiPost<PlanRecord>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`)
      await loadData()
      setActivePlan(plan)
      setNotice("Ignore rule removed. A fresh plan is ready for review.")
    } catch (cause) { setError(errorMessage(cause)) }
    finally { setBusy(false) }
  }

  async function refreshSummary() {
    try {
      const [inventory, operationList, topologyResult, planList, ruleList] = await Promise.all([
        api<ListResponse<ManagedResource>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/resources`),
        api<ListResponse<Operation>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/operations`),
        api<ResourceTopology>(`/api/v1/applications/${encodeURIComponent(applicationID)}/topology`).catch(() => null),
        api<ListResponse<PlanRecord>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`),
        api<ListResponse<IgnoreRule>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-rules`),
      ])
      setResources(inventory.items); setOperations(operationList.items); setTopology(topologyResult); setPlans(planList.items); setIgnoreRules(ruleList.items)
      setActivePlan((current) => current ? planList.items.find((plan) => plan.id === current.id) ?? current : planList.items[0] ?? null)
    } catch { /* the primary plan result remains useful when a secondary panel is unavailable */ }
  }

  async function approvePlan() {
    if (!activePlan || selectionDirty) return
    const deletes = activePlan.plan.changes.filter((change) => change.kind === "delete")
    const privileged = activePlan.plan.changes.filter((change) => change.identity.clusterScoped)
    const summary = deletes.length ? `This approves deletion of ${deletes.length} resource${deletes.length === 1 ? "" : "s"}:\n\n${deletes.map((change) => `${change.identity.kind} ${change.identity.namespace}/${change.identity.name}`).join("\n")}\n\nThe approval applies only to this plan and expires shortly.` : `This approves ${privileged.length} cluster-scoped change${privileged.length === 1 ? "" : "s"} in this exact plan. It expires shortly.`
    if (!window.confirm(summary)) return
    setBusy(true); setError(""); setNotice("")
    try {
      const approval = await apiPost<{ id: string; expiresAt: string }>(`/api/v1/plans/${encodeURIComponent(activePlan.id)}/approvals`)
      setApprovalId(approval.id)
      setNotice(`Owner approval recorded. It expires at ${new Date(approval.expiresAt).toLocaleTimeString()}.`)
    } catch (cause) { if (!acceptRefreshedPlan(cause)) setError(errorMessage(cause)) } finally { setBusy(false) }
  }

  async function applyPlan() {
    if (!activePlan || selectionDirty) return
    setBusy(true); setError(""); setNotice("")
    try {
      const result = await apiPost<{ operation: Operation }>(`/api/v1/plans/${encodeURIComponent(activePlan.id)}/apply`, { approvalId })
      setNotice(result.operation.message || "Sync queued.")
      setApprovalId("")
      await loadData()
    } catch (cause) {
      if (!acceptRefreshedPlan(cause)) setError(errorMessage(cause))
    } finally { setBusy(false); await refreshSummary() }
  }

  const changeCounts = activePlan?.plan.changes.reduce((acc, item) => ({ ...acc, [item.kind]: acc[item.kind] + 1 }), { create: 0, update: 0, delete: 0 })
  const canDeploy = project?.role === "owner" || project?.role === "deployer"
  const canApprove = project?.role === "owner"
  const latestOperation = operations[0]
  const visibleOperation = operations.find((operation) => operation.status === "queued" || operation.status === "running") ?? (latestOperation?.status === "failed" ? latestOperation : null)
  const latestPlan = plans[0]
  const latestCounts = latestPlan?.plan.changes.reduce((counts, change) => ({ ...counts, [change.kind]: counts[change.kind] + 1 }), { create: 0, update: 0, delete: 0 })
  const resourceKinds = Object.entries((topology?.nodes ?? []).reduce<Record<string, number>>((counts, node) => {
    counts[node.identity.kind] = (counts[node.identity.kind] ?? 0) + 1
    return counts
  }, {})).sort((a, b) => b[1] - a[1])

  return <>
    <PageHeading title={application?.name ?? (loading ? "Loading application…" : "Application not found")} description={application ? `${application.renderer} · ${application.manifestPath} · ${application.revision}` : ""} actions={<><StatusBadge status={application?.health ?? "unknown"} /><Button variant="outline" onClick={() => void createPlan()} disabled={!application || !canDeploy || busy}><span aria-hidden="true">↻</span> {busy ? "Working…" : "Refresh plan"}</Button></>} />
    {error && <div role="alert" className="mb-4 rounded-lg border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-destructive">{error}</div>}
    {notice && <div role="status" className="mb-4 rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-200">{notice}</div>}
    {!loading && application && <>
      {visibleOperation && <div role="status" aria-live="polite" className={`mb-5 rounded-xl border px-4 py-3 ${visibleOperation.status === "failed" ? "border-destructive/30 bg-destructive/5" : "bg-card"}`}>
        <div className="flex flex-wrap items-center gap-3"><StatusBadge status={visibleOperation.status} /><span className="text-xs font-medium">{operationPhaseLabel(visibleOperation.progress?.phase, visibleOperation.status)}</span><span className="ml-auto text-[11px] tabular-nums text-muted-foreground">{visibleOperation.progress?.completed.length ?? 0} / {visibleOperation.progress?.total ?? 0} resources</span></div>
        {visibleOperation.progress?.total ? <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-muted"><div className={`h-full rounded-full ${visibleOperation.status === "failed" ? "bg-destructive" : "bg-primary"}`} style={{ width: `${Math.min(100, Math.round((visibleOperation.progress.completed.length / visibleOperation.progress.total) * 100))}%` }} /></div> : null}
        <div className="mt-2 flex flex-wrap items-center justify-between gap-2 text-[11px] text-muted-foreground"><span>{visibleOperation.progress?.current ? `Now: ${resourceLabel(visibleOperation.progress.current)}` : visibleOperation.message}</span>{visibleOperation.status === "failed" && visibleOperation.progress?.current && <span>{visibleOperation.message}</span>}</div>
      </div>}
      <div className="mb-6 grid gap-3 rounded-xl border bg-card p-4 sm:grid-cols-3 sm:divide-x sm:p-5">
        <SummaryFact label="Target" value={application.namespaces.map((binding) => binding.namespace).join(", ") || "No namespace"} />
        <SummaryFact label="Latest plan" value={latestPlan ? `${latestPlan.plan.changes.length} changes · ${latestPlan.status}` : "No plan yet"} />
        <SummaryFact label="Last sync" value={application.lastSyncedRevision ? `${application.lastSyncedRevision.slice(0, 12)} · ${latestOperation?.status ?? "completed"}` : "Not synced yet"} />
      </div>

      <Tabs.Root value={activeTab} onValueChange={(value) => selectTab(String(value))} className="min-w-0">
        <Tabs.List aria-label="Application views" className="mb-6 flex gap-6 overflow-x-auto border-b" activateOnFocus>
          {[
            { id: "overview", label: "Overview" },
            { id: "topology", label: "Topology", count: topology?.nodes.length },
            { id: "changes", label: "Plan & diff", count: latestPlan ? latestPlan.plan.changes.length + (latestPlan.plan.ignored?.length ?? 0) : 0 },
            { id: "components", label: "Managed components", count: resources.length },
            { id: "activity", label: "Activity & source", count: operations.length },
          ].map((tab) => <Tabs.Tab key={tab.id} value={tab.id} className="flex shrink-0 items-center gap-2 border-b-2 border-transparent px-1 pb-3 text-xs font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-[active]:border-primary data-[active]:text-foreground">
            {tab.label}{tab.count !== undefined && <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] tabular-nums text-muted-foreground">{tab.count}</span>}
          </Tabs.Tab>)}
        </Tabs.List>

        <Tabs.Panel value="overview" className="space-y-5 outline-none">
          <div className="grid gap-5 xl:grid-cols-[minmax(0,1.5fr)_minmax(280px,1fr)]">
            <Panel title="Delivery state" description="The current Git-to-cluster picture, without opening the full diff.">
              <div className="space-y-5 p-5">
                <div className="grid grid-cols-3 gap-2"><ChangeCount label="Create" count={latestCounts?.create ?? 0} color="text-emerald-700 bg-emerald-50 dark:text-emerald-300 dark:bg-emerald-950/40" /><ChangeCount label="Update" count={latestCounts?.update ?? 0} color="text-blue-700 bg-blue-50 dark:text-blue-300 dark:bg-blue-950/40" /><ChangeCount label="Delete" count={latestCounts?.delete ?? 0} color="text-rose-700 bg-rose-50 dark:text-rose-300 dark:bg-rose-950/40" /></div>
                {latestPlan?.plan.requiresApproval && <p className="rounded-lg border border-amber-300/70 bg-amber-50/70 px-3 py-2 text-xs text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200">Owner approval required before this plan can be applied.</p>}
                <div className="flex flex-wrap items-center justify-between gap-3 border-t pt-4"><span className="text-xs text-muted-foreground">{latestPlan ? `Plan ${latestPlan.status} · expires ${new Date(latestPlan.expiresAt).toLocaleTimeString()}` : "Create a plan to compare Git with the cluster."}</span><Button size="sm" variant="outline" onClick={() => selectTab("changes")}>Review plan →</Button></div>
              </div>
            </Panel>
            <Panel title="Resource footprint" description="Connected objects observed for this application.">
              <div className="space-y-4 p-5"><p className="text-2xl font-semibold tabular-nums">{topology?.nodes.length ?? 0}<span className="ml-2 text-xs font-normal text-muted-foreground">resources · {topology?.edges.length ?? 0} relationships</span></p>
                <div className="flex flex-wrap gap-1.5">{resourceKinds.length ? resourceKinds.slice(0, 8).map(([kind, count]) => <span key={kind} className="rounded-md border bg-muted/30 px-2 py-1 text-[11px]">{count} {kind}</span>) : <span className="text-xs text-muted-foreground">No resources observed yet.</span>}</div>
                <div className="border-t pt-4"><Button size="sm" variant="outline" onClick={() => selectTab("topology")}>Explore topology →</Button></div>
              </div>
            </Panel>
          </div>
          <div className="grid gap-5 sm:grid-cols-2"><InfoCard label="Git revision" value={application.revision} note={`${application.renderer} · ${application.manifestPath}`} mono /><InfoCard label="Sync policy" value={application.syncPolicy === "auto-safe" ? "Auto-safe" : "Manual"} note={application.syncPolicy === "auto-safe" ? `Checks every ${application.pollSeconds}s; stops before deletion` : "Every sync is user initiated"} /></div>
          {latestOperation && <Panel title="Latest operation" action={<Button size="sm" variant="ghost" onClick={() => selectTab("activity")}>View activity →</Button>}><div className="flex flex-wrap items-center gap-3 p-5"><StatusBadge status={latestOperation.status} /><span className="min-w-0 flex-1 truncate text-xs">{latestOperation.message || "Sync operation"}</span><span className="text-[11px] text-muted-foreground">{new Date(latestOperation.startedAt).toLocaleString()}</span></div></Panel>}
        </Tabs.Panel>

        <Tabs.Panel value="topology" className="outline-none">
      <ResourceMap application={application} plan={plans[0] ?? null} inventory={resources} operations={operations} topology={topology} refreshing={topologyRefreshing} onRefresh={() => {
        setTopologyRefreshing(true)
        void api<ResourceTopology>(`/api/v1/applications/${encodeURIComponent(applicationID)}/topology?refresh=1`).then(setTopology).catch((cause) => setError(errorMessage(cause))).finally(() => setTopologyRefreshing(false))
      }} onViewDiff={(identity) => {
        const matching = plans[0]?.plan.changes.some((change) => diffId(change.identity) === diffId(identity))
        if (matching && plans[0]?.id !== activePlan?.id) setActivePlan(plans[0])
        selectTab("changes")
        window.setTimeout(() => document.getElementById(diffId(identity))?.scrollIntoView({ behavior: "smooth", block: "center" }), 100)
      }} />
        </Tabs.Panel>

        <Tabs.Panel value="changes" className="max-w-6xl outline-none">
          <Panel title="Plan & diff" description="A reviewed plan is a snapshot of the desired Git commit and live cluster state." action={plans.length > 0 && <FormSelect ariaLabel="Select plan" value={activePlan?.id ?? ""} onValueChange={(id) => { setApprovalId(""); setActivePlan(plans.find((plan) => plan.id === id) ?? null) }} className="h-8 max-w-[220px] text-xs" items={plans.map((plan) => ({ value: plan.id, label: `${new Date(plan.createdAt).toLocaleString()} · ${plan.status}` }))} />}>
            {!activePlan ? <EmptyState title="No review plan yet" description="Build a plan to render Git manifests and compare them with live, app-owned resources." /> : <div className="p-4 sm:p-5">
              <div className="mb-4 flex flex-wrap items-center gap-2"><StatusBadge status={activePlan.status} /><span className="font-mono text-[10px] text-muted-foreground">{activePlan.plan.revision.slice(0, 12)}</span><span className="text-[10px] text-muted-foreground">· expires {new Date(activePlan.expiresAt).toLocaleTimeString()}</span><span className="ml-auto font-mono text-[9px] text-muted-foreground">{activePlan.plan.digest.slice(0, 16)}</span></div>
              <div className="mb-5 grid grid-cols-3 gap-2"><ChangeCount label="Create" count={changeCounts?.create ?? 0} color="text-emerald-700 bg-emerald-50 dark:text-emerald-300 dark:bg-emerald-950/40" /><ChangeCount label="Update" count={changeCounts?.update ?? 0} color="text-blue-700 bg-blue-50 dark:text-blue-300 dark:bg-blue-950/40" /><ChangeCount label="Delete" count={changeCounts?.delete ?? 0} color="text-rose-700 bg-rose-50 dark:text-rose-300 dark:bg-rose-950/40" /></div>
              {ignoreRules.length > 0 && <section className="mb-5 rounded-lg border bg-muted/10"><div className="flex items-start justify-between gap-3 border-b px-4 py-3"><div><h3 className="text-xs font-semibold">Persistent ignore rules</h3><p className="mt-1 text-[10px] text-muted-foreground">Applied to manual and Auto-safe plans. Changes invalidate reviewed plans.</p></div><span className="rounded-md bg-muted px-2 py-1 text-[10px] tabular-nums">{ignoreRules.length}</span></div><div className="divide-y">{ignoreRules.map((rule) => <div key={rule.id} className="flex flex-wrap items-center gap-3 px-4 py-3"><span className="min-w-0 flex-1 text-xs font-medium">{rule.identity.kind} {rule.identity.namespace ? `${rule.identity.namespace}/` : ""}{rule.identity.name}<span className="ml-2 font-mono text-[10px] font-normal text-muted-foreground">{rule.path || "entire resource"}</span><span className="mt-1 block text-[10px] font-normal text-muted-foreground">{rule.reason}</span></span>{canApprove && <Button size="sm" variant="ghost" disabled={busy} onClick={() => void removeIgnoreRule(rule)}>Remove</Button>}</div>)}</div></section>}
              {activePlan.plan.changes.length ? <div className="space-y-3">{activePlan.plan.changes.map((change, index) => <DiffCard key={`${change.identity.apiVersion}/${change.identity.kind}/${change.identity.namespace}/${change.identity.name}/${index}`} change={change} canSelect={canDeploy} canManageIgnores={canApprove} excluded={selectionResources.some((identity) => sameIdentity(identity, change.identity))} excludedPaths={selectionFields.filter((field) => sameIdentity(field.identity, change.identity)).map((field) => field.path)} permanentResourceIgnore={ignoreRules.some((rule) => !rule.path && sameIdentity(rule.identity, change.identity))} permanentFieldIgnores={ignoreRules.filter((rule) => Boolean(rule.path) && sameIdentity(rule.identity, change.identity)).map((rule) => rule.path!)} reason={ignoreReasons[diffId(change.identity)] ?? ""} onReasonChange={(reason) => setIgnoreReasons((current) => ({ ...current, [diffId(change.identity)]: reason }))} onToggleResource={() => toggleResourceExclusion(change.identity)} onToggleField={(path) => toggleFieldExclusion(change.identity, path)} onSaveIgnore={(path) => void saveIgnoreRule(change.identity, path, ignoreReasons[diffId(change.identity)] ?? "")} busy={busy} />)}</div> : <div className="rounded-lg border border-dashed px-4 py-8 text-center"><span className="text-emerald-600">✓</span><p className="mt-2 text-sm font-medium">No changes to apply</p><p className="mt-1 text-xs text-muted-foreground">Git and the managed cluster fields are in sync, or every difference is excluded below.</p></div>}
              {(activePlan.plan.ignored?.length ?? 0) > 0 && <section className="mt-6 space-y-3"><div className="flex items-end justify-between gap-3"><div><h3 className="text-sm font-semibold">Excluded from this sync</h3><p className="mt-1 text-xs text-muted-foreground">These differences stay visible and will appear again in a later plan unless covered by a permanent rule.</p></div><span className="text-xs tabular-nums text-muted-foreground">{activePlan.plan.ignored?.length} excluded</span></div>{activePlan.plan.ignored?.map((change, index) => <DiffCard key={`ignored-${change.identity.kind}-${change.identity.namespace}-${change.identity.name}-${index}`} change={change} canSelect={canDeploy} canManageIgnores={canApprove} excluded={selectionResources.some((identity) => sameIdentity(identity, change.identity))} excludedPaths={selectionFields.filter((field) => sameIdentity(field.identity, change.identity)).map((field) => field.path)} permanentResourceIgnore={ignoreRules.some((rule) => !rule.path && sameIdentity(rule.identity, change.identity))} permanentFieldIgnores={ignoreRules.filter((rule) => Boolean(rule.path) && sameIdentity(rule.identity, change.identity)).map((rule) => rule.path!)} reason={ignoreReasons[diffId(change.identity)] ?? ""} onReasonChange={(reason) => setIgnoreReasons((current) => ({ ...current, [diffId(change.identity)]: reason }))} onToggleResource={() => toggleResourceExclusion(change.identity)} onToggleField={(path) => toggleFieldExclusion(change.identity, path)} onSaveIgnore={(path) => void saveIgnoreRule(change.identity, path, ignoreReasons[diffId(change.identity)] ?? "")} busy={busy} ignored />)}</section>}
              {selectionDirty && <div className="mt-5 flex flex-wrap items-center gap-3 rounded-lg border border-primary/20 bg-primary/5 p-3"><p className="min-w-0 flex-1 text-xs text-muted-foreground">Exclusions are drafts until you save. Saving creates a new immutable plan and clears any earlier approval.</p><Button size="sm" variant="outline" onClick={() => { setSelectionResources(activePlan.plan.selection?.resources ?? []); setSelectionFields(activePlan.plan.selection?.fields ?? []) }} disabled={busy}>Reset</Button><Button size="sm" onClick={() => void savePlanSelection()} disabled={busy}>{busy ? "Recalculating…" : "Create selected plan"}</Button></div>}
              {activePlan.plan.requiresApproval && <div className="mt-5 rounded-lg border border-amber-300/70 bg-amber-50/70 p-3.5 text-xs leading-5 text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><strong>Owner approval required.</strong> {changeCounts?.delete ? "Deletion is never automatic. Review the exact targets above; an owner must approve this plan before the sync button is enabled." : "Cluster-scoped changes need explicit owner approval."}</div>}
              <div className="mt-5 flex flex-wrap justify-end gap-2 border-t pt-4">
                {activePlan.plan.requiresApproval && <Button variant="outline" onClick={() => void approvePlan()} disabled={busy || selectionDirty || !canApprove || activePlan.status !== "current"}>{approvalId ? "Approved ✓" : "Approve reviewed changes"}</Button>}
                <Button onClick={() => void applyPlan()} disabled={busy || selectionDirty || !canDeploy || activePlan.status !== "current" || activePlan.plan.changes.length === 0 || (activePlan.plan.requiresApproval && !approvalId)}>{busy ? "Syncing…" : activePlan.plan.requiresApproval ? "Apply approved plan" : "Sync application"}</Button>
              </div>
              {!canApprove && activePlan.plan.requiresApproval && <p className="mt-2 text-right text-[10px] text-muted-foreground">Only a project owner can approve this plan.</p>}
            </div>}
          </Panel>
        </Tabs.Panel>

        <Tabs.Panel value="components" className="outline-none">
          <Panel surface="flat" title="Managed components" description="JustCD tracks only resources that it created and owns on the cluster.">
            {resources.length ? <div className="min-w-0"><DataGridList rows={resources} columns={[
              { id: "resource", title: "Resource", cell: (item) => <span className="font-medium">{item.identity.name}<span className="mt-0.5 block text-[10px] font-normal text-muted-foreground">{item.identity.kind} · {item.identity.apiVersion}</span></span> },
              { id: "namespace", title: "Namespace", cell: (item) => <span className="text-xs text-muted-foreground">{item.identity.namespace || "cluster scope"}</span> },
              { id: "cluster", title: "Cluster", cell: (item) => <span className="font-mono text-[10px] text-muted-foreground">{item.identity.clusterId?.slice(0, 8)}</span> },
              { id: "version", title: "Live version", cell: (item) => <span className="font-mono text-[10px] text-muted-foreground">{item.resourceVersion}</span> },
            ]} empty="A component inventory appears after the first successful sync." /></div> : <EmptyState title="No managed components yet" description="After the first successful sync, this inventory shows every resource owned by this application." />}
          </Panel>
        </Tabs.Panel>

        <Tabs.Panel value="activity" className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px] outline-none">
          <Panel title="Recent operations" description="Sync history and result state">
            {operations.length ? <div className="divide-y">{operations.slice(0, 8).map((operation) => <div key={operation.id} className="px-5 py-3.5"><div className="flex items-center justify-between gap-3"><span className="text-xs font-medium">{operation.status === "succeeded" ? "Sync completed" : operation.status === "failed" ? "Sync stopped" : operation.status === "queued" ? "Sync queued" : "Sync running"}</span><StatusBadge status={operation.status} /></div><p className="mt-1 line-clamp-2 text-[10px] leading-4 text-muted-foreground">{operation.message || "Sync operation"}</p>{operation.progress && <p className="mt-1 text-[10px] text-muted-foreground">{operationPhaseLabel(operation.progress.phase, operation.status)} · {operation.progress.completed.length}/{operation.progress.total} resources{operation.progress.current ? ` · ${resourceLabel(operation.progress.current)}` : ""}</p>}<p className="mt-1.5 text-[9px] text-muted-foreground">{new Date(operation.startedAt).toLocaleString()}</p></div>)}</div> : <EmptyState title="No syncs yet" description="Operations will be recorded here with the actor and resulting status." />}
          </Panel>
          <Panel title="Application source" description="Configuration stored in JustCD"><div className="space-y-3 p-5 text-xs"><KeyValue label="Manifest path" value={application.manifestPath} mono /><KeyValue label="Renderer" value={application.renderer} /><KeyValue label="Cluster" value={application.clusterId.slice(0, 12)} mono /><KeyValue label="Poll interval" value={`${application.pollSeconds} seconds`} /><Link href={`/projects/${application.projectId}`} className="mt-1 inline-block text-xs font-medium text-primary hover:underline">Open project →</Link></div></Panel>
        </Tabs.Panel>
      </Tabs.Root>
    </>}
  </>
}

function InfoCard({ label, value, note, mono = false }: { label: string; value: string; note: string; mono?: boolean }) {
  return <div className="min-w-0 rounded-xl border bg-card p-4"><p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</p><p className={`mt-2 truncate text-sm font-semibold ${mono ? "font-mono text-xs" : ""}`}>{value}</p><p className="mt-1 truncate text-[10px] text-muted-foreground">{note}</p></div>
}

function SummaryFact({ label, value }: { label: string; value: string }) {
  return <div className="min-w-0 sm:px-4 first:sm:pl-0 last:sm:pr-0"><p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</p><p className="mt-1 truncate text-sm font-medium" title={value}>{value}</p></div>
}

function ChangeCount({ label, count, color }: { label: string; count: number; color: string }) {
  return <div className={`rounded-lg px-3 py-2 ${color}`}><span className="text-lg font-semibold tabular-nums">{count}</span><span className="ml-2 text-[10px] font-medium">{label}</span></div>
}

function DiffCard({ change, canSelect, canManageIgnores, excluded, excludedPaths, permanentResourceIgnore, permanentFieldIgnores, reason, onReasonChange, onToggleResource, onToggleField, onSaveIgnore, busy, ignored = false }: {
  change: Change
  canSelect: boolean
  canManageIgnores: boolean
  excluded: boolean
  excludedPaths: string[]
  permanentResourceIgnore: boolean
  permanentFieldIgnores: string[]
  reason: string
  onReasonChange: (reason: string) => void
  onToggleResource: () => void
  onToggleField: (path: string) => void
  onSaveIgnore: (path: string) => void
  busy: boolean
  ignored?: boolean
}) {
  const name = `${change.identity.kind} ${change.identity.namespace ? `${change.identity.namespace}/` : ""}${change.identity.name}`
  const color = change.kind === "delete" ? "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-900 dark:bg-rose-950/25 dark:text-rose-300" : change.kind === "create" ? "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/25 dark:text-emerald-300" : "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/25 dark:text-blue-300"
  const fieldPaths = useMemo(() => (change.changedPaths ?? []).filter(safeIgnorePath), [change.changedPaths])
  const lineDiff = useMemo(() => change.kind === "update" && change.before != null && change.after != null
    ? diffJsonLines(change.before, change.after)
    : null, [change.kind, change.before, change.after])
  return <article id={diffId(change.identity)} className="scroll-mt-6 overflow-hidden rounded-lg border">
    <div className="flex flex-wrap items-center gap-2 border-b bg-muted/20 px-3 py-2"><span className={`rounded border px-1.5 py-0.5 text-[9px] font-semibold uppercase ${color}`}>{change.kind}</span><span className="min-w-0 flex-1 truncate text-xs font-medium">{name}</span>{ignored && <span className="rounded-full border border-amber-300 bg-amber-50 px-2 py-0.5 text-[9px] text-amber-800">Excluded</span>}{lineDiff && <span className="flex shrink-0 items-center gap-3 text-[10px] text-muted-foreground"><span><span className="font-semibold text-rose-700 dark:text-rose-300">−</span> removed</span><span><span className="font-semibold text-emerald-700 dark:text-emerald-300">+</span> added</span></span>}{change.identity.clusterScoped && <span className="rounded-full border px-2 py-0.5 text-[9px] text-amber-700">cluster-wide</span>}</div>
    <div className="grid divide-y md:grid-cols-2 md:divide-x md:divide-y-0">
      <ManifestBlock title={change.kind === "create" ? "Current" : "Live before"} value={change.before} empty={change.kind === "create" ? "Not present" : "Unavailable"} lines={lineDiff?.before} highlighted={lineDiff?.removed} tone="removed" />
      <ManifestBlock title={change.kind === "delete" ? "After sync" : "Desired after"} value={change.after} empty={change.kind === "delete" ? "Resource removed" : "Unavailable"} lines={lineDiff?.after} highlighted={lineDiff?.added} tone="added" />
    </div>
    {(canSelect || canManageIgnores) && <div className="border-t bg-muted/10 px-3 py-3">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
        <label className="flex items-center gap-2 text-[11px] font-medium"><Checkbox checked={permanentResourceIgnore || excluded} disabled={!canSelect || permanentResourceIgnore} onCheckedChange={onToggleResource} /><span>{permanentResourceIgnore ? "Resource is ignored permanently" : "Exclude this resource from this plan"}</span></label>
        {fieldPaths.length > 0 && change.kind !== "delete" && <span className="text-[10px] text-muted-foreground">Select changed fields:</span>}
      </div>
      {fieldPaths.length > 0 && change.kind !== "delete" && <div className="mt-2 grid gap-1.5 sm:grid-cols-2">{fieldPaths.map((path) => {
        const permanent = permanentFieldIgnores.includes(path)
        const selected = permanent || excludedPaths.includes(path)
        return <label key={path} className="flex min-w-0 items-start gap-2 rounded-md border bg-background/70 px-2.5 py-2 text-[10px]">
          <Checkbox checked={selected} disabled={!canSelect || permanent} onCheckedChange={() => onToggleField(path)} className="mt-0.5" />
          <span className="min-w-0"><span className="block break-all font-mono text-foreground">{path}</span><span className="mt-0.5 block text-muted-foreground">{permanent ? "Ignored by a persistent rule" : "Skip this field for this plan"}</span></span>
        </label>
      })}</div>}
      {(fieldPaths.length > 0 && change.kind !== "delete") && <p className="mt-2 text-[10px] leading-4 text-muted-foreground">Field ignores require another Kubernetes field manager to own the value first. Lists can only be excluded as a whole; JustCD validates ownership before saving.</p>}
      {canManageIgnores && <details className="mt-3 border-t pt-2.5">
        <summary className="cursor-pointer text-[10px] font-medium text-primary">Permanent ignore rule…</summary>
        <div className="mt-2 space-y-2.5">
          <Textarea aria-label={`Reason for ignoring ${name}`} rows={2} value={reason} onChange={(event) => onReasonChange(event.target.value)} placeholder="Why should future plans ignore this? (required)" className="min-h-16 resize-y text-xs" maxLength={500} />
          <div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" disabled={busy || permanentResourceIgnore || reason.trim().length < 5} onClick={() => onSaveIgnore("")}>{permanentResourceIgnore ? "Resource already ignored" : "Ignore resource in future plans"}</Button>
            {fieldPaths.filter((path) => !permanentFieldIgnores.includes(path)).map((path) => <Button key={path} size="sm" variant="ghost" disabled={busy || reason.trim().length < 5 || change.kind === "delete"} onClick={() => onSaveIgnore(path)}>Ignore {path} in future plans</Button>)}</div>
          <p className="text-[10px] leading-4 text-muted-foreground">Only project owners can change permanent rules. Each change is audited and invalidates existing plans.</p>
        </div>
      </details>}
      {!canManageIgnores && canSelect && <p className="mt-2 text-[10px] text-muted-foreground">Permanent ignore rules can be managed by project owners.</p>}
      {change.ignoreReason && ignored && <p className="mt-2 text-[10px] text-amber-800 dark:text-amber-300">{change.ignoreReason}</p>}
    </div>}
  </article>
}

function ManifestBlock({ title, value, empty, lines, highlighted, tone }: { title: string; value: unknown; empty: string; lines?: string[]; highlighted?: Set<number>; tone: "removed" | "added" }) {
  const body = value === undefined || value === null ? empty : JSON.stringify(value, null, 2)
  return <details className="group min-w-0 px-3 py-2.5" open={body !== empty && body.length < 1600}>
    <summary className="cursor-pointer list-none text-[9px] font-semibold uppercase tracking-wider text-muted-foreground"><span className="inline-block transition-transform group-open:rotate-90">›</span> {title}</summary>
    <pre className="mt-2 max-h-64 overflow-auto rounded bg-muted/50 py-2 font-mono text-[9px] leading-4 text-foreground/80">{lines ? lines.map((line, index) => {
      const changed = highlighted?.has(index)
      return <span key={index} className={`block w-max min-w-full px-2 ${changed ? tone === "removed" ? "border-l-2 border-rose-500 bg-rose-100 text-rose-950 dark:bg-rose-950/50 dark:text-rose-100" : "border-l-2 border-emerald-500 bg-emerald-100 text-emerald-950 dark:bg-emerald-950/50 dark:text-emerald-100" : "border-l-2 border-transparent"}`}><span aria-hidden="true" className="inline-block w-3 select-none">{changed ? tone === "removed" ? "−" : "+" : ""}</span>{line}{index < lines.length - 1 ? "\n" : ""}</span>
    }) : <span className="px-2">{body}</span>}</pre>
  </details>
}

function KeyValue({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="flex items-start justify-between gap-3"><span className="text-muted-foreground">{label}</span><span className={`max-w-[65%] truncate text-right ${mono ? "font-mono text-[10px]" : "font-medium"}`}>{value}</span></div>
}

function operationPhaseLabel(phase: string | undefined, status: string) {
  if (status === "queued") return "Waiting for a sync worker"
  if (status === "failed") return "Sync stopped"
  if (phase === "validating") return "Rechecking Git, cluster state, and approval"
  if (phase === "applying") return "Applying creates and updates"
  if (phase === "deleting") return "Applying approved deletions"
  if (phase === "complete" || status === "succeeded") return "Sync complete"
  return "Sync in progress"
}

function resourceLabel(identity: Identity) {
  return `${identity.kind} ${identity.namespace ? `${identity.namespace}/` : ""}${identity.name}`
}
