"use client"
import { useState } from "react"
import Link from "next/link"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon, Settings02Icon, Shield01Icon } from "@hugeicons/core-free-icons"
import { Disclosure } from "@/components/ui/collapsible"
import type { Identity, Plan } from "@/lib/types"

export function ApprovalEvidence({
  plan,
  onResource,
  prefix,
  workspaceId,
  embedded = false,
  inline = false,
}: {
  plan: Plan
  onResource: (identity: Identity) => void
  workspaceId?: string
  prefix: string
  embedded?: boolean
  inline?: boolean
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
  if (embedded)
    return (
      <ul aria-label="Why approval is required" className="divide-y">
        {reasons.map((reason, i) => <ReasonRow key={`${reason.rule}-${i}`} reason={reason} plan={plan} workspaceId={workspaceId} onResource={onResource} id={`${prefix}-rule-${i}`} />)}
      </ul>
    )
  return (
    <section
      aria-label="Why approval is required"
      className={embedded || inline ? "" : "mb-4 rounded-2xl bg-muted/30 p-4"}
    >
      {!embedded && (
        <h4 className="text-sm font-semibold">Why approval is required</h4>
      )}
      {!embedded && (
        <p className="mt-1 text-sm text-muted-foreground">
          Saved rule: {plan.requiredApprovals ?? 1} distinct approval(s) ·
          eligible roles: {plan.approverRoles?.join(", ") || "selected members"}
          {plan.approverUserIds?.length
            ? ` · ${plan.approverUserIds.length} selected member(s)`
            : ""}
          . These requirements apply to the exact plan digest.
        </p>
      )}
      {reasons.map((reason, i) => (
        <Disclosure
          key={`${reason.rule}-${i}`}
          className={embedded ? "py-1 [&+&]:border-t" : "mt-2 border-t pt-2"}
          summary={`${embedded ? "Policy details: " : ""}${reason.rule} · ${reason.resources.length} affected resource${reason.resources.length === 1 ? "" : "s"}`}
        >
          <p id={`${prefix}-rule-${i}`} className="mb-2 text-sm">
            {reason.description}
          </p>
          <p className="text-xs text-muted-foreground">
            Governing rule: <code>{reason.rule}</code>
          </p>
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
              variant="outline"
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
              variant="outline"
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
                variant="outline"
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

type Reason = NonNullable<Plan["approvalReasons"]>[number]

const SHOWN = 3

/** One reason approval is needed: what the rule says, what it touches, and where it is configured. */
function ReasonRow({ reason, plan, workspaceId, onResource, id }: { reason: Reason; plan: Plan; workspaceId?: string; onResource: (identity: Identity) => void; id: string }) {
  const [all, setAll] = useState(false)
  const resources = all ? reason.resources : reason.resources.slice(0, SHOWN)
  const settings = reason.rule.startsWith("application.")
    ? { href: `/applications/${encodeURIComponent(plan.applicationId)}/edit`, label: "Application policy" }
    : workspaceId && reason.rule.startsWith("workspace.")
      ? { href: `/workspaces/${encodeURIComponent(workspaceId)}`, label: "Workspace policy" }
      : null
  return (
    <li className="flex gap-3 py-4 first:pt-0 last:pb-0">
      <span aria-hidden="true" className="grid size-9 shrink-0 place-items-center rounded-xl bg-warning/15 text-warning-foreground dark:text-warning"><HugeiconsIcon icon={Shield01Icon} strokeWidth={1.8} className="size-4" /></span>
      <div className="min-w-0 flex-1 space-y-2.5">
        <p id={id} className="text-sm font-medium leading-5">{reason.description}</p>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
          <Badge variant="outline" size="sm" className="font-mono">{reason.rule}</Badge>
          {settings && <Button size="sm" variant="ghost" className="-ms-1 h-6 px-1.5 text-xs text-muted-foreground" render={<Link href={settings.href} />} nativeButton={false}><HugeiconsIcon icon={Settings02Icon} strokeWidth={1.8} data-icon="inline-start" aria-hidden="true" />{settings.label}</Button>}
        </div>
        {reason.resources.length > 0 ? (
          <div className="space-y-1.5">
            <p className="text-xs text-muted-foreground">Affects {reason.resources.length} resource{reason.resources.length === 1 ? "" : "s"}</p>
            <ul className="space-y-1">
              {resources.map((identity, index) => <li key={index}>
                <button type="button" onClick={() => onResource(identity)} className="group flex w-full min-w-0 items-center gap-2.5 rounded-xl border bg-muted/30 px-3 py-2 text-left text-sm outline-none transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring">
                  <Badge variant="secondary" size="sm">{identity.kind}</Badge>
                  <span className="min-w-0 flex-1 truncate">{identity.namespace ? `${identity.namespace}/` : ""}{identity.name}</span>
                  <span className="shrink-0 text-xs text-muted-foreground group-hover:text-foreground">Review diff</span>
                  <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground group-hover:text-foreground" />
                </button>
              </li>)}
            </ul>
            {reason.resources.length > SHOWN && <Button size="sm" variant="ghost" className="-ms-1 h-7 px-2 text-xs" onClick={() => setAll((value) => !value)}>{all ? "Show fewer" : `Show all ${reason.resources.length}`}</Button>}
          </div>
        ) : <p className="text-xs text-muted-foreground">This safeguard governs the rollback target even when no resources change.</p>}
      </div>
    </li>
  )
}
