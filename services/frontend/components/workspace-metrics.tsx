"use client"

import Link from "next/link"
import type { ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  Alert02Icon,
  Layers01Icon,
  ServerStack01Icon,
  Shield01Icon,
} from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Frame, FramePanel, FrameTitle } from "@/components/reui/frame"
import { IconTile } from "@/components/reui/icon-tile"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import type { Health } from "@/hooks/use-workspace-health"
import type { ConnectionHealth, HealthLevel } from "@/lib/connection-health"
import type { Application } from "@/lib/types"
import { ago } from "@/lib/relative-time"

type Segment = { label: string; count: number; color: string }

// These bars describe a current status distribution, not a time series.
function Distribution({
  segments,
  ticks = false,
  empty = "No data yet",
}: {
  segments: Segment[]
  ticks?: boolean
  empty?: string
}) {
  const total = segments.reduce((sum, segment) => sum + segment.count, 0)
  const label = total
    ? segments
        .map((segment) => `${segment.count} ${segment.label.toLowerCase()}`)
        .join(", ")
    : empty
  return (
    <div
      role="img"
      aria-label={label}
      className={`flex min-w-0 gap-0.5 ${ticks ? "h-3.5" : "h-1.5 overflow-hidden rounded-full bg-muted"}`}
    >
      {total
        ? segments
            .filter((segment) => segment.count > 0)
            .map((segment) => (
              <span
                key={segment.label}
                className="flex min-w-0 gap-0.5"
                style={{ flex: `${segment.count} 1 0%` }}
              >
                {Array.from(
                  {
                    length: ticks
                      ? Math.max(1, Math.round((segment.count / total) * 40))
                      : 1,
                  },
                  (_, index) => (
                    <span
                      key={index}
                      aria-hidden="true"
                      className={`min-w-0 flex-1 rounded-full ${segment.color}`}
                    />
                  )
                )}
              </span>
            ))
        : Array.from({ length: ticks ? 40 : 1 }, (_, index) => (
            <span
              key={index}
              aria-hidden="true"
              className="min-w-0 flex-1 rounded-full bg-muted"
            />
          ))}
    </div>
  )
}

function Legend({ segments }: { segments: Segment[] }) {
  return (
    <ul className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
      {segments.map((segment) => (
        <li key={segment.label} className="inline-flex items-center gap-1.5">
          <span
            aria-hidden="true"
            className={`h-2 w-1 rounded-full ${segment.color}`}
          />
          <span>
            <span className="tabular-nums">{segment.count}</span>{" "}
            {segment.label}
          </span>
        </li>
      ))}
    </ul>
  )
}

function MetricCard({
  title,
  subtitle,
  icon,
  value,
  suffix,
  status,
  loading,
  href,
  children,
}: {
  title: string
  subtitle: string
  icon: typeof Layers01Icon
  value: string
  suffix?: string
  status?: { label: string; level: HealthLevel }
  loading?: boolean
  href: string
  children: ReactNode
}) {
  return (
    <Frame className="h-full min-w-0">
      <FramePanel className="flex min-w-0 flex-col gap-4">
        <div className="flex min-w-0 items-start gap-3">
          <IconTile variant="elevated" size="sm" aria-hidden="true">
            <HugeiconsIcon icon={icon} strokeWidth={1.8} />
          </IconTile>
          <div className="min-w-0">
            <FrameTitle className="font-medium">{title}</FrameTitle>
            <p className="mt-0.5 text-xs text-muted-foreground">{subtitle}</p>
          </div>
        </div>
        <div className="flex flex-wrap items-baseline gap-2">
          {loading ? (
            <Skeleton className="h-7 w-20 motion-reduce:animate-none" />
          ) : (
            <p className="text-2xl font-semibold tracking-tight tabular-nums">
              {value}
              {suffix && (
                <span className="ml-1 text-sm font-normal text-muted-foreground">
                  {" "}
                  {suffix}
                </span>
              )}
            </p>
          )}
          {status && (
            <Badge
              size="xs"
              variant={
                status.level === "critical"
                  ? "destructive-light"
                  : status.level === "warning"
                    ? "warning-light"
                    : status.level === "healthy"
                      ? "success-light"
                      : "secondary"
              }
            >
              {status.label}
            </Badge>
          )}
        </div>
        <div className="mt-auto flex min-h-16 flex-col justify-end gap-2.5">
          {children}
        </div>
        <Button
          variant="ghost"
          render={<Link href={href} />}
          nativeButton={false}
          className="absolute inset-0 z-10 h-full w-full rounded-[inherit] hover:bg-muted/10 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
        >
          <span className="sr-only">Open {title.toLowerCase()}</span>
        </Button>
      </FramePanel>
    </Frame>
  )
}

