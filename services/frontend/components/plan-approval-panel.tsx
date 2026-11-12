"use client"

import { useEffect, useId, useState, type ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Tick02Icon } from "@hugeicons/core-free-icons"
import { Avatar, AvatarBadge } from "@/components/panelui/avatar"
import { Badge } from "@/components/panelui/badge"
import { ApprovalEvidence } from "@/components/approval-evidence"
import { errorMessage } from "@/lib/api"
import type {
  Identity,
  PlanApproval,
  PlanApprovalSummary,
  PlanRecord,
} from "@/lib/types"

export function ApproverAvatar({
  name,
  approved = false,
}: {
  name: string
  approved?: boolean
}) {
  return (
    <Avatar name={name} aria-hidden="true" color={approved ? "chart-2" : "muted"}>
      {approved && <AvatarBadge label="Approved" className="bg-success text-success-foreground"><HugeiconsIcon icon={Tick02Icon} strokeWidth={2} /></AvatarBadge>}
    </Avatar>
  )
}

export function RecordedApprovals({
  approvals,
}: {
  approvals: PlanApproval[]
}) {
  return (
    <ul aria-label="Recorded approvals" className="space-y-3">
      {approvals.map((approval) => (
        <li key={approval.id} className="flex items-start gap-3">
          <ApproverAvatar
            name={approval.displayName || approval.email || "Former member"}
            approved={approval.eligible}
          />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <span className="text-sm font-medium break-words">
                {approval.displayName || approval.email || "Former member"}
              </span>
              <span
                className={`text-xs ${approval.eligible ? "text-success-foreground dark:text-success" : "text-muted-foreground"}`}
              >
                {approval.eligible ? "Approved" : "Not counted"}
              </span>
            </div>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {approval.role || "Former member"}
              {approval.createdAt && ` · ${new Date(approval.createdAt).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })}`}
              {!approval.eligible && " · Expired or no longer eligible"}
            </p>
            {approval.comment && (
              <p className="mt-1 text-sm break-words text-muted-foreground">
                “{approval.comment}”
              </p>
            )}
          </div>
        </li>
      ))}
    </ul>
  )
}

