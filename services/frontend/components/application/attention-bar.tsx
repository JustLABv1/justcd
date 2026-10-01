"use client"

import Link from "next/link"
import { useState, type ReactNode } from "react"
import { HugeiconsIcon, type IconSvgElement } from "@hugeicons/react"
import { Alert02Icon, AlertCircleIcon, PauseIcon } from "@hugeicons/core-free-icons"
import { ActionMenu } from "@/components/action-menu"
import { ErrorDetailsButton, ErrorDetailsDialog } from "@/components/error-details"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { Input } from "@/components/ui/input"
import { FormField } from "@/components/ui-kit"
import type { Application } from "@/lib/types"

type Tone = "destructive" | "warning" | "info"

const toneClass: Record<Tone, string> = {
  destructive: "text-destructive",
  warning: "text-warning-foreground dark:text-warning",
  info: "text-muted-foreground",
}

function AttentionRow({ tone, icon, title, children, actions, alert = false }: { tone: Tone; icon: IconSvgElement; title: ReactNode; children?: ReactNode; actions?: ReactNode; alert?: boolean }) {
  return <li role={alert ? "alert" : "status"} className="flex flex-wrap items-start gap-x-3 gap-y-2 px-4 py-3">
    <HugeiconsIcon icon={icon} strokeWidth={2} aria-hidden="true" className={`mt-0.5 size-4 shrink-0 ${toneClass[tone]}`} />
    <div className="min-w-0 flex-1">
      <p className="text-sm font-medium">{title}</p>
      {children}
    </div>
    {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
  </li>
}

type Issue = Application["statusIssues"][number]

function StatusIssueRow({ application, issue, canApprove, onConfigureExclusion }: { application: Application; issue: Issue; canApprove: boolean; onConfigureExclusion: (issue: Issue) => void }) {
  const [open, setOpen] = useState(false)
  const diagnostics = { name: "ApplicationCheckFailed", message: issue.summary, applicationId: application.id, checkedAt: application.lastCheckedAt, issues: [issue] }
  const configurable = issue.code === "kubernetes.permission_denied" && canApprove
  return <AttentionRow alert tone="destructive" icon={Alert02Icon} title={issue.summary} actions={configurable
    ? <>
      <Button size="sm" variant="outline" onClick={() => onConfigureExclusion(issue)}>Configure exclusion</Button>
      <ActionMenu label="More" size="sm" variant="ghost" items={[{ label: "View diagnostics", onSelect: () => setOpen(true) }]} />
      <ErrorDetailsDialog error={diagnostics} open={open} onOpenChange={setOpen} />
    </>
    : <ErrorDetailsButton label="View diagnostics" error={diagnostics} />}>
    {issue.remediation && <p className="mt-0.5 text-sm text-muted-foreground">{issue.remediation}</p>}
  </AttentionRow>
}

/**
 * Single priority-ordered region for everything that needs the user's attention above the tabs:
 * check failures, load errors, ownership conflicts, namespace mismatch, deletion in progress and paused auto-sync.
 * Each row is a short summary with at most one primary action.
 */
export function AttentionBar({ application, canApprove, busy, hasPendingOperation, pendingAction, plansError, retryingPlans, onRetryPlans, conflictCount, onReviewConflicts, namespaceMismatch, kustomizeNamespace, onReviewNamespaceSetting, onCancelDecommission, onConfigureExclusion, resumeRevision, onResumeRevisionChange, onKeepPin, onResume }: {
  application: Application
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
  resumeRevision: string
  onResumeRevisionChange: (value: string) => void
  onKeepPin: () => void
  onResume: () => void
}) {
  const issues = application.statusIssues ?? []
  const paused = Boolean(application.autoSyncPaused && !application.branchTest)
  const mismatch = namespaceMismatch && !application.kustomizeNamespaceOverride
  if (!issues.length && plansError == null && !conflictCount && !mismatch && !application.decommissioning && !paused) return null
  const resumeBlocked = busy || hasPendingOperation
  return <section aria-label="Needs attention" className="mb-5 overflow-hidden rounded-xl border bg-card">
    <ul className="divide-y">
      {issues.map((issue, index) => <StatusIssueRow key={`${issue.source}-${index}`} application={application} issue={issue} canApprove={canApprove} onConfigureExclusion={onConfigureExclusion} />)}
      {plansError != null && <AttentionRow alert tone="destructive" icon={Alert02Icon} title="Plans could not be loaded." actions={<>
        <Button size="sm" loading={retryingPlans} onClick={onRetryPlans}>Retry</Button>
        <ErrorDetailsButton error={plansError} />
      </>} />}
      {conflictCount > 0 && <AttentionRow alert tone="warning" icon={AlertCircleIcon} title={`${conflictCount} existing resource${conflictCount === 1 ? "" : "s"} block${conflictCount === 1 ? "s" : ""} sync`} actions={<Button size="sm" variant="outline" onClick={onReviewConflicts}>Review conflicts</Button>}>
        <p className="mt-0.5 text-sm text-muted-foreground">Rendered resources already exist in Kubernetes but are not managed by this application.</p>
      </AttentionRow>}
      {mismatch && <AttentionRow alert tone="warning" icon={AlertCircleIcon} title={`Kustomize namespace ${kustomizeNamespace} differs from the JustCD target ${application.namespaces[0]?.namespace}`} actions={application.applicationGroupId
        ? <Button size="sm" variant="outline" render={<Link href={`/application-groups/${application.applicationGroupId}`} />}>Review group settings</Button>
        : <Button size="sm" variant="outline" onClick={onReviewNamespaceSetting}>Review namespace setting</Button>}>
        <p className="mt-0.5 text-sm text-muted-foreground">A plan cannot be refreshed until an owner enables the namespace transform.</p>
      </AttentionRow>}
      {application.decommissioning && <AttentionRow tone="warning" icon={AlertCircleIcon} title="Deletion in progress" actions={canApprove && <Button size="sm" variant="outline" disabled={busy || hasPendingOperation} onClick={onCancelDecommission}>Cancel deletion</Button>}>
        <p className="mt-0.5 text-sm text-muted-foreground">Auto-sync is paused. Review and apply the deletion plan, then finish removing the application.</p>
      </AttentionRow>}
      {paused && <AttentionRow tone="info" icon={PauseIcon} title="Auto-sync paused" actions={canApprove && <>
        <Button size="sm" variant="outline" loading={pendingAction === "rollback-state-resume"} loadingText="Resuming…" disabled={resumeBlocked || (Boolean(application.rollbackResumeRequiresRevision) && !resumeRevision.trim())} onClick={onResume}>{application.rollbackResumeAvailable ? "Resume previous source" : "Resume auto-sync"}</Button>
        {application.rollbackResumeAvailable && <ActionMenu label="More" size="sm" variant="ghost" items={[{ label: pendingAction === "rollback-state-keep" ? "Keeping pin…" : "Keep rollback pin", disabled: resumeBlocked, onSelect: onKeepPin }]} />}
      </>}>
        <Disclosure className="mt-1" summary="What happens when I resume?">
          <p className="max-w-2xl text-sm leading-6 text-muted-foreground">{application.rollbackResumeAvailable ? "JustCD is pinned to the approved target. An owner can keep this pin or explicitly resume the previous tracked source." : "JustCD will not automatically sync while paused. Manual changes are protected from automatic reconciliation. Resuming may overwrite them with the Git configuration."}</p>
        </Disclosure>
        {application.rollbackResumeRequiresRevision && <div className="mt-3 max-w-md"><FormField label="Git revision to use before resuming" htmlFor="rollback-resume-revision"><Input id="rollback-resume-revision" value={resumeRevision} onChange={(event) => onResumeRevisionChange(event.target.value)} placeholder="branch, tag, or commit" /></FormField></div>}
      </AttentionRow>}
    </ul>
  </section>
}