export function WorkspaceMetrics({
  total,
  healthy,
  other,
  attention,
  stalled,
  connections,
  health,
  now,
  applicationsHref,
  attentionHref,
  connectionsHref,
}: {
  total: number
  healthy: number
  other: number
  attention: Application[]
  stalled: Application[]
  connections: { key: string; name: string; health: ConnectionHealth }[]
  health: Health
  now: number
  applicationsHref: string
  attentionHref: string
  connectionsHref: string
}) {
  const appSegments: Segment[] = [
    { label: "Healthy", count: healthy, color: "bg-success" },
    { label: "Attention", count: attention.length, color: "bg-warning" },
    { label: "Other", count: other, color: "bg-muted-foreground/40" },
  ]
  const connectionSegments: Segment[] = [
    {
      label: "Reachable",
      count: connections.filter((item) => item.health.level === "healthy")
        .length,
      color: "bg-success",
    },
    {
      label: "Down",
      count: connections.filter((item) => item.health.level === "critical")
        .length,
      color: "bg-destructive",
    },
    {
      label: "Degraded",
      count: connections.filter((item) => item.health.level === "warning")
        .length,
      color: "bg-warning",
    },
    {
      label: "Unknown",
      count: connections.filter((item) => item.health.level === "unknown")
        .length,
      color: "bg-muted-foreground/40",
    },
  ]
  const failedApps = attention.filter(
    (app) =>
      ["failed", "error"].includes(app.health) ||
      ["Degraded", "Missing"].includes(app.healthCondition?.status ?? "")
  )
  const failedIds = new Set(failedApps.map((app) => app.id))
  const paused = attention.filter(
    (app) => !failedIds.has(app.id) && app.autoSyncPaused
  ).length
  const attentionCount = attention.length + stalled.length
  const attentionSegments: Segment[] = [
    { label: "Failed", count: failedApps.length, color: "bg-destructive" },
    { label: "Paused", count: paused, color: "bg-focus" },
    {
      label: "Review",
      count: attentionCount - failedApps.length - paused,
      color: "bg-warning",
    },
  ]
  const missingConnections =
    health.failed.includes("clusters") || health.failed.includes("gitSources")
  const missingApprovals = health.failed.includes("approvals")
  const connectionProblem = connections.find(
    (item) => item.health.level !== "healthy"
  )
  const review = health.approvals[0]
  const expiry = review ? new Date(review.expiresAt) : null
  const approvalRatio =
    review && review.requiredApprovals > 0
      ? Math.min(
          100,
          (review.approvedApprovals / review.requiredApprovals) * 100
        )
      : 0

  return (
    <section
      aria-label="Workspace metrics"
      className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4"
    >
      <MetricCard
        title="Healthy applications"
        subtitle="Sync and runtime health"
        icon={Layers01Icon}
        value={total ? `${Math.round((healthy / total) * 100)}%` : "—"}
        suffix={`· ${healthy} of ${total}`}
        href={applicationsHref}
      >
        <Distribution segments={appSegments} ticks empty="No applications" />
        <Legend segments={appSegments} />
      </MetricCard>
      <MetricCard
        title="Connections"
        subtitle={
          !health.loaded
            ? "Checking connections…"
            : missingConnections
              ? "Some connections could not be loaded"
              : `${health.clusters.length} cluster${health.clusters.length === 1 ? "" : "s"} · ${health.gitSources.length} Git source${health.gitSources.length === 1 ? "" : "s"}`
        }
        icon={ServerStack01Icon}
        value={missingConnections ? "—" : String(connectionSegments[0].count)}
        suffix={missingConnections ? undefined : `of ${connections.length}`}
        loading={!health.loaded}
        href={connectionsHref}
        status={
          missingConnections
            ? { label: "Partial data", level: "warning" }
            : connectionProblem
              ? {
                  label:
                    connectionProblem.health.level === "critical"
                      ? "Needs attention"
                      : "Check status",
                  level: connectionProblem.health.level,
                }
              : undefined
        }
      >
        {!health.loaded ? (
          <Skeleton className="h-3.5 w-full motion-reduce:animate-none" />
        ) : missingConnections ? (
          <p className="text-xs text-muted-foreground">
            Status is unavailable for part of this workspace.
          </p>
        ) : (
          <>
            <Distribution segments={connectionSegments} />
            <Legend
              segments={connectionSegments.filter(
                (segment) => segment.count > 0 || segment.label === "Reachable"
              )}
            />
          </>
        )}
      </MetricCard>
      <MetricCard
        title="Needs attention"
        subtitle={
          stalled.length
            ? `${stalled.length} stalled checks`
            : "Drift, runtime problems, and pauses"
        }
        icon={Alert02Icon}
        value={String(attentionCount)}
        suffix={`of ${total}`}
        href={attentionHref}
        status={
          failedApps.length
            ? { label: "Failures", level: "critical" }
            : attentionCount
              ? { label: "Review needed", level: "warning" }
              : undefined
        }
      >
        <Distribution
          segments={attentionSegments}
          empty="No applications need attention"
        />
        <Legend segments={attentionSegments} />
      </MetricCard>
      <MetricCard
        title="Pending approvals"
        subtitle={
          !health.loaded
            ? "Loading review queue…"
            : review
              ? `Latest request ${ago(review.createdAt, now)}`
              : missingApprovals
                ? "Review queue unavailable"
                : "Plans waiting for review"
        }
        icon={Shield01Icon}
        value={missingApprovals ? "—" : String(health.approvalTotal)}
        loading={!health.loaded}
        href="/approvals"
        status={
          missingApprovals
            ? { label: "Unavailable", level: "warning" }
            : health.loaded && health.approvalTotal > 0
              ? { label: "Review needed", level: "warning" }
              : undefined
        }
      >
        {!health.loaded ? (
          <Skeleton className="h-8 w-full motion-reduce:animate-none" />
        ) : missingApprovals ? (
          <p className="text-xs text-muted-foreground">
            Could not load pending approvals.
          </p>
        ) : review ? (
          <>
            <div className="flex min-w-0 items-center justify-between gap-2 text-xs">
              <span className="truncate font-medium">
                {review.applicationName}
              </span>
              <span className="shrink-0 text-muted-foreground tabular-nums">
                {review.approvedApprovals} / {review.requiredApprovals}{" "}
                approvals
              </span>
            </div>
            <div
              role="img"
              aria-label={`${review.approvedApprovals} of ${review.requiredApprovals} approvals recorded for ${review.applicationName}`}
              className="h-1.5 overflow-hidden rounded-full bg-muted"
            >
              <div
                className="h-full rounded-full bg-primary"
                style={{ width: `${approvalRatio}%` }}
              />
            </div>
            <p className="text-xs text-muted-foreground">
              {expiry && Number.isFinite(expiry.getTime())
                ? `Expires ${expiry.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })}`
                : "Awaiting review"}
            </p>
          </>
        ) : (
          <>
            <Distribution
              segments={[]}
              empty={
                health.approvalTotal
                  ? "Review details unavailable"
                  : "No plans waiting for review"
              }
            />
            <p className="text-xs text-muted-foreground">
              {health.approvalTotal
                ? "Open the queue to review waiting plans"
                : "Nothing to review"}
            </p>
          </>
        )}
      </MetricCard>
    </section>
  )
}
