"use client"

import { useEffect, useRef, useState } from "react"
import { BarChart } from "@/components/panelui/bar-chart"
import { Button } from "@/components/panelui/button"
import { Card } from "@/components/panelui/card"
import { Tabs } from "@/components/panelui/tabs"
import { Skeleton } from "@/components/ui/skeleton"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/panelui/tooltip"
import { HugeiconsIcon } from "@hugeicons/react"
import { InformationCircleIcon } from "@hugeicons/core-free-icons"
import { ErrorDetailsButton } from "@/components/error-details"
import { summarizeSyncHistory } from "@/lib/sync-history"
import { useSyncHistory, type SyncHistoryState } from "@/hooks/use-sync-history"

const config = {
  succeeded: { label: "Succeeded", color: "var(--success)" },
  failed: { label: "Failed", color: "var(--destructive)" },
  cancelled: { label: "Cancelled", color: "var(--warning)" },
  active: { label: "Active", color: "var(--info)" },
}
// The plot reserves a box from width ÷ height, so a fixed ratio makes wide cards
// very tall. Aim for a steady plot height instead.
const PLOT_HEIGHT = 260

function useAspectRatio() {
  const ref = useRef<HTMLDivElement>(null)
  const [ratio, setRatio] = useState(2)
  useEffect(() => {
    const node = ref.current
    if (!node) return
    const read = () => setRatio(Math.min(6, Math.max(1.5, node.clientWidth / PLOT_HEIGHT)))
    read()
    const observer = new ResizeObserver(read)
    observer.observe(node)
    return () => observer.disconnect()
  }, [])
  return { ref, ratio }
}
const outcomes = ["succeeded", "failed", "cancelled", "active"] as const

export function SyncHistoryChart({
  applicationID,
  workspaceID,
  revision,
  onViewActivity,
}: {
  revision?: string
  onViewActivity?: () => void
} & (
  | { applicationID: string; workspaceID?: never }
  | { workspaceID: string; applicationID?: never }
)) {
  const state = useSyncHistory(
    workspaceID ? "workspaces" : "applications",
    workspaceID ?? applicationID,
    revision
  )
  return <SyncHistoryChartView state={state} onViewActivity={onViewActivity} />
}

