"use client"

import { useMemo, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowDown01Icon } from "@hugeicons/core-free-icons"
import { FilterMenu } from "@/components/reui/filters/filters-editors"
import { DEFAULT_FILTER_LABELS } from "@/components/reui/filters/filters-i18n"
import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { tone } from "@/components/health-tone"
import type { ConnectionHealth } from "@/lib/connection-health"

type ClusterOption = {
  id: string
  name: string
  health: ConnectionHealth
  count: number
}

const labels = {
  ...DEFAULT_FILTER_LABELS,
  empty: "No clusters match your search.",
}

export function ApplicationClusterFilter({
  clusters,
  value,
  onValueChange,
}: {
  clusters: ClusterOption[]
  value: string
  onValueChange: (value: string) => void
}) {
  const [open, setOpen] = useState(false)
  const selected = clusters.find((cluster) => cluster.id === value)
  const items = useMemo(
    () => [
      {
        value: "all",
        label: "All clusters",
        description: `${clusters.length} clusters`,
      },
      ...clusters.map((cluster) => ({
        value: cluster.id,
        label: cluster.name,
        description: `${cluster.health.label} · ${cluster.count} application${cluster.count === 1 ? "" : "s"}`,
        keywords: [cluster.id, cluster.health.label],
        icon: (
          <span
            aria-hidden="true"
            className={`size-2 shrink-0 rounded-full ${tone[cluster.health.level].dot}`}
          />
        ),
      })),
    ],
    [clusters]
  )

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            variant="outline"
            className="w-full min-w-0 justify-between sm:w-64"
            aria-label={`Filter by cluster: ${selected?.name ?? "All clusters"}`}
            title={selected?.name}
          />
        }
      >
        <span className="flex min-w-0 items-center gap-2">
          {selected && (
            <span
              aria-hidden="true"
              className={`size-2 shrink-0 rounded-full ${tone[selected.health.level].dot}`}
            />
          )}
          <span className="truncate">{selected?.name ?? "All clusters"}</span>
        </span>
        <HugeiconsIcon icon={ArrowDown01Icon} aria-hidden="true" />
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-[min(24rem,calc(100vw-2rem))] gap-0 p-0"
      >
        <FilterMenu
          items={items}
          selected={[value || "all"]}
          onSelectionChange={(values) => {
            onValueChange(values[0] === "all" ? "" : (values[0] ?? ""))
            setOpen(false)
          }}
          labels={labels}
          ariaLabel="Search clusters"
          searchPlaceholder="Search clusters…"
          maxHeight={320}
        />
      </PopoverContent>
    </Popover>
  )
}
