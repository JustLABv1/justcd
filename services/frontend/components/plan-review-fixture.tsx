"use client"
import { useMemo, useState } from "react"
import { ChangesTab } from "@/components/application/changes-tab"
import type { Change, PlanRecord, Identity } from "@/lib/types"
import { diffId, sameIdentity } from "@/components/application/helpers"
import { PlanReview } from "@/components/plan-review"
import { ManifestDiff } from "@/components/manifest-diff"
import { makeEntry, resourceKey } from "@/lib/plan-review"
import { Button } from "@/components/ui/button"

export function PlanReviewFixture() {
  const fixture = useMemo(() => {
    const data = Object.fromEntries(
      Array.from({ length: 10000 }, (_, i) => [
        `field-${String(i).padStart(5, "0")}`,
        `value-${i}`,
      ])
    )
    const changes: Change[] = Array.from({ length: 10000 }, (_, i) => ({
      kind: i % 10 === 0 ? "delete" : i % 3 === 0 ? "create" : "update",
      identity: {
        clusterId: `cluster-${i % 5}`,
        apiVersion: "v1",
        kind: i % 97 === 0 ? "Namespace" : "ConfigMap",
        namespace: i % 97 === 0 ? "" : `team-${i % 20}`,
        name: `resource-${String(i).padStart(5, "0")}`,
        clusterScoped: i % 97 === 0,
      },
      takeover: i % 43 === 0,
      before: { data: { version: "old" } },
      after: { data: { version: "new" } },
      ignoredPaths: i % 31 === 0 ? ["/data/external"] : undefined,
    }))
    for (let i = 101; i < changes.length; i += 101) {
      if (changes[i].kind !== "update") continue
      changes[i] = {
        ...changes[i],
        identity: {
          ...changes[i].identity,
          apiVersion: "apps/v1",
          kind: "Deployment",
        },
        before: { spec: { selector: { matchLabels: { app: "old" } } } },
        after: { spec: { selector: { matchLabels: { app: "new" } } } },
        changedPaths: ["/spec/selector/matchLabels/app"],
      }
    }
    changes[1] = {
      ...changes[1],
      changedPaths: [
        "/data/field-00020",
        "/data/field-05000",
        "/data/field-09990",
      ],
      before: { data },
      after: {
        data: {
          ...data,
          "field-00020": "changed",
          "field-05000": "changed",
          "field-09990": "changed",
        },
      },
    }
    const ignored = changes
      .slice(0, 1000)
      .map((c) => ({ ...c, ignoreReason: "Synthetic exclusion" }))
    const plan = {
      applicationId: "synthetic-fixture",
      revision: "fixture",
      changes,
      ignored,
      bindings: [],
      requiresApproval: true,
      approvalKind: "takeover",
      requiredApprovals: 1,
      approverRoles: ["owner"],
      digest: "synthetic-immutable-fixture",
      approvalReasons: [
        {
          code: "takeover",
          rule: "safeguard.takeover",
          description:
            "Synthetic fixture: ownership transfers restrict approvers to owners.",
          resources: changes.filter((c) => c.takeover).map((c) => c.identity),
        },
      ],
    }
    return {
      id: "fixture",
      plan,
      createdBy: "fixture",
      createdAt: "2026-01-01T00:00:00Z",
      expiresAt: "2099-01-01T00:00:00Z",
      status: "current",
    } satisfies PlanRecord
  }, [])
  const [selected, setSelected] = useState(
      diffId(fixture.plan.changes[1].identity)
    ),
    [selection, setSelection] = useState<Identity[]>([]),
    [comment, setComment] = useState(""),
    [approved, setApproved] = useState(false),
    [group, setGroup] = useState(false)
  const entries = useMemo(
    () =>
      fixture.plan.changes.map((change, i) =>
        makeEntry(
          change,
          false,
          `child-${i % 5}:${resourceKey(change)}`,
          `Child ${i % 5}`,
          `child-${i % 5}`,
          fixture.plan
        )
      ),
    [fixture]
  )
  return (
    <main className="mx-auto max-w-7xl p-5">
      <h1 className="mb-3 text-2xl font-semibold">
        Synthetic large plan review fixture
      </h1>
      <p className="mb-3 text-sm">
        10,000 changes, 1,000 ignored resources; one manifest contains 10,000
        data fields. All actions stay in browser memory.
      </p>
      <Button
        className="mb-4"
        variant="outline"
        onClick={() => setGroup((g) => !g)}
      >
        {group ? "Review single application" : "Review five group children"}
      </Button>
      {group ? (
        <PlanReview
          entries={entries}
          changes={[]}
          ignored={[]}
          selected={selected}
          onSelect={setSelected}
          identityKey={resourceKey}
          isExcluded={() => false}
          renderChange={(c, ignored, entry) => (
            <article>
              <h2>
                {entry.child} · {c.identity.name}
              </h2>
              <ManifestDiff
                key={entry.key}
                before={c.before}
                after={c.after}
                excluded={ignored}
              />
            </article>
          )}
        />
      ) : (
        <ChangesTab
          plans={[fixture]}
          activePlan={fixture}
          onSelectPlan={() => {}}
          plansLoading={false}
          plansError={null}
          retryingPlans={false}
          onRetryPlans={() => {}}
          decommissioning={false}
          canDeploy
          canApprove
          busy={false}
          hasPendingOperation={false}
          pendingAction=""
          hasOwnershipConflict={false}
          reviewResource={selected}
          onReviewResourceChange={setSelected}
          selectionResources={selection}
          selectionFields={[]}
          selectionDirty={selection.length > 0}
          onToggleResource={(id) =>
            setSelection((s) =>
              s.some((x) => sameIdentity(x, id))
                ? s.filter((x) => !sameIdentity(x, id))
                : [...s, id]
            )
          }
          onToggleField={() => {}}
          onResetSelection={() => setSelection([])}
          onSaveSelection={() => {}}
          ignoreRules={[]}
          ignoreReasons={{}}
          onIgnoreReasonChange={() => {}}
          onSaveIgnore={() => {}}
          onRemoveRule={async () => {}}
          requiredApprovals={1}
          approvedApprovals={approved ? 1 : 0}
          approvalsComplete={approved}
          approverRoles={["owner"]}
          approverMembers={[]}
          approvalSummary={{
            planId: fixture.id,
            planDigest: fixture.plan.digest,
            approvalKind: "takeover",
            requiredApprovals: 1,
            approvedApprovals: approved ? 1 : 0,
            approverRoles: ["owner"],
            approverUserIds: [],
            currentUserApproved: approved,
            canApprove: !approved,
            approvals: [],
          }}
          approvalComment={comment}
          onApprovalCommentChange={setComment}
          onApprove={async () => setApproved(true)}
          onApply={() => {}}
          onCreatePlan={() => {}}
        />
      )}
    </main>
  )
}
