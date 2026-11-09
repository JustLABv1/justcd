"use client"

import { useMemo } from "react"
import { PanelSelect } from "@/components/applications/panel-wizard"
import { tone } from "@/components/health-tone"
import type { ConnectionHealth } from "@/lib/connection-health"

type ClusterOption = {
  id: string
  name: string
  health: ConnectionHealth
  count: number
}

const ALL = "all"

export function ApplicationClusterFilter({
  clusters,
  value,
  onValueChange,
}: {
  clusters: ClusterOption[]
  value: string
  onValueChange: (value: string) => void
}) {
  const items = useMemo(
    () => [
      { value: ALL, label: "All clusters", description: `${clusters.length} clusters` },
      ...clusters.map((cluster) => ({
        value: cluster.id,
        label: cluster.name,
        description: `${cluster.health.label} · ${cluster.count} application${cluster.count === 1 ? "" : "s"}`,
        icon: <span aria-hidden="true" className={`size-2 rounded-full ${tone[cluster.health.level].dot}`} />,
      })),
    ],
    [clusters]
  )
  const selected = clusters.find((cluster) => cluster.id === value)
  return (
    <PanelSelect
      ariaLabel={`Filter by cluster: ${selected?.name ?? "All clusters"}`}
      value={value || ALL}
      onValueChange={(next) => onValueChange(next === ALL ? "" : next)}
      items={items}
      className="sm:w-64"
    />
  )
}
