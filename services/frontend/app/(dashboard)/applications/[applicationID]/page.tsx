"use client"

import { useParams, useRouter } from "next/navigation"
import { useEffect, useRef, useState } from "react"
import { Tabs } from "@base-ui/react/tabs"
import { HugeiconsIcon } from "@hugeicons/react"
import { Edit02Icon, PauseIcon, RefreshIcon, TestTubeIcon } from "@hugeicons/core-free-icons"
import { ActionMenu } from "@/components/action-menu"
import { ActivityTab } from "@/components/application/activity-tab"
import { AttentionBar } from "@/components/application/attention-bar"
import { ChangesTab } from "@/components/application/changes-tab"
import { diffId, isClaimable, sameIdentity, type IgnorePrefill, type NewIgnore } from "@/components/application/helpers"
import { OperationBar, type RollbackRequest } from "@/components/application/operation-bar"
import { OverviewTab } from "@/components/application/overview-tab"
import { OwnershipConflictDialog } from "@/components/application/ownership-conflict-dialog"
import { RuntimeHealthStrip } from "@/components/application/runtime-health"
import { SettingsTab } from "@/components/application/settings-tab"
import { ApplicationDetailSkeleton } from "@/components/application-detail-skeleton"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { ResourceMap } from "@/components/resource-map"
import { PageHeading, StatusBadge } from "@/components/ui-kit"
import { ErrorDetailsButton } from "@/components/error-details"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { APIError, api, apiDelete, apiPost, errorMessage } from "@/lib/api"
import { BranchTestControls } from "@/components/branch-test-controls"
import { PullRequestsWorkspace } from "@/components/pull-requests-workspace"
import type { Application, ApplicationHealthTransition, FieldExclusion, Identity, IgnoreRule, IgnoreSelector, ListResponse, ManagedResource, Operation, OwnershipConflict, PlanApprovalSummary, PlanRecord, Workspace, WorkspaceMember, ResourceTopology, RollbackTarget } from "@/lib/types"

