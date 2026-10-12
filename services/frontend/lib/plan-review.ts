import type { Change, Plan } from "./types.ts"

export const riskLabels = {
  deletion: "Deletion",
  replacement: "Immutable field",
  ownership: "Ownership takeover",
  cluster: "Cluster scope",
  ignored: "Ignored fields",
  ordinary: "Ordinary change",
} as const
export type Risk = keyof typeof riskLabels
export type ReviewEntry = {
  change: Change
  ignored: boolean
  child?: string
  childId?: string
  plan?: Plan
  key: string
  risks: Risk[]
  immutablePaths: string[]
  search: string
}
export type ReviewGroup = "risk" | "action" | "namespace" | "kind" | "child"
export function resourceKey(change: Change) {
  const id = change.identity
  return [id.clusterId, id.apiVersion, id.kind, id.namespace, id.name].join("/")
}

// Evidence only: Kubernetes rejects these well-known immutable updates. JustCD
// does not automatically delete/recreate them, and CRD immutability is unknown.
export function immutablePaths(change: Change): string[] {
  if (change.kind !== "update" || !change.before || !change.after) return []
  const rules: Record<string, string[]> = {
    "apps/v1/Deployment": ["/spec/selector"],
    "apps/v1/DaemonSet": ["/spec/selector"],
    "apps/v1/StatefulSet": [
      "/spec/selector",
      "/spec/serviceName",
      "/spec/volumeClaimTemplates",
    ],
    "batch/v1/Job": ["/spec/selector", "/spec/template"],
    "rbac.authorization.k8s.io/v1/RoleBinding": ["/roleRef"],
    "rbac.authorization.k8s.io/v1/ClusterRoleBinding": ["/roleRef"],
  }
  const get = (value: unknown, path: string): unknown =>
    path
      .split("/")
      .slice(1)
      .reduce<unknown>(
        (v, k) =>
          v && typeof v === "object"
            ? (v as Record<string, unknown>)[k]
            : undefined,
        value
      )
  const stable = (value: unknown): string =>
    JSON.stringify(value, (_, v) =>
      v && typeof v === "object" && !Array.isArray(v)
        ? Object.fromEntries(
            Object.entries(v).sort(([a], [b]) => a.localeCompare(b))
          )
        : v
    )
  const paths = [
    ...(rules[`${change.identity.apiVersion}/${change.identity.kind}`] ?? []),
  ]
  if (
    change.identity.apiVersion === "v1" &&
    ["Secret", "ConfigMap"].includes(change.identity.kind) &&
    get(change.before, "/immutable") === true
  )
    paths.push("/data", "/binaryData", "/immutable")
  return paths.filter(
    (p) =>
      stable(get(change.before, p)) !== stable(get(change.after, p)) ||
      ((change.changedPaths ?? []).some(
        (changed) => changed === p || changed.startsWith(`${p}/`)
      ) &&
        (JSON.stringify(get(change.before, p)) ?? "").includes("[redacted]"))
  )
}
export function makeEntry(
  change: Change,
  ignored: boolean,
  key = resourceKey(change),
  child?: string,
  childId?: string,
  plan?: Plan
): ReviewEntry {
  const paths = immutablePaths(change),
    risks: Risk[] = []
  if (!ignored && change.kind === "delete") risks.push("deletion")
  if (!ignored && paths.length) risks.push("replacement")
  if (!ignored && change.takeover) risks.push("ownership")
  if (!ignored && change.identity.clusterScoped) risks.push("cluster")
  if (ignored || change.ignoredPaths?.length) risks.push("ignored")
  if (!risks.length) risks.push("ordinary")
  const id = change.identity
  const search =
    `${id.clusterId} ${id.apiVersion} ${id.kind} ${id.namespace} ${id.name} ${child ?? ""} ${(change.changedPaths ?? []).join(" ")} ${(change.ignoredPaths ?? []).join(" ")} ${JSON.stringify(change.before) ?? ""} ${JSON.stringify(change.after) ?? ""}`.toLowerCase()
  return {
    change,
    ignored,
    key,
    child,
    childId,
    plan,
    risks,
    immutablePaths: paths,
    search,
  }
}
export function matchesEntry(
  entry: ReviewEntry,
  query: string,
  filter: string
) {
  const c = entry.change
  return (
    (filter === "all" ||
      (filter === "excluded"
        ? entry.ignored
        : filter in riskLabels
          ? entry.risks.includes(filter as Risk)
          : !entry.ignored && c.kind === filter)) &&
    entry.search.includes(query.toLowerCase())
  )
}
export function groupLabel(entry: ReviewEntry, group: ReviewGroup) {
  return group === "risk"
    ? riskLabels[entry.risks[0]]
    : group === "action"
      ? entry.ignored
        ? "Excluded"
        : entry.change.kind
      : group === "namespace"
        ? entry.change.identity.namespace || "Cluster scope"
        : group === "kind"
          ? entry.change.identity.kind
          : (entry.child ?? "Current application")
}
export function riskCounts(entries: ReviewEntry[]) {
  return Object.fromEntries(
    Object.keys(riskLabels).map((risk) => [
      risk,
      entries.filter((e) => e.risks.includes(risk as Risk)).length,
    ])
  ) as Record<Risk, number>
}
