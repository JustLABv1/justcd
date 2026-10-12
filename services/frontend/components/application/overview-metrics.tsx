"use client"

import {
  Layers01Icon,
  Shield01Icon,
  Alert02Icon,
} from "@hugeicons/core-free-icons"
import { MetricCard } from "@/components/metric-card"
import { Badge } from "@/components/reui/badge"
import type { Application, PlanRecord, ResourceTopology } from "@/lib/types"

export function OverviewMetrics({
  application,
  latestPlan,
  topology,
  pending,
  plansError,
  topologyError,
  onSelectTab,
}: {
  application: Application
  latestPlan?: PlanRecord
  topology: ResourceTopology | null
  pending: { topology: boolean; plans: boolean }
  plansError: unknown
  topologyError: unknown
  onSelectTab: (tab: string) => void
}) {
  const health = application.healthCondition
  const current = latestPlan?.status === "current"
  const changes = current ? latestPlan.plan.changes : []
  const observed =
    topology?.nodes.filter((node) => node.source === "kubernetes").length ?? 0
  const counts = changes.reduce(
    (acc, change) => ({ ...acc, [change.kind]: acc[change.kind] + 1 }),
    { create: 0, update: 0, delete: 0 }
  )
  return (
    <div className="grid gap-3 sm:grid-cols-3" aria-label="Application metrics">
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
        subtitle="Desired and discovered resources"
        icon={Layers01Icon}
        loading={pending.topology}
        value={
          topologyError
            ? "Unavailable"
            : topology
              ? String(topology.nodes.length)
              : "—"
        }
        onClick={() => onSelectTab("topology")}
      >
        <div
          aria-hidden="true"
          className="flex h-1.5 overflow-hidden rounded-full bg-muted"
        >
          {topology && topology.nodes.length > 0 && (
            <span
              className="bg-success"
              style={{ width: `${(observed / topology.nodes.length) * 100}%` }}
            />
          )}
        </div>
        <p className="text-xs text-muted-foreground">
          {topologyError
            ? "Resource discovery could not be loaded."
            : topology
              ? `${observed} observed in the cluster · ${topology.nodes.length - observed} not observed`
              : "No resource inventory yet."}
        </p>
      </MetricCard>
      <MetricCard
        title="Planned changes"
        subtitle="Latest actionable plan"
        icon={Alert02Icon}
        loading={pending.plans}
        value={
          plansError ? "Unavailable" : current ? String(changes.length) : "—"
        }
        onClick={() => onSelectTab("changes")}
        status={
          current && latestPlan.plan.requiresApproval
            ? { label: "Approval required", level: "warning" }
            : undefined
        }
      >
        {current ? (
          <div className="flex flex-wrap gap-1.5">
            <Badge size="xs" variant="success-light">
              {counts.create} create
            </Badge>
            <Badge size="xs" variant="info-light">
              {counts.update} update
            </Badge>
            <Badge size="xs" variant="destructive-light">
              {counts.delete} delete
            </Badge>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">
            {plansError
              ? "Plan could not be loaded."
              : latestPlan
                ? `Latest plan is ${latestPlan.status}. Refresh to check for changes.`
                : "Create a plan to check for changes."}
          </p>
        )}
      </MetricCard>
    </div>
  )
}
