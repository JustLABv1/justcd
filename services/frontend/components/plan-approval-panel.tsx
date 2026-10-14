"use client"

import { useEffect, useId, useState, type ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Tick02Icon } from "@hugeicons/core-free-icons"
import { Avatar, AvatarBadge, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/reui/badge"
import { ApprovalEvidence } from "@/components/approval-evidence"
import { errorMessage } from "@/lib/api"
import type {
  Identity,
  PlanApproval,
  PlanApprovalSummary,
  PlanRecord,
} from "@/lib/types"

function initials(name: string) {
  const parts = name
    .trim()
    .split(/[\s.@_-]+/)
    .filter(Boolean)
  return (
    (parts.length > 1
      ? parts[0][0] + parts[1][0]
      : name.slice(0, 2)
    ).toUpperCase() || "?"
  )
}

export function ApproverAvatar({
  name,
  approved = false,
}: {
  name: string
  approved?: boolean
}) {
  return (
    <Avatar aria-hidden="true">
      <AvatarFallback
        className={
          approved
            ? "bg-success/10 text-success-foreground dark:text-success"
            : ""
        }
      >
        {initials(name)}
      </AvatarFallback>
      {approved && (
        <AvatarBadge className="bg-success text-white">
          <HugeiconsIcon icon={Tick02Icon} strokeWidth={2} />
        </AvatarBadge>
      )}
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
              <Badge
                size="xs"
                variant={approval.eligible ? "success-light" : "secondary"}
              >
                {approval.eligible ? "Approved" : "Not counted"}
              </Badge>
            </div>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {approval.role || "Former member"}
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
  blockedReason,
  error,
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
  blockedReason?: string | null
  error?: unknown
}) {
  const prefix = useId()
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 5000)
    return () => window.clearInterval(timer)
  }, [])
  const plan = record.plan
  const complete = Boolean(summary && approved >= required)
  const current = record.status === "current"
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
  return (
    <section
      id="plan-approval-review"
      tabIndex={-1}
      aria-label="Approval review"
      className="mb-5 scroll-mt-5 overflow-hidden rounded-xl border bg-muted/15 outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold">
              {current ? "Approval review" : "Saved approval policy"}
            </h3>
            <Badge
              radius="full"
              variant={
                !current
                  ? "secondary"
                  : complete
                    ? "success-light"
                    : "warning-light"
              }
            >
              {!current
                ? "Historical plan"
                : error
                  ? "Status unavailable"
                  : !summary
                    ? "Checking status…"
                    : expired
                      ? "Plan expired"
                      : complete
                        ? "Approvals complete"
                        : `${approved} of ${required} approved`}
            </Badge>
          </div>
          <p className="mt-2 text-sm">
            {plan.rollback
              ? "Rollbacks require owner approval, including any additional approvals required for deletions."
              : risks.length
                ? `Approval is required for ${risks.join(" and ")}.`
                : "This plan requires approval under its saved deployment policy."}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {required} distinct eligible approval{required === 1 ? "" : "s"}{" "}
            required for this exact plan.
          </p>
        </div>
        {current && actions && <div className="shrink-0">{actions}</div>}
      </div>
      <div className="grid gap-5 border-t p-4 md:grid-cols-2">
        <div>
          <h4 className="mb-3 text-xs font-medium text-muted-foreground">
            {current ? "Who has approved" : "Active approval records"}
          </h4>
          {error ? (
            <p role="alert" className="text-sm text-destructive">
              Could not load approvals: {errorMessage(error)}. Status will be
              checked again automatically.
            </p>
          ) : !summary ? (
            <p role="status" className="text-sm text-muted-foreground">
              Checking recorded approvals…
            </p>
          ) : summary.approvals.length === 0 ? (
            <div className="flex items-center gap-3">
              <Avatar aria-hidden="true" className="border border-dashed">
                <AvatarFallback>—</AvatarFallback>
              </Avatar>
              <div>
                <p className="text-sm">
                  {current ? "No approvals yet" : "No active approvals"}
                </p>
                <p className="text-xs text-muted-foreground">
                  {current
                    ? `${required} approval${required === 1 ? "" : "s"} still needed.`
                    : "Approvals are valid only for a current plan."}
                </p>
              </div>
            </div>
          ) : (
            <RecordedApprovals approvals={summary.approvals} />
          )}
        </div>
        <div>
          <h4 className="mb-3 text-xs font-medium text-muted-foreground">
            Who can approve
          </h4>
          <div className="flex flex-wrap gap-2">
            {roles.map((role) => (
              <Badge key={role} variant="outline" className="capitalize">
                {role}
              </Badge>
            ))}
            {members.map((member) => (
              <span
                key={member.id}
                className="inline-flex items-center gap-2 rounded-full border bg-card py-1 pr-3 pl-1 text-xs"
              >
                <Avatar size="sm" aria-hidden="true">
                  <AvatarFallback>{initials(member.label)}</AvatarFallback>
                </Avatar>
                {member.label}
              </span>
            ))}
          </div>
          <p className="mt-2 text-xs text-muted-foreground">
            {roles.length
              ? `Members with these workspace roles or higher qualify.${members.length ? " Selected members also qualify." : ""}`
              : "Only the selected workspace members qualify."}
          </p>
          {current && summary && (
            <p className="mt-3 text-sm">
              {expired
                ? "This plan has expired. Create a fresh plan before approving or applying."
                : summary.currentUserApproved
                  ? "Your approval is recorded for this plan."
                  : complete
                    ? "All required approvals have been recorded."
                    : summary.canApprove
                      ? "You can approve this plan after reviewing its changes."
                      : "You are not eligible to approve under this plan’s rule."}
            </p>
          )}
          {current && blockedReason && (
            <p role="status" className="mt-2 text-xs text-muted-foreground">
              {blockedReason}
            </p>
          )}
        </div>
      </div>
      <div className="border-t px-4 py-3">
        <ApprovalEvidence
          plan={plan}
          workspaceId={workspaceId}
          prefix={prefix}
          onResource={onResource}
          embedded
        />
      </div>
    </section>
  )
}
