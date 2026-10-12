import type { BackgroundTask, Identity, Operation, PlanRecord } from "./types"

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
  return {
    current,
    stages: stagesWithStatus(
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
    ),
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
  const resourceCounts = (deleting: boolean) => {
    if (!changes) return `${completed.size} / ${operation.progress?.total ?? 0}`
    const resources = changes.filter(
      (change) => (change.kind === "delete") === deleting
    )
    return `${resources.filter((change) => completed.has(resourceKey(change.identity))).length} / ${resources.length}`
  }
  const phase = operation.progress?.phase
  const includeDeletes =
    phase === "deleting" ||
    Boolean(changes?.some((change) => change.kind === "delete"))
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
    summary: done
      ? "Changes applied. Runtime readiness is checked separately."
      : operation.status === "failed" || operation.status === "cancelled"
        ? operation.message || "Sync stopped before completion."
        : phase === "deleting"
          ? "Applying the plan's approved deletions."
          : phase === "applying"
            ? "Applying the plan's resource changes."
            : phase === "validating"
              ? "Rechecking Git, cluster state, and approval before applying."
              : "Waiting for a sync worker to report progress.",
  }
}
