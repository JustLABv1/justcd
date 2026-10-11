"use client"
import { useState } from "react"
import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import type { Identity, Plan } from "@/lib/types"

export function ApprovalEvidence({
  plan,
  onResource,
  prefix,
  workspaceId,
}: {
  plan: Plan
  onResource: (identity: Identity) => void
  workspaceId?: string
  prefix: string
}) {
  if (!plan.requiresApproval) return null
  const reasons = plan.approvalReasons?.length
    ? plan.approvalReasons
    : [
        {
          code: "legacy",
          rule: `saved.${plan.approvalKind || "owner"}`,
          description:
            "This older plan records its approval eligibility and count, but does not record rule provenance. Refresh the plan to see the governing policy and safeguards.",
          resources: plan.changes.map((c) => c.identity),
        },
      ]
  return (
    <section
      aria-label="Why approval is required"
      className="mb-4 rounded-lg border bg-muted/20 p-3"
    >
      <h4 className="text-sm font-semibold">Why approval is required</h4>
      <p className="mt-1 text-sm text-muted-foreground">
        Saved rule: {plan.requiredApprovals ?? 1} distinct approval(s) ·
        eligible roles: {plan.approverRoles?.join(", ") || "selected members"}
        {plan.approverUserIds?.length
          ? ` · ${plan.approverUserIds.length} selected member(s)`
          : ""}
        . These requirements apply to the exact plan digest.
      </p>
      {reasons.map((reason, i) => (
        <Disclosure
          key={`${reason.rule}-${i}`}
          className="mt-2 border-t pt-2"
          summary={`${reason.rule} · ${reason.resources.length} affected resources`}
        >
          <p id={`${prefix}-rule-${i}`} className="mb-2 text-sm">
            {reason.description}
          </p>
          <a
            href={`#${prefix}-rule-${i}`}
            className="text-sm text-primary underline"
          >
            Governing rule: {reason.rule}
          </a>
          {reason.resources.length ? (
            <AffectedResources
              key={`${plan.digest}-${reason.rule}`}
              resources={reason.resources}
              onResource={onResource}
            />
          ) : (
            <p className="mt-2 text-sm text-muted-foreground">
              This safeguard governs the rollback target even when no resources
              change.
            </p>
          )}
          {workspaceId && reason.rule.startsWith("workspace.") && (
            <Button
              variant="link"
              render={
                <Link href={`/workspaces/${encodeURIComponent(workspaceId)}`} />
              }
              nativeButton={false}
            >
              View workspace policy settings
            </Button>
          )}
          {reason.rule.startsWith("application.") && (
            <Button
              variant="link"
              render={
                <Link
                  href={`/applications/${encodeURIComponent(plan.applicationId)}/edit`}
                />
              }
              nativeButton={false}
            >
              View application policy settings
            </Button>
          )}
        </Disclosure>
      ))}
    </section>
  )
}

export function AffectedResources({
  resources,
  onResource,
}: {
  resources: Identity[]
  onResource?: (identity: Identity) => void
}) {
  const [page, setPage] = useState(0),
    pages = Math.ceil(resources.length / 20)
  if (!resources.length) return null
  return (
    <div className="mt-2">
      <p role="status" className="text-xs text-muted-foreground">
        Resources {page * 20 + 1}–{Math.min((page + 1) * 20, resources.length)}{" "}
        of {resources.length}
      </p>
      <ul className="max-h-48 overflow-auto">
        {resources.slice(page * 20, (page + 1) * 20).map((id, i) => (
          <li key={i}>
            {onResource ? (
              <Button
                type="button"
                variant="link"
                className="h-auto justify-start text-left whitespace-normal"
                onClick={() => onResource(id)}
              >
                Review {id.kind} {id.namespace ? `${id.namespace}/` : ""}
                {id.name}
              </Button>
            ) : (
              <span className="text-sm">
                {id.kind} {id.namespace ? `${id.namespace}/` : ""}
                {id.name}
              </span>
            )}
          </li>
        ))}
      </ul>
      {pages > 1 && (
        <div className="mt-2 flex gap-2">
          <Button
            size="sm"
            variant="outline"
            disabled={page === 0}
            onClick={() => setPage((n) => n - 1)}
          >
            Previous affected resources
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={page === pages - 1}
            onClick={() => setPage((n) => n + 1)}
          >
            Next affected resources
          </Button>
        </div>
      )}
    </div>
  )
}