export function PlanApprovalPanel({
  record,
  summary,
  required,
  approved,
  roles,
  members,
  workspaceId,
  onResource,
  actions,
  error,
  readOnly = false,
}: {
  record: PlanRecord
  summary: PlanApprovalSummary | null
  required: number
  approved: number
  roles: string[]
  members: { id: string; label: string }[]
  workspaceId?: string
  onResource: (identity: Identity) => void
  actions?: ReactNode
  error?: unknown
  readOnly?: boolean
}) {
  const prefix = useId()
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 5000)
    return () => window.clearInterval(timer)
  }, [])
  const plan = record.plan
  const complete = Boolean(summary && approved >= required)
  const current = record.status === "current" && !readOnly
  const expired = Date.parse(record.expiresAt) <= now
  const deletions = plan.changes.filter(
    (change) => change.kind === "delete"
  ).length
  const clusterScoped = plan.changes.filter(
    (change) => change.identity.clusterScoped
  ).length
  const takeovers = plan.changes.filter((change) => change.takeover).length
  const risks = [
    deletions && `${deletions} resource deletion${deletions === 1 ? "" : "s"}`,
    clusterScoped &&
      `${clusterScoped} cluster-scoped change${clusterScoped === 1 ? "" : "s"}`,
    takeovers &&
      `${takeovers} field ownership takeover${takeovers === 1 ? "" : "s"}`,
  ].filter(Boolean)
  const applied = record.status === "applied"
  const state = applied
    ? "Approved and applied"
    : !current
    ? "Historical plan"
    : error
      ? "Status unavailable"
      : !summary
        ? "Checking status…"
        : expired
          ? "Plan expired"
          : complete
            ? "Approved"
            : "Waiting for approval"
  const guidance = current && summary
    ? expired
      ? "This plan has expired. Create a fresh plan before approving or applying."
      : summary.currentUserApproved
        ? "Your approval is recorded for this plan."
        : complete
          ? "All required approvals have been recorded."
          : summary.canApprove
            ? "You can approve this plan after reviewing its changes."
            : "You are not eligible to approve under this plan’s rule."
    : null
  const canAct = current && !complete && !expired && summary?.canApprove && !summary.currentUserApproved && actions
  return (
    <section
      id="plan-approval-review"
      tabIndex={-1}
      aria-label="Approval review"
      className="scroll-mt-5 space-y-7 outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 space-y-2">
          {current || (applied && summary) ? <>
            <p className="flex items-baseline gap-2">
              <span role="status" className="text-3xl font-semibold tabular-nums tracking-tight">{approved} <span className="text-muted-foreground">of {required}</span></span>
              <span className="text-sm text-muted-foreground">approved</span>
            </p>
            <div aria-hidden="true" className="flex gap-1.5">
              {Array.from({ length: Math.min(required, 8) }, (_, index) => <span key={index} className={`h-1.5 w-8 rounded-full ${index < approved ? "bg-success" : "bg-muted"}`} />)}
            </div>
          </> : <p role="status" className="text-3xl font-semibold tracking-tight">{applied ? "Approved" : "Not applied"}</p>}
          <p className="text-sm text-muted-foreground">{state}{guidance ? ` · ${guidance}` : ""}</p>
        </div>
        {canAct ? <div className="shrink-0">{actions}</div> : current && summary?.currentUserApproved ? (
          <span className="inline-flex shrink-0 items-center gap-1.5 text-sm text-success-foreground dark:text-success">
            <HugeiconsIcon icon={Tick02Icon} strokeWidth={2} className="size-4" aria-hidden="true" />
            Your approval is recorded
          </span>
        ) : null}
      </div>
      {Boolean(error) && (
        <p role="alert" className="text-sm text-destructive">
          Could not load approvals: {errorMessage(error)}. Retrying automatically.
        </p>
      )}
      <div className="space-y-4">
        <div className="space-y-1.5">
          <h4 className="text-sm font-semibold">Why approval is required</h4>
          <p className="text-sm text-muted-foreground">
            {plan.rollback
              ? "Rollbacks require owner approval, including any additional approvals required for deletions."
              : risks.length
                ? `This plan includes ${risks.join(" and ")}.`
                : "This plan requires approval under its saved deployment policy."}{" "}
            {required} distinct eligible approval{required === 1 ? "" : "s"} required for this exact plan.
          </p>
        </div>
        <ApprovalEvidence plan={plan} workspaceId={workspaceId} prefix={prefix} onResource={onResource} embedded />
      </div>
      {(current || applied) && <div className="space-y-3">
        <h4 className="text-sm font-semibold">Approvals</h4>
        {error ? (
          <p role="alert" className="text-sm text-destructive">Status will be checked again automatically.</p>
        ) : !summary ? (
          <p role="status" className="text-sm text-muted-foreground">Checking recorded approvals…</p>
        ) : summary.approvals.length === 0 ? (
          <div className="flex items-center gap-3">
            <Avatar aria-hidden="true" fallback="—" color="muted" className="border border-dashed" />
            <div>
              <p className="text-sm">{current ? "No approvals yet" : "No active approvals"}</p>
              <p className="text-xs text-muted-foreground">
                {current ? `${required} approval${required === 1 ? "" : "s"} still needed.` : "Approvals are valid only for a current plan."}
              </p>
            </div>
          </div>
        ) : (
          <RecordedApprovals approvals={summary.approvals} />
        )}
      </div>}
      <div className="space-y-3">
        <h4 className="text-sm font-semibold">Who can approve</h4>
        <div className="flex flex-wrap items-center gap-2">
          {roles.map((role) => (
            <Badge key={role} variant="outline" className="capitalize">{role}</Badge>
          ))}
          {members.map((member) => (
            <span key={member.id} className="inline-flex items-center gap-2 text-sm">
              <Avatar size="sm" name={member.label} aria-hidden="true" />
              {member.label}
            </span>
          ))}
        </div>
        <p className="text-xs text-muted-foreground">
          {roles.length
            ? `Members with these workspace roles or higher qualify.${members.length ? " Selected members also qualify." : ""}`
            : "Only the selected workspace members qualify."}
        </p>
      </div>
    </section>
  )
}
