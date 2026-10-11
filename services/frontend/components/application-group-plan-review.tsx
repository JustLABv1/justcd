"use client"

import { useEffect, useMemo, useState } from "react"
import Link from "next/link"
import { api } from "@/lib/api"
import {
  makeEntry,
  resourceKey,
  riskLabels,
  type ReviewEntry,
} from "@/lib/plan-review"
import type { Application, ListResponse, PlanRecord } from "@/lib/types"
import { PlanReview } from "@/components/plan-review"
import { ApprovalEvidence } from "@/components/approval-evidence"
import { ManifestDiff } from "@/components/manifest-diff"
import { Button } from "@/components/ui/button"
import { Panel } from "@/components/ui-kit"

export function ApplicationGroupPlanReview({
  applications,
}: {
  applications: Application[]
}) {
  const [plans, setPlans] = useState<Record<string, PlanRecord>>({}),
    [failures, setFailures] = useState<string[]>([]),
    [loading, setLoading] = useState(true),
    [generation, setGeneration] = useState(0),
    [selected, setSelected] = useState("")
  useEffect(() => {
    let active = true
    async function load() {
      const result: Record<string, PlanRecord> = {},
        failed: string[] = []
      // Bound request fan-out for large groups; this is read-only and never creates plans.
      for (let i = 0; i < applications.length; i += 4)
        await Promise.all(
          applications.slice(i, i + 4).map(async (app) => {
            try {
              const response = await api<ListResponse<PlanRecord>>(
                `/api/v1/applications/${encodeURIComponent(app.id)}/plans`
              )
              const plan = response.items.find((p) => p.status === "current")
              if (plan) result[app.id] = plan
            } catch {
              failed.push(app.name)
            }
          })
        )
      if (active) {
        setPlans(result)
        setFailures(failed)
        setLoading(false)
      }
    }
    void load()
    return () => {
      active = false
    }
  }, [applications, generation])
  const entries = useMemo(
    () =>
      applications.flatMap((app) => {
        const record = plans[app.id]
        return record
          ? [
              ...record.plan.changes.map((c) =>
                makeEntry(
                  c,
                  false,
                  `${app.id}:${resourceKey(c)}`,
                  app.name,
                  app.id,
                  record.plan
                )
              ),
              ...(record.plan.ignored ?? []).map((c) =>
                makeEntry(
                  c,
                  true,
                  `${app.id}:ignored:${resourceKey(c)}`,
                  app.name,
                  app.id,
                  record.plan
                )
              ),
            ]
          : []
      }),
    [applications, plans]
  )
  const withoutPlan = applications.filter(
    (app) => !plans[app.id] && !failures.includes(app.name)
  )
  return (
    <Panel
      title="Review target plans"
      description="Compare each child’s current immutable plan. Every target retains its own approval and apply action."
      action={
        <Button
          size="sm"
          variant="outline"
          disabled={loading}
          onClick={() => {
            setLoading(true)
            setGeneration((n) => n + 1)
          }}
        >
          Refresh review
        </Button>
      }
    >
      <div className="p-4 sm:p-5">
        {loading ? (
          <p role="status">Loading target plans…</p>
        ) : (
          <>
            {failures.length > 0 && (
              <p role="alert" className="mb-3 text-sm text-destructive">
                Plans could not be read for: {failures.join(", ")}. Retry with
                Refresh review.
              </p>
            )}
            {withoutPlan.length > 0 && (
              <p className="mb-3 text-sm text-muted-foreground">
                No current plan for: {withoutPlan.map((a) => a.name).join(", ")}
                . Open the target to build or refresh its plan.
              </p>
            )}
            <PlanReview
              changes={[]}
              ignored={[]}
              entries={entries}
              selected={selected}
              onSelect={setSelected}
              identityKey={resourceKey}
              isExcluded={() => false}
              renderChange={(change, ignored, entry: ReviewEntry, review) => (
                <article className="overflow-hidden rounded-lg border">
                  <div className="border-b p-3">
                    <h4 className="font-semibold">
                      {entry.child} · {change.identity.kind}{" "}
                      {change.identity.namespace}/{change.identity.name}
                    </h4>
                    <p className="mt-1 text-sm">
                      {ignored ? "Excluded · not applied" : change.kind} ·{" "}
                      {entry.risks.map((r) => riskLabels[r]).join(", ")}
                    </p>
                    <p className="mt-1 font-mono text-xs break-all text-muted-foreground">
                      Plan digest {entry.plan?.digest}
                    </p>
                    <Button
                      size="sm"
                      variant="link"
                      render={
                        <Link
                          href={`/applications/${encodeURIComponent(entry.childId ?? "")}?tab=changes`}
                        />
                      }
                      nativeButton={false}
                    >
                      Open child plan to approve or apply
                    </Button>
                  </div>
                  {entry.plan && (
                    <div className="p-3">
                      <ApprovalEvidence
                        plan={entry.plan}
                        workspaceId={
                          applications.find((app) => app.id === entry.childId)
                            ?.workspaceId
                        }
                        prefix={`child-${entry.childId}`}
                        onResource={review}
                      />
                    </div>
                  )}
                  {entry.immutablePaths.length > 0 && (
                    <p role="alert" className="bg-warning/10 p-3 text-sm">
                      Immutable-field update: {entry.immutablePaths.join(", ")}.
                      Kubernetes may reject this update; deletion/recreation
                      requires separate review.
                    </p>
                  )}
                  {(change.ignoredPaths?.length ?? 0) > 0 && (
                    <p className="bg-muted/20 p-3 text-sm">
                      Ignored fields · not applied:{" "}
                      {change.ignoredPaths?.join(", ")}
                    </p>
                  )}
                  <ManifestDiff
                    key={entry.key}
                    before={change.before}
                    after={change.after}
                    excluded={ignored}
                  />
                </article>
              )}
            />
          </>
        )}
      </div>
    </Panel>
  )
}
