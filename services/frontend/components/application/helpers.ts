import type { Identity, OwnershipConflict } from "@/lib/types"

export function diffId(identity: Identity) {
  return `diff-${[identity.clusterId ?? "", identity.apiVersion, identity.kind, identity.namespace, identity.name].map(encodeURIComponent).join("-")}`
}

export function sameIdentity(a: Identity, b: Identity) { return diffId(a) === diffId(b) }

export function safeIgnorePath(pointer: string) {
  const parts = pointer.split("/").slice(1).map((part) => part.replaceAll("~1", "/").replaceAll("~0", "~"))
  if (parts.some((part) => !part || /^\d+$/.test(part))) return false
  if (parts[0] === "apiVersion" || parts[0] === "kind") return false
  if (parts[0] === "metadata") {
    if (parts.length === 1 || ["name", "namespace", "uid", "resourceVersion", "generation", "managedFields"].includes(parts[1])) return false
    if (["labels", "annotations"].includes(parts[1]) && parts.length === 2) return false
    if (parts[1] === "labels" && parts[2] === "justcd.io/application-id") return false
  }
  return true
}

export function operationPhaseLabel(phase: string | undefined, status: string) {
  if (status === "queued") return "Waiting for a sync worker"
  if (status === "failed") return "Sync stopped"
  if (phase === "validating") return "Rechecking Git, cluster state, and approval"
  if (phase === "applying") return "Applying creates and updates"
  if (phase === "deleting") return "Applying approved deletions"
  if (phase === "complete" || status === "succeeded") return "Sync complete"
  return "Sync in progress"
}

export function resourceLabel(identity: Identity) {
  return `${identity.kind} ${identity.namespace ? `${identity.namespace}/` : ""}${identity.name}`
}

/** A conflict can be claimed when no Kubernetes owner reference blocks it and no other live application owns it. */
export function isClaimable(item: OwnershipConflict, applicationID: string) {
  return !item.hasOwnerReferences && (!item.owner || item.owner === applicationID || Boolean(item.ownerMissing))
}

/** Prefill request for the Settings tab's exclusion form. A new `nonce` re-applies the values. */
export type IgnorePrefill = { nonce: number; mode: "kind" | "label" | "resource"; version?: string; kind?: string; namespace?: string; name?: string; reason?: string }

export type NewIgnore = { mode: string; version: string; kind: string; namespace: string; name: string; labelKey: string; labelValue: string; reason: string }
