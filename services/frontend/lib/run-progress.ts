import type { BackgroundTask, Identity, Operation, PlanRecord } from "./types"

/** A generated plan stops needing a review notice once its sync is queued. */
export function planTaskHasOperation(
  task: BackgroundTask,
  operations: readonly Operation[]
) {
  if (task.kind !== "plan" || task.status !== "succeeded") return false
  const result = task.result
  if (
    !result ||
    typeof result !== "object" ||
    !("id" in result) ||
    typeof result.id !== "string"
  )
    return false
  return operations.some(
    (operation) =>
      operation.applicationId === task.applicationId &&
      operation.planId === result.id &&
      operation.type !== "resource_action"
  )
}

export type StageStatus =
  | "pending"
  | "queued"
  | "running"
  | "complete"
  | "failed"
  | "cancelled"
  | "skipped"
export type RunStage = {
  id: string
  title: string
  status: StageStatus
  time?: string
  /** A deadline instead of a step: drawn as a live countdown that runs out. */
  expiresAt?: string
  /** Replaces the status text under the title. */
  caption?: string
  /** A call to action shown in place of the caption, in the accent colour. */
  action?: string
  details: { label: string; value: string }[]
}
export type RunProgress = {
  current: number
  stages: RunStage[]
  summary: string
}

type StageDefinition = Omit<RunStage, "status">

function stagesWithStatus(
  stages: StageDefinition[],
  current: number,
  status: StageStatus
): RunStage[] {
  return stages.map((stage, index) => ({
    ...stage,
    status:
      index + 1 < current
        ? "complete"
        : index + 1 === current
          ? status
          : status === "failed" || status === "cancelled"
            ? "skipped"
            : "pending",
  }))
}

export function planRunProgress(task: BackgroundTask): RunProgress {
  const done = task.status === "succeeded"
  const current = done
    ? 4
    : task.status === "queued" || (task.status === "failed" && !task.startedAt)
      ? 1
      : 2
  const plan = task.result as PlanRecord | undefined
  const changes = Array.isArray(plan?.plan?.changes)
    ? plan.plan.changes.length
    : undefined
  const stages = stagesWithStatus(
      [
        {
          id: "queued",
          title: "Queued",
          time: task.createdAt,
          details: [{ label: "Work", value: "Plan generation" }],
        },
        {
          id: "generating",
          title: "Generate plan",
          time: task.startedAt,
          details: [{ label: "Scope", value: "Git and cluster state" }],
        },
        {
          id: "ready",
          title: "Ready for review",
          time: done ? task.finishedAt : undefined,
          details:
            changes !== undefined
              ? [{ label: "Changes", value: String(changes) }]
              : [],
        },
      ],
      current,
      task.status === "failed"
        ? "failed"
        : task.status === "queued"
          ? "queued"
          : "running"
    )
  return {
    current,
    stages,
    summary: done
      ? "Plan generation complete. Review the changes before applying."
      : task.status === "failed"
        ? "Plan generation stopped. Review the error details before retrying."
        : task.status === "queued"
          ? "Waiting for a plan worker."
          : "Calculating changes from Git and the current cluster state.",
  }
}

const resourceKey = (identity: Identity) =>
  JSON.stringify([
    identity.clusterId ?? "",
    identity.apiVersion,
    identity.kind,
    identity.namespace,
    identity.name,
  ])

