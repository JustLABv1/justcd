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
