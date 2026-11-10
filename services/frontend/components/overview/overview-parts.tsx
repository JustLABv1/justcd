"use client"

import Link from "next/link"
import type { ReactNode } from "react"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Frame } from "@/components/panelui/frame"
import { Kpi } from "@/components/panelui/kpi"
import { HugeiconsIcon } from "@hugeicons/react"
import { Alert02Icon, CheckmarkCircle02Icon, Rocket01Icon, Task01Icon } from "@hugeicons/core-free-icons"
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

/** A KPI card that links to the list behind its number. */
export function KpiLink({ href, children }: { href: string; children: ReactNode }) {
  return (
    <Link href={href} className="block rounded-2xl outline-none transition-shadow hover:shadow-md focus-visible:ring-2 focus-visible:ring-ring">
      <Kpi surface className="h-full">{children}</Kpi>
    </Link>
  )
}

type SummaryProps = {
  total: number
  healthy: number
  attention: number
  approvals: number
  syncs: { total: number; successRate: number | null; series: number[] } | null
  hrefs: { healthy: string; attention: string; approvals: string; syncs: string }
}

/** The four numbers that matter first, each with an icon, a bar or a sparkline so the page has something to look at. */
export function SummaryKpis({ total, healthy, attention, approvals, syncs, hrefs }: SummaryProps) {
  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <KpiLink href={hrefs.healthy}>
        <Kpi.Header>
          <Kpi.Icon tone={total > 0 && healthy === total ? "good" : "neutral"}><HugeiconsIcon icon={CheckmarkCircle02Icon} strokeWidth={1.8} /></Kpi.Icon>
          <Kpi.Title>Healthy</Kpi.Title>
        </Kpi.Header>
        <Kpi.Content><Kpi.Value>{healthy}<span className="ml-1 text-base font-medium text-muted-foreground">of {total}</span></Kpi.Value></Kpi.Content>
        <Kpi.Progress value={healthy} maxValue={Math.max(total, 1)} color="success" aria-label={`${healthy} of ${total} applications healthy`} />
      </KpiLink>
      <KpiLink href={hrefs.attention}>
        <Kpi.Header>
          <Kpi.Icon tone={attention > 0 ? "bad" : "good"}><HugeiconsIcon icon={Alert02Icon} strokeWidth={1.8} /></Kpi.Icon>
          <Kpi.Title>Needs attention</Kpi.Title>
        </Kpi.Header>
        <Kpi.Content><Kpi.Value>{attention}</Kpi.Value></Kpi.Content>
        <Kpi.Footer>{attention > 0 ? "Review what is blocking delivery" : "Nothing is blocking delivery"}</Kpi.Footer>
      </KpiLink>
      <KpiLink href={hrefs.approvals}>
        <Kpi.Header>
          <Kpi.Icon tone={approvals > 0 ? "bad" : "neutral"}><HugeiconsIcon icon={Task01Icon} strokeWidth={1.8} /></Kpi.Icon>
          <Kpi.Title>Awaiting approval</Kpi.Title>
        </Kpi.Header>
        <Kpi.Content><Kpi.Value>{approvals}</Kpi.Value></Kpi.Content>
        <Kpi.Footer>{approvals > 0 ? "Plans waiting for a decision" : "No plans waiting"}</Kpi.Footer>
      </KpiLink>
      <KpiLink href={hrefs.syncs}>
        <Kpi.Header>
          <Kpi.Icon tone="neutral"><HugeiconsIcon icon={Rocket01Icon} strokeWidth={1.8} /></Kpi.Icon>
          <Kpi.Title>Syncs, last 24 h</Kpi.Title>
        </Kpi.Header>
        <Kpi.Content layout="inline">
          <Kpi.Stat><Kpi.Value>{syncs?.total ?? "–"}</Kpi.Value></Kpi.Stat>
          {syncs && syncs.series.length > 1 && <Kpi.Chart data={syncs.series} color="primary" filled inline aria-hidden="true" />}
        </Kpi.Content>
        <Kpi.Footer>{syncs?.successRate != null ? `${syncs.successRate.toFixed(0)}% succeeded` : "No runs yet"}</Kpi.Footer>
      </KpiLink>
    </div>
  )
}
