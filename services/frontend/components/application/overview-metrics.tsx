"use client"

import { Layers01Icon, Shield01Icon } from "@hugeicons/core-free-icons"
import { applicationResourceNodes } from "@/lib/topology-view"
import { MetricCard } from "@/components/metric-card"
import type { Application, ResourceTopology } from "@/lib/types"

export function OverviewMetrics({
  application,
  topology,
  pending,
  topologyError,
  onSelectTab,
}: {
  application: Application
  topology: ResourceTopology | null
  pending: { topology: boolean }
  topologyError: unknown
  onSelectTab: (tab: string) => void
}) {
  const health = application.healthCondition
  const resources = topology ? applicationResourceNodes(topology) : []
  const observed = resources.filter(
    (node) => node.source === "kubernetes"
  ).length
  return (
    <div className="grid gap-3 sm:grid-cols-2" aria-label="Application metrics">
      <MetricCard
        title="Runtime health"
        subtitle="Latest cluster assessment"
        icon={Shield01Icon}
        value={health?.status ?? "Unknown"}
        valueClassName={
          health?.status === "Healthy"
            ? "text-success-foreground"
            : health?.status === "Degraded"
              ? "text-destructive"
              : health?.status === "Partial"
                ? "text-warning-foreground"
                : "text-muted-foreground"
        }
        onClick={() => onSelectTab("topology")}
      >
        <p className="line-clamp-3 text-xs leading-relaxed text-muted-foreground">
          {health?.message || "Health has not been observed yet."}
        </p>
        {health?.observedAt && (
          <p className="text-xs text-muted-foreground">
            Observed {new Date(health.observedAt).toLocaleString()}
          </p>
        )}
      </MetricCard>
      <MetricCard
        title="Resources"
        subtitle="Declared or managed by JustCD"
        icon={Layers01Icon}
        loading={pending.topology}
        value={
          topologyError
            ? "Unavailable"
            : topology
              ? String(resources.length)
              : "—"
        }
        onClick={() => onSelectTab("topology")}
      >
        <div
          aria-hidden="true"
          className="flex h-1.5 overflow-hidden rounded-full bg-muted"
        >
          {topology && resources.length > 0 && (
            <span
              className="bg-success"
              style={{ width: `${(observed / resources.length) * 100}%` }}
            />
          )}
        </div>
        <p className="text-xs text-muted-foreground">
          {topologyError
            ? "Resource discovery could not be loaded."
            : topology
              ? `${observed} observed in the cluster · ${resources.length - observed} not observed`
              : "No resource inventory yet."}
        </p>
        {topology && topology.nodes.length > resources.length && (
          <p className="text-xs text-muted-foreground">
            {topology.nodes.length - resources.length} runtime children shown in
            topology
          </p>
        )}
      </MetricCard>
    </div>
  )
}