export default function ApplicationDetailPage() {
  const { applicationID } = useParams<{ applicationID: string }>()
  const router = useRouter()
  const [reviewResource, setReviewResource] = useState("")
  const toast = useToast()
  const [application, setApplication] = useState<Application | null>(null)
  const [branchTestOpen, setBranchTestOpen] = useState(false)
  const [kustomization, setKustomization] = useState<{ namespace: string; commit: string } | null>(null)
  const [kustomizationError, setKustomizationError] = useState<unknown | null>(null)
  const [workspace, setWorkspace] = useState<Workspace | null>(null)
  const [workspaceMembers, setWorkspaceMembers] = useState<WorkspaceMember[]>([])
  const [plans, setPlans] = useState<PlanRecord[]>([])
  const [activePlan, setActivePlan] = useState<PlanRecord | null>(null)
  const [resources, setResources] = useState<ManagedResource[]>([])
  const [topologyView, setTopologyView] = useState<"graph" | "list">("graph")
  const [topology, setTopology] = useState<ResourceTopology | null>(null)
  const [topologyError, setTopologyError] = useState<unknown | null>(null)
  const [topologyRefreshing, setTopologyRefreshing] = useState(false)
  const [operations, setOperations] = useState<Operation[]>([])
  const [healthHistory, setHealthHistory] = useState<ApplicationHealthTransition[]>([])
  const [rollbackTargets, setRollbackTargets] = useState<RollbackTarget[]>([])
  const [resumeRevision, setResumeRevision] = useState("")
  const [ignoreRules, setIgnoreRules] = useState<IgnoreRule[]>([])
  const [ignoreSelectors, setIgnoreSelectors] = useState<IgnoreSelector[]>([])
  const [selectionDraft, setSelectionDraft] = useState<{ planId: string; resources: Identity[]; fields: FieldExclusion[] } | null>(null)
  const [ignoreReasons, setIgnoreReasons] = useState<Record<string, string>>({})
  const [approvalSummary, setApprovalSummary] = useState<PlanApprovalSummary | null>(null)
  const [approvalComment, setApprovalComment] = useState("")
  const [loading, setLoading] = useState(true)
  const [pendingData, setPendingData] = useState({ workspace: true, plans: true, resources: true, operations: true, topology: true, rollback: true, rules: true, selectors: true })
  const [busy, setBusy] = useState(false)
  const [pendingAction, setPendingAction] = useState("")
  const [operationsError, setOperationsError] = useState<unknown | null>(null)
  const [plansError, setPlansError] = useState<unknown | null>(null)
  const [retryingPlans, setRetryingPlans] = useState(false)
  const [error, setError] = useState<unknown | null>(null)
  const [ownershipConflict, setOwnershipConflict] = useState<OwnershipConflict | null>(null)
  const [ownershipConflicts, setOwnershipConflicts] = useState<OwnershipConflict[]>([])
  const [selectedConflictKeys, setSelectedConflictKeys] = useState<string[]>([])
  const conflictEpoch = useRef(0)
  const loadEpoch = useRef(0)
  const [activeTab, setActiveTab] = useState("overview")
  const [conflictOpen, setConflictOpen] = useState(false)
  const [ignorePrefill, setIgnorePrefill] = useState<IgnorePrefill | null>(null)

  function setConflictReview(conflicts: OwnershipConflict[]) {
    setOwnershipConflicts(conflicts)
    setOwnershipConflict(conflicts[0] ?? null)
    if (conflicts.length === 0) setConflictOpen(false)
    setSelectedConflictKeys(conflicts.filter((item) => isClaimable(item, applicationID)).map((item) => diffId(item.identity)))
  }

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
      setTopologyView(value === "components" || new URLSearchParams(window.location.search).get("view") === "list" ? "list" : "graph")
      setActiveTab(value === "components" ? "topology" : ["overview", "topology", "changes", "activity", "pull-requests", "settings"].includes(value ?? "") ? value! : "overview")
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
    const epoch = ++loadEpoch.current
    const app = await api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`)
    if (epoch !== loadEpoch.current) return
    setApplication(app)
    setLoading(false)
    const current = () => epoch === loadEpoch.current
    const done = (section: keyof typeof pendingData) => { if (current()) setPendingData((state) => ({ ...state, [section]: false })) }
    const appPath = `/api/v1/applications/${encodeURIComponent(applicationID)}`
    const results = await Promise.allSettled([
      api<ListResponse<Workspace>>("/api/v1/workspaces").then((value) => { if (current()) setWorkspace(value.items.find((item) => item.id === app.workspaceId) ?? null) }).finally(() => done("workspace")),
      api<ListResponse<WorkspaceMember>>(`/api/v1/workspaces/${encodeURIComponent(app.workspaceId)}/members`).catch(() => ({ items: [] as WorkspaceMember[] })).then((value) => { if (current()) setWorkspaceMembers(value.items) }),
      api<ListResponse<PlanRecord>>(`${appPath}/plans`).then((value) => { if (current()) { setPlansError(null); setPlans(value.items); setActivePlan((selected) => selected ? value.items.find((item) => item.id === selected.id) ?? value.items[0] ?? null : value.items[0] ?? null) } }).catch((cause) => { if (current()) setPlansError(cause) }).finally(() => done("plans")),
      api<ListResponse<ManagedResource>>(`${appPath}/resources`).then((value) => { if (current()) setResources(value.items) }).finally(() => done("resources")),
      api<ListResponse<Operation>>(`${appPath}/operations`).then((value) => { if (current()) { setOperations(value.items); setOperationsError(null) } }).catch((cause) => { if (current()) setOperationsError(cause) }).finally(() => done("operations")),
      api<ListResponse<ApplicationHealthTransition>>(`${appPath}/health-history`).then((value) => { if (current()) setHealthHistory(value.items) }).catch(() => { if (current()) setHealthHistory([]) }),
      api<ListResponse<IgnoreRule>>(`${appPath}/ignore-rules`).then((value) => { if (current()) setIgnoreRules(value.items) }).finally(() => done("rules")),
      api<ListResponse<IgnoreSelector>>(`${appPath}/ignore-selectors`).then((value) => { if (current()) setIgnoreSelectors(value.items) }).finally(() => done("selectors")),
      api<ListResponse<RollbackTarget>>(`${appPath}/rollback-targets`).catch(() => ({ items: [] as RollbackTarget[] })).then((value) => { if (current()) setRollbackTargets(value.items) }).finally(() => done("rollback")),
    ])
    if (current()) {
      const failed = results.find((result) => result.status === "rejected")
      if (failed?.status === "rejected") setError(failed.reason)
    }
  }

  useEffect(() => {
    let active = true
    Promise.resolve().then(() => {
      if (!active) return
      setLoading(true)
      setPendingData({ workspace: true, plans: true, resources: true, operations: true, topology: true, rollback: true, rules: true, selectors: true })
      setApplication(null)
      setError(null)
      setWorkspace(null)
      setWorkspaceMembers([])
      setPlansError(null)
      setPlans([])
      setActivePlan(null)
      setResources([])
      setOperationsError(null)
      setOperations([])
      setHealthHistory([])
      setRollbackTargets([])
      setIgnoreRules([])
      setIgnoreSelectors([])
      setTopology(null)
      setTopologyError(null)
      setKustomization(null)
      setKustomizationError(null)
      setIgnorePrefill(null)
      setConflictOpen(false)
      return loadData()
    }).catch((cause) => active && setError(cause)).finally(() => active && setLoading(false))
    return () => { active = false; loadEpoch.current += 1 }
  // loadData is intentionally tied to this resource identifier only.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [applicationID])

  const loadedAppID = application?.id
  const checkKustomization = application?.renderer === "kustomize" && (activeTab === "settings" || (!application.kustomizeNamespaceOverride && application.namespaces.length === 1))

  useEffect(() => {
    if (!loadedAppID) return
    let active = true
    void api<ResourceTopology>(`/api/v1/applications/${encodeURIComponent(loadedAppID)}/topology?cached=1`)
      .then((result) => { if (active) { setTopology(result); setTopologyError(null) } })
      .catch((cause) => { if (active) setTopologyError(cause) })
      .finally(() => { if (active) setPendingData((state) => ({ ...state, topology: false })) })
    return () => { active = false }
  }, [loadedAppID])

  useEffect(() => {
    if (!loadedAppID || !checkKustomization) return
    let active = true
    void api<{ namespace: string; commit: string }>(`/api/v1/applications/${encodeURIComponent(loadedAppID)}/kustomization`)
      .then((value) => { if (active) { setKustomization(value); setKustomizationError(null) } })
      .catch((cause) => { if (active) setKustomizationError(cause) })
    return () => { active = false }
  }, [loadedAppID, checkKustomization])

  useEffect(() => {
    if (loading || !application) return
    let active = true
    const epoch = conflictEpoch.current
    void api<{ conflict: OwnershipConflict | null; conflicts: OwnershipConflict[] }>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ownership-conflict`)
      .then((result) => { if (active && conflictEpoch.current === epoch) setConflictReview(result.conflicts ?? (result.conflict ? [result.conflict] : [])) })
      .catch(() => {})
    return () => { active = false }
  // Run discovery once after the page loads, independently of data refreshes.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [applicationID, loading])

  const hasPendingOperation = operations.some((operation) => operation.status === "queued" || operation.status === "running")
  const activePlanId = activePlan?.id
  const activePlanRequiresApproval = activePlan?.plan.requiresApproval ?? false
  useEffect(() => {
    if (!activePlanId || !activePlanRequiresApproval) {
      return
    }
    let active = true
    const refreshApprovals = () => api<PlanApprovalSummary>(`/api/v1/plans/${encodeURIComponent(activePlanId)}/approvals`)
      .then((summary) => { if (active) setApprovalSummary(summary) })
      .catch(() => { if (active) setApprovalSummary(null) })
    void refreshApprovals()
    const timer = window.setInterval(() => void refreshApprovals(), 5000)
    return () => { active = false; window.clearInterval(timer) }
  }, [activePlanId, activePlanRequiresApproval])

  useEffect(() => {
    if (!busy && !hasPendingOperation) return
    let healthRefreshStarted = false
    const timer = window.setInterval(() => {
      void Promise.all([
        api<ListResponse<Operation>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/operations`),
        api<ListResponse<ManagedResource>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/resources`),
        api<ListResponse<PlanRecord>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`),
        api<ListResponse<RollbackTarget>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/rollback-targets`).catch(() => ({ items: [] as RollbackTarget[] })),
        api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`),
      ]).then(([nextOperations, nextResources, nextPlans, nextRollbackTargets, nextApplication]) => {
        setOperations(nextOperations.items)
        setResources(nextResources.items)
        setPlans(nextPlans.items)
        setRollbackTargets(nextRollbackTargets.items)
        setApplication(nextApplication)
        setActivePlan(current => current ? nextPlans.items.find((plan) => plan.id === current.id) ?? nextPlans.items[0] ?? null : nextPlans.items[0] ?? null)
        const operationStillRunning = nextOperations.items.some((operation) => operation.status === "queued" || operation.status === "running")
        if (hasPendingOperation && !operationStillRunning && !healthRefreshStarted) {
          healthRefreshStarted = true
          void api<ResourceTopology>(`/api/v1/applications/${encodeURIComponent(applicationID)}/topology?refresh=1`).then((topologyResult) => {
            setTopology(topologyResult)
            return Promise.all([
              api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`),
              api<ListResponse<ApplicationHealthTransition>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/health-history`),
            ])
          }).then(([refreshedApplication, history]) => {
            setApplication(refreshedApplication)
            setHealthHistory(history.items)
          }).catch(() => {})
        }
      }).catch(() => {})
    }, 1500)
    return () => window.clearInterval(timer)
  }, [applicationID, busy, hasPendingOperation])

  async function createPlan() {
    conflictEpoch.current += 1
    setBusy(true); setPendingAction("create-plan"); setError(null); setApprovalSummary(null)
    try {
      const plan = await apiPost<PlanRecord>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`)
      setConflictReview([])
      setPlansError(null)
      setActivePlan(plan)
      setPlans((current) => [plan, ...current.filter((item) => item.id !== plan.id)])
      toast.success(plan.plan.changes.length ? `Plan ready: ${plan.plan.changes.length} change${plan.plan.changes.length === 1 ? "" : "s"} to review.` : "The application already matches its Git revision.")
      selectTab("changes")
      await refreshSummary()
    } catch (cause) {
      if (cause instanceof APIError && cause.status === 409 && typeof cause.payload === "object" && cause.payload && "conflict" in cause.payload) {
        const payload = cause.payload as { conflict: OwnershipConflict; conflicts?: OwnershipConflict[] }
        setConflictReview(payload.conflicts ?? [payload.conflict])
      } else if (!acceptRefreshedPlan(cause)) toast.error(errorMessage(cause), cause)
    } finally {
      try { setApplication(await api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`)) } catch { /* Retain the current application if refresh is unavailable. */ }
      setBusy(false); setPendingAction("")
    }
  }

  async function adoptConflict(reason: string, previousControllerDisabled: boolean) {
    if (!ownershipConflict) return
    conflictEpoch.current += 1
    setBusy(true); setPendingAction("adopt-resource")
    try {
      await apiPost(`/api/v1/applications/${encodeURIComponent(applicationID)}/adopt`, {
        conflict: ownershipConflict,
        reason: reason.trim(),
        previousControllerDisabled,
      })
      setConflictReview([])
      setApprovalSummary(null)
      await loadData().catch(() => {})
      const next = await api<{ conflict: OwnershipConflict | null; conflicts: OwnershipConflict[] }>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ownership-conflict`).catch(() => null)
      if (next) setConflictReview(next.conflicts ?? (next.conflict ? [next.conflict] : []))
      toast.success("Resource claimed without changing its workload. Auto-sync is paused; create and review a fresh plan before syncing.")
    } finally { setBusy(false); setPendingAction("") }
  }

  async function adoptSelectedConflicts(reason: string, previousControllerDisabled: boolean) {
    const selected = ownershipConflicts.filter((item) => isClaimable(item, applicationID) && selectedConflictKeys.includes(diffId(item.identity)))
    if (selected.length === 0) return
    conflictEpoch.current += 1
    setBusy(true); setPendingAction("adopt-batch")
    try {
      const result = await apiPost<{ claimed: number; failed: number; results: { identity: Identity; status: string; error?: string }[] }>(`/api/v1/applications/${encodeURIComponent(applicationID)}/adopt-batch`, {
        conflicts: selected,
        reason: reason.trim(),
        previousControllerDisabled,
      })
      setApprovalSummary(null)
      await loadData().catch(() => {})
      const next = await api<{ conflict: OwnershipConflict | null; conflicts: OwnershipConflict[] }>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ownership-conflict`).catch(() => null)
      if (next) setConflictReview(next.conflicts ?? (next.conflict ? [next.conflict] : []))
      if (result.failed) toast.error(`${result.claimed} claimed; ${result.failed} failed. Refresh the conflict list and retry the remaining resources.`)
      else toast.success(`${result.claimed} resources claimed without changing workloads. Auto-sync is paused; review a fresh plan before syncing.`)
    } finally { setBusy(false); setPendingAction("") }
  }

  function configureIgnore(prefill: Omit<IgnorePrefill, "nonce">) {
    setIgnorePrefill((current) => ({ ...prefill, nonce: (current?.nonce ?? 0) + 1 }))
    setConflictOpen(false)
    selectTab("settings")
  }

  function configureConflictIgnore() {
    if (!ownershipConflict) return
    configureIgnore({ mode: "resource", version: ownershipConflict.identity.apiVersion, kind: ownershipConflict.identity.kind, namespace: ownershipConflict.identity.namespace, name: ownershipConflict.identity.name, reason: "Managed by another controller" })
  }

  async function saveRenderSettings(settings: { kustomizeHelmEnabled: boolean; kustomizeNamespaceOverride: boolean }) {
    if (!application || application.renderer !== "kustomize") return
    setBusy(true); setPendingAction("render-settings"); setError(null)
    try {
      await api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}/render-settings`, { method: "PUT", body: JSON.stringify(settings) })
      setApprovalSummary(null)
      await loadData()
      toast.success("Render setting saved. Earlier plans are stale; create and review a new plan before syncing.")
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setBusy(false); setPendingAction("") }
  }

  function acceptRefreshedPlan(cause: unknown) {
    if (!(cause instanceof APIError) || cause.status !== 409 || typeof cause.payload !== "object" || !cause.payload || !("plan" in cause.payload)) return false
    const refreshed = (cause.payload as { plan: PlanRecord }).plan
    setActivePlan(refreshed)
    setPlans((current) => [refreshed, ...current.filter((item) => item.id !== refreshed.id)])
    setApprovalSummary(null)
    toast.error("The plan changed since it was reviewed. A fresh snapshot is shown; review it before continuing.", cause)
    return true
  }

  function toggleResourceExclusion(identity: Identity) {
    setSelectionResources((current) => current.some((item) => sameIdentity(item, identity)) ? current.filter((item) => !sameIdentity(item, identity)) : [...current, identity])
    setApprovalSummary(null)
  }

  function toggleFieldExclusion(identity: Identity, path: string) {
    setSelectionFields((current) => current.some((item) => sameIdentity(item.identity, identity) && item.path === path)
      ? current.filter((item) => !(sameIdentity(item.identity, identity) && item.path === path))
      : [...current, { identity, path }])
    setApprovalSummary(null)
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
    setBusy(true); setPendingAction("selection"); setError(null); setApprovalSummary(null)
    try {
      const plan = await apiPost<PlanRecord>(`/api/v1/plans/${encodeURIComponent(activePlan.id)}/selections`, { resources: selectionResources, fields: selectionFields })
      setActivePlan(plan)
      setPlans((current) => [plan, ...current.filter((item) => item.id !== plan.id)])
      toast.success(`A new immutable plan is ready with ${plan.plan.changes.length} change${plan.plan.changes.length === 1 ? "" : "s"} to apply.`)
    } catch (cause) { if (!acceptRefreshedPlan(cause)) toast.error(errorMessage(cause), cause) }
    finally { setBusy(false); setPendingAction("") }
  }

  function invalidatePlans() {
    setPlans((current) => current.map((record) => record.status === "current" ? { ...record, status: "stale" } : record))
    setActivePlan((current) => current?.status === "current" ? { ...current, status: "stale" } : current)
    setApprovalSummary(null)
  }

  async function saveIgnoreRule(identity: Identity, path: string, reason: string) {
    const cleanReason = reason.trim()
    if (cleanReason.length < 5) { toast.error("Add a short reason before saving a permanent ignore rule."); return }
    setBusy(true); setPendingAction("save-ignore-rule"); setError(null)
    try {
      const rule = await apiPost<IgnoreRule>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-rules`, { identity, path, reason: cleanReason })
      setIgnoreRules((current) => [rule, ...current])
      invalidatePlans()
      toast.success("Ignore rule saved. Existing plans are stale; refresh the plan when ready.")
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setBusy(false); setPendingAction("") }
  }

  async function removeIgnoreRule(rule: IgnoreRule) {
    setBusy(true); setPendingAction("remove-ignore-rule"); setError(null)
    try {
      await apiDelete<void>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-rules/${encodeURIComponent(rule.id)}`)
      setIgnoreRules((current) => current.filter((item) => item.id !== rule.id))
      invalidatePlans()
      toast.success("Ignore rule removed. Refresh the plan before syncing.")
    }
    finally { setBusy(false); setPendingAction("") }
  }

  async function addIgnore(ignore: NewIgnore): Promise<boolean> {
    if (!application) return false
    setBusy(true); setPendingAction("add-ignore"); setError(null)
    try {
      if (ignore.mode === "resource") {
        const rule = await apiPost<IgnoreRule>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-rules`, { identity: { clusterId: application.clusterId, apiVersion: ignore.version.trim(), kind: ignore.kind.trim(), namespace: ignore.namespace.trim(), name: ignore.name.trim() }, reason: ignore.reason.trim() })
        setIgnoreRules((current) => [rule, ...current])
      } else {
        const selector = await apiPost<IgnoreSelector>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-selectors`, { apiVersion: ignore.mode === "kind" ? ignore.version.trim() : "", kind: ignore.mode === "kind" ? ignore.kind.trim() : "", labelKey: ignore.mode === "label" ? ignore.labelKey.trim() : "", labelValue: ignore.mode === "label" ? ignore.labelValue.trim() : "", reason: ignore.reason.trim() })
        setIgnoreSelectors((current) => [selector, ...current])
      }
      invalidatePlans()
      toast.success("Exclusion saved. Existing plans are stale; refresh the plan before syncing.")
      return true
    } catch (cause) { toast.error(errorMessage(cause), cause); return false }
    finally { setBusy(false); setPendingAction("") }
  }

  async function removeIgnoreSelector(selector: IgnoreSelector) {
    setBusy(true); setPendingAction("remove-ignore-selector"); setError(null)
    try {
      await apiDelete(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-selectors/${encodeURIComponent(selector.id)}`)
      setIgnoreSelectors((current) => current.filter((item) => item.id !== selector.id))
      invalidatePlans()
      toast.success("Exclusion removed. Refresh the plan before syncing.")
    } finally { setBusy(false); setPendingAction("") }
  }

  async function deleteApplication(deletePolicy: string) {
    const result = await api<{ deleted: boolean; plan?: PlanRecord }>(`/api/v1/applications/${encodeURIComponent(applicationID)}?resources=${deletePolicy}`, { method: "DELETE" })
    if (result.deleted) { toast.success("Application deleted."); router.push("/applications"); router.refresh(); return }
    if (result.plan) {
      setActivePlan(result.plan)
      setPlans((current) => [result.plan!, ...current.filter((item) => item.id !== result.plan!.id)])
      setApplication((current) => current ? { ...current, decommissioning: true } : current)
      setApprovalSummary(null)
      toast.success(`Deletion plan ready for ${result.plan.plan.changes.length} managed resources. Review and approve it before applying.`)
      selectTab("changes")
    }
  }

  async function cancelDecommission() {
    setBusy(true); setPendingAction("cancel-decommission"); setError(null)
    try { await apiPost(`/api/v1/applications/${encodeURIComponent(applicationID)}/cancel-decommission`); await loadData(); toast.success("Application deletion cancelled. Refresh a normal plan before syncing.") }
    catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setBusy(false); setPendingAction("") }
  }

  async function retryPlans() {
    const epoch = loadEpoch.current
    setRetryingPlans(true)
    try {
      const result = await api<ListResponse<PlanRecord>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`)
      if (epoch !== loadEpoch.current) return
      setPlans(result.items)
      setActivePlan((selected) => result.items.find((plan) => plan.id === selected?.id) ?? result.items[0] ?? null)
      setPlansError(null)
    } catch (cause) { if (epoch === loadEpoch.current) setPlansError(cause) }
    finally { setRetryingPlans(false) }
  }

  async function refreshSummary() {
    try {
      const [inventory, operationList, topologyResult, planList, ruleList, rollbackTargetList] = await Promise.all([
        api<ListResponse<ManagedResource>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/resources`),
        api<ListResponse<Operation>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/operations`),
        api<ResourceTopology>(`/api/v1/applications/${encodeURIComponent(applicationID)}/topology`).catch(() => null),
        api<ListResponse<PlanRecord>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`),
        api<ListResponse<IgnoreRule>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-rules`),
        api<ListResponse<RollbackTarget>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/rollback-targets`).catch(() => ({ items: [] as RollbackTarget[] })),
      ])
      setPlansError(null); setOperationsError(null); setResources(inventory.items); setOperations(operationList.items); setTopology(topologyResult); setPlans(planList.items); setIgnoreRules(ruleList.items); setRollbackTargets(rollbackTargetList.items)
      setActivePlan((current) => current ? planList.items.find((plan) => plan.id === current.id) ?? current : planList.items[0] ?? null)
    } catch { /* the primary plan result remains useful when a secondary panel is unavailable */ }
  }

  async function retryReconciliation(operationId?: string) {
    setBusy(true); setPendingAction("retry-reconciliation"); setError(null)
    try {
      const result = await apiPost<{ status: "queued" | "review_required" | "up_to_date"; operation?: Operation; plan?: PlanRecord }>(`/api/v1/applications/${encodeURIComponent(applicationID)}/retry`, operationId ? { operationId } : {})
      if (result.plan) {
        setPlans((current) => [result.plan!, ...current.filter((item) => item.id !== result.plan!.id)])
        setActivePlan(result.plan)
      }
      if (result.operation) setOperations((current) => [result.operation!, ...current.filter((item) => item.id !== result.operation!.id)])
      if (result.status === "review_required") {
        toast.success("Fresh reconciliation plan ready for review.")
        selectTab("changes")
      } else if (result.status === "up_to_date") {
        toast.success("Live cluster state already matches the desired revision.")
      } else {
        toast.success("Fresh reconciliation plan queued.")
      }
      await loadData()
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setBusy(false); setPendingAction("") }
  }

  async function approvePlan() {
    if (!activePlan || selectionDirty) return
    setBusy(true); setPendingAction("approve-plan"); setError(null)
    try {
      await apiPost<{ id: string; expiresAt: string }>(`/api/v1/plans/${encodeURIComponent(activePlan.id)}/approvals`, { comment: approvalComment.trim(), planDigest: activePlan.plan.digest })
      const summary = await api<PlanApprovalSummary>(`/api/v1/plans/${encodeURIComponent(activePlan.id)}/approvals`)
      setApprovalSummary(summary)
      setApprovalComment("")
      toast.success(`Approval recorded: ${summary.approvedApprovals} of ${summary.requiredApprovals} required.`)
    } catch (cause) { if (!acceptRefreshedPlan(cause)) toast.error(errorMessage(cause), cause) } finally { setBusy(false); setPendingAction("") }
  }

  async function applyPlan() {
    if (!activePlan || selectionDirty) return
    setBusy(true); setPendingAction("apply-plan"); setError(null)
    try {
      const approvalIds = currentApprovalSummary?.approvals.filter((item) => item.eligible).map((item) => item.id) ?? []
      const result = await apiPost<{ operation: Operation }>(`/api/v1/plans/${encodeURIComponent(activePlan.id)}/apply`, { approvalIds })
      toast.success(result.operation.message || "Sync queued.")
      setApprovalSummary(null)
      await loadData()
    } catch (cause) {
      if (!acceptRefreshedPlan(cause)) toast.error(errorMessage(cause), cause)
    } finally { setBusy(false); setPendingAction(""); await refreshSummary() }
  }

  async function createRollbackPlan(target: RollbackRequest): Promise<boolean> {
    setBusy(true); setPendingAction("rollback-plan"); setError(null); setApprovalSummary(null)
    try {
      const plan = await apiPost<PlanRecord>(`/api/v1/applications/${encodeURIComponent(applicationID)}/rollback-plans`, { kind: target.kind, targetId: target.id, revision: target.revision })
      setActivePlan(plan)
      setPlans((current) => [plan, ...current.filter((item) => item.id !== plan.id)])
      toast.success(`Rollback plan ready: ${plan.plan.changes.length} reviewed change${plan.plan.changes.length === 1 ? "" : "s"}. No cluster resources have changed.`)
      selectTab("changes")
      await refreshSummary()
      return true
    } catch (cause) { if (!acceptRefreshedPlan(cause)) toast.error(errorMessage(cause), cause); return false }
    finally { setBusy(false); setPendingAction("") }
  }

  async function pauseSync() {
    setBusy(true); setPendingAction("pause-sync")
    try {
      const updated = await apiPost<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}/pause`, {})
      setApplication(updated)
      toast.success("Auto-sync paused. You can now make manual changes.")
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setBusy(false); setPendingAction("") }
  }

  async function updateRollbackTracking(action: "keep" | "resume") {
    if (!application) return
    if (action === "resume" && application.rollbackResumeRequiresRevision && !resumeRevision.trim()) {
      toast.error("Choose a Git revision before resuming after the first deployment.")
      return
    }
    setBusy(true); setPendingAction(`rollback-state-${action}`); setError(null)
    try {
      const updated = await apiPost<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}/rollback-state`, { action, revision: action === "resume" ? resumeRevision.trim() : "" })
      setApplication(updated)
      setResumeRevision("")
      toast.success(action === "keep" ? "Rollback pin kept. Auto-sync remains paused." : "Tracked source restored and auto-sync resumed.")
      await refreshSummary()
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setBusy(false); setPendingAction("") }
  }

  const canDeploy = workspace?.role === "owner" || workspace?.role === "deployer"
  const canApprove = workspace?.role === "owner"
  const claimableConflictKeys = ownershipConflicts.filter((item) => isClaimable(item, applicationID)).map((item) => diffId(item.identity))

  const requiredApprovals = activePlan ? activePlan.plan.requiredApprovals ?? (activePlan.plan.requiresApproval ? 1 : 0) : 0
  const currentApprovalSummary = approvalSummary?.planId === activePlan?.id ? approvalSummary : null
  const approvedApprovals = currentApprovalSummary?.approvedApprovals ?? 0
  const approvalsComplete = approvedApprovals >= requiredApprovals
  const approverRoles = activePlan?.plan.approverRoles ?? []
  const approverUserIds = activePlan?.plan.approverUserIds ?? []
  const displayedApproverRoles = approverRoles.length > 0 || approverUserIds.length > 0 || requiredApprovals === 0 ? approverRoles : ["owner"]
  const displayedApproverMembers = approverUserIds.map((userId) => {
    const member = workspaceMembers.find((item) => item.id === userId)
    return { id: userId, label: member?.displayName || member?.email || `Former member (${userId.slice(0, 8)})` }
  })
  const deletionApprovalCount = application?.approvalPolicyOverride?.deletion?.requiredApprovals ?? workspace?.approvalPolicy.deletion.requiredApprovals ?? 1
  const latestOperation = operations[0]
  const visibleOperation = operations.find((operation) => operation.status === "queued" || operation.status === "running") ?? (latestOperation?.status === "failed" ? latestOperation : null)
  const visibleOperationCheckpoint = visibleOperation?.rollbackCheckpointId ?? rollbackTargets.find((target) => target.kind === "pre_operation" && target.operationId === visibleOperation?.id)?.id
  const latestPlan = plans[0]

  const namespaceMismatch = Boolean(application?.renderer === "kustomize" && kustomization?.namespace && application.namespaces.length === 1 && kustomization.namespace !== application.namespaces[0].namespace)
  if (loading && !application) return <ApplicationDetailSkeleton />

  const createPlanDisabled = Boolean(application && (application.configurationMissing || application.decommissioning || !canDeploy || busy || (namespaceMismatch && !application.kustomizeNamespaceOverride)))
  const planBlockedByNamespace = Boolean(application && namespaceMismatch && !application.kustomizeNamespaceOverride)

  return <>
    <PageHeading title={application?.name ?? "Application not found"} description={application ? `${application.clusterName || application.clusterId} / ${application.namespaces.map((binding) => binding.namespace).join(", ") || "No namespace"}${application.autoSyncPaused ? " · Auto-sync paused" : ""}` : ""} badge={application && <StatusBadge status={application.health} />} actions={application && <>
      {pendingData.workspace && <span role="status" className="text-sm text-muted-foreground">Loading permissions…</span>}
      {planBlockedByNamespace && !application.configurationMissing && !application.decommissioning && canDeploy
        ? <Tooltip>
          <TooltipTrigger render={<span tabIndex={0} className="inline-flex" />}><Button loading={pendingAction === "create-plan"} loadingText="Calculating plan…" disabled><HugeiconsIcon icon={RefreshIcon} strokeWidth={1.8} aria-hidden="true" />Refresh plan</Button></TooltipTrigger>
          <TooltipContent>Choose a namespace override before refreshing the plan</TooltipContent>
        </Tooltip>
        : <Button loading={pendingAction === "create-plan"} loadingText="Calculating plan…" onClick={() => void createPlan()} disabled={createPlanDisabled}><HugeiconsIcon icon={RefreshIcon} strokeWidth={1.8} aria-hidden="true" />Refresh plan</Button>}
      {canApprove && <ActionMenu label="More actions" items={[
        ...(!application.decommissioning && !application.repositoryConfigurationId ? [{ label: "Edit application", icon: Edit02Icon, disabled: busy || hasPendingOperation || Boolean(application.branchTest), onSelect: () => router.push(`/applications/${applicationID}/edit`) }] : []),
        ...(!application.branchTest ? [{ label: "Test branch…", icon: TestTubeIcon, disabled: busy || hasPendingOperation || application.decommissioning || Boolean(application.configurationMissing) || Boolean(application.rollbackResumeAvailable) || Boolean(application.applicationGroupId), onSelect: () => setBranchTestOpen(true) }] : []),
        ...(!application.autoSyncPaused ? [{ label: "Pause auto-sync", icon: PauseIcon, separatorBefore: true, disabled: busy || hasPendingOperation, onSelect: () => void pauseSync() }] : []),
      ]} />}
    </>} />
      <Tabs.Root value={activeTab} onValueChange={(value) => selectTab(String(value))} className="min-w-0">
        {application && <Tabs.List aria-label="Application views" className="mb-6 flex gap-6 overflow-x-auto border-b" activateOnFocus>
          {[
            { id: "overview", label: "Overview" },
            { id: "topology", label: "Resources", count: pendingData.topology ? undefined : topology?.nodes.length },
            { id: "changes", label: "Changes", count: pendingData.plans || plansError != null ? undefined : latestPlan?.status === "current" ? latestPlan.plan.changes.length + (latestPlan.plan.ignored?.length ?? 0) : 0 },
            { id: "activity", label: "Activity" },
            { id: "pull-requests", label: "Pull requests" },
            { id: "settings", label: "Settings" },
          ].map((tab) => <Tabs.Tab key={tab.id} value={tab.id} className="flex shrink-0 items-center gap-2 border-b-2 border-transparent px-1 pb-3 text-sm font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-[active]:border-primary data-[active]:text-foreground">
            {tab.label}{tab.count !== undefined && <span className="rounded-md bg-muted px-1.5 py-0.5 text-xs tabular-nums text-muted-foreground">{tab.count}</span>}
          </Tabs.Tab>)}
        </Tabs.List>}

    {application && <BranchTestControls open={branchTestOpen} onOpenChange={setBranchTestOpen} application={application} canManage={canApprove} disabled={busy || hasPendingOperation} onSettings={() => selectTab("settings")} onReview={() => void createPlan()} onChanged={async (updated) => {
      if (updated.id !== applicationID) { router.push(`/applications/${updated.id}`); return }
      setApplication(updated)
      await loadData()
      selectTab("changes")
      toast.success(updated.branchTest ? "Branch selected. Create and review a fresh plan before deploying." : "Tracked source restored. Review a fresh plan before deploying and resuming auto-sync.")
    }} />}
    {error != null && <ErrorNotice error={error} />}
    {application && <>
      <AttentionBar application={application} canApprove={canApprove} busy={busy} hasPendingOperation={hasPendingOperation} pendingAction={pendingAction} plansError={plansError} retryingPlans={retryingPlans} onRetryPlans={() => void retryPlans()}
        conflictCount={ownershipConflict ? ownershipConflicts.length : 0} onReviewConflicts={() => setConflictOpen(true)}
        namespaceMismatch={namespaceMismatch} kustomizeNamespace={kustomization?.namespace} onReviewNamespaceSetting={() => selectTab("settings")}
        onCancelDecommission={() => void cancelDecommission()}
        onConfigureExclusion={(issue) => configureIgnore({ mode: "kind", ...(issue.resource ? { version: issue.resource.apiVersion, kind: issue.resource.kind } : {}) })}
        resumeRevision={resumeRevision} onResumeRevisionChange={setResumeRevision} onKeepPin={() => void updateRollbackTracking("keep")} onResume={() => void updateRollbackTracking("resume")} />
      <RuntimeHealthStrip application={application} healthHistory={healthHistory} />
      {ownershipConflict && <OwnershipConflictDialog open={conflictOpen} onOpenChange={setConflictOpen} conflicts={ownershipConflicts} applicationId={application.id} canApprove={canApprove} busy={busy} selectedKeys={selectedConflictKeys} onSelectedKeysChange={setSelectedConflictKeys} claimableKeys={claimableConflictKeys} onAdopt={adoptConflict} onAdoptSelected={adoptSelectedConflicts} onConfigureIgnore={configureConflictIgnore} />}
    </>}
    {!loading && application && <>
      {visibleOperation && <OperationBar operation={visibleOperation} checkpointId={visibleOperationCheckpoint} canRestore={canApprove} disabled={busy || hasPendingOperation} onRestore={(target) => void createRollbackPlan(target)} />}

        <Tabs.Panel value="overview" className="outline-none">
          <OverviewTab application={application} latestPlan={latestPlan} plansError={plansError} retryingPlans={retryingPlans} topology={topology} topologyError={topologyError} pending={{ topology: pendingData.topology, plans: pendingData.plans, operations: pendingData.operations }} operations={operations} operationsError={operationsError} canDeploy={canDeploy} busy={busy} hasPendingOperation={hasPendingOperation} creatingPlan={pendingAction === "create-plan"} planBlockedByNamespace={planBlockedByNamespace} onCreatePlan={() => void createPlan()} onRetryPlans={() => void retryPlans()} onSelectTab={selectTab} />
        </Tabs.Panel>

        <Tabs.Panel value="topology" className="outline-none">
      {pendingData.topology || pendingData.resources || pendingData.plans || pendingData.operations ? <SectionLoading label="Loading the resource map" /> : <>
      {topologyError && <div role="alert" className="mb-4 flex flex-wrap items-center gap-3 rounded-lg border border-destructive/30 p-3 text-sm"><span>Saved topology could not be loaded. The map uses available inventory.</span><ErrorDetailsButton error={topologyError} /></div>}
      <ResourceMap view={topologyView} onViewChange={(view) => { setTopologyView(view); const url = new URL(window.location.href); url.searchParams.set("tab", "topology"); if (view === "list") url.searchParams.set("view", "list"); else url.searchParams.delete("view"); window.history.pushState(null, "", url) }} canManageResources={canApprove} resourceActionsDisabled={busy || hasPendingOperation} onResourceActionComplete={async () => { setApplication(await api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`)); await refreshSummary() }} application={application} plan={plans[0]?.status === "current" ? plans[0] : null} inventory={resources} operations={operations} topology={topology} refreshing={topologyRefreshing} onRefresh={() => {
        setTopologyRefreshing(true)
        void api<ResourceTopology>(`/api/v1/applications/${encodeURIComponent(applicationID)}/topology?refresh=1`).then((result) => {
          setTopology(result); setTopologyError(null)
          void api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`).then(setApplication).catch(() => {})
          void api<ListResponse<ApplicationHealthTransition>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/health-history`).then((history) => setHealthHistory(history.items)).catch(() => {})
        }).catch((cause) => { setTopologyError(cause); toast.error(errorMessage(cause), cause) }).finally(() => setTopologyRefreshing(false))
      }} onViewDiff={(identity) => {
        const matching = plans[0]?.plan.changes.some((change) => diffId(change.identity) === diffId(identity))
        if (matching && plans[0]?.id !== activePlan?.id) setActivePlan(plans[0])
        setReviewResource(diffId(identity))
        selectTab("changes")
        window.setTimeout(() => document.getElementById(diffId(identity))?.scrollIntoView({ behavior: "smooth", block: "center" }), 100)
      }} /></>}
        </Tabs.Panel>

        <Tabs.Panel value="changes" className="min-w-0 outline-none">
          <ChangesTab plans={plans} activePlan={activePlan} onSelectPlan={(id) => { setApprovalSummary(null); setActivePlan(plans.find((plan) => plan.id === id) ?? null) }} plansLoading={pendingData.plans} plansError={plansError} retryingPlans={retryingPlans} onRetryPlans={() => void retryPlans()}
            decommissioning={application.decommissioning} canDeploy={canDeploy} canApprove={canApprove} busy={busy} hasPendingOperation={hasPendingOperation} pendingAction={pendingAction} hasOwnershipConflict={Boolean(ownershipConflict)}
            reviewResource={reviewResource} onReviewResourceChange={setReviewResource}
            selectionResources={selectionResources} selectionFields={selectionFields} selectionDirty={selectionDirty} onToggleResource={toggleResourceExclusion} onToggleField={toggleFieldExclusion}
            onResetSelection={() => { setSelectionResources(activePlan?.plan.selection?.resources ?? []); setSelectionFields(activePlan?.plan.selection?.fields ?? []) }} onSaveSelection={() => void savePlanSelection()}
            ignoreRules={ignoreRules} ignoreReasons={ignoreReasons} onIgnoreReasonChange={(key, reason) => setIgnoreReasons((current) => ({ ...current, [key]: reason }))} onSaveIgnore={(identity, path, reason) => void saveIgnoreRule(identity, path, reason)} onRemoveRule={removeIgnoreRule}
            requiredApprovals={requiredApprovals} approvedApprovals={approvedApprovals} approvalsComplete={approvalsComplete} approverRoles={displayedApproverRoles} approverMembers={displayedApproverMembers} approvalSummary={currentApprovalSummary}
            approvalComment={approvalComment} onApprovalCommentChange={setApprovalComment} onApprove={approvePlan} onApply={() => void applyPlan()} onCreatePlan={() => void createPlan()} />
        </Tabs.Panel>

        <Tabs.Panel value="activity" className="outline-none">
          <ActivityTab application={application} operations={operations} operationsLoading={pendingData.operations} rollbackTargets={rollbackTargets} rollbackLoading={pendingData.rollback} kustomization={kustomization} kustomizationError={kustomizationError} canDeploy={canDeploy} canApprove={canApprove} busy={busy} hasPendingOperation={hasPendingOperation} pendingAction={pendingAction} onRetryReconciliation={(operationId) => void retryReconciliation(operationId)} onCreateRollbackPlan={createRollbackPlan} />
        </Tabs.Panel>

        <Tabs.Panel value="pull-requests" className="outline-none">
          <PullRequestsWorkspace embedded canConfigure={canApprove} />
        </Tabs.Panel>

        <Tabs.Panel value="settings" keepMounted className="outline-none">
          <SettingsTab application={application} kustomization={kustomization} kustomizationError={kustomizationError} canApprove={canApprove} busy={busy} hasPendingOperation={hasPendingOperation} pendingAction={pendingAction} rulesLoading={pendingData.rules || pendingData.selectors} ignoreRules={ignoreRules} ignoreSelectors={ignoreSelectors} managedResourceCount={resources.length} deletionApprovalCount={deletionApprovalCount} prefill={ignorePrefill} onSaveRenderSettings={saveRenderSettings} onAddIgnore={addIgnore} onRemoveRule={removeIgnoreRule} onRemoveSelector={removeIgnoreSelector} onDeleteApplication={deleteApplication} onCancelDecommission={() => void cancelDecommission()} />
        </Tabs.Panel>
    </>}
      </Tabs.Root>
  </>
}

function SectionLoading({ label }: { label: string }) {
  return <div role="status" aria-label={label} className="space-y-4 p-5 sm:p-6"><span className="sr-only">{label}…</span><div aria-hidden="true" className="space-y-3"><Skeleton className="h-5 w-2/3 max-w-72 motion-reduce:animate-none" /><Skeleton className="h-4 w-full max-w-lg motion-reduce:animate-none" /><Skeleton className="h-4 w-4/5 max-w-md motion-reduce:animate-none" /></div></div>
}
