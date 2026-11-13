"use client"

import Link from "next/link"
import type { ReactNode } from "react"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Frame } from "@/components/panelui/frame"
import { Kpi } from "@/components/panelui/kpi"
import { HugeiconsIcon } from "@hugeicons/react"
import { Alert02Icon, Clock01Icon, Layers01Icon, Rocket01Icon, Task01Icon } from "@hugeicons/core-free-icons"
import { RollingNumber } from "@/components/panelui/rolling-number"
import { Section } from "@/components/section"
import { Tabs } from "@/components/panelui/tabs"
import { Skeleton } from "@/components/ui/skeleton"
import { failureRate, type DeliveryMetrics } from "@/hooks/use-delivery-metrics"
import { statusTone, type StatusTone } from "@/components/ui-kit"

const toneVariant = { success: "success", destructive: "destructive", warning: "warning", neutral: "secondary" } as const

/** A free-form status string (sync, health, check result) as a tinted PanelUI badge. */
export function StatusPill({ status, tone }: { status: string; tone?: StatusTone }) {
  return <Badge variant={toneVariant[tone ?? statusTone(status)]} size="sm" dot className="capitalize">{status.toLowerCase().replaceAll("_", " ")}</Badge>
}

/** A frame row that navigates with the Next.js router instead of a full page load. */
export function LinkRow({ href, children }: { href: string; children: ReactNode }) {
  return (
    <Frame.Row
      render={<Link href={href} />}
      chevron
      className="cursor-pointer outline-none transition-colors hover:bg-muted/60 focus-visible:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
    >
      {children}
    </Frame.Row>
  )
}

/** "View all" style link that sits in a frame header. */
export function FrameLink({ href, children }: { href: string; children: ReactNode }) {
  return <Button render={<Link href={href} />} nativeButton={false} variant="outline" size="sm" className="bg-background">{children}</Button>
}

type SummaryProps = {
  total: number
  healthy: number
  attention: number
  /** Attention includes something critical or high: draw it red rather than yellow. */
  urgent: boolean
  approvals: number
  failedSyncs: { failed: number; total: number; label: string; series: number[] } | null
  hrefs: { applications: string; approvals: string; syncs: string }
}

/** Wraps a Kpi as a link to the place where its number is explained. */
function KpiLink({ href, children }: { href: string; children: ReactNode }) {
  return <Kpi surface render={<Link href={href} />} className="h-full gap-3 outline-none transition-shadow hover:shadow-md focus-visible:ring-2 focus-visible:ring-ring">{children}</Kpi>
}

/**
 * Healthy, needing attention and everything else as one bar. Healthy and "needs attention" are
 * complements of the same total, so they are one number with its split, not two tiles.
 */
function SplitBar({ total, parts }: { total: number; parts: { value: number; className: string; label: string }[] }) {
  return <div role="img" aria-label={parts.map((part) => `${part.value} ${part.label}`).join(", ")} className="flex h-1.5 w-full gap-0.5 overflow-hidden rounded-full bg-muted">
    {parts.filter((part) => part.value > 0).map((part) => <span key={part.label} className={`h-full ${part.className}`} style={{ width: `${(part.value / Math.max(total, 1)) * 100}%` }} />)}
  </div>
}

