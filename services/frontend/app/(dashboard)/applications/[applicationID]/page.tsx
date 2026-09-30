"use client"

import Link from "next/link"
import { useParams, useRouter } from "next/navigation"
import { useEffect, useMemo, useRef, useState } from "react"
import { Dialog } from "@base-ui/react/dialog"
import { Tabs } from "@base-ui/react/tabs"
import { Button } from "@/components/ui/button"
import { ApplicationDetailSkeleton } from "@/components/application-detail-skeleton"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { Checkbox } from "@/components/ui/checkbox"
import { DataGridList } from "@/components/data-grid-table"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { ResourceMap } from "@/components/resource-map"
import { Textarea } from "@/components/ui/textarea"
import { EmptyState, FormField, PageHeading, Panel, StatusBadge } from "@/components/ui-kit"
import { ErrorDetailsButton } from "@/components/error-details"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { APIError, api, apiDelete, apiPost, errorMessage } from "@/lib/api"
import { ManifestDiff } from "@/components/manifest-diff"
import { PlanReview } from "@/components/plan-review"
import { PullRequestsWorkspace } from "@/components/pull-requests-workspace"
import type { Application, ApplicationHealthTransition, Change, FieldExclusion, Identity, IgnoreRule, IgnoreSelector, ListResponse, ManagedResource, Operation, OwnershipConflict, PlanApprovalSummary, PlanRecord, Workspace, WorkspaceMember, ResourceTopology, RollbackTarget } from "@/lib/types"

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
  const router = useRouter()
  const [reviewResource, setReviewResource] = useState("")
  const toast = useToast()
  const [application, setApplication] = useState<Application | null>(null)
  const [kustomizeHelmDraft, setKustomizeHelmDraft] = useState(false)
  const [namespaceOverrideDraft, setNamespaceOverrideDraft] = useState(false)
  const [kustomization, setKustomization] = useState<{ namespace: string; commit: string } | null>(null)
  const [kustomizationError, setKustomizationError] = useState<unknown | null>(null)
  const [workspace, setWorkspace] = useState<Workspace | null>(null)
  const [workspaceMembers, setWorkspaceMembers] = useState<WorkspaceMember[]>([])
  const [plans, setPlans] = useState<PlanRecord[]>([])
  const [activePlan, setActivePlan] = useState<PlanRecord | null>(null)
  const [expandedAppliedPlan, setExpandedAppliedPlan] = useState<string | null>(null)
  const [resources, setResources] = useState<ManagedResource[]>([])
  const [topology, setTopology] = useState<ResourceTopology | null>(null)
  const [topologyError, setTopologyError] = useState<unknown | null>(null)
  const [topologyRefreshing, setTopologyRefreshing] = useState(false)
  const [operations, setOperations] = useState<Operation[]>([])
  const [healthHistory, setHealthHistory] = useState<ApplicationHealthTransition[]>([])
  const [rollbackTargets, setRollbackTargets] = useState<RollbackTarget[]>([])
  const [rollbackRevision, setRollbackRevision] = useState("")
  const [resumeRevision, setResumeRevision] = useState("")
  const [ignoreRules, setIgnoreRules] = useState<IgnoreRule[]>([])
  const [ignoreSelectors, setIgnoreSelectors] = useState<IgnoreSelector[]>([])
  const [ignoreMode, setIgnoreMode] = useState("kind")
  const [ignoreVersion, setIgnoreVersion] = useState("")
  const [ignoreKind, setIgnoreKind] = useState("")
  const [ignoreNamespace, setIgnoreNamespace] = useState("")
  const [ignoreName, setIgnoreName] = useState("")
  const [ignoreLabelKey, setIgnoreLabelKey] = useState("")
  const [ignoreLabelValue, setIgnoreLabelValue] = useState("")
  const [ignoreReason, setIgnoreReason] = useState("")
  const [deletePolicy, setDeletePolicy] = useState("keep")
  const [selectionDraft, setSelectionDraft] = useState<{ planId: string; resources: Identity[]; fields: FieldExclusion[] } | null>(null)
  const [ignoreReasons, setIgnoreReasons] = useState<Record<string, string>>({})
  const [approvalSummary, setApprovalSummary] = useState<PlanApprovalSummary | null>(null)
  const [approvalComment, setApprovalComment] = useState("")
  const [loading, setLoading] = useState(true)
  const [pendingData, setPendingData] = useState({ workspace: true, plans: true, resources: true, operations: true, topology: true, rollback: true, rules: true, selectors: true })
  const [busy, setBusy] = useState(false)
  const [pendingAction, setPendingAction] = useState("")
  const [error, setError] = useState<unknown | null>(null)
  const [ownershipConflict, setOwnershipConflict] = useState<OwnershipConflict | null>(null)
  const [ownershipConflicts, setOwnershipConflicts] = useState<OwnershipConflict[]>([])
  const [selectedConflictKeys, setSelectedConflictKeys] = useState<string[]>([])
  const conflictEpoch = useRef(0)
  const loadEpoch = useRef(0)
  const [adoptionReason, setAdoptionReason] = useState("")
  const [previousControllerDisabled, setPreviousControllerDisabled] = useState(false)
  const [activeTab, setActiveTab] = useState("overview")

  function setConflictReview(conflicts: OwnershipConflict[]) {
    setOwnershipConflicts(conflicts)
    setOwnershipConflict(conflicts[0] ?? null)
    setSelectedConflictKeys(conflicts.filter((item) => !item.hasOwnerReferences && (!item.owner || item.owner === applicationID)).map((item) => diffId(item.identity)))
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
      setActiveTab(["overview", "topology", "changes", "components", "activity", "pull-requests", "settings"].includes(value ?? "") ? value! : "overview")
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
    if (app.decommissioning) setDeletePolicy("delete")
    setKustomizeHelmDraft(app.kustomizeHelmEnabled)
    setNamespaceOverrideDraft(app.kustomizeNamespaceOverride)
    const current = () => epoch === loadEpoch.current
    const done = (section: keyof typeof pendingData) => { if (current()) setPendingData((state) => ({ ...state, [section]: false })) }
    const appPath = `/api/v1/applications/${encodeURIComponent(applicationID)}`
    const results = await Promise.allSettled([
      api<ListResponse<Workspace>>("/api/v1/workspaces").then((value) => { if (current()) setWorkspace(value.items.find((item) => item.id === app.workspaceId) ?? null) }).finally(() => done("workspace")),
      api<ListResponse<WorkspaceMember>>(`/api/v1/workspaces/${encodeURIComponent(app.workspaceId)}/members`).catch(() => ({ items: [] as WorkspaceMember[] })).then((value) => { if (current()) setWorkspaceMembers(value.items) }),
      api<ListResponse<PlanRecord>>(`${appPath}/plans`).then((value) => { if (current()) { setPlans(value.items); setActivePlan((selected) => selected ? value.items.find((item) => item.id === selected.id) ?? value.items[0] ?? null : value.items[0] ?? null) } }).finally(() => done("plans")),
      api<ListResponse<ManagedResource>>(`${appPath}/resources`).then((value) => { if (current()) setResources(value.items) }).finally(() => done("resources")),
      api<ListResponse<Operation>>(`${appPath}/operations`).then((value) => { if (current()) setOperations(value.items) }).finally(() => done("operations")),
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
      setPlans([])
      setActivePlan(null)
      setResources([])
      setOperations([])
      setHealthHistory([])
      setRollbackTargets([])
      setIgnoreRules([])
      setIgnoreSelectors([])
      setTopology(null)
      setTopologyError(null)
      setKustomization(null)
      setKustomizationError(null)
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

  async function adoptConflict() {
    if (!ownershipConflict) return
    conflictEpoch.current += 1
    setBusy(true); setPendingAction("adopt-resource")
    try {
      await apiPost(`/api/v1/applications/${encodeURIComponent(applicationID)}/adopt`, {
        conflict: ownershipConflict,
        reason: adoptionReason.trim(),
        previousControllerDisabled,
      })
      setConflictReview([])
      setAdoptionReason("")
      setPreviousControllerDisabled(false)
      setApprovalSummary(null)
      await loadData().catch(() => {})
      const next = await api<{ conflict: OwnershipConflict | null; conflicts: OwnershipConflict[] }>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ownership-conflict`).catch(() => null)
      if (next) setConflictReview(next.conflicts ?? (next.conflict ? [next.conflict] : []))
      toast.success("Resource claimed without changing its workload. Auto-sync is paused; create and review a fresh plan before syncing.")
    } finally { setBusy(false); setPendingAction("") }
  }

  async function adoptSelectedConflicts() {
    const selected = ownershipConflicts.filter((item) => selectedConflictKeys.includes(diffId(item.identity)))
    if (selected.length === 0) return
    conflictEpoch.current += 1
    setBusy(true); setPendingAction("adopt-batch")
    try {
      const result = await apiPost<{ claimed: number; failed: number; results: { identity: Identity; status: string; error?: string }[] }>(`/api/v1/applications/${encodeURIComponent(applicationID)}/adopt-batch`, {
        conflicts: selected,
        reason: adoptionReason.trim(),
        previousControllerDisabled,
      })
      setAdoptionReason("")
      setPreviousControllerDisabled(false)
      setApprovalSummary(null)
      await loadData().catch(() => {})
      const next = await api<{ conflict: OwnershipConflict | null; conflicts: OwnershipConflict[] }>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ownership-conflict`).catch(() => null)
      if (next) setConflictReview(next.conflicts ?? (next.conflict ? [next.conflict] : []))
      if (result.failed) toast.error(`${result.claimed} claimed; ${result.failed} failed. Refresh the conflict list and retry the remaining resources.`)
      else toast.success(`${result.claimed} resources claimed without changing workloads. Auto-sync is paused; review a fresh plan before syncing.`)
    } finally { setBusy(false); setPendingAction("") }
  }

  function configureConflictIgnore() {
    if (!ownershipConflict) return
    setIgnoreMode("resource")
    setIgnoreVersion(ownershipConflict.identity.apiVersion)
    setIgnoreKind(ownershipConflict.identity.kind)
    setIgnoreNamespace(ownershipConflict.identity.namespace)
    setIgnoreName(ownershipConflict.identity.name)
    setIgnoreReason("Managed by another controller")
    selectTab("settings")
  }

  async function saveRenderSettings() {
    if (!application || application.renderer !== "kustomize") return
    setBusy(true); setPendingAction("render-settings"); setError(null)
    try {
      await api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}/render-settings`, { method: "PUT", body: JSON.stringify({ kustomizeHelmEnabled: kustomizeHelmDraft, kustomizeNamespaceOverride: namespaceOverrideDraft }) })
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

  async function addIgnore() {
    if (!application) return
    setBusy(true); setPendingAction("add-ignore"); setError(null)
    try {
      if (ignoreMode === "resource") {
        const rule = await apiPost<IgnoreRule>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-rules`, { identity: { clusterId: application.clusterId, apiVersion: ignoreVersion.trim(), kind: ignoreKind.trim(), namespace: ignoreNamespace.trim(), name: ignoreName.trim() }, reason: ignoreReason.trim() })
        setIgnoreRules((current) => [rule, ...current])
      } else {
        const selector = await apiPost<IgnoreSelector>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-selectors`, { apiVersion: ignoreMode === "kind" ? ignoreVersion.trim() : "", kind: ignoreMode === "kind" ? ignoreKind.trim() : "", labelKey: ignoreMode === "label" ? ignoreLabelKey.trim() : "", labelValue: ignoreMode === "label" ? ignoreLabelValue.trim() : "", reason: ignoreReason.trim() })
        setIgnoreSelectors((current) => [selector, ...current])
      }
      setIgnoreReason(""); setIgnoreName(""); invalidatePlans()
      toast.success("Exclusion saved. Existing plans are stale; refresh the plan before syncing.")
    } catch (cause) { toast.error(errorMessage(cause), cause) }
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

  async function deleteApplication() {
    const result = await api<{ deleted: boolean; plan?: PlanRecord }>(`/api/v1/applications/${encodeURIComponent(applicationID)}?resources=${deletePolicy}`, { method: "DELETE" })
    if (result.deleted) { toast.success("Application deleted."); router.push("/applications"); router.refresh(); return }
    if (result.plan) {
      setDeletePolicy("delete")
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
      setResources(inventory.items); setOperations(operationList.items); setTopology(topologyResult); setPlans(planList.items); setIgnoreRules(ruleList.items); setRollbackTargets(rollbackTargetList.items)
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

  async function createRollbackPlan(target: { kind: "successful_sync" | "pre_operation" | "git_revision"; id?: string; revision?: string }) {
    setBusy(true); setPendingAction("rollback-plan"); setError(null); setApprovalSummary(null)
    try {
      const plan = await apiPost<PlanRecord>(`/api/v1/applications/${encodeURIComponent(applicationID)}/rollback-plans`, { kind: target.kind, targetId: target.id, revision: target.revision })
      setActivePlan(plan)
      setPlans((current) => [plan, ...current.filter((item) => item.id !== plan.id)])
      toast.success(`Rollback plan ready: ${plan.plan.changes.length} reviewed change${plan.plan.changes.length === 1 ? "" : "s"}. No cluster resources have changed.`)
      selectTab("changes")
      setRollbackRevision("")
      await refreshSummary()
    } catch (cause) { if (!acceptRefreshedPlan(cause)) toast.error(errorMessage(cause), cause) }
    finally { setBusy(false); setPendingAction("") }
  }

  async function pauseSync() {
    setBusy(true); setPendingAction("pause-sync")
    try {
      const updated = await apiPost<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}/pause`, {})
      setApplication(updated)
      toast.success("Reconciliation paused. You can now make manual changes.")
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
      toast.success(action === "keep" ? "Rollback pin kept. Automatic reconciliation remains paused." : "Tracked source restored and reconciliation resumed.")
      await refreshSummary()
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setBusy(false); setPendingAction("") }
  }

  const changeCounts = activePlan?.plan.changes.reduce((acc, item) => ({ ...acc, [item.kind]: acc[item.kind] + 1 }), { create: 0, update: 0, delete: 0 })
  const canDeploy = workspace?.role === "owner" || workspace?.role === "deployer"
  const canApprove = workspace?.role === "owner"
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
  const latestCounts = latestPlan?.plan.changes.reduce((counts, change) => ({ ...counts, [change.kind]: counts[change.kind] + 1 }), { create: 0, update: 0, delete: 0 })
  const resourceKinds = Object.entries((topology?.nodes ?? []).reduce<Record<string, number>>((counts, node) => {
    counts[node.identity.kind] = (counts[node.identity.kind] ?? 0) + 1
    return counts
  }, {})).sort((a, b) => b[1] - a[1])

  const namespaceMismatch = Boolean(application?.renderer === "kustomize" && kustomization?.namespace && application.namespaces.length === 1 && kustomization.namespace !== application.namespaces[0].namespace)

  if (loading && !application) return <ApplicationDetailSkeleton />

  return <>
    <PageHeading title={application?.name ?? "Application not found"} description={application ? `${application.renderer} · ${application.manifestPath} · ${application.revision}` : ""} badge={application && <StatusBadge status={application.health} />} actions={application && <>{pendingData.workspace && <span role="status" className="text-xs text-muted-foreground">Loading permissions…</span>}{canApprove && !application.decommissioning && <Link href={`/applications/${applicationID}/edit`}><Button variant="outline">Edit application</Button></Link>}<Button variant="outline" loading={pendingAction === "create-plan"} loadingText="Calculating plan…" onClick={() => void createPlan()} disabled={application.decommissioning || !canDeploy || busy || (namespaceMismatch && !application.kustomizeNamespaceOverride)} title={namespaceMismatch && !application.kustomizeNamespaceOverride ? "Choose a namespace override before refreshing the plan" : undefined}><span aria-hidden="true">↻</span> Refresh plan</Button>{canApprove && !application.autoSyncPaused && <Button variant="outline" loading={pendingAction === "pause-sync"} loadingText="Pausing…" disabled={busy || hasPendingOperation} onClick={() => void pauseSync()}>Pause reconciliation</Button>}</>} />
      <Tabs.Root value={activeTab} onValueChange={(value) => selectTab(String(value))} className="min-w-0">
        {application && <Tabs.List aria-label="Application views" className="mb-6 flex gap-6 overflow-x-auto border-b" activateOnFocus>
          {[
            { id: "overview", label: "Overview" },
            { id: "topology", label: "Topology", count: pendingData.topology ? undefined : topology?.nodes.length },
            { id: "changes", label: "Plan & diff", count: pendingData.plans ? undefined : latestPlan?.status === "current" ? latestPlan.plan.changes.length + (latestPlan.plan.ignored?.length ?? 0) : 0 },
            { id: "components", label: "Managed components", count: pendingData.resources ? undefined : resources.length },
            { id: "activity", label: "Activity & source", count: pendingData.operations ? undefined : operations.length },
            { id: "pull-requests", label: "Pull requests" },
            { id: "settings", label: "Settings" },
          ].map((tab) => <Tabs.Tab key={tab.id} value={tab.id} className="flex shrink-0 items-center gap-2 border-b-2 border-transparent px-1 pb-3 text-xs font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-[active]:border-primary data-[active]:text-foreground">
            {tab.label}{tab.count !== undefined && <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] tabular-nums text-muted-foreground">{tab.count}</span>}
          </Tabs.Tab>)}
        </Tabs.List>}

    {error != null && <ErrorNotice error={error} />}
    {application && <div className="mb-5 flex flex-wrap items-center justify-between gap-3 rounded-xl border bg-card px-4 py-3">
      <div className="min-w-0 flex-1 text-xs">
        {application.statusIssues?.length > 0 ? <div role="alert" className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <span className="font-medium text-destructive">{application.statusIssues.map(issue => issue.source === "git" ? "Git repository" : issue.source === "cluster" ? "Target cluster" : "Application").join(" & ")} check failed</span>
          <ErrorDetailsButton label="View diagnostics" error={{ name: "ApplicationCheckFailed", message: "The current state could not be verified. Refresh the plan to recheck Git and Kubernetes connectivity.", applicationId: application.id, checkedAt: application.lastCheckedAt, issues: application.statusIssues }} />
        </div> : <span className="text-muted-foreground">{application.lastCheckedAt ? `Last checked ${new Date(application.lastCheckedAt).toLocaleString()}` : "Application checks pending"}</span>}
      </div>
      <Dialog.Root>
        <Dialog.Trigger render={<Button variant="ghost" size="sm" className="gap-2" />}><span className="text-xs text-muted-foreground">Runtime health</span><StatusBadge status={application.healthCondition?.status ?? "Unknown"} /><span aria-hidden="true">↗</span></Dialog.Trigger>
        <Dialog.Portal>
          <Dialog.Backdrop className="fixed inset-0 z-50 bg-black/50" />
          <Dialog.Popup className="fixed left-1/2 top-1/2 z-50 max-h-[85dvh] w-[calc(100%-2rem)] max-w-2xl -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-2xl border bg-card p-5 shadow-xl sm:p-6">
            <div className="flex items-center justify-between gap-3"><Dialog.Title className="text-lg font-semibold">Runtime health</Dialog.Title><Dialog.Close render={<Button variant="ghost" size="sm" />}>Close</Dialog.Close></div>
            <Dialog.Description className="mt-1 text-sm text-muted-foreground">Kubernetes workload health is separate from Git sync state.</Dialog.Description>
            <div className="mt-4"><StatusBadge status={application.healthCondition?.status ?? "Unknown"} /></div>
      <p className="mt-3 text-sm"><strong>{application.healthCondition?.reason ?? "HealthNotObserved"}:</strong> {application.healthCondition?.message ?? "Live Kubernetes health has not been observed yet."}</p>
      <p className="mt-1 text-xs text-muted-foreground">Last transition {application.healthCondition?.lastTransitionTime ? new Date(application.healthCondition.lastTransitionTime).toLocaleString() : "not recorded"}{application.healthCondition?.observedAt ? ` · observed ${new Date(application.healthCondition.observedAt).toLocaleString()}` : ""}</p>
      {(application.healthCondition?.warnings?.length ?? 0) > 0 && <ul className="mt-3 list-disc space-y-1 pl-5 text-xs text-amber-700 dark:text-amber-300">{application.healthCondition?.warnings.map((warning, index) => <li key={`${warning}-${index}`}>{warning}</li>)}</ul>}
      {(application.healthCondition?.resources?.length ?? 0) > 0 && <div className="mt-4 border-t pt-3"><h3 className="text-xs font-semibold">Resources needing attention</h3><ul className="mt-2 space-y-2">{application.healthCondition?.resources.map((item, index) => <li key={`${item.identity.kind}-${item.identity.namespace}-${item.identity.name}-${index}`} className="flex flex-wrap items-start gap-2 rounded-lg border p-2.5 text-xs"><StatusBadge status={item.status} /><span className="min-w-0 flex-1"><strong>{item.identity.kind} {item.identity.namespace ? `${item.identity.namespace}/` : ""}{item.identity.name}</strong><span className="mt-1 block text-muted-foreground">{item.reason}{item.readiness ? ` · ${item.readiness}` : ""}{item.phase ? ` · phase ${item.phase}` : ""} · {item.message}</span></span></li>)}</ul></div>}
      <details className="mt-4 border-t pt-3"><summary className="cursor-pointer text-xs font-medium">Recent condition transitions ({healthHistory.length})</summary>{healthHistory.length ? <ol className="mt-3 space-y-2">{healthHistory.map((item) => <li key={item.id} className="flex flex-wrap items-center gap-2 text-xs"><StatusBadge status={item.status} /><strong>{item.reason}</strong><span className="min-w-0 flex-1 text-muted-foreground">{item.message}</span><time className="text-muted-foreground" dateTime={item.changedAt}>{new Date(item.changedAt).toLocaleString()}</time></li>)}</ol> : <p className="mt-2 text-xs text-muted-foreground">No health transitions have been recorded yet.</p>}</details>

          </Dialog.Popup>
        </Dialog.Portal>
      </Dialog.Root>
    </div>}
    {application?.decommissioning && <div role="status" className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-300/70 bg-amber-50 p-4 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><span>Deletion in progress. Auto-sync is paused. Review and apply the deletion plan, then finish removing the application.</span>{canApprove && <Button size="sm" variant="outline" disabled={busy || hasPendingOperation} onClick={() => void cancelDecommission()}>Cancel deletion</Button>}</div>}
    {application?.autoSyncPaused && <div role="status" className="mb-5 flex flex-wrap items-center justify-between gap-4 rounded-xl border border-amber-300/70 bg-amber-50/70 p-4 text-amber-950 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-100"><div className="min-w-0 flex-1"><p className="text-sm font-semibold">Automatic reconciliation is paused</p><p className="mt-1 text-xs leading-5">{application.rollbackResumeAvailable ? "JustCD is pinned to the approved target. An owner can keep this pin or explicitly resume the previous tracked source." : "JustCD will not automatically sync while paused. Manual changes are protected from automatic reconciliation. Resuming may overwrite them with the Git configuration."}</p>{application.rollbackResumeRequiresRevision && <div className="mt-3 max-w-md"><FormField label="Git revision to use before resuming" htmlFor="rollback-resume-revision"><Input id="rollback-resume-revision" value={resumeRevision} onChange={(event) => setResumeRevision(event.target.value)} placeholder="branch, tag, or commit" /></FormField></div>}</div>{canApprove && <div className="flex flex-wrap gap-2">{application.rollbackResumeAvailable && <Button size="sm" variant="outline" loading={pendingAction === "rollback-state-keep"} loadingText="Keeping pin…" disabled={busy || hasPendingOperation} onClick={() => void updateRollbackTracking("keep")}>Keep rollback pin</Button>}<Button size="sm" loading={pendingAction === "rollback-state-resume"} loadingText="Resuming…" disabled={busy || hasPendingOperation || (application.rollbackResumeRequiresRevision && !resumeRevision.trim())} onClick={() => void updateRollbackTracking("resume")}>{application.rollbackResumeAvailable ? "Resume previous source" : "Resume reconciliation"}</Button></div>}</div>}
    {namespaceMismatch && !application?.kustomizeNamespaceOverride && <div role="alert" className="mb-5 rounded-xl border border-amber-300/60 bg-amber-50 px-4 py-3 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><strong>Kustomize namespace: {kustomization?.namespace}.</strong> JustCD target: {application?.namespaces[0]?.namespace}. The namespaces differ, so a plan cannot be refreshed until an owner enables the transform. {application?.applicationGroupId ? <Link className="font-semibold underline underline-offset-4" href={`/application-groups/${application.applicationGroupId}`}>Review group settings →</Link> : <button type="button" className="font-semibold underline underline-offset-4" onClick={() => selectTab("settings")}>Review namespace setting →</button>}</div>}
    {ownershipConflict && <section role="alert" aria-label="Existing resource ownership conflict" className="mb-5 rounded-xl border border-amber-400/70 bg-amber-50 p-5 text-amber-950 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-100">
      <h2 className="text-sm font-semibold">{ownershipConflicts.length} rendered resource{ownershipConflicts.length === 1 ? "" : "s"} already exist{ownershipConflicts.length === 1 ? "s" : ""}</h2>
      <p className="mt-1 text-xs leading-5">{ownershipConflict.identity.kind} <span className="font-mono">{ownershipConflict.identity.namespace || "cluster"}/{ownershipConflict.identity.name}</span> exists in Kubernetes, but JustCD has not recorded it as managed by this application. Sync is blocked until these conflicts are resolved. Review each resource before claiming it; resources with another owner or Kubernetes owner references cannot be claimed here.</p>
      <dl className="mt-3 grid gap-2 text-[11px] sm:grid-cols-3"><div><dt className="text-amber-700 dark:text-amber-300">UID</dt><dd className="break-all font-mono">{ownershipConflict.uid}</dd></div><div><dt className="text-amber-700 dark:text-amber-300">Resource version</dt><dd className="font-mono">{ownershipConflict.resourceVersion}</dd></div><div><dt className="text-amber-700 dark:text-amber-300">JustCD owner label</dt><dd className="break-all font-mono">{ownershipConflict.owner || "None"}</dd></div></dl>
      {ownershipConflicts.length > 1 && <div className="mt-4 rounded-lg border border-amber-300/70 bg-background/70 p-3 dark:border-amber-900">
        <div className="mb-2 flex flex-wrap items-center justify-between gap-2"><strong className="text-xs">Select resources to claim</strong><span className="text-[11px]">{selectedConflictKeys.length} of {ownershipConflicts.length} selected</span></div>
        <div className="max-h-64 divide-y overflow-y-auto">{ownershipConflicts.map((item) => { const key = diffId(item.identity); const blocked = item.hasOwnerReferences || Boolean(item.owner && item.owner !== application?.id); return <label key={key} className="flex cursor-pointer items-start gap-3 py-2 text-xs"><Checkbox checked={selectedConflictKeys.includes(key)} disabled={blocked || busy} onCheckedChange={(checked) => setSelectedConflictKeys((current) => checked ? [...current, key] : current.filter((value) => value !== key))} /><span className="min-w-0 flex-1"><span className="font-medium">{item.identity.kind}</span> <span className="break-all font-mono">{item.identity.namespace || "cluster"}/{item.identity.name}</span><span className="block break-all text-[10px] text-muted-foreground">UID {item.uid} · RV {item.resourceVersion}</span></span>{blocked && <span className="text-[10px] text-amber-700 dark:text-amber-300">Other owner</span>}</label> })}</div>
      </div>}
      <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-amber-300/60 pt-4 dark:border-amber-900">
        {canApprove && <Button size="sm" variant="outline" onClick={configureConflictIgnore}>Keep externally managed · configure ignore</Button>}
        {canApprove && ownershipConflicts.length > 1 && <ConfirmDisclosure trigger={`Take over selected (${selectedConflictKeys.length})`} triggerVariant="outline" title={`Take over ${selectedConflictKeys.length} selected resources?`} description="JustCD will claim only ownership labels and inventory records. Auto-sync will pause. Each resource is rechecked before its claim; earlier successful claims remain if a later resource changes. Review a fresh plan before applying workload changes." confirmLabel="Claim selected resources" confirmDisabled={!previousControllerDisabled || selectedConflictKeys.length === 0} onConfirm={adoptSelectedConflicts} disabled={busy || selectedConflictKeys.length === 0}>
          <div className="space-y-3"><FormField label="Reason for takeover (optional)" htmlFor="batch-adoption-reason"><Textarea id="batch-adoption-reason" value={adoptionReason} onChange={(event) => setAdoptionReason(event.target.value)} maxLength={500} placeholder="Migrating this deployment from the previous controller" /></FormField><label className="flex items-start gap-2 text-xs"><Checkbox checked={previousControllerDisabled} onCheckedChange={(checked) => setPreviousControllerDisabled(Boolean(checked))} /><span>I have stopped the previous controller from reconciling these resources.</span></label></div>
        </ConfirmDisclosure>}
        {canApprove && ownershipConflicts.length === 1 && <ConfirmDisclosure trigger="Take over in JustCD" triggerVariant="outline" title={`Take over ${ownershipConflict.identity.kind} ${ownershipConflict.identity.name}?`} description="JustCD will claim the ownership label and inventory record only. It will pause auto-sync; no workload fields change until you review and apply a fresh plan. Stop the previous controller first or it may undo this claim." confirmLabel="Claim resource" confirmDisabled={!previousControllerDisabled} onConfirm={adoptConflict} disabled={busy || Boolean(ownershipConflict.owner && ownershipConflict.owner !== application?.id) || ownershipConflict.hasOwnerReferences}>
          <div className="space-y-3"><FormField label="Reason for takeover (optional)" htmlFor="adoption-reason"><Textarea id="adoption-reason" value={adoptionReason} onChange={(event) => setAdoptionReason(event.target.value)} maxLength={500} placeholder="Migrating this deployment from the previous controller" /></FormField><label className="flex items-start gap-2 text-xs"><Checkbox checked={previousControllerDisabled} onCheckedChange={(checked) => setPreviousControllerDisabled(Boolean(checked))} /><span>I have stopped the previous controller from reconciling this resource.</span></label></div>
        </ConfirmDisclosure>}
        {!canApprove && <span className="text-xs">A workspace owner must choose how to resolve this conflict.</span>}
      </div>
      <p className="mt-3 text-xs leading-5">Or change the Git manifest or remove the old object through its current controller, then refresh the plan. JustCD will not delete an untracked resource for you.</p>
      {(ownershipConflict.owner && ownershipConflict.owner !== application?.id || ownershipConflict.hasOwnerReferences) && <p className="mt-3 text-xs">Takeover is unavailable because the object belongs to another JustCD application or is a Kubernetes dependent. Resolve that ownership first.</p>}
    </section>}
    {!loading && application && <>
      {visibleOperation && <div role="status" aria-live="polite" className={`mb-5 rounded-xl border px-4 py-3 ${visibleOperation.status === "failed" ? "border-destructive/30 bg-destructive/5" : "bg-card"}`}>
        <div className="flex flex-wrap items-center gap-3"><StatusBadge status={visibleOperation.status} /><span className="text-xs font-medium">{operationPhaseLabel(visibleOperation.progress?.phase, visibleOperation.status)}</span><span className="ml-auto text-[11px] tabular-nums text-muted-foreground">{visibleOperation.progress?.completed.length ?? 0} / {visibleOperation.progress?.total ?? 0} resources</span></div>
        {visibleOperation.progress?.total ? <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-muted"><div className={`h-full rounded-full ${visibleOperation.status === "failed" ? "bg-destructive" : "bg-primary"}`} style={{ width: `${Math.min(100, Math.round(((visibleOperation.progress.completed?.length ?? 0) / visibleOperation.progress.total) * 100))}%` }} /></div> : null}
        <div className="mt-2 flex flex-wrap items-center justify-between gap-2 text-[11px] text-muted-foreground"><span>{visibleOperation.progress?.current ? `Now: ${resourceLabel(visibleOperation.progress.current)}` : visibleOperation.message}</span><div className="flex items-center gap-2">{visibleOperation.status === "failed" && canApprove && visibleOperationCheckpoint && <Button size="sm" variant="outline" disabled={busy || hasPendingOperation} onClick={() => void createRollbackPlan({ kind: "pre_operation", id: visibleOperationCheckpoint })}>Restore state before this {visibleOperation.type === "rollback" ? "rollback" : "sync"}</Button>}{visibleOperation.status === "failed" && <ErrorDetailsButton label="Debug details" error={{ name: "SyncOperationError", message: visibleOperation.message, operationId: visibleOperation.id, applicationId: visibleOperation.applicationId, planId: visibleOperation.planId, status: visibleOperation.status, startedAt: visibleOperation.startedAt, finishedAt: visibleOperation.finishedAt, progress: visibleOperation.progress }} />}</div></div>
      </div>}


        <Tabs.Panel value="overview" className="space-y-5 outline-none">
      <div className="grid gap-3 rounded-xl border bg-card p-3 sm:grid-cols-3 sm:divide-x sm:p-4">
        <SummaryFact label="Target" value={application.namespaces.map((binding) => binding.namespace).join(", ") || "No namespace"} />
        <SummaryFact label="Latest plan" value={latestPlan ? `${latestPlan.plan.changes.length} changes · ${latestPlan.status}` : "No plan yet"} loading={pendingData.plans} />
        <SummaryFact label="Last sync" value={application.lastSyncedRevision ? `${application.lastSyncedRevision.slice(0, 12)} · ${latestOperation?.status ?? "completed"}` : "Not synced yet"} loading={pendingData.operations} />
      </div>
          <div className="grid gap-5 xl:grid-cols-[minmax(0,1.5fr)_minmax(280px,1fr)]">
            <Panel title="Delivery state" description="The current Git-to-cluster picture, without opening the full diff.">
              {pendingData.plans ? <SectionLoading label="Loading the latest plan" compact /> :
              <div className="space-y-5 p-5">
                <div className="grid grid-cols-3 gap-2"><ChangeCount label="Create" count={latestCounts?.create ?? 0} color="text-emerald-700 bg-emerald-50 dark:text-emerald-300 dark:bg-emerald-950/40" /><ChangeCount label="Update" count={latestCounts?.update ?? 0} color="text-blue-700 bg-blue-50 dark:text-blue-300 dark:bg-blue-950/40" /><ChangeCount label="Delete" count={latestCounts?.delete ?? 0} color="text-rose-700 bg-rose-50 dark:text-rose-300 dark:bg-rose-950/40" /></div>
                {latestPlan?.plan.requiresApproval && <p className="rounded-lg border border-amber-300/70 bg-amber-50/70 px-3 py-2 text-xs text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200">{latestPlan.plan.requiredApprovals ?? 1} eligible approval{(latestPlan.plan.requiredApprovals ?? 1) === 1 ? "" : "s"} required before this plan can be applied.</p>}
                <div className="flex flex-wrap items-center justify-between gap-3 border-t pt-4"><span className="text-xs text-muted-foreground">{latestPlan ? `Plan ${latestPlan.status} · expires ${new Date(latestPlan.expiresAt).toLocaleTimeString()}` : "Create a plan to compare Git with the cluster."}</span><Button size="sm" variant="outline" onClick={() => selectTab("changes")}>Review plan →</Button></div>
              </div>}
            </Panel>
            <Panel title="Resource footprint" description="Connected objects observed for this application.">
              {pendingData.topology ? <SectionLoading label="Loading saved topology" compact /> : topologyError ? <div role="alert" className="space-y-2 p-5 text-xs text-muted-foreground"><p>Saved topology could not be loaded.</p><ErrorDetailsButton error={topologyError} /></div> :
              <div className="space-y-4 p-5"><p className="text-2xl font-semibold tabular-nums">{topology?.nodes.length ?? 0}<span className="ml-2 text-xs font-normal text-muted-foreground">resources · {topology?.edges.length ?? 0} relationships</span></p>
                <div className="flex flex-wrap gap-1.5">{resourceKinds.length ? resourceKinds.slice(0, 8).map(([kind, count]) => <span key={kind} className="rounded-md border bg-muted/30 px-2 py-1 text-[11px]">{count} {kind}</span>) : <span className="text-xs text-muted-foreground">No resources observed yet.</span>}</div>
                <div className="border-t pt-4"><Button size="sm" variant="outline" onClick={() => selectTab("topology")}>Explore topology →</Button></div>
              </div>}
            </Panel>
          </div>
          <div className="grid gap-5 sm:grid-cols-2"><InfoCard label="Git revision" value={application.revision} note={`${application.renderer} · ${application.manifestPath}`} mono /><InfoCard label="Sync policy" value={application.syncPolicy === "auto-safe" ? "Auto-safe" : "Manual"} note={application.syncPolicy === "auto-safe" ? `Checks every ${application.pollSeconds}s; stops before deletion` : "Every sync is user initiated"} /></div>
          {pendingData.operations ? <Panel title="Latest operation"><SectionLoading label="Loading recent operations" compact /></Panel> : latestOperation && <Panel title="Latest operation" action={<Button size="sm" variant="ghost" onClick={() => selectTab("activity")}>View activity →</Button>}><div className="flex flex-wrap items-center gap-3 p-5"><StatusBadge status={latestOperation.status} /><span className="min-w-0 flex-1 truncate text-xs">{latestOperation.message || "Sync operation"}</span><span className="text-[11px] text-muted-foreground">{new Date(latestOperation.startedAt).toLocaleString()}</span></div></Panel>}
        </Tabs.Panel>

        <Tabs.Panel value="topology" className="outline-none">
      {pendingData.topology || pendingData.resources || pendingData.plans || pendingData.operations ? <SectionLoading label="Loading the resource map" /> : <>
      {topologyError && <div role="alert" className="mb-4 rounded-lg border border-destructive/30 p-3 text-xs">Saved topology could not be loaded. The map uses available inventory. <ErrorDetailsButton error={topologyError} /></div>}
      <ResourceMap canManageResources={canApprove} resourceActionsDisabled={busy || hasPendingOperation} onResourceActionComplete={async () => { setApplication(await api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`)); await refreshSummary() }} application={application} plan={plans[0]?.status === "current" ? plans[0] : null} inventory={resources} operations={operations} topology={topology} refreshing={topologyRefreshing} onRefresh={() => {
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
          <Panel title={activePlan?.plan.rollback ? "Rollback plan & diff" : activePlan?.plan.decommission ? "Deletion plan & diff" : "Plan & diff"} description={activePlan?.plan.rollback ? "A rollback is a new reviewed plan that restores a recorded deployment or Git revision." : activePlan?.plan.decommission ? "Every managed resource below will be checked again before it is deleted." : "A reviewed plan is a snapshot of the desired Git commit and live cluster state."} action={plans.length > 0 && <FormSelect ariaLabel="Select plan" value={activePlan?.id ?? ""} onValueChange={(id) => { setApprovalSummary(null); setActivePlan(plans.find((plan) => plan.id === id) ?? null) }} className="h-8 max-w-[220px] text-xs" items={plans.map((plan) => ({ value: plan.id, label: `${new Date(plan.createdAt).toLocaleString()} · ${plan.plan.rollback ? "rollback · " : plan.plan.decommission ? "deletion · " : ""}${plan.status}` }))} />}>
            {pendingData.plans ? <SectionLoading label="Loading plans and differences" /> : !activePlan ? <EmptyState title="No review plan yet" description="Build a plan to render Git manifests and compare them with live, app-owned resources." /> : <div className="p-4 sm:p-5">
              {activePlan.status === "applied" && <div role="status" className="mb-4 rounded-lg border border-emerald-200 bg-emerald-50/60 p-4 dark:border-emerald-900 dark:bg-emerald-950/20"><p className="text-sm font-semibold">Plan successfully applied</p><p className="mt-1 text-xs leading-5 text-muted-foreground">These changes have already been executed. This plan is a historical snapshot, not a list of pending changes. Refresh the plan to compare Git with the current cluster state.</p><div className="mt-3 flex flex-wrap gap-2"><Button size="sm" variant="outline" onClick={() => setExpandedAppliedPlan(expandedAppliedPlan === activePlan.id ? null : activePlan.id)}>{expandedAppliedPlan === activePlan.id ? "Hide applied diff" : "View applied diff"}</Button><Button size="sm" onClick={() => void createPlan()} disabled={busy || hasPendingOperation || !canDeploy || application?.decommissioning}>Refresh plan</Button></div></div>}
              {(activePlan.status !== "applied" || expandedAppliedPlan === activePlan.id) && <>
              <div className="mb-4 flex flex-wrap items-center gap-2"><StatusBadge status={activePlan.status} /><span className="font-mono text-xs text-muted-foreground">{activePlan.plan.revision.slice(0, 12)}</span><span className="text-xs text-muted-foreground">· expires {new Date(activePlan.expiresAt).toLocaleTimeString()}</span><span className="ml-auto font-mono text-xs text-muted-foreground">{activePlan.plan.digest.slice(0, 16)}</span></div>
              {activePlan.trigger && <p className="mb-4 break-all text-xs text-muted-foreground">Triggered by {activePlan.trigger.provider} push to <code>{activePlan.trigger.ref}</code> · delivery <code>{activePlan.trigger.deliveryId}</code> · reported <code>{activePlan.trigger.reportedCommit.slice(0, 12)}</code>{activePlan.trigger.reportedCommit !== activePlan.plan.revision ? " · branch advanced before planning" : ""}</p>}
              {activePlan.plan.rollback && <section className="mb-5 rounded-lg border border-amber-300/70 bg-amber-50/70 p-3.5 text-sm leading-6 text-amber-950 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-100"><div className="font-semibold">Rollback target · {activePlan.plan.rollback.kind === "successful_sync" ? "successful deployment" : activePlan.plan.rollback.kind === "pre_operation" ? "state before an interrupted operation" : "Git revision"}</div><p className="mt-1">{activePlan.plan.rollback.revision ? `Resolved commit ${activePlan.plan.rollback.revision.slice(0, 12)}.` : "This checkpoint has no historical successful commit; the application ref will remain paused."}{activePlan.plan.rollback.createdAt ? ` Captured ${new Date(activePlan.plan.rollback.createdAt).toLocaleString()}.` : ""}{activePlan.plan.rollback.operationId ? ` Operation ${activePlan.plan.rollback.operationId.slice(0, 8)}.` : ""} {activePlan.plan.rollback.resourceCount !== undefined ? `${activePlan.plan.rollback.resourceCount} resources in target.` : ""}</p><p className="mt-2 font-medium">Kubernetes resources cannot be changed atomically. This diff may contain creations, updates, and deletions; a failure can leave a visible partial result.</p>{(activePlan.plan.ignored?.length ?? 0) > 0 && <p className="mt-2">{activePlan.plan.ignored?.length} difference{activePlan.plan.ignored?.length === 1 ? " is" : "s are"} limited by the application&apos;s current ignore rules.</p>}</section>}
              <div className="mb-5 grid grid-cols-3 gap-2"><ChangeCount label="Create" count={changeCounts?.create ?? 0} color="text-emerald-700 bg-emerald-50 dark:text-emerald-300 dark:bg-emerald-950/40" /><ChangeCount label="Update" count={changeCounts?.update ?? 0} color="text-blue-700 bg-blue-50 dark:text-blue-300 dark:bg-blue-950/40" /><ChangeCount label="Delete" count={changeCounts?.delete ?? 0} color="text-rose-700 bg-rose-50 dark:text-rose-300 dark:bg-rose-950/40" /></div>
              {ignoreRules.length > 0 && <details className="mb-5 rounded-lg border bg-muted/10"><summary className="cursor-pointer px-4 py-3 text-sm font-medium">Persistent ignore rules ({ignoreRules.length})</summary><div className="flex items-start justify-between gap-3 border-b px-4 py-3"><div><h3 className="text-xs font-semibold">Persistent ignore rules</h3><p className="mt-1 text-xs text-muted-foreground">Applied to manual and Auto-safe plans. Changes invalidate reviewed plans.</p></div><span className="rounded-md bg-muted px-2 py-1 text-xs tabular-nums">{ignoreRules.length}</span></div><div className="divide-y">{ignoreRules.map((rule) => <div key={rule.id} className="flex flex-wrap items-center gap-3 px-4 py-3"><span className="min-w-0 flex-1 text-xs font-medium">{rule.identity.kind} {rule.identity.namespace ? `${rule.identity.namespace}/` : ""}{rule.identity.name}<span className="ml-2 font-mono text-xs font-normal text-muted-foreground">{rule.path || "entire resource"}</span><span className="mt-1 block text-xs font-normal text-muted-foreground">{rule.reason}</span></span>{canApprove && <ConfirmDisclosure trigger="Remove" title="Remove ignore rule?" description="The next plan may update or delete this resource again." confirmLabel="Remove rule" onConfirm={async () => { await removeIgnoreRule(rule) }} disabled={busy} />}</div>)}</div></details>}
              <PlanReview key={activePlan.id} changes={activePlan.plan.changes} ignored={activePlan.plan.ignored ?? []} selected={reviewResource} onSelect={setReviewResource} identityKey={(change) => diffId(change.identity)} isExcluded={(change) => selectionResources.some((identity) => sameIdentity(identity, change.identity))} renderChange={(change, ignored) => <DiffCard change={change} ignored={ignored} canSelect={activePlan.status === "current" && canDeploy && !activePlan.plan.decommission && !activePlan.plan.rollback} canManageIgnores={activePlan.status === "current" && canApprove && !activePlan.plan.decommission && !activePlan.plan.rollback} excluded={selectionResources.some((identity) => sameIdentity(identity, change.identity))} excludedPaths={selectionFields.filter((field) => sameIdentity(field.identity, change.identity)).map((field) => field.path)} permanentResourceIgnore={ignoreRules.some((rule) => !rule.path && sameIdentity(rule.identity, change.identity))} permanentFieldIgnores={ignoreRules.filter((rule) => Boolean(rule.path) && sameIdentity(rule.identity, change.identity)).map((rule) => rule.path!)} reason={ignoreReasons[diffId(change.identity)] ?? ""} onReasonChange={(reason) => setIgnoreReasons((current) => ({ ...current, [diffId(change.identity)]: reason }))} onToggleResource={() => toggleResourceExclusion(change.identity)} onToggleField={(path) => toggleFieldExclusion(change.identity, path)} onSaveIgnore={(path) => void saveIgnoreRule(change.identity, path, ignoreReasons[diffId(change.identity)] ?? "")} busy={busy} />} />
              {activePlan.plan.changes.length === 0 && <p className="mt-4 text-sm text-muted-foreground">{activePlan.plan.rollback ? "The target matches the managed state. Applying will pin the source and pause reconciliation." : "No changes to apply. Git and managed fields match, or all differences are excluded."}</p>}
              {selectionDirty && <div className="mt-5 flex flex-wrap items-center gap-3 rounded-lg border border-primary/20 bg-primary/5 p-3"><p className="min-w-0 flex-1 text-xs text-muted-foreground">Exclusions are drafts until you save. Saving creates a new immutable plan and clears any earlier approval.</p><Button size="sm" variant="outline" onClick={() => { setSelectionResources(activePlan.plan.selection?.resources ?? []); setSelectionFields(activePlan.plan.selection?.fields ?? []) }} disabled={busy}>Reset</Button><Button size="sm" loading={pendingAction === "selection"} loadingText="Recalculating…" onClick={() => void savePlanSelection()} disabled={busy}>Create selected plan</Button></div>}
              {activePlan.status === "current" && activePlan.plan.requiresApproval && <details className="mt-5 rounded-lg border border-amber-300/70 bg-amber-50/70 p-3.5 text-sm leading-6 text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><summary className="cursor-pointer font-medium">Approval requirements · {approvedApprovals}/{requiredApprovals} approved</summary><div className="mt-3"><strong>{requiredApprovals} distinct approval{requiredApprovals === 1 ? "" : "s"} required.</strong> {activePlan.plan.approvalKind === "rollback" ? "A workspace owner must approve every rollback; the deletion policy may require additional distinct owner approvals for removed resources." : activePlan.plan.approvalKind === "deletion" ? "Review the exact resources being removed." : activePlan.plan.approvalKind === "takeover" ? "This plan transfers Kubernetes field ownership for the marked resources. Review every changed field before approval." : "Review the cluster-scoped or sync changes."} {approvedApprovals} of {requiredApprovals} approved.<p className="mt-2"><strong>Eligible approvers:</strong> {displayedApproverRoles.length ? displayedApproverRoles.map((role) => role[0].toUpperCase() + role.slice(1)).join(", ") : "selected members only"}{displayedApproverMembers.length ? `; ${displayedApproverMembers.map((member) => member.label).join(", ")}` : ""}. {displayedApproverRoles.length ? "Higher workspace roles also qualify." : "Only the selected members qualify."}</p>{currentApprovalSummary?.approvals.length ? <ul className="mt-2 space-y-1">{currentApprovalSummary.approvals.map((approval) => <li key={approval.id}>{approval.displayName || approval.email}{approval.role ? ` · ${approval.role}` : ""}{approval.eligible ? " · approved" : " · no longer eligible"}{approval.comment && <span className="block text-xs text-muted-foreground">“{approval.comment}”</span>}</li>)}</ul> : null}</div></details>}
              {activePlan.status === "current" && <div className="sticky bottom-[max(0.75rem,var(--toast-clearance,0px))] z-20 mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-card/95 px-3 py-2.5 shadow-sm backdrop-blur">
              <p className="text-xs font-medium">{activePlan.plan.changes.length} changes · {changeCounts?.delete ?? 0} deletions{activePlan.plan.requiresApproval ? ` · ${approvedApprovals}/${requiredApprovals} approvals` : ""}</p>
              <div className="flex flex-wrap items-center justify-end gap-2">
                {activePlan.plan.requiresApproval && <ConfirmDisclosure trigger={currentApprovalSummary?.currentUserApproved ? "Approval recorded ✓" : approvalsComplete ? "Approvals complete" : "Add approval"} triggerVariant="outline" confirmVariant="default" title="Approve this exact plan?" description={`${requiredApprovals} distinct eligible workspace members must approve this exact plan before it can be applied.`} confirmLabel="Approve plan" onConfirm={approvePlan} disabled={busy || hasPendingOperation || Boolean(ownershipConflict) || selectionDirty || !currentApprovalSummary?.canApprove || activePlan.status !== "current"}><div className="space-y-3"><Input aria-label="Approval comment" value={approvalComment} maxLength={1000} onChange={(event) => setApprovalComment(event.target.value)} placeholder="Comment (optional)" /><ul className="max-h-36 space-y-1 overflow-y-auto text-xs text-muted-foreground">{activePlan.plan.changes.filter((change) => change.kind === "delete" || change.takeover).map((change) => <li key={diffId(change.identity)}>{change.takeover ? "Take over" : "Delete"} {change.identity.kind} {change.identity.namespace}/{change.identity.name}</li>)}</ul></div></ConfirmDisclosure>}
                <Button loading={pendingAction === "apply-plan"} loadingText={activePlan.plan.rollback ? "Starting rollback…" : "Starting sync…"} onClick={() => void applyPlan()} disabled={busy || hasPendingOperation || Boolean(ownershipConflict) || selectionDirty || !canDeploy || (activePlan.plan.decommission && !canApprove) || (activePlan.plan.rollback && !canApprove) || activePlan.status !== "current" || (!activePlan.plan.rollback && activePlan.plan.changes.length === 0) || (activePlan.plan.requiresApproval && !approvalsComplete)}>{activePlan.plan.rollback ? "Apply approved rollback" : activePlan.plan.decommission ? "Delete approved resources" : activePlan.plan.requiresApproval ? "Apply approved plan" : "Sync application"}</Button>
              </div>
              </div>}
              {activePlan.status === "current" && !currentApprovalSummary?.canApprove && activePlan.plan.requiresApproval && !approvalsComplete && <p className="mt-2 text-right text-xs text-muted-foreground">Approvals can be added by workspace members eligible under this plan&apos;s approval rule.</p>}
              </>}
            </div>}
          </Panel>
        </Tabs.Panel>

        <Tabs.Panel value="components" className="outline-none">
          <Panel surface="flat" title="Managed components" description="JustCD tracks only resources that it created and owns on the cluster.">
            {pendingData.resources ? <SectionLoading label="Loading managed components" /> : resources.length ? <div className="min-w-0"><DataGridList rows={resources} columns={[
              { id: "resource", title: "Resource", cell: (item) => <span className="font-medium">{item.identity.name}<span className="mt-0.5 block text-[10px] font-normal text-muted-foreground">{item.identity.kind} · {item.identity.apiVersion}{item.adopted ? " · field handover pending" : ""}</span></span> },
              { id: "namespace", title: "Namespace", cell: (item) => <span className="text-xs text-muted-foreground">{item.identity.namespace || "cluster scope"}</span> },
              { id: "cluster", title: "Cluster", cell: (item) => <span className="font-mono text-[10px] text-muted-foreground">{item.identity.clusterId?.slice(0, 8)}</span> },
              { id: "version", title: "Live version", cell: (item) => <span className="font-mono text-[10px] text-muted-foreground">{item.resourceVersion}</span> },
            ]} empty="A component inventory appears after the first successful sync." /></div> : <EmptyState title="No managed components yet" description="After the first successful sync, this inventory shows every resource owned by this application." />}
          </Panel>
        </Tabs.Panel>

        <Tabs.Panel value="activity" className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px] outline-none">
          <div className="space-y-5">
          <Panel title="Recent operations" description="Sync history and result state">
            {application && (application.retryTerminalReason || application.retryNextAt) && <div className="flex flex-wrap items-center gap-3 border-b bg-muted/20 px-5 py-3"><div className="min-w-0 flex-1"><p className="text-xs font-medium">Reconciliation needs attention</p><p className="mt-1 text-[10px] text-muted-foreground">{application.retryNextAt ? `Next automatic attempt: ${new Date(application.retryNextAt).toLocaleString()}` : application.retryTerminalReason?.replaceAll("_", " ")}{application.retryLastErrorCode ? ` · ${application.retryLastErrorCode}` : ""}</p></div>{canDeploy && !application.decommissioning && <Button size="sm" variant="outline" loading={pendingAction === "retry-reconciliation"} disabled={busy || hasPendingOperation} onClick={() => void retryReconciliation()}>Retry now</Button>}</div>}
            {pendingData.operations ? <SectionLoading label="Loading recent operations" /> : operations.length ? <div className="divide-y">{operations.slice(0, 8).map((operation) => { const checkpoint = operation.rollbackCheckpointId ?? rollbackTargets.find((target) => target.kind === "pre_operation" && target.operationId === operation.id)?.id; return <div key={operation.id} className="px-5 py-3.5"><div className="flex items-center justify-between gap-3"><span className="text-xs font-medium">{operation.type === "resource_action" ? `Resource action ${operation.status}` : operation.type === "rollback" ? operation.status === "succeeded" ? "Rollback completed" : operation.status === "failed" ? "Rollback stopped" : operation.status === "queued" ? "Rollback queued" : "Rollback running" : operation.status === "succeeded" ? "Sync completed" : operation.status === "failed" ? "Sync stopped" : operation.status === "queued" ? "Sync queued" : "Sync running"}</span><StatusBadge status={operation.status} /></div><p className="mt-1 line-clamp-2 text-[10px] leading-4 text-muted-foreground">{operation.message || (operation.type === "resource_action" ? "Resource action" : operation.type === "rollback" ? "Rollback operation" : "Sync operation")}</p>{operation.status === "failed" && <p className="mt-1 text-[10px] text-muted-foreground">Attempt {operation.attemptCount}{operation.nextRetryAt ? ` · next retry ${new Date(operation.nextRetryAt).toLocaleString()}` : operation.terminalReason ? ` · ${operation.terminalReason.replaceAll("_", " ")}` : ""}{operation.errorCode ? ` · ${operation.errorCode}` : ""}</p>}{operation.progress && operation.type !== "resource_action" && <p className="mt-1 text-[10px] text-muted-foreground">{operationPhaseLabel(operation.progress.phase, operation.status)} · {(operation.progress.completed?.length ?? 0)}/{operation.progress.total} resources{operation.progress.current ? ` · ${resourceLabel(operation.progress.current)}` : ""}</p>}{operation.status === "failed" && <div className="mt-2 flex flex-wrap items-center gap-2">{canDeploy && !application?.decommissioning && operation.type === "sync" && <Button size="sm" variant="outline" disabled={busy || hasPendingOperation} onClick={() => void retryReconciliation(operation.id)}>Retry with a fresh plan</Button>}{canApprove && checkpoint && <Button size="sm" variant="outline" disabled={busy || hasPendingOperation} onClick={() => void createRollbackPlan({ kind: "pre_operation", id: checkpoint })}>Restore state before this {operation.type === "rollback" ? "rollback" : "sync"}</Button>}<ErrorDetailsButton label="Debug details" error={{ name: "SyncOperationError", message: operation.message, operationId: operation.id, applicationId: operation.applicationId, planId: operation.planId, status: operation.status, attemptCount: operation.attemptCount, errorCode: operation.errorCode, nextRetryAt: operation.nextRetryAt, terminalReason: operation.terminalReason, startedAt: operation.startedAt, finishedAt: operation.finishedAt, progress: operation.progress }} /></div>}<p className="mt-1.5 text-[9px] text-muted-foreground">{new Date(operation.startedAt).toLocaleString()}</p></div>})}</div> : <EmptyState title="No syncs yet" description="Operations will be recorded here with the actor and resulting status." />}
          </Panel>
          <Panel title="Rollback targets" description="Review a new plan to restore a previous deployment. Opening a target never changes the cluster.">
            <div className="space-y-4 p-5">
              {pendingData.rollback ? <SectionLoading label="Loading rollback targets" compact /> : rollbackTargets.length > 0 ? <div className="divide-y rounded-lg border">{rollbackTargets.map((target) => <div key={target.id} className="flex flex-wrap items-center gap-3 px-3 py-3"><div className="min-w-0 flex-1"><p className="text-xs font-medium">{target.kind === "successful_sync" ? "Successful deployment" : "Before failed operation"}</p><p className="mt-1 text-[10px] text-muted-foreground">{new Date(target.createdAt).toLocaleString()} · {target.resourceCount} resources{target.revision ? ` · ${target.revision.slice(0, 12)}` : " · no successful revision"}</p>{target.operationId && <p className="mt-0.5 font-mono text-[9px] text-muted-foreground">Operation {target.operationId.slice(0, 12)}</p>}</div>{canApprove && <Button size="sm" variant="outline" loading={pendingAction === "rollback-plan"} disabled={busy || hasPendingOperation} onClick={() => void createRollbackPlan({ kind: target.kind, id: target.id })}>Review rollback</Button>}</div>)}</div> : <EmptyState title="No rollback history yet" description="A target appears after a successful deployment or a failed/interrupted operation with a saved checkpoint." />}
              <div className="border-t pt-4"><FormField label="Git revision" htmlFor="rollback-git-revision"><Input id="rollback-git-revision" value={rollbackRevision} onChange={(event) => setRollbackRevision(event.target.value)} placeholder="branch, tag, or commit SHA" disabled={!canApprove || busy || hasPendingOperation} /></FormField><div className="mt-2 flex flex-wrap items-center justify-between gap-3"><p className="max-w-xl text-[10px] leading-4 text-muted-foreground">JustCD resolves and renders this revision, then shows a normal create/update/delete diff for owner approval.</p><Button size="sm" variant="outline" loading={pendingAction === "rollback-plan"} loadingText="Rendering target…" disabled={!canApprove || busy || hasPendingOperation || !rollbackRevision.trim()} onClick={() => void createRollbackPlan({ kind: "git_revision", revision: rollbackRevision.trim() })}>Review Git rollback</Button></div></div>
            </div>
          </Panel>
          </div>
          <Panel title="Application source" description="Configuration stored in JustCD"><div className="space-y-3 p-5 text-xs"><KeyValue label="Manifest path" value={application.manifestPath} mono /><KeyValue label="Renderer" value={application.renderer} />{application.renderer === "kustomize" && <><KeyValue label="Cluster overlay path" value={application.targetManifestPath || "Shared path"} mono />{Object.keys(application.namespaceManifestPaths ?? {}).length > 0 && <KeyValue label="Namespace overlays" value={Object.entries(application.namespaceManifestPaths).map(([namespace, path]) => `${namespace}: ${path}`).join(", ")} mono />}</>}<KeyValue label="Cluster" value={application.clusterId.slice(0, 12)} mono /><KeyValue label="Poll interval" value={`${application.pollSeconds} seconds`} />{application.renderer === "kustomize" && <><KeyValue label="Namespace from Git" value={kustomization ? kustomization.namespace || "Not set in kustomization" : kustomizationError ? errorMessage(kustomizationError) : "Checking kustomization…"} />{kustomizationError && <ErrorDetailsButton error={kustomizationError} />}</>}{application.applicationGroupId && <Link href={`/application-groups/${application.applicationGroupId}`} className="block text-xs font-medium text-primary hover:underline">Open deployment group →</Link>}{canApprove && <Link href={`/applications/${applicationID}/edit`} className="inline-block text-xs font-medium text-primary hover:underline">Edit application →</Link>}<Link href={`/workspaces/${application.workspaceId}`} className="block text-xs font-medium text-primary hover:underline">Open workspace →</Link></div></Panel>
        </Tabs.Panel>

        <Tabs.Panel value="pull-requests" className="outline-none">
          <PullRequestsWorkspace embedded canConfigure={canApprove} />
        </Tabs.Panel>

        <Tabs.Panel value="settings" className="space-y-5 outline-none">
          {application.renderer === "kustomize" && <Panel title="Render settings" description={application.applicationGroupId ? "Kustomize settings are shared by this deployment group." : "Control how Kustomize builds this application's manifests."}><div className="space-y-4 p-5 text-xs">
            <div className="rounded-lg border bg-muted/30 p-3"><p className="font-medium">Namespace from Git</p><p className="mt-1 font-mono text-muted-foreground">{kustomization ? kustomization.namespace || "Not set in kustomization" : kustomizationError ? errorMessage(kustomizationError) : "Checking kustomization…"}</p>{kustomizationError != null && <div className="mt-2"><ErrorDetailsButton error={kustomizationError} /></div>}<p className="mt-1 text-muted-foreground">JustCD target: {application.namespaces.map((binding) => binding.namespace).join(", ")}</p></div>
            {application.applicationGroupId ? <div className="rounded-lg border bg-muted/30 p-3">Namespace transforms and Helm chart rendering are configured for every target in the <Link href={`/application-groups/${application.applicationGroupId}`} className="font-medium text-primary hover:underline">deployment group</Link>.</div> : <>
              {application.namespaces.length > 0 ? <label className="flex items-start gap-2"><Checkbox checked={namespaceOverrideDraft} onCheckedChange={(checked) => setNamespaceOverrideDraft(Boolean(checked))} disabled={!canApprove || busy || hasPendingOperation} /><span><span className="block font-medium">Apply namespace transform</span><span className="mt-1 block leading-5 text-muted-foreground">Build the selected Kustomize path once per bound namespace and set namespace metadata to that target. This can create or delete resources on the next sync, so review a new plan before applying.</span></span></label> : <p className="text-muted-foreground">Bind at least one namespace to configure a namespace transform.</p>}
              <label className="flex items-start gap-2"><Checkbox checked={kustomizeHelmDraft} onCheckedChange={(checked) => setKustomizeHelmDraft(Boolean(checked))} disabled={!canApprove || busy || hasPendingOperation} /><span><span className="block font-medium">Enable Helm charts in Kustomize</span><span className="mt-1 block leading-5 text-muted-foreground">Permits helmCharts from this Git source. Helm may fetch pinned charts from public HTTPS repositories while building the plan.</span></span></label>
              {canApprove && <Button size="sm" variant="outline" loading={pendingAction === "render-settings"} loadingText="Saving settings…" disabled={busy || hasPendingOperation || (kustomizeHelmDraft === application.kustomizeHelmEnabled && namespaceOverrideDraft === application.kustomizeNamespaceOverride)} onClick={() => void saveRenderSettings()}>Save render settings</Button>}
            </>}
          </div></Panel>}
          <Panel title="Ignored resources" description="Exclude an exact resource, an entire Kubernetes kind, or resources with a label. Exclusions apply before cluster discovery and live reads.">
            <div className="space-y-5 p-5">
              {canApprove && <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                <FormField label="Exclude by" htmlFor="ignore-mode"><FormSelect id="ignore-mode" value={ignoreMode} onValueChange={setIgnoreMode} items={[{ value: "kind", label: "API kind" }, { value: "label", label: "Label" }, { value: "resource", label: "Exact resource" }]} /></FormField>
                {(ignoreMode === "kind" || ignoreMode === "resource") && <><FormField label="API version" htmlFor="ignore-api-version"><Input id="ignore-api-version" placeholder="secrets.hashicorp.com/v1beta1" value={ignoreVersion} onChange={(event) => setIgnoreVersion(event.target.value)} /></FormField><FormField label="Kind" htmlFor="ignore-kind"><Input id="ignore-kind" placeholder="VaultStaticSecret" value={ignoreKind} onChange={(event) => setIgnoreKind(event.target.value)} /></FormField></>}
                {ignoreMode === "resource" && <><FormField label="Namespace" htmlFor="ignore-namespace"><Input id="ignore-namespace" placeholder={application.namespaces[0]?.namespace ?? "namespace"} value={ignoreNamespace} onChange={(event) => setIgnoreNamespace(event.target.value)} /></FormField><FormField label="Resource name" htmlFor="ignore-name"><Input id="ignore-name" placeholder="ntfy-secret" value={ignoreName} onChange={(event) => setIgnoreName(event.target.value)} /></FormField></>}
                {ignoreMode === "label" && <><FormField label="Label key" htmlFor="ignore-label-key"><Input id="ignore-label-key" placeholder="justcd.io/exclude" value={ignoreLabelKey} onChange={(event) => setIgnoreLabelKey(event.target.value)} /></FormField><FormField label="Label value" htmlFor="ignore-label-value"><Input id="ignore-label-value" placeholder="true" value={ignoreLabelValue} onChange={(event) => setIgnoreLabelValue(event.target.value)} /></FormField></>}
                <div className="sm:col-span-2 lg:col-span-4"><FormField label="Reason" htmlFor="ignore-reason"><Textarea id="ignore-reason" value={ignoreReason} onChange={(event) => setIgnoreReason(event.target.value)} placeholder="Managed by another controller or outside this service account's permissions" maxLength={500} /></FormField></div>
                <Button size="sm" className="w-fit sm:col-span-2" loading={pendingAction === "add-ignore"} loadingText="Saving exclusion…" onClick={() => void addIgnore()} disabled={busy || hasPendingOperation || ignoreReason.trim().length < 5 || ((ignoreMode === "kind" || ignoreMode === "resource") && (!ignoreVersion.trim() || !ignoreKind.trim())) || (ignoreMode === "resource" && !ignoreName.trim()) || (ignoreMode === "label" && !ignoreLabelKey.trim())}>Add exclusion</Button>
                {pendingAction === "add-ignore" && <p role="status" aria-live="polite" className="self-center text-xs text-muted-foreground sm:col-span-2">Saving the exclusion and invalidating reviewed plans…</p>}
              </div>}
              <p className="text-xs text-muted-foreground">Ignored resources stay visible here and are not read, updated, or deleted by normal plans. Existing plans become stale when these rules change.</p>
              {pendingData.rules || pendingData.selectors ? <SectionLoading label="Loading exclusions" compact /> : (ignoreSelectors.length > 0 || ignoreRules.length > 0) && <div className="divide-y rounded-lg border">{ignoreSelectors.map((rule) => <div key={rule.id} className="flex flex-wrap items-center gap-3 px-4 py-3 text-xs"><span className="min-w-0 flex-1"><strong>{rule.kind ? `${rule.apiVersion} · ${rule.kind}` : "Any kind"}</strong>{rule.labelKey && <span className="ml-2 font-mono text-muted-foreground">{rule.labelKey}={rule.labelValue}</span>}<span className="mt-1 block text-muted-foreground">{rule.reason}</span></span>{canApprove && <ConfirmDisclosure trigger="Remove" title="Remove exclusion?" description="Future plans may manage matching resources again." confirmLabel="Remove exclusion" onConfirm={() => removeIgnoreSelector(rule)} disabled={busy || hasPendingOperation} />}</div>)}{ignoreRules.map((rule) => <div key={rule.id} className="flex flex-wrap items-center gap-3 px-4 py-3 text-xs"><span className="min-w-0 flex-1"><strong>{rule.identity.kind} {rule.identity.namespace}/{rule.identity.name}</strong><span className="ml-2 font-mono text-muted-foreground">{rule.path || "whole resource"}</span><span className="mt-1 block text-muted-foreground">{rule.reason}</span></span>{canApprove && <ConfirmDisclosure trigger="Remove" title="Remove ignore rule?" description="Future plans may manage this resource again." confirmLabel="Remove rule" onConfirm={async () => { await removeIgnoreRule(rule) }} disabled={busy || hasPendingOperation} />}</div>)}</div>}
            </div>
          </Panel>
          {canApprove && <Panel title="Delete application" description="Choose whether JustCD keeps or removes the resources it manages."><div className="flex flex-wrap items-center justify-between gap-3 p-5"><p className="text-xs text-muted-foreground">{`${resources.length} managed resource${resources.length === 1 ? "" : "s"} currently recorded.`} Keeping them removes ownership tracking from JustCD.</p><ConfirmDisclosure trigger="Delete application" title={`Delete ${application.name}?`} description="Removing this application from JustCD cannot be undone. Choose what happens to its managed Kubernetes resources." confirmLabel="Delete application" onConfirm={deleteApplication} disabled={busy || hasPendingOperation}><FormField label="Managed cluster resources" htmlFor="application-delete-policy"><FormSelect id="application-delete-policy" value={deletePolicy} onValueChange={setDeletePolicy} items={[{ value: "keep", label: "Keep resources in Kubernetes" }, { value: "delete", label: "Delete resources through a reviewed plan" }]} /></FormField>{deletePolicy === "delete" && resources.length > 0 && <p className="mt-2 text-xs text-muted-foreground">A deletion plan must receive {deletionApprovalCount} approval{deletionApprovalCount === 1 ? "" : "s"} from eligible workspace members before the resources can be removed.</p>}</ConfirmDisclosure></div></Panel>}
        </Tabs.Panel>
    </>}
      </Tabs.Root>
  </>
}

function InfoCard({ label, value, note, mono = false }: { label: string; value: string; note: string; mono?: boolean }) {
  return <div className="min-w-0 rounded-xl border bg-card p-4"><p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</p><p className={`mt-2 truncate text-sm font-semibold ${mono ? "font-mono text-xs" : ""}`}>{value}</p><p className="mt-1 truncate text-[10px] text-muted-foreground">{note}</p></div>
}

function SummaryFact({ label, value, loading = false }: { label: string; value: string; loading?: boolean }) {
  return <div className="min-w-0 sm:px-4 first:sm:pl-0 last:sm:pr-0"><p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</p>{loading ? <Skeleton role="status" aria-label={`Loading ${label.toLowerCase()}`} className="mt-2 h-4 w-28 max-w-full motion-reduce:animate-none" /> : <p className="mt-1 truncate text-sm font-medium" title={value}>{value}</p>}</div>
}

function SectionLoading({ label, compact = false }: { label: string; compact?: boolean }) {
  return <div role="status" aria-label={label} className={`space-y-4 ${compact ? "p-5" : "p-5 sm:p-6"}`}><span className="text-xs text-muted-foreground">{label}…</span><div aria-hidden="true" className="space-y-3"><Skeleton className="h-5 w-2/3 max-w-72 motion-reduce:animate-none" /><Skeleton className="h-4 w-full max-w-lg motion-reduce:animate-none" /><Skeleton className="h-4 w-4/5 max-w-md motion-reduce:animate-none" /></div></div>
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
  const color = ignored ? "border-border bg-muted text-muted-foreground" : change.kind === "delete" ? "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-900 dark:bg-rose-950/25 dark:text-rose-300" : change.kind === "create" ? "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/25 dark:text-emerald-300" : "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/25 dark:text-blue-300"
  const fieldPaths = useMemo(() => (change.changedPaths ?? []).filter(safeIgnorePath), [change.changedPaths])
  return <article id={diffId(change.identity)} className="scroll-mt-6 overflow-hidden rounded-lg border">
    <div className="flex flex-wrap items-center gap-2 border-b bg-muted/20 px-3 py-2"><span className={`rounded border px-1.5 py-0.5 text-xs font-semibold uppercase ${color}`}>{ignored ? "Excluded" : change.kind}</span><span className="min-w-0 flex-1 break-words text-sm font-medium">{name}</span>{change.takeover && <span className="rounded-full border border-amber-300 bg-amber-50 px-2 py-0.5 text-xs text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200">Field ownership takeover</span>}{ignored && <span className="rounded-full border border-amber-300 bg-amber-50 px-2 py-0.5 text-xs text-amber-800">Not applied</span>}{change.identity.clusterScoped && <span className="rounded-full border px-2 py-0.5 text-xs text-amber-700">cluster-wide</span>}</div>
    {ignored && <div role="status" className="border-b bg-muted/30 px-4 py-3 text-sm"><strong>Excluded difference — no changes will be applied for this entry.</strong><p className="mt-1 text-xs text-muted-foreground">{change.ignoreReason || "Excluded from this sync."} The diff below shows the difference between live state and Git, not a scheduled update.</p></div>}
    {!ignored && change.kind === "delete" && <div role="alert" className="border-b border-rose-200 bg-rose-50 px-4 py-3 text-rose-950 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-100"><p className="text-sm font-semibold">This resource will be deleted from Kubernetes</p><p className="mt-1 text-xs">Applying this plan removes {name}. This may interrupt service or remove persisted data. Review this deletion before approving the plan.</p></div>}
    <ManifestDiff key={diffId(change.identity)} before={change.before} after={change.after} excluded={ignored} />
    {(canSelect || canManageIgnores) && <details className="border-t bg-muted/10 px-4 py-3">
      <summary className="cursor-pointer text-sm font-medium">Exclusions & ignore rules{excluded || excludedPaths.length ? ` · ${excluded ? "whole resource" : `${excludedPaths.length} fields`} selected` : ""}</summary>
      <div className="mt-4">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
        <label className="flex items-center gap-2 text-sm font-medium"><Checkbox checked={permanentResourceIgnore || excluded} disabled={!canSelect || permanentResourceIgnore} onCheckedChange={onToggleResource} /><span>{permanentResourceIgnore ? "Resource is ignored permanently" : "Exclude this resource from this plan"}</span></label>
        {fieldPaths.length > 0 && change.kind !== "delete" && <span className="text-xs text-muted-foreground">Select changed fields:</span>}
      </div>
      {fieldPaths.length > 0 && change.kind !== "delete" && <div className="mt-2 grid gap-1.5 sm:grid-cols-2">{fieldPaths.map((path) => {
        const permanent = permanentFieldIgnores.includes(path)
        const selected = permanent || excludedPaths.includes(path)
        return <label key={path} className="flex min-w-0 items-start gap-2 rounded-md border bg-background/70 px-2.5 py-2 text-xs">
          <Checkbox checked={selected} disabled={!canSelect || permanent} onCheckedChange={() => onToggleField(path)} className="mt-0.5" />
          <span className="min-w-0"><span className="block break-all font-mono text-foreground">{path}</span><span className="mt-0.5 block text-muted-foreground">{permanent ? "Ignored by a persistent rule" : "Skip this field for this plan"}</span></span>
        </label>
      })}</div>}
      {(fieldPaths.length > 0 && change.kind !== "delete") && <p className="mt-2 text-xs leading-4 text-muted-foreground">Field ignores require another Kubernetes field manager to own the value first. Lists can only be excluded as a whole; JustCD validates ownership before saving.</p>}
      {canManageIgnores && <details className="mt-3 border-t pt-2.5">
        <summary className="cursor-pointer text-xs font-medium text-primary">Permanent ignore rule…</summary>
        <div className="mt-2 space-y-2.5">
          <Textarea aria-label={`Reason for ignoring ${name}`} rows={2} value={reason} onChange={(event) => onReasonChange(event.target.value)} placeholder="Why should future plans ignore this? (required)" className="min-h-16 resize-y text-xs" maxLength={500} />
          <div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" disabled={busy || permanentResourceIgnore || reason.trim().length < 5} onClick={() => onSaveIgnore("")}>{permanentResourceIgnore ? "Resource already ignored" : "Ignore resource in future plans"}</Button>
            {fieldPaths.filter((path) => !permanentFieldIgnores.includes(path)).map((path) => <Button key={path} size="sm" variant="ghost" disabled={busy || reason.trim().length < 5 || change.kind === "delete"} onClick={() => onSaveIgnore(path)}>Ignore {path} in future plans</Button>)}</div>
          <p className="text-xs leading-4 text-muted-foreground">Only workspace owners can change permanent rules. Each change is audited and invalidates existing plans.</p>
        </div>
      </details>}
      {!canManageIgnores && canSelect && <p className="mt-2 text-xs text-muted-foreground">Permanent ignore rules can be managed by workspace owners.</p>}
      {change.ignoreReason && ignored && <p className="mt-2 text-xs text-amber-800 dark:text-amber-300">{change.ignoreReason}</p>}
      </div>
    </details>}
    {ignored && <p className="border-t p-4 text-sm text-muted-foreground">{change.ignoreReason || "Excluded from this sync; no changes will be applied for this difference."}</p>}
  </article>
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
