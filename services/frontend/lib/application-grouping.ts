import type { Application } from "./types"

export type GroupMode = "cluster" | "namespace" | "status" | "none"
export type ApplicationBucket<T extends Application> = { key: string; label: string; applications: T[] }

export type StatusBucket = "attention" | "synced" | "other"

const statusLabels = { attention: "Needs attention", synced: "In sync", other: "Other states" } as const

/** Splits applications into stable buckets. Order inside a bucket is preserved. */
export function groupApplications<T extends Application>(apps: T[], mode: GroupMode, classify: (app: T) => StatusBucket): ApplicationBucket<T>[] {
  if (mode === "none") return apps.length ? [{ key: "all", label: "", applications: apps }] : []
  const buckets = new Map<string, ApplicationBucket<T>>()
  for (const app of apps) {
    const [key, label] =
      mode === "cluster" ? [app.clusterId, app.clusterName || app.clusterId]
      : mode === "status" ? [classify(app), statusLabels[classify(app)]]
      : (() => { const joined = app.namespaces.map((item) => item.namespace).join(", "); return [joined || "-", joined || "No namespace"] })()
    const bucket = buckets.get(key) ?? { key, label, applications: [] }
    bucket.applications.push(app)
    buckets.set(key, bucket)
  }
  const order = ["attention", "synced", "other"]
  return [...buckets.values()].sort((a, b) => mode === "status" ? order.indexOf(a.key) - order.indexOf(b.key) : a.label.localeCompare(b.label))
}