/** The three numbers that need a person: application health, decisions waiting, and runs that failed. */
export function SummaryKpis({ total, healthy, attention, urgent, approvals, failedSyncs, hrefs }: SummaryProps) {
  const other = Math.max(0, total - healthy - attention)
  const attentionTone = urgent ? "bg-destructive" : "bg-warning"
  return (
    <div className="grid gap-3 sm:grid-cols-3">
      <KpiLink href={hrefs.applications}>
        <Kpi.Header><Kpi.Icon tone={attention ? "bad" : total ? "good" : "neutral"}><HugeiconsIcon icon={Layers01Icon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>Applications</Kpi.Title></Kpi.Header>
        <Kpi.Content><Kpi.Value><RollingNumber value={healthy} /><span className="ml-1 text-base font-medium text-muted-foreground">of {total} healthy</span></Kpi.Value></Kpi.Content>
        <SplitBar total={total} parts={[
          { value: healthy, className: "bg-success", label: "healthy" },
          { value: attention, className: attentionTone, label: "need attention" },
          { value: other, className: "bg-muted-foreground/40", label: "other" },
        ]} />
        <Kpi.Footer className={attention ? (urgent ? "text-destructive" : "text-warning-foreground dark:text-warning") : undefined}>
          {attention ? `${attention} need${attention === 1 ? "s" : ""} attention` : total ? "All healthy" : "No applications yet"}{other ? ` · ${other} other` : ""}
        </Kpi.Footer>
      </KpiLink>
      <KpiLink href={hrefs.approvals}>
        {/* A decision waiting on a person is yellow, never red. */}
        <Kpi.Header><Kpi.Icon className={approvals ? "bg-warning/15 text-warning" : undefined}><HugeiconsIcon icon={Task01Icon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>Awaiting approval</Kpi.Title></Kpi.Header>
        <Kpi.Content><Kpi.Value><RollingNumber value={approvals} /></Kpi.Value></Kpi.Content>
        <Kpi.Footer>{approvals ? `plan${approvals === 1 ? "" : "s"} waiting for review` : "Nothing waiting"}</Kpi.Footer>
      </KpiLink>
      <KpiLink href={hrefs.syncs}>
        <Kpi.Header><Kpi.Icon tone={failedSyncs?.failed ? "bad" : "neutral"}><HugeiconsIcon icon={Alert02Icon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>Failed syncs, {failedSyncs?.label ?? "last 24 h"}</Kpi.Title></Kpi.Header>
        <Kpi.Content layout={failedSyncs?.failed ? "inline" : "below"}>
          <Kpi.Stat className="w-full">
            <Kpi.Value>{failedSyncs ? <RollingNumber value={failedSyncs.failed} /> : "–"}</Kpi.Value>
            <Kpi.Footer className="w-full">{failedSyncs ? failedSyncs.total ? `of ${failedSyncs.total} run${failedSyncs.total === 1 ? "" : "s"}` : "No runs" : "Loading…"}</Kpi.Footer>
          </Kpi.Stat>
          {failedSyncs && failedSyncs.failed > 0 && failedSyncs.series.length > 1 && <Kpi.Chart data={failedSyncs.series} color="destructive" filled inline height={40} style={{ width: 96 }} aria-hidden="true" />}
        </Kpi.Content>
      </KpiLink>
    </div>
  )
}

function duration(seconds: number) {
  if (seconds < 60) return `${Math.round(seconds)}s`
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `${hours}h ${minutes % 60}m`
  return `${Math.round(hours / 24)}d`
}

/** Relative change; null when there is nothing to compare with. */
const change = (current: number, previous: number) => previous ? ((current - previous) / previous) * 100 : null

/**
 * How delivery is going over time: deploy frequency, failure rate and time to restore
 * (three of the four DORA measures), each against the period before.
 */
export function DeliverySection({ metrics, error, loading, range, onRangeChange, applicationHref, description }: {
  metrics?: DeliveryMetrics
  error?: unknown
  loading: boolean
  range: "7d" | "30d"
  onRangeChange: (range: "7d" | "30d") => void
  /** Omit for one application's own measures: there is no "most failures" list to link from. */
  applicationHref?: (id: string) => string
  description?: string
}) {
  const days = range === "30d" ? 30 : 7
  const caption = `vs previous ${days} days`
  const current = metrics?.current
  const previous = metrics?.previous
  const rate = current ? failureRate(current) : null
  const previousRate = previous ? failureRate(previous) : null
  const deployChange = current && previous ? change(current.deployments, previous.deployments) : null
  const restoreChange = current?.restoreSeconds != null && previous?.restoreSeconds != null ? change(current.restoreSeconds, previous.restoreSeconds) : null
  const pending = <Skeleton className="h-8 w-20 motion-reduce:animate-none" />
  return (
    <Section title="Delivery" description={description ?? `The last ${days} days compared with the ${days} before.`} action={
      <Tabs value={range} onValueChange={(value) => { if (value === "7d" || value === "30d") onRangeChange(value) }}>
        <Tabs.List aria-label="Delivery period"><Tabs.Trigger value="7d">7 days</Tabs.Trigger><Tabs.Trigger value="30d">30 days</Tabs.Trigger></Tabs.List>
      </Tabs>
    }>
      {error ? <p role="status" className="text-sm text-muted-foreground">Delivery measures could not be loaded.</p> : (
        <div className={`grid gap-3 ${applicationHref ? "lg:grid-cols-[repeat(3,minmax(0,1fr))_minmax(0,1.2fr)]" : "sm:grid-cols-3"}`}>
          <Kpi surface className="h-full gap-3">
            <Kpi.Header><Kpi.Icon><HugeiconsIcon icon={Rocket01Icon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>Deployments per day</Kpi.Title></Kpi.Header>
            <Kpi.Content>{loading || !current ? pending : <Kpi.Value>{(current.deployments / days).toFixed(1)}</Kpi.Value>}</Kpi.Content>
            <Kpi.Footer>{current ? <>{deployChange != null ? <Kpi.Trend value={deployChange} threshold={1} caption={caption} /> : `${current.deployments} in ${days} days`}</> : null}</Kpi.Footer>
          </Kpi>
          <Kpi surface className="h-full gap-3">
            <Kpi.Header><Kpi.Icon tone={rate ? "bad" : "neutral"}><HugeiconsIcon icon={Alert02Icon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>Failure rate</Kpi.Title></Kpi.Header>
            <Kpi.Content>{loading || !current ? pending : <Kpi.Value>{rate == null ? "–" : `${(rate * 100).toFixed(rate < 0.1 && rate > 0 ? 1 : 0)}%`}</Kpi.Value>}</Kpi.Content>
            <Kpi.Footer>{current ? rate != null && previousRate != null
              ? <Kpi.Trend value={(rate - previousRate) * 100} goodDirection="down" threshold={0.5} format={(value) => `${value > 0 ? "+" : value < 0 ? "−" : ""}${Math.abs(value).toFixed(1)} pts`} caption={caption} />
              : rate == null ? "No finished runs" : `${current.failed} of ${current.deployments + current.failed} runs failed` : null}</Kpi.Footer>
          </Kpi>
          <Kpi surface className="h-full gap-3">
            <Kpi.Header><Kpi.Icon><HugeiconsIcon icon={Clock01Icon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>Time to restore</Kpi.Title></Kpi.Header>
            <Kpi.Content>{loading || !current ? pending : <Kpi.Value>{current.restoreSeconds == null ? "–" : duration(current.restoreSeconds)}</Kpi.Value>}</Kpi.Content>
            <Kpi.Footer>{current ? restoreChange != null
              ? <Kpi.Trend value={restoreChange} goodDirection="down" threshold={5} caption={caption} />
              : current.restored ? `median of ${current.restored} recover${current.restored === 1 ? "y" : "ies"}` : current.failed ? "No failure fixed yet" : "No failures" : null}</Kpi.Footer>
          </Kpi>
          {applicationHref && <Frame>
            <Frame.Header><Frame.Title as="h3">Most failures</Frame.Title></Frame.Header>
            <Frame.Panel>
              {loading || !metrics ? <p role="status" className="px-4 py-4 text-sm text-muted-foreground">Loading…</p>
                : metrics.failuresByApplication.length ? metrics.failuresByApplication.slice(0, 3).map((item) => (
                  <LinkRow key={item.applicationId} href={applicationHref(item.applicationId)}>
                    <Frame.Content>
                      <Frame.Title as="p">{item.name}</Frame.Title>
                      <Frame.Description>{item.failed} of {item.total} run{item.total === 1 ? "" : "s"} failed</Frame.Description>
                    </Frame.Content>
                  </LinkRow>
                )) : <p className="px-4 py-4 text-sm text-muted-foreground">No failed runs in the last {days} days.</p>}
            </Frame.Panel>
          </Frame>}
        </div>
      )}
    </Section>
  )
}
