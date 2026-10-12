import type { RollbackTarget } from "./types.ts"

export function filterRollbackTargets(targets: RollbackTarget[], search: string, kind: string): RollbackTarget[] {
  const query = search.trim().toLowerCase()
  return targets.filter((target) => (!kind || target.kind === kind) && (!query || [target.id, target.operationId, target.revision, target.kind === "successful_sync" ? "Successful deployment" : "Before failed operation"].filter(Boolean).join(" ").toLowerCase().includes(query)))
}
