import type { Identity, Operation } from "./types"

export type RolloutResource = NonNullable<
  NonNullable<Operation["progress"]>["resources"]
>[number]

export function rolloutResourceKey(identity: Identity) {
  return JSON.stringify([
    identity.clusterId ?? "",
    identity.apiVersion,
    identity.kind,
    identity.namespace,
    identity.name,
  ])
}

/** A stale progress.current must not keep the panel on an already completed step. */
export function rolloutFocusResource(
  operation: Operation
): RolloutResource | undefined {
  const resources = operation.progress?.resources ?? []
  const live = operation.status === "running" || operation.status === "queued"
  if (live) {
    const current = operation.progress?.current
    const reported =
      current &&
      resources.find(
        (resource) =>
          rolloutResourceKey(resource.identity) ===
            rolloutResourceKey(current) &&
          (resource.status === "running" || resource.status === "retrying")
      )
    if (reported) return reported
    const active = resources.find(
      (resource) =>
        resource.status === "running" || resource.status === "retrying"
    )
    if (active) return active
  }
  const failed = resources.find((resource) => resource.status === "failed")
  if (failed) return failed
  if (live) {
    const queued = resources.find((resource) => resource.status === "queued")
    if (queued) return queued
  }
  // Terminal operations settle on the last attempted result, not an unattempted tail.
  const attempted = resources.filter(
    (resource) => resource.status !== "queued" && resource.status !== "skipped"
  )
  return (
    attempted.reduce<RolloutResource | undefined>((latest, resource) => {
      const timestamp = (item: RolloutResource) =>
        Date.parse(item.finishedAt ?? item.startedAt ?? "") || 0
      return !latest || timestamp(resource) >= timestamp(latest)
        ? resource
        : latest
    }, undefined) ?? resources[0]
  )
}

export function rolloutStatus(resource: RolloutResource) {
  return resource.status === "failed" && resource.mutationApplied
    ? resource.action === "recreate" ? "Recreation incomplete" : "Applied · follow-up failed"
    : resource.status
}
export function rolloutSummary(resources: readonly RolloutResource[]) {
  const applied = resources.filter(
    (r) => r.mutationApplied || r.status === "succeeded"
  ).length
  const complete = resources.filter((r) => r.status === "succeeded").length
  const skipped = resources.filter((r) => r.status === "skipped").length
  return `${applied} applied · ${complete} fully completed · ${skipped} not attempted`
}
export const resourceStages = [
  { id: "checking_live", label: "Check live state" },
  { id: "applying", label: "Apply to Kubernetes" },
  { id: "ownership_cleanup", label: "Clean up field ownership" },
  { id: "recording", label: "Record completion" },
] as const
export function resourceStagesFor(resource: RolloutResource) {
  return resource.action === "recreate" ? [
    { id: "checking_live", label: "Check reviewed state and PVC safety" },
    { id: "deleting_statefulset", label: "Delete StatefulSet with reviewed policy" },
    { id: "waiting_statefulset_dependents", label: "Wait for deletion or orphaning" },
    { id: "recreating_statefulset", label: "Create replacement StatefulSet" },
    ...(resource.propagationPolicy === "Orphan" ? [{ id: "waiting_statefulset_adoption", label: "Wait for orphan adoption" }] : []),
    { id: "recording", label: "Record completion" },
  ] : resourceStages
}
export function resourceStageStatus(resource: RolloutResource, stage: string) {
  if (resource.status === "succeeded") return "complete"
  const stages = resourceStagesFor(resource)
  const current = stages.findIndex((s) => s.id === resource.stage)
  const index = stages.findIndex((s) => s.id === stage)
  if (resource.mutationApplied && stage === "applying") return "complete"
  if (
    current < 0 ||
    resource.status === "skipped" ||
    resource.status === "queued"
  )
    return "not started"
  if (index < current) return "complete"
  if (index > current) return "not started"
  return resource.status === "failed" ? "failed" : "running"
}
