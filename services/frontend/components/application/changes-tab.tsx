"use client"

import { useMemo, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Alert02Icon, Tick02Icon } from "@hugeicons/core-free-icons"
import { RowActions } from "@/components/action-menu"
import { diffId, safeIgnorePath, sameIdentity } from "@/components/application/helpers"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { ManifestDiff } from "@/components/manifest-diff"
import { PlanReview } from "@/components/plan-review"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { CheckboxCard, EmptyState, Panel, StatusBadge } from "@/components/ui-kit"
import type { Change, FieldExclusion, Identity, IgnoreRule, PlanApprovalSummary, PlanRecord } from "@/lib/types"

const kindTone = { create: "success-light", update: "info-light", delete: "destructive-light" } as const
const countTone = {
  create: "border-success/15 bg-success/10 text-success-foreground dark:text-success",
  update: "border-info/15 bg-info/10 text-info-foreground dark:text-info",
  delete: "border-destructive/15 bg-destructive/10 text-destructive-foreground dark:text-destructive",
} as const

function ChangeCount({ label, count, kind }: { label: string; count: number; kind: keyof typeof countTone }) {
  return <div className={`rounded-lg border px-3 py-2 ${countTone[kind]}`}><span className="text-lg font-semibold tabular-nums">{count}</span><span className="ml-2 text-sm font-medium">{label}</span></div>
}

