"use client"

import { useEffect, useId, useState } from "react"
import { Bar, BarChart, XAxis, YAxis } from "recharts"
import { Frame, FramePanel } from "@/components/reui/frame"
import { Button } from "@/components/ui/button"
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Skeleton } from "@/components/ui/skeleton"
import { Disclosure } from "@/components/ui/collapsible"
import { ErrorDetailsButton } from "@/components/error-details"
import { api } from "@/lib/api"
import { summarizeSyncHistory, type SyncHistory } from "@/lib/sync-history"

const config = {
  succeeded: { label: "Succeeded", color: "var(--success)" },
  failed: { label: "Failed", color: "var(--destructive)" },
  cancelled: { label: "Cancelled", color: "var(--warning)" },
  active: { label: "Active", color: "var(--chart-2)" },
} satisfies ChartConfig
const outcomes = ["succeeded", "failed", "cancelled", "active"] as const

export function SyncHistoryChart({
  applicationID,
  revision,
  onViewActivity,
}: {
  applicationID: string
  revision: string
  onViewActivity: () => void
}) {
  const [range, setRange] = useState<"24h" | "7d">("24h")
  const [refresh, setRefresh] = useState(0)
  const requestKey = `${applicationID}:${range}:${refresh}:${revision}`
  const [response, setResult] = useState<{
    key: string
    history?: SyncHistory
    error?: unknown
    loading: boolean
  }>({ key: "", loading: true })
  const result =
    response.key === requestKey
      ? response
      : { loading: true, history: undefined, error: undefined }
  const patternID = useId().replace(/:/g, "")
  useEffect(() => {
    const controller = new AbortController()
    let loading = false
    async function load() {
      if (loading) return
      loading = true
      try {
        const history = await api<SyncHistory>(
          `/api/v1/applications/${encodeURIComponent(applicationID)}/sync-history?range=${range}`,
          { signal: controller.signal }
        )
        if (!controller.signal.aborted)
          setResult({ key: requestKey, history, loading: false })
      } catch (error) {
        if (!controller.signal.aborted)
          setResult({ key: requestKey, error, loading: false })
      } finally {
        loading = false
      }
    }
    void load()
    const timer = window.setInterval(() => void load(), 60_000)
    return () => {
      controller.abort()
      window.clearInterval(timer)
    }
  }, [applicationID, range, requestKey])
  const history = result.history
  const totals = history ? summarizeSyncHistory(history) : null
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
    <Frame>
      <FramePanel className="space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="space-y-1">
            <h2 className="text-sm font-semibold">Sync activity</h2>
            <p className="text-xs text-muted-foreground" aria-live="polite">
              {result.loading
                ? "Loading sync history…"
                : result.error
                  ? "Sync history unavailable"
                  : totals
                    ? `${totals.total} runs in the last ${label}${totals.successRate == null ? "" : ` · ${totals.successRate.toFixed(1)}% succeeded`}`
                    : ""}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-4">
            <ul
              aria-label="Run outcomes"
              className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground"
            >
              {outcomes.map((outcome) => (
                <li key={outcome} className="inline-flex items-center gap-1.5">
                  <span
                    className="size-1.5 rounded-full"
                    style={{ background: config[outcome].color }}
                    aria-hidden="true"
                  />
                  {config[outcome].label}
                </li>
              ))}
            </ul>
            <ToggleGroup
              aria-label="Sync history period"
              value={[range]}
              onValueChange={(values) => {
                if (values[0] === "24h" || values[0] === "7d")
                  setRange(values[0])
              }}
            >
              <ToggleGroupItem value="24h" aria-label="Last 24 hours">
                24 hours
              </ToggleGroupItem>
              <ToggleGroupItem value="7d" aria-label="Last 7 days">
                7 days
              </ToggleGroupItem>
            </ToggleGroup>
          </div>
        </div>
        {result.loading ? (
          <Skeleton className="h-56 w-full motion-reduce:animate-none" />
        ) : result.error ? (
          <div
            role="status"
            className="flex min-h-40 flex-wrap items-center justify-center gap-3 rounded-lg border bg-muted/20 p-5"
          >
            <p className="text-sm text-muted-foreground">
              Sync history could not be loaded.
            </p>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setRefresh((value) => value + 1)}
            >
              Retry history
            </Button>
            <ErrorDetailsButton error={result.error} />
          </div>
        ) : totals?.total === 0 ? (
          <div className="flex h-56 items-center justify-center rounded-lg border bg-muted/20 p-5 text-center">
            <div>
              <p className="text-sm font-medium">
                No sync runs in the last {label}
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                Syncs and rollbacks will appear here as they are recorded.
              </p>
            </div>
          </div>
        ) : (
          history && (
            <div className="rounded-lg border bg-muted/10 px-2 pt-4">
              <ChartContainer
                config={config}
                className="aspect-auto h-56 w-full"
                aria-label={`Sync and rollback outcomes over the last ${label}`}
              >
                <BarChart
                  data={history.buckets}
                  accessibilityLayer
                  margin={{ top: 4, right: 8, bottom: 4, left: 0 }}
                  barCategoryGap="24%"
                >
                  <defs>
                    {outcomes.map((outcome) => (
                      <pattern
                        key={outcome}
                        id={`${patternID}-${outcome}`}
                        width="6"
                        height="6"
                        patternUnits="userSpaceOnUse"
                      >
                        <rect
                          width="6"
                          height="6"
                          fill={`var(--color-${outcome})`}
                          fillOpacity="0.75"
                        />
                        <path
                          d="M-1 1L1-1M0 6L6 0M5 7L7 5"
                          stroke="var(--card)"
                          strokeOpacity="0.4"
                          strokeWidth="1.5"
                        />
                      </pattern>
                    ))}
                  </defs>
                  <XAxis
                    dataKey="startedAt"
                    tickFormatter={tick}
                    tickLine={false}
                    axisLine={false}
                    minTickGap={45}
                    tickMargin={10}
                  />
                  <YAxis
                    domain={[0, max]}
                    allowDecimals={false}
                    tickLine={false}
                    axisLine={false}
                    width={32}
                    tickCount={4}
                  />
                  <ChartTooltip
                    cursor={false}
                    content={
                      <ChartTooltipContent
                        labelFormatter={(label) => bucketLabel(String(label))}
                      />
                    }
                  />
                  {outcomes.map((outcome, index) => (
                    <Bar
                      key={outcome}
                      dataKey={outcome}
                      stackId="outcome"
                      fill={`url(#${patternID}-${outcome})`}
                      maxBarSize={28}
                      isAnimationActive={false}
                      background={
                        index === 0
                          ? { fill: "var(--muted)", radius: 4 }
                          : undefined
                      }
                    />
                  ))}
                </BarChart>
              </ChartContainer>
            </div>
          )
        )}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="max-w-2xl text-xs leading-relaxed text-muted-foreground">
            Syncs and rollbacks grouped by start time. Success rate includes
            succeeded, failed and cancelled runs; active runs are excluded.
          </p>
          <Button variant="ghost" size="sm" onClick={onViewActivity}>
            View activity
          </Button>
        </div>
        {history && totals && totals.total > 0 && (
          <Disclosure summary="View history as text">
            <ol className="grid gap-2 text-xs text-muted-foreground sm:grid-cols-2">
              {history.buckets.map((bucket) => (
                <li key={bucket.startedAt}>
                  <span className="font-medium text-foreground">
                    {bucketLabel(bucket.startedAt)}
                  </span>
                  <br />
                  {outcomes
                    .map(
                      (outcome) =>
                        `${bucket[outcome]} ${config[outcome].label.toLowerCase()}`
                    )
                    .join(" · ")}
                </li>
              ))}
            </ol>
          </Disclosure>
        )}
      </FramePanel>
    </Frame>
  )
}
