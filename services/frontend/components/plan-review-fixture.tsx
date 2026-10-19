"use client"
import { useMemo, useState } from "react"
import { ChangesTab } from "@/components/application/changes-tab"
import type { Change, PlanRecord, Identity } from "@/lib/types"
import { diffId, sameIdentity } from "@/components/application/helpers"
import { PlanReview } from "@/components/plan-review"
import { ManifestDiff } from "@/components/manifest-diff"
import { makeEntry, resourceKey } from "@/lib/plan-review"
import { Button } from "@/components/ui/button"

const iniBefore = [
  "[analytics]",
  "check_for_updates = true",
  "",
  "[auth.generic_oauth]",
  "enabled = true",
  "name = Example SSO",
  "auth_url = https://login.example.invalid/realms/demo/protocol/openid-connect/auth",
  "token_url = https://login.example.invalid/realms/demo/protocol/openid-connect/token",
  "",
  "[paths]",
  "data = /var/lib/grafana",
  "logs = /var/log/grafana",
  "plugins = /var/lib/grafana/plugins",
  "",
  "[server]",
  "domain = grafana.example.invalid",
  "",
  "[smtp]",
  "enabled = true",
  "host = smtp.old.example.invalid:2525",
  "from_name = Grafana Alerts",
  "",
  "[unified_storage]",
  "index_path = /var/lib/grafana-search/bleve",
  "",
].join("\n")
const embeddedBefore = {
  apiVersion: "v1",
  kind: "ConfigMap",
  metadata: { name: "grafana-config", labels: { team: "platform" } },
  data: {
    "grafana.ini": iniBefore,
    "removed.conf": "",
    "unchanged.json": "{}",
  },
}
const embeddedAfter = {
  ...embeddedBefore,
  metadata: { ...embeddedBefore.metadata, labels: { team: "observability" } },
  data: {
    "grafana.ini": iniBefore.replace(
      "smtp.old.example.invalid:2525",
      "smtp.new.example.invalid:587"
    ),
    "added.yaml": "enabled: true\n",
    "unchanged.json": "{}",
  },
}

export function PlanReviewFixture() {
  const [policyMode, setPolicyMode] = useState<"warn" | "block">("warn")
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
    changes[2] = {
      ...changes[2],
      identity: { ...changes[2].identity, kind: "Secret" },
      before: { kind: "Secret", data: "[redacted]" },
      after: { kind: "Secret", data: "[redacted]" },
      changedPaths: ["/data/admin-password"],
    }
    changes[4] = {
      ...changes[4],
      identity: {
        ...changes[4].identity,
        apiVersion: "apps/v1",
        kind: "Deployment",
      },
      before: {
        spec: {
          template: {
            metadata: { annotations: { "checksum/secret": "[redacted]" } },
          },
        },
      },
      after: {
        spec: {
          template: {
            metadata: { annotations: { "checksum/secret": "[redacted]" } },
          },
        },
      },
      changedPaths: ["/spec/template/metadata/annotations/checksum~1secret"],
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
      digest: `synthetic-immutable-fixture-${policyMode}`,
      policyEvaluation: {
        configDigest: "synthetic-policy-config",
        mode: policyMode,
        results: [
          {
            resource: changes[101].identity,
            rule: "mutable-image-tags",
            evidence: "containers[0].image is not pinned to a SHA-256 digest",
            remediation:
              "Pin the image using image@sha256:<64 hexadecimal characters>.",
          },
        ],
      },
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
  }, [policyMode])
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
      <section
        aria-label="Embedded configuration fixture"
        className="mb-6 overflow-hidden rounded-xl border"
      >
        <h2 className="border-b px-4 py-3 font-semibold">
          Embedded ConfigMap files
        </h2>
        <ManifestDiff before={embeddedBefore} after={embeddedAfter} />
      </section>
      <Button
        className="mb-4"
        variant="outline"
        onClick={() => setGroup((g) => !g)}
      >
        {group ? "Review single application" : "Review five group children"}
      </Button>
      <Button
        className="mb-4 ml-2"
        variant="outline"
        onClick={() => {
          setPolicyMode((mode) => (mode === "warn" ? "block" : "warn"))
          setApproved(false)
        }}
      >
        {policyMode === "warn" ? "Test blocking policy" : "Test warning policy"}
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
                changedPaths={c.changedPaths}
                takeover={c.takeover}
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