function PlanLoading({ label }: { label: string }) {
  return <div role="status" aria-label={label} className="space-y-4 p-5 sm:p-6"><span className="sr-only">{label}…</span><div aria-hidden="true" className="space-y-3"><Skeleton className="h-5 w-2/3 max-w-72 motion-reduce:animate-none" /><Skeleton className="h-4 w-full max-w-lg motion-reduce:animate-none" /><Skeleton className="h-4 w-4/5 max-w-md motion-reduce:animate-none" /></div></div>
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
  const fieldPaths = useMemo(() => (change.changedPaths ?? []).filter(safeIgnorePath), [change.changedPaths])
  const showFields = fieldPaths.length > 0 && change.kind !== "delete"
  return <article id={diffId(change.identity)} className="scroll-mt-6 overflow-hidden rounded-lg border">
    <div className="flex flex-wrap items-center gap-2 border-b bg-muted/20 px-3 py-2">
      <Badge variant={ignored ? "secondary" : kindTone[change.kind]} className="uppercase">{ignored ? "Excluded" : change.kind}</Badge>
      <span className="min-w-0 flex-1 break-words text-sm font-medium">{name}</span>
      {change.takeover && <Badge variant="warning-light" radius="full">Field ownership takeover</Badge>}
      {ignored && <Badge variant="warning-light" radius="full">Not applied</Badge>}
      {change.identity.clusterScoped && <Badge variant="outline" radius="full">cluster-wide</Badge>}
    </div>
    {ignored && <div role="status" className="border-b bg-muted/30 px-4 py-3 text-sm"><strong>Excluded difference — no changes will be applied for this entry.</strong><p className="mt-1 text-sm text-muted-foreground">{change.ignoreReason || "Excluded from this sync."} The diff below shows the difference between live state and Git, not a scheduled update.</p></div>}
    {!ignored && change.kind === "delete" && <div role="alert" className="flex items-start gap-3 border-b border-destructive/30 bg-destructive/5 px-4 py-3"><HugeiconsIcon icon={Alert02Icon} strokeWidth={2} aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-destructive" /><div><p className="text-sm font-semibold text-destructive">This resource will be deleted from Kubernetes</p><p className="mt-1 text-sm">Applying this plan removes {name}. This may interrupt service or remove persisted data. Review this deletion before approving the plan.</p></div></div>}
    <ManifestDiff key={diffId(change.identity)} before={change.before} after={change.after} excluded={ignored} />
    {(canSelect || canManageIgnores) && <Disclosure className="border-t bg-muted/10 px-4 py-3" summary={`Exclusions & ignore rules${excluded || excludedPaths.length ? ` · ${excluded ? "whole resource" : `${excludedPaths.length} fields`} selected` : ""}`}>
      <div className="space-y-3">
        <CheckboxCard checked={permanentResourceIgnore || excluded} disabled={!canSelect || permanentResourceIgnore} onCheckedChange={onToggleResource} title={permanentResourceIgnore ? "Resource is ignored permanently" : "Exclude this resource from this plan"} />
        {showFields && <div className="space-y-2">
          <p className="text-sm font-medium">Select changed fields</p>
          <div className="grid gap-2 sm:grid-cols-2">{fieldPaths.map((path) => {
            const permanent = permanentFieldIgnores.includes(path)
            return <CheckboxCard key={path} checked={permanent || excludedPaths.includes(path)} disabled={!canSelect || permanent} onCheckedChange={() => onToggleField(path)} title={<span className="break-all font-mono">{path}</span>} description={permanent ? "Ignored by a persistent rule" : "Skip this field for this plan"} />
          })}</div>
          <p className="text-sm text-muted-foreground">Field ignores require another Kubernetes field manager to own the value first. Lists can only be excluded as a whole; JustCD validates ownership before saving.</p>
        </div>}
        {canManageIgnores && <Disclosure className="border-t pt-3" summary="Permanent ignore rule…">
          <div className="space-y-2.5">
            <Textarea aria-label={`Reason for ignoring ${name}`} rows={2} value={reason} onChange={(event) => onReasonChange(event.target.value)} placeholder="Why should future plans ignore this? (required)" className="min-h-16 resize-y" maxLength={500} />
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="outline" disabled={busy || permanentResourceIgnore || reason.trim().length < 5} onClick={() => onSaveIgnore("")}>{permanentResourceIgnore ? "Resource already ignored" : "Ignore resource in future plans"}</Button>
              {fieldPaths.filter((path) => !permanentFieldIgnores.includes(path)).map((path) => <Button key={path} size="sm" variant="ghost" disabled={busy || reason.trim().length < 5 || change.kind === "delete"} onClick={() => onSaveIgnore(path)}>Ignore {path} in future plans</Button>)}
            </div>
            <p className="text-sm text-muted-foreground">Only workspace owners can change permanent rules. Each change is audited and invalidates existing plans.</p>
          </div>
        </Disclosure>}
        {!canManageIgnores && canSelect && <p className="text-sm text-muted-foreground">Permanent ignore rules can be managed by workspace owners.</p>}
        {change.ignoreReason && ignored && <p className="text-sm text-muted-foreground">{change.ignoreReason}</p>}
      </div>
    </Disclosure>}
    {ignored && <p className="border-t p-4 text-sm text-muted-foreground">{change.ignoreReason || "Excluded from this sync; no changes will be applied for this difference."}</p>}
  </article>
}

export function ChangesTab({ plans, activePlan, onSelectPlan, plansLoading, plansError, retryingPlans, onRetryPlans, decommissioning, canDeploy, canApprove, busy, hasPendingOperation, pendingAction, hasOwnershipConflict, reviewResource, onReviewResourceChange, selectionResources, selectionFields, selectionDirty, onToggleResource, onToggleField, onResetSelection, onSaveSelection, ignoreRules, ignoreReasons, onIgnoreReasonChange, onSaveIgnore, onRemoveRule, requiredApprovals, approvedApprovals, approvalsComplete, approverRoles, approverMembers, approvalSummary, approvalComment, onApprovalCommentChange, onApprove, onApply, onCreatePlan }: {
  plans: PlanRecord[]
  activePlan: PlanRecord | null
  onSelectPlan: (id: string) => void
  plansLoading: boolean
  plansError: unknown | null
  retryingPlans: boolean
  onRetryPlans: () => void
  decommissioning: boolean
  canDeploy: boolean
  canApprove: boolean
  busy: boolean
  hasPendingOperation: boolean
  pendingAction: string
  hasOwnershipConflict: boolean
  reviewResource: string
  onReviewResourceChange: (key: string) => void
  selectionResources: Identity[]
  selectionFields: FieldExclusion[]
  selectionDirty: boolean
  onToggleResource: (identity: Identity) => void
  onToggleField: (identity: Identity, path: string) => void
  onResetSelection: () => void
  onSaveSelection: () => void
  ignoreRules: IgnoreRule[]
  ignoreReasons: Record<string, string>
  onIgnoreReasonChange: (key: string, reason: string) => void
  onSaveIgnore: (identity: Identity, path: string, reason: string) => void
  onRemoveRule: (rule: IgnoreRule) => Promise<void>
  requiredApprovals: number
  approvedApprovals: number
  approvalsComplete: boolean
  approverRoles: string[]
  approverMembers: { id: string; label: string }[]
  approvalSummary: PlanApprovalSummary | null
  approvalComment: string
  onApprovalCommentChange: (comment: string) => void
  onApprove: () => Promise<void>
  onApply: () => void
  onCreatePlan: () => void
}) {
  const [expandedAppliedPlan, setExpandedAppliedPlan] = useState<string | null>(null)
  const counts = activePlan?.plan.changes.reduce((acc, item) => ({ ...acc, [item.kind]: acc[item.kind] + 1 }), { create: 0, update: 0, delete: 0 })
  const title = activePlan?.plan.rollback ? "Rollback plan & diff" : activePlan?.plan.decommission ? "Deletion plan & diff" : "Plan & diff"
  const description = activePlan?.plan.rollback ? "A rollback is a new reviewed plan that restores a recorded deployment or Git revision." : activePlan?.plan.decommission ? "Every managed resource below will be checked again before it is deleted." : "A reviewed plan is a snapshot of the desired Git commit and live cluster state."
  const blocked = busy || hasPendingOperation

  return <Panel title={title} description={description} action={plans.length > 0 && <FormSelect ariaLabel="Select plan" value={activePlan?.id ?? ""} onValueChange={onSelectPlan} className="h-8 max-w-[220px]" items={plans.map((plan) => ({ value: plan.id, label: `${new Date(plan.createdAt).toLocaleString()} · ${plan.plan.rollback ? "rollback · " : plan.plan.decommission ? "deletion · " : ""}${plan.status}` }))} />}>
    {plansLoading ? <PlanLoading label="Loading plans and differences" />
      : plansError != null ? <div role="alert" className="flex flex-wrap items-center justify-between gap-3 p-5 text-sm"><span>Plans could not be loaded.</span><Button size="sm" loading={retryingPlans} onClick={onRetryPlans}>Retry</Button></div>
      : !activePlan ? <EmptyState title="No review plan yet" description="Build a plan to render Git manifests and compare them with live, app-owned resources." />
      : <div className="p-4 sm:p-5">
        {activePlan.status === "applied" && <div role="status" className="mb-4 rounded-lg border border-success/20 bg-success/5 p-4"><p className="flex items-center gap-2 text-sm font-semibold"><HugeiconsIcon icon={Tick02Icon} strokeWidth={2} aria-hidden="true" className="size-4 text-success-foreground dark:text-success" />Plan successfully applied</p><p className="mt-1 text-sm text-muted-foreground">These changes have already been executed. This plan is a historical snapshot, not a list of pending changes. Refresh the plan to compare Git with the current cluster state.</p><div className="mt-3 flex flex-wrap gap-2"><Button size="sm" variant="outline" onClick={() => setExpandedAppliedPlan(expandedAppliedPlan === activePlan.id ? null : activePlan.id)}>{expandedAppliedPlan === activePlan.id ? "Hide applied diff" : "View applied diff"}</Button><Button size="sm" onClick={onCreatePlan} disabled={blocked || !canDeploy || decommissioning}>Refresh plan</Button></div></div>}
        {(activePlan.status !== "applied" || expandedAppliedPlan === activePlan.id) && <>
          <div className="mb-4 flex flex-wrap items-center gap-2"><StatusBadge status={activePlan.status} /><span className="font-mono text-xs text-muted-foreground">{activePlan.plan.revision.slice(0, 12)}</span><span className="text-sm text-muted-foreground">· expires {new Date(activePlan.expiresAt).toLocaleTimeString()}</span><span className="ml-auto font-mono text-xs text-muted-foreground">{activePlan.plan.digest.slice(0, 16)}</span></div>
          {activePlan.trigger && <p className="mb-4 break-all text-sm text-muted-foreground">Triggered by {activePlan.trigger.provider} push to <code>{activePlan.trigger.ref}</code> · delivery <code>{activePlan.trigger.deliveryId}</code> · reported <code>{activePlan.trigger.reportedCommit.slice(0, 12)}</code>{activePlan.trigger.reportedCommit !== activePlan.plan.revision ? " · branch advanced before planning" : ""}</p>}
          {activePlan.plan.rollback && <section className="mb-5 rounded-lg border bg-muted/30 p-3.5 text-sm leading-6"><div className="flex items-center gap-2 font-semibold"><HugeiconsIcon icon={Alert02Icon} strokeWidth={2} aria-hidden="true" className="size-4 text-warning-foreground dark:text-warning" />Rollback target · {activePlan.plan.rollback.kind === "successful_sync" ? "successful deployment" : activePlan.plan.rollback.kind === "pre_operation" ? "state before an interrupted operation" : "Git revision"}</div><p className="mt-1">{activePlan.plan.rollback.revision ? `Resolved commit ${activePlan.plan.rollback.revision.slice(0, 12)}.` : "This checkpoint has no historical successful commit; the application ref will remain paused."}{activePlan.plan.rollback.createdAt ? ` Captured ${new Date(activePlan.plan.rollback.createdAt).toLocaleString()}.` : ""}{activePlan.plan.rollback.operationId ? ` Operation ${activePlan.plan.rollback.operationId.slice(0, 8)}.` : ""} {activePlan.plan.rollback.resourceCount !== undefined ? `${activePlan.plan.rollback.resourceCount} resources in target.` : ""}</p><p className="mt-2 font-medium">Kubernetes resources cannot be changed atomically. This diff may contain creations, updates, and deletions; a failure can leave a visible partial result.</p>{(activePlan.plan.ignored?.length ?? 0) > 0 && <p className="mt-2">{activePlan.plan.ignored?.length} difference{activePlan.plan.ignored?.length === 1 ? " is" : "s are"} limited by the application&apos;s current ignore rules.</p>}</section>}
          <div className="mb-5 grid grid-cols-3 gap-2"><ChangeCount label="Create" count={counts?.create ?? 0} kind="create" /><ChangeCount label="Update" count={counts?.update ?? 0} kind="update" /><ChangeCount label="Delete" count={counts?.delete ?? 0} kind="delete" /></div>
          {ignoreRules.length > 0 && <Disclosure className="mb-5 rounded-lg border bg-muted/10 px-4 py-3" summary={`Persistent ignore rules (${ignoreRules.length})`}>
            <p className="mb-3 text-sm text-muted-foreground">Applied to manual and Auto-safe plans. Changes invalidate reviewed plans.</p>
            <ul className="divide-y rounded-lg border bg-card">{ignoreRules.map((rule) => <li key={rule.id} className="flex flex-wrap items-center gap-3 px-4 py-3"><span className="min-w-0 flex-1 text-sm font-medium">{rule.identity.kind} {rule.identity.namespace ? `${rule.identity.namespace}/` : ""}{rule.identity.name}<span className="ml-2 font-mono text-xs font-normal text-muted-foreground">{rule.path || "entire resource"}</span><span className="mt-1 block text-sm font-normal text-muted-foreground">{rule.reason}</span></span>{rule.managedByGit ? <Badge variant="outline" radius="full">Managed by Git</Badge> : canApprove && <RowActions label={`ignore rule for ${rule.identity.kind} ${rule.identity.name}`} items={[{ label: "Remove ignore rule", destructive: true, disabled: busy, confirm: { title: `Remove ignore rule for ${rule.identity.kind} ${rule.identity.name}?`, description: "The next plan may update or delete this resource again.", confirmLabel: "Remove ignore rule", onConfirm: () => onRemoveRule(rule) } }]} />}</li>)}</ul>
          </Disclosure>}
          <PlanReview key={activePlan.id} changes={activePlan.plan.changes} ignored={activePlan.plan.ignored ?? []} selected={reviewResource} onSelect={onReviewResourceChange} identityKey={(change) => diffId(change.identity)} isExcluded={(change) => selectionResources.some((identity) => sameIdentity(identity, change.identity))} renderChange={(change, ignored) => <DiffCard change={change} ignored={ignored} canSelect={activePlan.status === "current" && canDeploy && !activePlan.plan.decommission && !activePlan.plan.rollback} canManageIgnores={activePlan.status === "current" && canApprove && !activePlan.plan.decommission && !activePlan.plan.rollback} excluded={selectionResources.some((identity) => sameIdentity(identity, change.identity))} excludedPaths={selectionFields.filter((field) => sameIdentity(field.identity, change.identity)).map((field) => field.path)} permanentResourceIgnore={ignoreRules.some((rule) => !rule.path && sameIdentity(rule.identity, change.identity))} permanentFieldIgnores={ignoreRules.filter((rule) => Boolean(rule.path) && sameIdentity(rule.identity, change.identity)).map((rule) => rule.path!)} reason={ignoreReasons[diffId(change.identity)] ?? ""} onReasonChange={(reason) => onIgnoreReasonChange(diffId(change.identity), reason)} onToggleResource={() => onToggleResource(change.identity)} onToggleField={(path) => onToggleField(change.identity, path)} onSaveIgnore={(path) => onSaveIgnore(change.identity, path, ignoreReasons[diffId(change.identity)] ?? "")} busy={busy} />} />
          {activePlan.plan.changes.length === 0 && <p className="mt-4 text-sm text-muted-foreground">{activePlan.plan.rollback ? "The target matches the managed state. Applying will pin the source and pause reconciliation." : "No changes to apply. Git and managed fields match, or all differences are excluded."}</p>}
          {selectionDirty && <div className="mt-5 flex flex-wrap items-center gap-3 rounded-lg border border-primary/20 bg-primary/5 p-3"><p className="min-w-0 flex-1 text-sm text-muted-foreground">Exclusions are drafts until you save. Saving creates a new immutable plan and clears any earlier approval.</p><Button size="sm" variant="outline" onClick={onResetSelection} disabled={busy}>Reset</Button><Button size="sm" loading={pendingAction === "selection"} loadingText="Recalculating…" onClick={onSaveSelection} disabled={busy}>Create selected plan</Button></div>}
          {activePlan.status === "current" && activePlan.plan.requiresApproval && <Disclosure className="mt-5 rounded-lg border bg-muted/30 p-3.5" summary={`Approval requirements · ${approvedApprovals}/${requiredApprovals} approved`}>
            <div className="text-sm leading-6"><strong>{requiredApprovals} distinct approval{requiredApprovals === 1 ? "" : "s"} required.</strong> {activePlan.plan.approvalKind === "rollback" ? "A workspace owner must approve every rollback; the deletion policy may require additional distinct owner approvals for removed resources." : activePlan.plan.approvalKind === "deletion" ? "Review the exact resources being removed." : activePlan.plan.approvalKind === "takeover" ? "This plan transfers Kubernetes field ownership for the marked resources. Review every changed field before approval." : "Review the cluster-scoped or sync changes."} {approvedApprovals} of {requiredApprovals} approved.<p className="mt-2"><strong>Eligible approvers:</strong> {approverRoles.length ? approverRoles.map((role) => role[0].toUpperCase() + role.slice(1)).join(", ") : "selected members only"}{approverMembers.length ? `; ${approverMembers.map((member) => member.label).join(", ")}` : ""}. {approverRoles.length ? "Higher workspace roles also qualify." : "Only the selected members qualify."}</p>{approvalSummary?.approvals.length ? <ul className="mt-2 space-y-1">{approvalSummary.approvals.map((approval) => <li key={approval.id}>{approval.displayName || approval.email}{approval.role ? ` · ${approval.role}` : ""}{approval.eligible ? " · approved" : " · no longer eligible"}{approval.comment && <span className="block text-sm text-muted-foreground">“{approval.comment}”</span>}</li>)}</ul> : null}</div>
          </Disclosure>}
          {activePlan.status === "current" && <div className="sticky bottom-[max(0.75rem,var(--toast-clearance,0px))] z-20 mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-card/95 px-3 py-2.5 shadow-sm backdrop-blur">
            <p className="text-sm font-medium">{activePlan.plan.changes.length} changes · {counts?.delete ?? 0} deletions{activePlan.plan.requiresApproval ? ` · ${approvedApprovals}/${requiredApprovals} approvals` : ""}</p>
            <div className="flex flex-wrap items-center justify-end gap-2">
              {activePlan.plan.requiresApproval && <ConfirmDisclosure trigger={approvalSummary?.currentUserApproved ? <><HugeiconsIcon icon={Tick02Icon} strokeWidth={2} aria-hidden="true" />Approval recorded</> : approvalsComplete ? "Approvals complete" : "Add approval"} triggerVariant="outline" confirmVariant="default" title="Approve this exact plan?" description={`${requiredApprovals} distinct eligible workspace members must approve this exact plan before it can be applied.`} confirmLabel="Approve plan" onConfirm={onApprove} disabled={blocked || hasOwnershipConflict || selectionDirty || !approvalSummary?.canApprove || activePlan.status !== "current"}>
                <div className="space-y-3"><Input aria-label="Approval comment" value={approvalComment} maxLength={1000} onChange={(event) => onApprovalCommentChange(event.target.value)} placeholder="Comment (optional)" /><ul className="max-h-36 space-y-1 overflow-y-auto text-sm text-muted-foreground">{activePlan.plan.changes.filter((change) => change.kind === "delete" || change.takeover).map((change) => <li key={diffId(change.identity)}>{change.takeover ? "Take over" : "Delete"} {change.identity.kind} {change.identity.namespace}/{change.identity.name}</li>)}</ul></div>
              </ConfirmDisclosure>}
              <Button loading={pendingAction === "apply-plan"} loadingText={activePlan.plan.rollback ? "Starting rollback…" : "Starting sync…"} onClick={onApply} disabled={blocked || hasOwnershipConflict || selectionDirty || !canDeploy || (activePlan.plan.decommission && !canApprove) || (activePlan.plan.rollback && !canApprove) || activePlan.status !== "current" || (!activePlan.plan.rollback && activePlan.plan.changes.length === 0) || (activePlan.plan.requiresApproval && !approvalsComplete)}>{activePlan.plan.rollback ? "Apply approved rollback" : activePlan.plan.decommission ? "Delete approved resources" : activePlan.plan.requiresApproval ? "Apply approved plan" : "Sync application"}</Button>
            </div>
          </div>}
          {activePlan.status === "current" && !approvalSummary?.canApprove && activePlan.plan.requiresApproval && !approvalsComplete && <p className="mt-2 text-right text-sm text-muted-foreground">Approvals can be added by workspace members eligible under this plan&apos;s approval rule.</p>}
        </>}
      </div>}
  </Panel>
}