export function syncRunProgress(
  operation: Operation,
  plan?: PlanRecord
): RunProgress {
  // Only the operation's own reviewed plan can describe its resource phases.
  const changes =
    plan && plan.id === operation.planId ? plan.plan.changes : undefined
  const completed = new Set(
    operation.progress?.completed?.map(resourceKey) ?? []
  )
  const results = operation.progress?.resources
  const resourceCounts = (deleting: boolean) => {
    if (results?.length) {
      const resources = results.filter(
        (resource) => (resource.action === "delete") === deleting
      )
      return `${resources.filter((resource) => resource.status === "succeeded").length} / ${resources.length}`
    }
    if (!changes) return `${completed.size} / ${operation.progress?.total ?? 0}`
    const resources = changes.filter(
      (change) => (change.kind === "delete") === deleting
    )
    return `${resources.filter((change) => completed.has(resourceKey(change.identity))).length} / ${resources.length}`
  }
  const phase = operation.progress?.phase
  const includeDeletes =
    phase === "deleting" ||
    Boolean(
      results?.some((resource) => resource.action === "delete") ||
      changes?.some((change) => change.kind === "delete")
    )
  const definitions: StageDefinition[] = [
    {
      id: "queued",
      title: "Queued",
      time: operation.startedAt,
      details: [
        {
          label: "Work",
          value: operation.type === "rollback" ? "Rollback" : "Sync",
        },
      ],
    },
    {
      id: "validating",
      title: "Validate",
      details: [{ label: "Checks", value: "Git, cluster, approval" }],
    },
    {
      id: "applying",
      title: includeDeletes || changes ? "Apply" : "Apply resources",
      details: [{ label: "Resources", value: resourceCounts(false) }],
    },
    ...(includeDeletes
      ? [
          {
            id: "deleting",
            title: "Delete",
            details: [{ label: "Resources", value: resourceCounts(true) }],
          },
        ]
      : []),
    {
      id: "complete",
      title: "Complete",
      time: operation.status === "succeeded" ? operation.finishedAt : undefined,
      details: [{ label: "Runtime", value: "Checked separately" }],
    },
  ]
  const done = operation.status === "succeeded"
  const current = done
    ? definitions.length + 1
    : operation.status === "queued"
      ? 1
      : Math.max(1, definitions.findIndex((stage) => stage.id === phase) + 1)
  const status: StageStatus =
    operation.status === "failed"
      ? "failed"
      : operation.status === "cancelled"
        ? "cancelled"
        : operation.status === "queued"
          ? "queued"
          : "running"
  return {
    current,
    stages: stagesWithStatus(definitions, current, status),
    summary:
      operation.status === "queued" &&
      operation.scheduleDeferred &&
      !operation.scheduleOverrideReason
        ? "Sync is queued until the deployment schedule allows it to start. No resource changes have started."
        : done
          ? "Changes applied. Runtime readiness is checked separately."
          : operation.status === "failed" || operation.status === "cancelled"
            ? operation.message || "Sync stopped before completion."
            : phase === "complete"
              ? "Resource changes have finished. Waiting for the sync worker to finalize the operation."
              : phase === "deleting"
                ? "Applying the plan's approved deletions."
                : phase === "applying"
                  ? "Applying the plan's resource changes."
                  : phase === "validating"
                    ? "Rechecking Git, cluster state, and approval before applying."
                    : "Waiting for a sync worker to report progress.",
  }
}

/**
 * Plan lifecycle: generated, valid until, approved, applied, complete. The sync
 * operation's own stages are shown separately while it runs.
 */
export function planLifecycleProgress({
  plan,
  operation,
  required,
  approved,
  expired,
  approvalPending = false,
  canApprove = false,
}: {
  plan: PlanRecord
  operation?: Operation
  required: number
  approved: number
  expired: boolean
  /** An approval is being recorded in the background. */
  approvalPending?: boolean
  /** The viewer may approve and has not yet. */
  canApprove?: boolean
}): RunProgress {
  const applied = plan.status === "applied" || operation?.status === "succeeded"
  const dead = !applied && (expired || plan.status !== "current")
  const approvalDone = approved >= required
  const opStatus: StageStatus =
    operation?.status === "running"
      ? "running"
      : operation?.status === "queued"
        ? "queued"
        : operation?.status === "succeeded"
          ? "complete"
          : operation?.status === "failed"
            ? "failed"
            : operation
              ? "cancelled"
              : applied
                ? "complete"
                : dead
                  ? "skipped"
                  : "pending"
  const stages: RunStage[] = [
    { id: "ready", title: "Plan ready", status: "complete", time: plan.createdAt, details: [] },
  ]
  if (!applied && plan.status === "current")
    stages.push({ id: "expires", title: "Plan expires", status: "complete", expiresAt: plan.expiresAt, details: [] })
  if (plan.plan.requiresApproval)
    stages.push({
      id: "approval",
      title: !applied && !approvalDone && !dead && !approvalPending && canApprove ? `Approval ${approved}/${required}` : "Approval",
      action: !applied && !approvalDone && !dead && !approvalPending && canApprove ? "Click to approve" : undefined,
      status: applied || approvalDone ? "complete" : dead ? "skipped" : approvalPending ? "running" : "queued",
      caption: approvalPending && !approvalDone ? "Recording approval…" : applied ? (approvalDone ? `${approved} of ${required} approved` : "Approved") : approvalDone ? `${approved} of ${required} approved` : dead ? "Not needed" : `${approved} of ${required} approved`,
      details: [],
    })
  stages.push(
    {
      id: "apply",
      title: "Apply",
      status: opStatus,
      time: operation?.startedAt,
      caption:
        opStatus === "running"
          ? operation?.progress?.total ? `${operation.progress.completed?.length ?? 0} / ${operation.progress.total} resources` : "Applying…"
          : opStatus === "queued"
            ? operation?.scheduleDeferred && !operation.scheduleOverrideReason ? "Waiting for schedule" : "Waiting to start"
            : opStatus === "failed"
              ? operation?.progress?.cancelRequested ? "Stopped" : "Failed"
              : undefined,
      details: [],
    },
    {
      id: "complete",
      title: "Complete",
      status: opStatus === "complete" ? "complete" : opStatus === "failed" || opStatus === "cancelled" ? "skipped" : opStatus === "skipped" ? "skipped" : "pending",
      time: operation?.status === "succeeded" ? operation.finishedAt : undefined,
      details: [],
    }
  )
  return { current: stages.findIndex((stage) => stage.status !== "complete") + 1 || stages.length + 1, stages, summary: "" }
}
