"use client"

import { useEffect, useState } from "react"
import { api, errorMessage } from "@/lib/api"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Button } from "@/components/ui/button"
import { RecordedApprovals } from "@/components/plan-approval-panel"
import type { PlanApprovalSummary } from "@/lib/types"

/** Fetch identities only when a request is expanded, and keep them current. */
export function ApprovalRequestPeople({
  planId,
  approved,
  required,
}: {
  planId: string
  approved: number
  required: number
}) {
  const [open, setOpen] = useState(false)
  const [summary, setSummary] = useState<PlanApprovalSummary | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [revision, setRevision] = useState(0)
  useEffect(() => {
    if (!open) return
    let active = true
    const load = () =>
      api<PlanApprovalSummary>(
        `/api/v1/plans/${encodeURIComponent(planId)}/approvals`
      )
        .then((value) => {
          if (active) {
            setSummary(value)
            setError(null)
          }
        })
        .catch((cause) => {
          if (active) setError(cause)
        })
    void load()
    const timer = window.setInterval(() => void load(), 5000)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [planId, open, revision])
  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className="mt-4 border-t pt-3"
    >
      <CollapsibleTrigger>
        Approvers · {summary?.approvedApprovals ?? approved} of{" "}
        {summary?.requiredApprovals ?? required} approved
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="pt-3">
          {error ? (
            <div
              role="alert"
              className="flex flex-wrap items-center gap-2 text-sm text-destructive"
            >
              {errorMessage(error)}
              <Button
                size="xs"
                variant="outline"
                onClick={() => setRevision((value) => value + 1)}
              >
                Retry
              </Button>
            </div>
          ) : !summary ? (
            <p role="status" className="text-sm text-muted-foreground">
              Loading approvers…
            </p>
          ) : (
            <>
              {summary.approvals.length ? (
                <RecordedApprovals approvals={summary.approvals} />
              ) : (
                <p className="text-sm text-muted-foreground">
                  No approvals recorded yet.
                </p>
              )}
              <p className="mt-3 text-xs text-muted-foreground">
                {Math.max(
                  0,
                  summary.requiredApprovals - summary.approvedApprovals
                )}{" "}
                more eligible approval
                {summary.requiredApprovals - summary.approvedApprovals === 1
                  ? ""
                  : "s"}{" "}
                needed.{" "}
                {summary.currentUserApproved
                  ? "Your approval is recorded."
                  : summary.canApprove
                    ? "You can review and approve this plan."
                    : "Open the plan to review its eligibility requirements."}
              </p>
            </>
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
