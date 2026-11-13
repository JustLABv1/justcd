"use client"

import Link from "next/link"
import { useState, type ReactNode } from "react"
import { HugeiconsIcon, type IconSvgElement } from "@hugeicons/react"
import { Alert02Icon, AlertCircleIcon, PauseIcon } from "@hugeicons/core-free-icons"
import { ActionMenu } from "@/components/action-menu"
import { ErrorDetailsButton, ErrorDetailsDialog } from "@/components/error-details"
import { Frame } from "@/components/panelui/frame"
import { Button } from "@/components/panelui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { Input } from "@/components/panelui/input"
import { FormField } from "@/components/ui-kit"
import { resourceLabel } from "@/components/application/helpers"
import type { Application, Operation } from "@/lib/types"

type Tone = "destructive" | "warning" | "info"

const toneClass: Record<Tone, string> = {
  destructive: "bg-destructive/15 text-destructive",
  warning: "bg-warning/15 text-warning",
  info: "bg-info/15 text-info",
}

function AttentionRow({ tone, icon, title, detail, children, actions, alert = false }: { tone: Tone; icon: IconSvgElement; title: ReactNode; detail?: string; children?: ReactNode; actions?: ReactNode; alert?: boolean }) {
  return <Frame.Row role={alert ? "alert" : "status"} className="flex-wrap items-start gap-x-4 gap-y-3">
    <span aria-hidden="true" className={`grid size-8 shrink-0 place-items-center rounded-full ${toneClass[tone]}`}><HugeiconsIcon icon={icon} strokeWidth={2} className="size-4" /></span>
    <div className="min-w-0 flex-1 basis-72 space-y-0.5">
      <p className="text-sm font-medium">{title}</p>
      {detail && <pre className="mt-2 max-h-48 overflow-auto rounded-md border bg-muted/40 p-2 text-xs leading-5 whitespace-pre-wrap break-words" aria-label="Renderer output">{detail}</pre>}
      {children}
    </div>
    {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
  </Frame.Row>
}

type Issue = Application["statusIssues"][number]

function StatusIssueRow({ application, issue, canApprove, onConfigureExclusion, extraActions }: { application: Application; issue: Issue; canApprove: boolean; onConfigureExclusion: (issue: Issue) => void; extraActions?: ReactNode }) {
  const [open, setOpen] = useState(false)
  const diagnostics = { name: "ApplicationCheckFailed", message: issue.summary, applicationId: application.id, checkedAt: application.lastCheckedAt, issues: [issue] }
  const rendererOutput = issue.causes?.find((cause) => cause.reason === "renderer_output")?.message
  const configurable = issue.code === "kubernetes.permission_denied" && canApprove
  return <AttentionRow alert tone="destructive" icon={Alert02Icon} title={issue.summary} detail={rendererOutput} actions={configurable
    ? <>
      {extraActions}
      <Button size="sm" variant="outline"  onClick={() => onConfigureExclusion(issue)}>Configure exclusion</Button>
      <ActionMenu label="More" size="sm" variant="ghost" items={[{ label: "View diagnostics", onSelect: () => setOpen(true) }]} />
      <ErrorDetailsDialog error={diagnostics} open={open} onOpenChange={setOpen} />
    </>
    : <>{extraActions}<ErrorDetailsButton label="View diagnostics" error={diagnostics} /></>} />
}

function IssueGroup({ application, issues, canApprove, onConfigureExclusion, aiAction }: { application: Application; issues: Issue[]; canApprove: boolean; onConfigureExclusion: (issue: Issue) => void; aiAction?: ReactNode }) {
  const [open, setOpen] = useState(false)
  return <>
    <AttentionRow alert tone="destructive" icon={Alert02Icon} title={`${issues.length} problems need attention`} actions={<>
      {aiAction}
      <Button size="sm" variant="ghost" aria-expanded={open} onClick={() => setOpen((value) => !value)}>{open ? "Hide problems" : "Show problems"}</Button>
    </>} />
    {open && issues.map((issue, index) => <StatusIssueRow key={`${issue.source}-${index}`} application={application} issue={issue} canApprove={canApprove} onConfigureExclusion={onConfigureExclusion} />)}
  </>
}

function RuntimeRow({ application, onViewResources, onOpenHealth }: { application: Application; onViewResources: () => void; onOpenHealth: () => void }) {
  const condition = application.healthCondition
  const resources = (condition?.resources ?? []).filter((item) => item.status !== "Healthy")
  return <AttentionRow alert tone="destructive" icon={Alert02Icon} title={`Runtime ${(condition?.status ?? "degraded").toLowerCase()}`} actions={<>
    <Button size="sm" variant="outline" onClick={onViewResources}>View resources</Button>
    <Button size="sm" variant="ghost" onClick={onOpenHealth}>Health details</Button>
  </>}>
    {condition?.message && <p className="mt-0.5 text-sm text-muted-foreground">{condition.message}</p>}
    {resources.length > 0 && <ul className="mt-1.5 space-y-1 text-sm">
      {resources.slice(0, 3).map((item, index) => <li key={`${resourceLabel(item.identity)}-${index}`} className="break-words"><span className="font-medium">{resourceLabel(item.identity)}</span><span className="text-muted-foreground"> · {item.reason}{item.readiness ? ` · ${item.readiness}` : ""}</span></li>)}
      {resources.length > 3 && <li className="text-muted-foreground">and {resources.length - 3} more</li>}
    </ul>}
  </AttentionRow>
}

/**
 * Single priority-ordered region for everything that needs the user's attention above the tabs:
 * check failures, load errors, ownership conflicts, namespace mismatch, deletion in progress and paused auto-sync.
 * Each row is a short summary with at most one primary action.
 */
export function AttentionBar({ runtimeProblem = false, onViewResources, onOpenHealth, operation, application, canApprove, busy, hasPendingOperation, pendingAction, plansError, retryingPlans, onRetryPlans, conflictCount, onReviewConflicts, namespaceMismatch, kustomizeNamespace, onReviewNamespaceSetting, onCancelDecommission, onConfigureExclusion, aiAction, resumeRevision, onResumeRevisionChange, onKeepPin, onResume, latestOperation, canDeploy, onRetryReconciliation, onViewActivity }: {
  /** The live runtime is degraded or missing. The header badge alone is too easy to miss. */
  runtimeProblem?: boolean
  onViewResources: () => void
  onOpenHealth: () => void
  /** A failed sync, drawn as rows of this same frame. */
  operation?: ReactNode
  application: Application
  latestOperation?: Operation
  canDeploy: boolean
  onRetryReconciliation: () => void
  onViewActivity?: () => void
  canApprove: boolean
  busy: boolean
  hasPendingOperation: boolean
  pendingAction: string
  plansError: unknown | null
  retryingPlans: boolean
  onRetryPlans: () => void
  conflictCount: number
  onReviewConflicts: () => void
  namespaceMismatch: boolean
  kustomizeNamespace?: string
  onReviewNamespaceSetting: () => void
  onCancelDecommission: () => void
  onConfigureExclusion: (issue: Issue) => void
  aiAction?: ReactNode
  resumeRevision: string
  onResumeRevisionChange: (value: string) => void
  onKeepPin: () => void
  onResume: () => void
}) {
  // Ownership conflicts are summarised by their own row with a direct action.
  const issues = (application.statusIssues ?? []).filter((issue) => !(conflictCount > 0 && issue.code === "kubernetes.ownership_conflict"))
  const paused = Boolean(application.autoSyncPaused && !application.branchTest)
  const mismatch = namespaceMismatch && !application.kustomizeNamespaceOverride
  const stalled = Boolean(application.retryTerminalReason || application.retryNextAt)
  const failedSync = latestOperation?.status === "failed" ? latestOperation : undefined
  if (!runtimeProblem && !operation && !issues.length && plansError == null && !conflictCount && !mismatch && !application.decommissioning && !paused && !stalled && !failedSync) return null
  const resumeBlocked = busy || hasPendingOperation
  const worst = runtimeProblem || operation || issues.length > 0 || stalled || failedSync || plansError != null || application.decommissioning ? "text-destructive" : mismatch || conflictCount > 0 ? "text-warning" : "text-info"
  return <Frame aria-label="Needs attention" className="mb-5">
    <Frame.Header><Frame.Title as="h2" className="flex items-center gap-2"><HugeiconsIcon icon={Alert02Icon} strokeWidth={1.8} aria-hidden="true" className={`size-4 ${worst}`} />Needs attention</Frame.Title></Frame.Header>
    <Frame.Panel>
      {issues.length === 1 && <StatusIssueRow application={application} issue={issues[0]} canApprove={canApprove} onConfigureExclusion={onConfigureExclusion} extraActions={aiAction} />}
      {issues.length > 1 && <IssueGroup application={application} issues={issues} canApprove={canApprove} onConfigureExclusion={onConfigureExclusion} aiAction={aiAction} />}
      {runtimeProblem && <RuntimeRow application={application} onViewResources={onViewResources} onOpenHealth={onOpenHealth} />}
      {(stalled || failedSync) && <AttentionRow alert tone="destructive" icon={Alert02Icon} title={application.retryTerminalReason ? "Auto-sync stopped" : application.retryNextAt ? "Sync failed, retry scheduled" : "Last sync failed"} actions={<>
        {canDeploy && !application.decommissioning && <Button size="sm" variant="outline"  loading={pendingAction === "retry-reconciliation"} disabled={resumeBlocked} onClick={onRetryReconciliation}>Retry now</Button>}
        {onViewActivity && <Button size="sm" variant="ghost" onClick={onViewActivity}>View activity</Button>}
      </>}>
        <p className="mt-0.5 text-sm text-muted-foreground">{failedSync?.message || (application.retryTerminalReason ? `Automatic retries ended: ${application.retryTerminalReason.replaceAll("_", " ")}.` : "The last sync did not complete.")}{application.retryNextAt && !application.retryTerminalReason ? ` Next automatic attempt: ${new Date(application.retryNextAt).toLocaleString()}.` : ""}{application.retryLastErrorCode ? ` (${application.retryLastErrorCode})` : ""}</p>
      </AttentionRow>}
      {plansError != null && <AttentionRow alert tone="destructive" icon={Alert02Icon} title="Plans could not be loaded." actions={<>
        <Button size="sm" loading={retryingPlans} onClick={onRetryPlans}>Retry</Button>
        <ErrorDetailsButton error={plansError} />
      </>} />}
      {conflictCount > 0 && <AttentionRow alert tone="warning" icon={AlertCircleIcon} title={`${conflictCount} existing resource${conflictCount === 1 ? " is" : "s are"} owned elsewhere and block${conflictCount === 1 ? "s" : ""} sync`} actions={<Button size="sm" variant="outline"  onClick={onReviewConflicts}>Review conflicts</Button>} />}
      {mismatch && <AttentionRow alert tone="warning" icon={AlertCircleIcon} title={`Kustomize namespace ${kustomizeNamespace} differs from the JustCD target ${application.namespaces[0]?.namespace}`} actions={application.applicationGroupId
        ? <Button size="sm" variant="outline"  render={<Link href={`/application-groups/${application.applicationGroupId}`} />}>Review group settings</Button>
        : <Button size="sm" variant="outline"  onClick={onReviewNamespaceSetting}>Review namespace setting</Button>}>
        <p className="mt-0.5 text-sm text-muted-foreground">A plan cannot be refreshed until an owner enables the namespace transform.</p>
      </AttentionRow>}
      {application.decommissioning && <AttentionRow tone="destructive" icon={Alert02Icon} title="Deletion in progress" actions={canApprove && <Button size="sm" variant="outline"  disabled={busy || hasPendingOperation} onClick={onCancelDecommission}>Cancel deletion</Button>}>
        <p className="mt-0.5 text-sm text-muted-foreground">Auto-sync is paused. Review and apply the deletion plan, then finish removing the application.{application.configurationMissing && " Restore the Git definition to cancel deletion; discovery will recreate the plan while it remains removed."}</p>
      </AttentionRow>}
      {paused && <AttentionRow tone="info" icon={PauseIcon} title="Auto-sync paused" actions={canApprove && <>
        <Button size="sm" variant="outline"  loading={pendingAction === "rollback-state-resume"} disabled={resumeBlocked || (Boolean(application.rollbackResumeRequiresRevision) && !resumeRevision.trim())} onClick={onResume}>{application.rollbackResumeAvailable ? "Resume previous source" : "Resume auto-sync"}</Button>
        {application.rollbackResumeAvailable && <ActionMenu label="More" size="sm" variant="ghost" items={[{ label: pendingAction === "rollback-state-keep" ? "Keeping pin…" : "Keep rollback pin", disabled: resumeBlocked, onSelect: onKeepPin }]} />}
      </>}>
        <Disclosure className="mt-1" summary="What happens when I resume?">
          <p className="max-w-2xl text-sm leading-6 text-muted-foreground">{application.rollbackResumeAvailable ? "JustCD is pinned to the approved target. An owner can keep this pin or explicitly resume the previous tracked source." : "JustCD will not automatically sync while paused. Manual changes are protected from automatic reconciliation. Resuming may overwrite them with the Git configuration."}</p>
        </Disclosure>
        {application.rollbackResumeRequiresRevision && <div className="mt-3 max-w-md"><FormField label="Git revision to use before resuming" htmlFor="rollback-resume-revision"><Input id="rollback-resume-revision" value={resumeRevision} onChange={(event) => onResumeRevisionChange(event.target.value)} placeholder="branch, tag, or commit" /></FormField></div>}
      </AttentionRow>}
      {operation}
    </Frame.Panel>
  </Frame>
}