export function SyncHistoryChartView({
  state,
  onViewActivity,
}: {
  state: SyncHistoryState
  onViewActivity?: () => void
}) {
  const { result, range, setRange, retry } = state
  const { ref: plotRef, ratio } = useAspectRatio()
  const history = result.history
  const totals = history ? summarizeSyncHistory(history) : null
  const showData = !result.loading && !result.error && Boolean(totals && totals.total > 0)
  const label = range === "24h" ? "24 hours" : "7 days"
  const max = Math.max(
    1,
    ...(history?.buckets.map((bucket) =>
      outcomes.reduce((sum, outcome) => sum + bucket[outcome], 0)
    ) ?? [])
  )
  const tick = (value: string) =>
    new Date(value).toLocaleString(
      undefined,
      range === "24h"
        ? { hour: "2-digit", minute: "2-digit" }
        : { weekday: "short", hour: "2-digit" }
    )
  const bucketLabel = (value: string) => {
    const start = new Date(value)
    const end = new Date(
      start.getTime() + (history?.bucketSeconds ?? 3600) * 1000
    )
    return `${start.toLocaleString()} – ${end.toLocaleTimeString()}`
  }
  return (
    <Card size="sm" className="min-w-0">
      <Card.Header className="flex flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1 basis-48"><Card.Title as="h2">Sync activity</Card.Title>
        <Card.Description aria-live="polite" className={result.error ? "text-destructive" : undefined}>
          {result.loading
            ? "Loading sync history…"
            : result.error
              ? "Sync history unavailable"
              : totals && totals.total > 0
                ? `${totals.total} runs in the last ${label}${totals.successRate == null ? "" : ` · ${totals.successRate.toFixed(1)}% succeeded`}`
                : ""}
          {showData && <Tooltip>
            <TooltipTrigger render={<button type="button" className="ms-1 inline-grid size-5 translate-y-[3px] place-items-center rounded-full text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring" aria-label="How runs are counted" />}>
              <HugeiconsIcon icon={InformationCircleIcon} strokeWidth={1.8} className="size-3.5" aria-hidden="true" />
            </TooltipTrigger>
            <TooltipContent className="max-w-xs">Syncs and rollbacks grouped by start time. Success rate includes succeeded, failed and cancelled runs; active runs are excluded.</TooltipContent>
          </Tooltip>}
        </Card.Description></div>
        <Card.Action>
          <Tabs value={range} onValueChange={(value) => { if (value === "24h" || value === "7d") setRange(value) }} aria-label="Sync history period">
            <Tabs.List aria-label="Sync history period">
              <Tabs.Trigger value="24h">24 hours</Tabs.Trigger>
              <Tabs.Trigger value="7d">7 days</Tabs.Trigger>
            </Tabs.List>
          </Tabs>
        </Card.Action>
      </Card.Header>
      <Card.Content>
      <div ref={plotRef} className="space-y-4">
      {showData && (
        <ul aria-label="Run outcomes" className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
          {/* Only outcomes that happened in this period; an always-present "Failed: 0" entry is noise. */}
          {outcomes.filter((outcome) => totals?.[outcome]).map((outcome) => (
            <li key={outcome} className="inline-flex items-center gap-1.5">
              <span className="size-1.5 rounded-full" style={{ background: config[outcome].color }} aria-hidden="true" />
              {config[outcome].label}
            </li>
          ))}
        </ul>
      )}
      {result.loading ? (
        <Skeleton className="h-56 w-full motion-reduce:animate-none" />
      ) : result.error ? (
        <div
          role="alert"
          className="flex min-h-40 flex-wrap items-center justify-center gap-3 p-5"
        >
          <p className="text-sm text-destructive">
            Sync history could not be loaded.
          </p>
          <Button variant="outline" size="sm" onClick={retry}>
            Retry history
          </Button>
          <ErrorDetailsButton error={result.error} />
        </div>
      ) : totals?.total === 0 ? (
        <div className="flex flex-col items-center gap-3 px-4 py-6 text-center">
          <p className="max-w-sm text-sm text-muted-foreground">
            No sync runs in the last {label}. Syncs and rollbacks appear here once recorded.
          </p>
          {onViewActivity && <Button variant="outline" size="sm" onClick={onViewActivity}>View activity</Button>}
        </div>
      ) : (
        history && (
          <div className="px-2 pt-4">
            <BarChart aspectRatio={ratio} data={history.buckets} xDataKey="startedAt" stacked barWidth={28} barGap={0.24}
              yDomain={[0, max]} animationDuration={0} domainDuration={0}
              label={`Sync and rollback outcomes over the last ${label}`}
              labelForDatum={(datum) => `${bucketLabel(String(datum.startedAt))}. ${outcomes.map(outcome => `${config[outcome].label}: ${datum[outcome]}`).join(". ")}`}>
              <BarChart.XAxis ticks={5} format={(datum) => tick(String(datum.startedAt))} />
              <BarChart.YAxis ticks={Math.min(4, max + 1)} format={(value) => Number.isInteger(value) ? String(value) : ""} />
              <BarChart.Tooltip labels={Object.fromEntries(outcomes.map((outcome) => [outcome, config[outcome].label]))} formatX={(datum) => bucketLabel(String(datum.startedAt))} />
              <BarChart.Bar dataKey="succeeded" color="success" />
              <BarChart.Bar dataKey="failed" color="destructive" />
              <BarChart.Bar dataKey="cancelled" color="warning" />
              <BarChart.Bar dataKey="active" color="info" />
            </BarChart>
          </div>
        )
      )}
      {onViewActivity && showData && (
        <div className="flex justify-end">
          <Button variant="outline" size="sm" onClick={onViewActivity}>View activity</Button>
        </div>
      )}
      </div>
      </Card.Content>
    </Card>
  )
}
