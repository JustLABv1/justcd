"use client"

import Link from "next/link"
import { useParams, useRouter } from "next/navigation"
import { useEffect, useMemo, useRef, useState } from "react"
import { Tabs } from "@base-ui/react/tabs"
import { Button } from "@/components/ui/button"
import { ApplicationDetailSkeleton } from "@/components/application-detail-skeleton"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { Checkbox } from "@/components/ui/checkbox"
import { DataGridList } from "@/components/data-grid-table"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { ResourceMap } from "@/components/resource-map"
import { Textarea } from "@/components/ui/textarea"
import { EmptyState, FormField, PageHeading, Panel, StatusBadge } from "@/components/ui-kit"
import { ErrorDetailsButton } from "@/components/error-details"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { APIError, api, apiDelete, apiPost, errorMessage } from "@/lib/api"
import { diffJsonLines } from "@/lib/line-diff"
import type { Application, Change, FieldExclusion, Identity, IgnoreRule, IgnoreSelector, ListResponse, ManagedResource, Operation, OwnershipConflict, PlanApprovalSummary, PlanRecord, Project, ProjectMember, ResourceTopology } from "@/lib/types"

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
  const toast = useToast()
  const [application, setApplication] = useState<Application | null>(null)
  const [kustomizeHelmDraft, setKustomizeHelmDraft] = useState(false)
  const [namespaceOverrideDraft, setNamespaceOverrideDraft] = useState(false)
  const [kustomization, setKustomization] = useState<{ namespace: string; commit: string } | null>(null)
  const [kustomizationError, setKustomizationError] = useState<unknown | null>(null)
  const [project, setProject] = useState<Project | null>(null)
  const [projectMembers, setProjectMembers] = useState<ProjectMember[]>([])
  const [plans, setPlans] = useState<PlanRecord[]>([])
  const [activePlan, setActivePlan] = useState<PlanRecord | null>(null)
  const [resources, setResources] = useState<ManagedResource[]>([])
  const [topology, setTopology] = useState<ResourceTopology | null>(null)
  const [topologyRefreshing, setTopologyRefreshing] = useState(false)
  const [operations, setOperations] = useState<Operation[]>([])
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
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [pendingAction, setPendingAction] = useState("")
  const [error, setError] = useState<unknown | null>(null)
  const [ownershipConflict, setOwnershipConflict] = useState<OwnershipConflict | null>(null)
  const [ownershipConflicts, setOwnershipConflicts] = useState<OwnershipConflict[]>([])
  const [selectedConflictKeys, setSelectedConflictKeys] = useState<string[]>([])
  const conflictEpoch = useRef(0)
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
      setActiveTab(["overview", "topology", "changes", "components", "activity", "settings"].includes(value ?? "") ? value! : "overview")
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
    const [projectList, memberList, planList, inventory, operationList, topologyResult, ignoreRuleList, selectorList, kustomizationResult] = await Promise.all([
      api<ListResponse<Project>>("/api/v1/projects"),
      api<ListResponse<ProjectMember>>(`/api/v1/projects/${encodeURIComponent(app.projectId)}/members`).catch(() => ({ items: [] as ProjectMember[] })),
      api<ListResponse<PlanRecord>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/plans`),
      api<ListResponse<ManagedResource>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/resources`),
      api<ListResponse<Operation>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/operations`),
      api<ResourceTopology>(`/api/v1/applications/${encodeURIComponent(applicationID)}/topology`).catch(() => null),
      api<ListResponse<IgnoreRule>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-rules`),
      api<ListResponse<IgnoreSelector>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/ignore-selectors`),
      app.renderer === "kustomize" ? api<{ namespace: string; commit: string }>(`/api/v1/applications/${encodeURIComponent(applicationID)}/kustomization`).then((value) => ({ value, error: null })).catch((cause) => ({ value: null, error: cause })) : Promise.resolve({ value: null, error: null }),
    ])
    setApplication(app)
    if (app.decommissioning) setDeletePolicy("delete")
    setKustomizeHelmDraft(app.kustomizeHelmEnabled)
    setNamespaceOverrideDraft(app.kustomizeNamespaceOverride)
    setKustomization(kustomizationResult.value)
    setKustomizationError(kustomizationResult.error)
    setProject(projectList.items.find((item) => item.id === app.projectId) ?? null)
    setProjectMembers(memberList.items)
    setPlans(planList.items)
    setActivePlan((current) => current ? planList.items.find((item) => item.id === current.id) ?? planList.items[0] ?? null : planList.items[0] ?? null)
    setResources(inventory.items)
    setTopology(topologyResult)
    setOperations(operationList.items)
    setIgnoreRules(ignoreRuleList.items)
    setIgnoreSelectors(selectorList.items)
  }

  useEffect(() => {
    let active = true
    Promise.resolve().then(loadData).catch((cause) => active && setError(cause)).finally(() => active && setLoading(false))
    return () => { active = false }
  // loadData is intentionally tied to this resource identifier only.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [applicationID])

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
    } finally { setBusy(false); setPendingAction("") }
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
    if (result.deleted) { toast.success("Application deleted."); router.push(`/projects/${application?.projectId ?? ""}`); router.refresh(); return }
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
    setBusy(true); setPendingAction("approve-plan"); setError(null)
    try {
      await apiPost<{ id: string; expiresAt: string }>(`/api/v1/plans/${encodeURIComponent(activePlan.id)}/approvals`)
      const summary = await api<PlanApprovalSummary>(`/api/v1/plans/${encodeURIComponent(activePlan.id)}/approvals`)
      setApprovalSummary(summary)
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

  const changeCounts = activePlan?.plan.changes.reduce((acc, item) => ({ ...acc, [item.kind]: acc[item.kind] + 1 }), { create: 0, update: 0, delete: 0 })
  const canDeploy = project?.role === "owner" || project?.role === "deployer"
  const canApprove = project?.role === "owner"
  const requiredApprovals = activePlan ? activePlan.plan.requiredApprovals ?? (activePlan.plan.requiresApproval ? 1 : 0) : 0
  const currentApprovalSummary = approvalSummary?.planId === activePlan?.id ? approvalSummary : null
  const approvedApprovals = currentApprovalSummary?.approvedApprovals ?? 0
  const approvalsComplete = approvedApprovals >= requiredApprovals
  const approverRoles = activePlan?.plan.approverRoles ?? []
  const approverUserIds = activePlan?.plan.approverUserIds ?? []
  const displayedApproverRoles = approverRoles.length > 0 || approverUserIds.length > 0 || requiredApprovals === 0 ? approverRoles : ["owner"]
  const displayedApproverMembers = approverUserIds.map((userId) => {
    const member = projectMembers.find((item) => item.id === userId)
    return { id: userId, label: member?.displayName || member?.email || `Former member (${userId.slice(0, 8)})` }
  })
  const deletionApprovalCount = application?.approvalPolicyOverride?.deletion?.requiredApprovals ?? project?.approvalPolicy.deletion.requiredApprovals ?? 1
  const latestOperation = operations[0]
  const visibleOperation = operations.find((operation) => operation.status === "queued" || operation.status === "running") ?? (latestOperation?.status === "failed" ? latestOperation : null)
  const latestPlan = plans[0]
  const latestCounts = latestPlan?.plan.changes.reduce((counts, change) => ({ ...counts, [change.kind]: counts[change.kind] + 1 }), { create: 0, update: 0, delete: 0 })
  const resourceKinds = Object.entries((topology?.nodes ?? []).reduce<Record<string, number>>((counts, node) => {
    counts[node.identity.kind] = (counts[node.identity.kind] ?? 0) + 1
    return counts
  }, {})).sort((a, b) => b[1] - a[1])

  const namespaceMismatch = Boolean(application?.renderer === "kustomize" && kustomization?.namespace && application.namespaces.length === 1 && kustomization.namespace !== application.namespaces[0].namespace)

  if (loading && !application) return <ApplicationDetailSkeleton />

  return <>
    <PageHeading title={application?.name ?? "Application not found"} description={application ? `${application.renderer} · ${application.manifestPath} · ${application.revision}` : ""} actions={application && <><StatusBadge status={application.health} />{canApprove && !application.decommissioning && <Link href={`/applications/${applicationID}/edit`}><Button variant="outline">Edit application</Button></Link>}<Button variant="outline" loading={pendingAction === "create-plan"} loadingText="Calculating plan…" onClick={() => void createPlan()} disabled={application.decommissioning || !canDeploy || busy || (namespaceMismatch && !application.kustomizeNamespaceOverride)} title={namespaceMismatch && !application.kustomizeNamespaceOverride ? "Choose a namespace override before refreshing the plan" : undefined}><span aria-hidden="true">↻</span> Refresh plan</Button></>} />
    {error && <ErrorNotice error={error} />}
    {application?.decommissioning && <div role="status" className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-300/70 bg-amber-50 p-4 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><span>Deletion in progress. Auto-sync is paused. Review and apply the deletion plan, then finish removing the application.</span>{canApprove && <Button size="sm" variant="outline" disabled={busy || hasPendingOperation} onClick={() => void cancelDecommission()}>Cancel deletion</Button>}</div>}
    {namespaceMismatch && !application?.kustomizeNamespaceOverride && <div role="alert" className="mb-5 rounded-xl border border-amber-300/60 bg-amber-50 px-4 py-3 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><strong>Kustomize namespace: {kustomization?.namespace}.</strong> JustCD target: {application?.namespaces[0]?.namespace}. The namespaces differ, so a plan cannot be refreshed until an owner enables the override. <button type="button" className="font-semibold underline underline-offset-4" onClick={() => selectTab("settings")}>Review namespace setting →</button></div>}
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
        {!canApprove && <span className="text-xs">A project owner must choose how to resolve this conflict.</span>}
      </div>
      <p className="mt-3 text-xs leading-5">Or change the Git manifest or remove the old object through its current controller, then refresh the plan. JustCD will not delete an untracked resource for you.</p>
      {(ownershipConflict.owner && ownershipConflict.owner !== application?.id || ownershipConflict.hasOwnerReferences) && <p className="mt-3 text-xs">Takeover is unavailable because the object belongs to another JustCD application or is a Kubernetes dependent. Resolve that ownership first.</p>}
    </section>}
    {!loading && application && <>
      {visibleOperation && <div role="status" aria-live="polite" className={`mb-5 rounded-xl border px-4 py-3 ${visibleOperation.status === "failed" ? "border-destructive/30 bg-destructive/5" : "bg-card"}`}>
        <div className="flex flex-wrap items-center gap-3"><StatusBadge status={visibleOperation.status} /><span className="text-xs font-medium">{operationPhaseLabel(visibleOperation.progress?.phase, visibleOperation.status)}</span><span className="ml-auto text-[11px] tabular-nums text-muted-foreground">{visibleOperation.progress?.completed.length ?? 0} / {visibleOperation.progress?.total ?? 0} resources</span></div>
        {visibleOperation.progress?.total ? <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-muted"><div className={`h-full rounded-full ${visibleOperation.status === "failed" ? "bg-destructive" : "bg-primary"}`} style={{ width: `${Math.min(100, Math.round((visibleOperation.progress.completed.length / visibleOperation.progress.total) * 100))}%` }} /></div> : null}
        <div className="mt-2 flex flex-wrap items-center justify-between gap-2 text-[11px] text-muted-foreground"><span>{visibleOperation.progress?.current ? `Now: ${resourceLabel(visibleOperation.progress.current)}` : visibleOperation.message}</span>{visibleOperation.status === "failed" && <ErrorDetailsButton label="Debug details" error={{ name: "SyncOperationError", message: visibleOperation.message, operationId: visibleOperation.id, applicationId: visibleOperation.applicationId, planId: visibleOperation.planId, status: visibleOperation.status, startedAt: visibleOperation.startedAt, finishedAt: visibleOperation.finishedAt, progress: visibleOperation.progress }} />}</div>
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
            { id: "settings", label: "Settings" },
          ].map((tab) => <Tabs.Tab key={tab.id} value={tab.id} className="flex shrink-0 items-center gap-2 border-b-2 border-transparent px-1 pb-3 text-xs font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-[active]:border-primary data-[active]:text-foreground">
            {tab.label}{tab.count !== undefined && <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] tabular-nums text-muted-foreground">{tab.count}</span>}
          </Tabs.Tab>)}
        </Tabs.List>

        <Tabs.Panel value="overview" className="space-y-5 outline-none">
          <div className="grid gap-5 xl:grid-cols-[minmax(0,1.5fr)_minmax(280px,1fr)]">
            <Panel title="Delivery state" description="The current Git-to-cluster picture, without opening the full diff.">
              <div className="space-y-5 p-5">
                <div className="grid grid-cols-3 gap-2"><ChangeCount label="Create" count={latestCounts?.create ?? 0} color="text-emerald-700 bg-emerald-50 dark:text-emerald-300 dark:bg-emerald-950/40" /><ChangeCount label="Update" count={latestCounts?.update ?? 0} color="text-blue-700 bg-blue-50 dark:text-blue-300 dark:bg-blue-950/40" /><ChangeCount label="Delete" count={latestCounts?.delete ?? 0} color="text-rose-700 bg-rose-50 dark:text-rose-300 dark:bg-rose-950/40" /></div>
                {latestPlan?.plan.requiresApproval && <p className="rounded-lg border border-amber-300/70 bg-amber-50/70 px-3 py-2 text-xs text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200">{latestPlan.plan.requiredApprovals ?? 1} eligible approval{(latestPlan.plan.requiredApprovals ?? 1) === 1 ? "" : "s"} required before this plan can be applied.</p>}
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
        void api<ResourceTopology>(`/api/v1/applications/${encodeURIComponent(applicationID)}/topology?refresh=1`).then(setTopology).catch((cause) => toast.error(errorMessage(cause), cause)).finally(() => setTopologyRefreshing(false))
      }} onViewDiff={(identity) => {
        const matching = plans[0]?.plan.changes.some((change) => diffId(change.identity) === diffId(identity))
        if (matching && plans[0]?.id !== activePlan?.id) setActivePlan(plans[0])
        selectTab("changes")
        window.setTimeout(() => document.getElementById(diffId(identity))?.scrollIntoView({ behavior: "smooth", block: "center" }), 100)
      }} />
        </Tabs.Panel>

        <Tabs.Panel value="changes" className="max-w-6xl outline-none">
          <Panel title={activePlan?.plan.decommission ? "Deletion plan & diff" : "Plan & diff"} description={activePlan?.plan.decommission ? "Every managed resource below will be checked again before it is deleted." : "A reviewed plan is a snapshot of the desired Git commit and live cluster state."} action={plans.length > 0 && <FormSelect ariaLabel="Select plan" value={activePlan?.id ?? ""} onValueChange={(id) => { setApprovalSummary(null); setActivePlan(plans.find((plan) => plan.id === id) ?? null) }} className="h-8 max-w-[220px] text-xs" items={plans.map((plan) => ({ value: plan.id, label: `${new Date(plan.createdAt).toLocaleString()} · ${plan.plan.decommission ? "deletion · " : ""}${plan.status}` }))} />}>
            {!activePlan ? <EmptyState title="No review plan yet" description="Build a plan to render Git manifests and compare them with live, app-owned resources." /> : <div className="p-4 sm:p-5">
              <div className="mb-4 flex flex-wrap items-center gap-2"><StatusBadge status={activePlan.status} /><span className="font-mono text-[10px] text-muted-foreground">{activePlan.plan.revision.slice(0, 12)}</span><span className="text-[10px] text-muted-foreground">· expires {new Date(activePlan.expiresAt).toLocaleTimeString()}</span><span className="ml-auto font-mono text-[9px] text-muted-foreground">{activePlan.plan.digest.slice(0, 16)}</span></div>
              <div className="mb-5 grid grid-cols-3 gap-2"><ChangeCount label="Create" count={changeCounts?.create ?? 0} color="text-emerald-700 bg-emerald-50 dark:text-emerald-300 dark:bg-emerald-950/40" /><ChangeCount label="Update" count={changeCounts?.update ?? 0} color="text-blue-700 bg-blue-50 dark:text-blue-300 dark:bg-blue-950/40" /><ChangeCount label="Delete" count={changeCounts?.delete ?? 0} color="text-rose-700 bg-rose-50 dark:text-rose-300 dark:bg-rose-950/40" /></div>
              {ignoreRules.length > 0 && <section className="mb-5 rounded-lg border bg-muted/10"><div className="flex items-start justify-between gap-3 border-b px-4 py-3"><div><h3 className="text-xs font-semibold">Persistent ignore rules</h3><p className="mt-1 text-[10px] text-muted-foreground">Applied to manual and Auto-safe plans. Changes invalidate reviewed plans.</p></div><span className="rounded-md bg-muted px-2 py-1 text-[10px] tabular-nums">{ignoreRules.length}</span></div><div className="divide-y">{ignoreRules.map((rule) => <div key={rule.id} className="flex flex-wrap items-center gap-3 px-4 py-3"><span className="min-w-0 flex-1 text-xs font-medium">{rule.identity.kind} {rule.identity.namespace ? `${rule.identity.namespace}/` : ""}{rule.identity.name}<span className="ml-2 font-mono text-[10px] font-normal text-muted-foreground">{rule.path || "entire resource"}</span><span className="mt-1 block text-[10px] font-normal text-muted-foreground">{rule.reason}</span></span>{canApprove && <ConfirmDisclosure trigger="Remove" title="Remove ignore rule?" description="The next plan may update or delete this resource again." confirmLabel="Remove rule" onConfirm={async () => { await removeIgnoreRule(rule) }} disabled={busy} />}</div>)}</div></section>}
              {activePlan.plan.changes.length ? <div className="space-y-3">{activePlan.plan.changes.map((change, index) => <DiffCard key={`${change.identity.apiVersion}/${change.identity.kind}/${change.identity.namespace}/${change.identity.name}/${index}`} change={change} canSelect={canDeploy && !activePlan.plan.decommission} canManageIgnores={canApprove && !activePlan.plan.decommission} excluded={selectionResources.some((identity) => sameIdentity(identity, change.identity))} excludedPaths={selectionFields.filter((field) => sameIdentity(field.identity, change.identity)).map((field) => field.path)} permanentResourceIgnore={ignoreRules.some((rule) => !rule.path && sameIdentity(rule.identity, change.identity))} permanentFieldIgnores={ignoreRules.filter((rule) => Boolean(rule.path) && sameIdentity(rule.identity, change.identity)).map((rule) => rule.path!)} reason={ignoreReasons[diffId(change.identity)] ?? ""} onReasonChange={(reason) => setIgnoreReasons((current) => ({ ...current, [diffId(change.identity)]: reason }))} onToggleResource={() => toggleResourceExclusion(change.identity)} onToggleField={(path) => toggleFieldExclusion(change.identity, path)} onSaveIgnore={(path) => void saveIgnoreRule(change.identity, path, ignoreReasons[diffId(change.identity)] ?? "")} busy={busy} />)}</div> : <div className="rounded-lg border border-dashed px-4 py-8 text-center"><span className="text-emerald-600">✓</span><p className="mt-2 text-sm font-medium">No changes to apply</p><p className="mt-1 text-xs text-muted-foreground">Git and the managed cluster fields are in sync, or every difference is excluded below.</p></div>}
              {(activePlan.plan.ignored?.length ?? 0) > 0 && <section className="mt-6 space-y-3"><div className="flex items-end justify-between gap-3"><div><h3 className="text-sm font-semibold">Excluded from this sync</h3><p className="mt-1 text-xs text-muted-foreground">These differences stay visible and will appear again in a later plan unless covered by a permanent rule.</p></div><span className="text-xs tabular-nums text-muted-foreground">{activePlan.plan.ignored?.length} excluded</span></div>{activePlan.plan.ignored?.map((change, index) => <DiffCard key={`ignored-${change.identity.kind}-${change.identity.namespace}-${change.identity.name}-${index}`} change={change} canSelect={canDeploy} canManageIgnores={canApprove} excluded={selectionResources.some((identity) => sameIdentity(identity, change.identity))} excludedPaths={selectionFields.filter((field) => sameIdentity(field.identity, change.identity)).map((field) => field.path)} permanentResourceIgnore={ignoreRules.some((rule) => !rule.path && sameIdentity(rule.identity, change.identity))} permanentFieldIgnores={ignoreRules.filter((rule) => Boolean(rule.path) && sameIdentity(rule.identity, change.identity)).map((rule) => rule.path!)} reason={ignoreReasons[diffId(change.identity)] ?? ""} onReasonChange={(reason) => setIgnoreReasons((current) => ({ ...current, [diffId(change.identity)]: reason }))} onToggleResource={() => toggleResourceExclusion(change.identity)} onToggleField={(path) => toggleFieldExclusion(change.identity, path)} onSaveIgnore={(path) => void saveIgnoreRule(change.identity, path, ignoreReasons[diffId(change.identity)] ?? "")} busy={busy} ignored />)}</section>}
              {selectionDirty && <div className="mt-5 flex flex-wrap items-center gap-3 rounded-lg border border-primary/20 bg-primary/5 p-3"><p className="min-w-0 flex-1 text-xs text-muted-foreground">Exclusions are drafts until you save. Saving creates a new immutable plan and clears any earlier approval.</p><Button size="sm" variant="outline" onClick={() => { setSelectionResources(activePlan.plan.selection?.resources ?? []); setSelectionFields(activePlan.plan.selection?.fields ?? []) }} disabled={busy}>Reset</Button><Button size="sm" loading={pendingAction === "selection"} loadingText="Recalculating…" onClick={() => void savePlanSelection()} disabled={busy}>Create selected plan</Button></div>}
              {activePlan.plan.requiresApproval && <div className="mt-5 rounded-lg border border-amber-300/70 bg-amber-50/70 p-3.5 text-xs leading-5 text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><strong>{requiredApprovals} distinct approval{requiredApprovals === 1 ? "" : "s"} required.</strong> {activePlan.plan.approvalKind === "deletion" ? "Review the exact resources being removed." : activePlan.plan.approvalKind === "takeover" ? "This plan transfers Kubernetes field ownership for the marked resources. Review every changed field before approval." : "Review the cluster-scoped or sync changes."} {approvedApprovals} of {requiredApprovals} approved.<p className="mt-2"><strong>Eligible approvers:</strong> {displayedApproverRoles.length ? displayedApproverRoles.map((role) => role[0].toUpperCase() + role.slice(1)).join(", ") : "selected members only"}{displayedApproverMembers.length ? `; ${displayedApproverMembers.map((member) => member.label).join(", ")}` : ""}. {displayedApproverRoles.length ? "Higher project roles also qualify." : "Only the selected members qualify."}</p>{currentApprovalSummary?.approvals.length ? <ul className="mt-2 space-y-1">{currentApprovalSummary.approvals.map((approval) => <li key={approval.id}>{approval.displayName || approval.email}{approval.role ? ` · ${approval.role}` : ""}{approval.eligible ? " · approved" : " · no longer eligible"}</li>)}</ul> : null}</div>}
              <div className="mt-5 flex flex-wrap justify-end gap-2 border-t pt-4">
                {activePlan.plan.requiresApproval && <ConfirmDisclosure trigger={currentApprovalSummary?.currentUserApproved ? "Approval recorded ✓" : approvalsComplete ? "Approvals complete" : "Add approval"} triggerVariant="outline" title="Approve this exact plan?" description={`${requiredApprovals} distinct eligible project members must approve this exact plan before it can be applied.`} confirmLabel="Approve plan" onConfirm={approvePlan} disabled={busy || hasPendingOperation || Boolean(ownershipConflict) || selectionDirty || !currentApprovalSummary?.canApprove || activePlan.status !== "current"}><ul className="max-h-36 space-y-1 overflow-y-auto text-xs text-muted-foreground">{activePlan.plan.changes.filter((change) => change.kind === "delete" || change.takeover).map((change) => <li key={diffId(change.identity)}>{change.takeover ? "Take over" : "Delete"} {change.identity.kind} {change.identity.namespace}/{change.identity.name}</li>)}</ul></ConfirmDisclosure>}
                <Button loading={pendingAction === "apply-plan"} loadingText="Starting sync…" onClick={() => void applyPlan()} disabled={busy || hasPendingOperation || Boolean(ownershipConflict) || selectionDirty || !canDeploy || (activePlan.plan.decommission && !canApprove) || activePlan.status !== "current" || activePlan.plan.changes.length === 0 || (activePlan.plan.requiresApproval && !approvalsComplete)}>{activePlan.plan.decommission ? "Delete approved resources" : activePlan.plan.requiresApproval ? "Apply approved plan" : "Sync application"}</Button>
              </div>
              {!currentApprovalSummary?.canApprove && activePlan.plan.requiresApproval && !approvalsComplete && <p className="mt-2 text-right text-[10px] text-muted-foreground">Approvals can be added by project members eligible under this plan&apos;s approval rule.</p>}
            </div>}
          </Panel>
        </Tabs.Panel>

        <Tabs.Panel value="components" className="outline-none">
          <Panel surface="flat" title="Managed components" description="JustCD tracks only resources that it created and owns on the cluster.">
            {resources.length ? <div className="min-w-0"><DataGridList rows={resources} columns={[
              { id: "resource", title: "Resource", cell: (item) => <span className="font-medium">{item.identity.name}<span className="mt-0.5 block text-[10px] font-normal text-muted-foreground">{item.identity.kind} · {item.identity.apiVersion}{item.adopted ? " · field handover pending" : ""}</span></span> },
              { id: "namespace", title: "Namespace", cell: (item) => <span className="text-xs text-muted-foreground">{item.identity.namespace || "cluster scope"}</span> },
              { id: "cluster", title: "Cluster", cell: (item) => <span className="font-mono text-[10px] text-muted-foreground">{item.identity.clusterId?.slice(0, 8)}</span> },
              { id: "version", title: "Live version", cell: (item) => <span className="font-mono text-[10px] text-muted-foreground">{item.resourceVersion}</span> },
            ]} empty="A component inventory appears after the first successful sync." /></div> : <EmptyState title="No managed components yet" description="After the first successful sync, this inventory shows every resource owned by this application." />}
          </Panel>
        </Tabs.Panel>

        <Tabs.Panel value="activity" className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px] outline-none">
          <Panel title="Recent operations" description="Sync history and result state">
            {operations.length ? <div className="divide-y">{operations.slice(0, 8).map((operation) => <div key={operation.id} className="px-5 py-3.5"><div className="flex items-center justify-between gap-3"><span className="text-xs font-medium">{operation.status === "succeeded" ? "Sync completed" : operation.status === "failed" ? "Sync stopped" : operation.status === "queued" ? "Sync queued" : "Sync running"}</span><StatusBadge status={operation.status} /></div><p className="mt-1 line-clamp-2 text-[10px] leading-4 text-muted-foreground">{operation.message || "Sync operation"}</p>{operation.progress && <p className="mt-1 text-[10px] text-muted-foreground">{operationPhaseLabel(operation.progress.phase, operation.status)} · {operation.progress.completed.length}/{operation.progress.total} resources{operation.progress.current ? ` · ${resourceLabel(operation.progress.current)}` : ""}</p>}{operation.status === "failed" && <div className="mt-2"><ErrorDetailsButton label="Debug details" error={{ name: "SyncOperationError", message: operation.message, operationId: operation.id, applicationId: operation.applicationId, planId: operation.planId, status: operation.status, startedAt: operation.startedAt, finishedAt: operation.finishedAt, progress: operation.progress }} /></div>}<p className="mt-1.5 text-[9px] text-muted-foreground">{new Date(operation.startedAt).toLocaleString()}</p></div>)}</div> : <EmptyState title="No syncs yet" description="Operations will be recorded here with the actor and resulting status." />}
          </Panel>
          <Panel title="Application source" description="Configuration stored in JustCD"><div className="space-y-3 p-5 text-xs"><KeyValue label="Manifest path" value={application.manifestPath} mono /><KeyValue label="Renderer" value={application.renderer} /><KeyValue label="Cluster" value={application.clusterId.slice(0, 12)} mono /><KeyValue label="Poll interval" value={`${application.pollSeconds} seconds`} />{application.renderer === "kustomize" && <><KeyValue label="Namespace from Git" value={kustomization ? kustomization.namespace || "Not set in kustomization" : kustomizationError ? errorMessage(kustomizationError) : "Checking kustomization…"} />{kustomizationError && <ErrorDetailsButton error={kustomizationError} />}</>}{canApprove && <Link href={`/applications/${applicationID}/edit`} className="inline-block text-xs font-medium text-primary hover:underline">Edit application →</Link>}<Link href={`/projects/${application.projectId}`} className="block text-xs font-medium text-primary hover:underline">Open project →</Link></div></Panel>
        </Tabs.Panel>

        <Tabs.Panel value="settings" className="space-y-5 outline-none">
          {application.renderer === "kustomize" && <Panel title="Render settings" description="Control how Kustomize builds this application's manifests."><div className="space-y-4 p-5 text-xs">
            <div className="rounded-lg border bg-muted/30 p-3"><p className="font-medium">Namespace from Git</p><p className="mt-1 font-mono text-muted-foreground">{kustomization ? kustomization.namespace || "Not set in kustomization" : kustomizationError ? errorMessage(kustomizationError) : "Checking kustomization…"}</p>{kustomizationError != null && <div className="mt-2"><ErrorDetailsButton error={kustomizationError} /></div>}<p className="mt-1 text-muted-foreground">JustCD target: {application.namespaces.map((binding) => binding.namespace).join(", ")}</p></div>
            {kustomization?.namespace && application.namespaces.length === 1 && <label className="flex items-start gap-2"><Checkbox checked={namespaceOverrideDraft} onCheckedChange={(checked) => setNamespaceOverrideDraft(Boolean(checked))} disabled={!canApprove || busy || hasPendingOperation} /><span><span className="block font-medium">Override Git namespace with JustCD target</span><span className="mt-1 block leading-5 text-muted-foreground">Kustomize builds resources in {application.namespaces[0].namespace} instead of {kustomization.namespace}. Explicit namespace references inside manifests may remain unchanged. This can create or delete resources on the next sync, so review the new plan first.</span></span></label>}
            {kustomization?.namespace && application.namespaces.length !== 1 && <p className="text-muted-foreground">An override needs exactly one bound target namespace.</p>}
            <label className="flex items-start gap-2"><Checkbox checked={kustomizeHelmDraft} onCheckedChange={(checked) => setKustomizeHelmDraft(Boolean(checked))} disabled={!canApprove || busy || hasPendingOperation} /><span><span className="block font-medium">Enable Helm charts in Kustomize</span><span className="mt-1 block leading-5 text-muted-foreground">Permits helmCharts from this Git source. Helm may fetch pinned charts from public HTTPS repositories while building the plan.</span></span></label>
            {canApprove && <Button size="sm" variant="outline" loading={pendingAction === "render-settings"} loadingText="Saving settings…" disabled={busy || hasPendingOperation || (kustomizeHelmDraft === application.kustomizeHelmEnabled && namespaceOverrideDraft === application.kustomizeNamespaceOverride)} onClick={() => void saveRenderSettings()}>Save render settings</Button>}
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
              {(ignoreSelectors.length > 0 || ignoreRules.length > 0) && <div className="divide-y rounded-lg border">{ignoreSelectors.map((rule) => <div key={rule.id} className="flex flex-wrap items-center gap-3 px-4 py-3 text-xs"><span className="min-w-0 flex-1"><strong>{rule.kind ? `${rule.apiVersion} · ${rule.kind}` : "Any kind"}</strong>{rule.labelKey && <span className="ml-2 font-mono text-muted-foreground">{rule.labelKey}={rule.labelValue}</span>}<span className="mt-1 block text-muted-foreground">{rule.reason}</span></span>{canApprove && <ConfirmDisclosure trigger="Remove" title="Remove exclusion?" description="Future plans may manage matching resources again." confirmLabel="Remove exclusion" onConfirm={() => removeIgnoreSelector(rule)} disabled={busy || hasPendingOperation} />}</div>)}{ignoreRules.map((rule) => <div key={rule.id} className="flex flex-wrap items-center gap-3 px-4 py-3 text-xs"><span className="min-w-0 flex-1"><strong>{rule.identity.kind} {rule.identity.namespace}/{rule.identity.name}</strong><span className="ml-2 font-mono text-muted-foreground">{rule.path || "whole resource"}</span><span className="mt-1 block text-muted-foreground">{rule.reason}</span></span>{canApprove && <ConfirmDisclosure trigger="Remove" title="Remove ignore rule?" description="Future plans may manage this resource again." confirmLabel="Remove rule" onConfirm={async () => { await removeIgnoreRule(rule) }} disabled={busy || hasPendingOperation} />}</div>)}</div>}
            </div>
          </Panel>
          {canApprove && <Panel title="Delete application" description="Choose whether JustCD keeps or removes the resources it manages."><div className="flex flex-wrap items-center justify-between gap-3 p-5"><p className="text-xs text-muted-foreground">{`${resources.length} managed resource${resources.length === 1 ? "" : "s"} currently recorded.`} Keeping them removes ownership tracking from JustCD.</p><ConfirmDisclosure trigger="Delete application" title={`Delete ${application.name}?`} description="Removing this application from JustCD cannot be undone. Choose what happens to its managed Kubernetes resources." confirmLabel="Delete application" onConfirm={deleteApplication} disabled={busy || hasPendingOperation}><FormField label="Managed cluster resources" htmlFor="application-delete-policy"><FormSelect id="application-delete-policy" value={deletePolicy} onValueChange={setDeletePolicy} items={[{ value: "keep", label: "Keep resources in Kubernetes" }, { value: "delete", label: "Delete resources through a reviewed plan" }]} /></FormField>{deletePolicy === "delete" && resources.length > 0 && <p className="mt-2 text-xs text-muted-foreground">A deletion plan must receive {deletionApprovalCount} approval{deletionApprovalCount === 1 ? "" : "s"} from eligible project members before the resources can be removed.</p>}</ConfirmDisclosure></div></Panel>}
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
    <div className="flex flex-wrap items-center gap-2 border-b bg-muted/20 px-3 py-2"><span className={`rounded border px-1.5 py-0.5 text-[9px] font-semibold uppercase ${color}`}>{change.kind}</span><span className="min-w-0 flex-1 truncate text-xs font-medium">{name}</span>{change.takeover && <span className="rounded-full border border-amber-300 bg-amber-50 px-2 py-0.5 text-[9px] text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200">Field ownership takeover</span>}{ignored && <span className="rounded-full border border-amber-300 bg-amber-50 px-2 py-0.5 text-[9px] text-amber-800">Excluded</span>}{lineDiff && <span className="flex shrink-0 items-center gap-3 text-[10px] text-muted-foreground"><span><span className="font-semibold text-rose-700 dark:text-rose-300">−</span> removed</span><span><span className="font-semibold text-emerald-700 dark:text-emerald-300">+</span> added</span></span>}{change.identity.clusterScoped && <span className="rounded-full border px-2 py-0.5 text-[9px] text-amber-700">cluster-wide</span>}</div>
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
