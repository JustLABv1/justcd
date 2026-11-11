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
      <Kpi surface className="h-full flex-row items-center gap-3 !rounded-2xl !p-3.5">{children}</Kpi>
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
  const tile = (icon: typeof Task01Icon, tone: "good" | "bad" | "neutral", title: string, value: ReactNode, hint?: ReactNode, trailing?: ReactNode) => <>
    <Kpi.Icon tone={tone}><HugeiconsIcon icon={icon} strokeWidth={1.8} /></Kpi.Icon>
    <div className="min-w-0 flex-1">
      <Kpi.Title>{title}</Kpi.Title>
      <div className="flex items-baseline gap-2"><Kpi.Value className="text-xl leading-7">{value}</Kpi.Value>{hint && <span className="truncate text-xs text-muted-foreground">{hint}</span>}</div>
    </div>
    {trailing}
  </>
  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <KpiLink href={hrefs.healthy}>
        {tile(CheckmarkCircle02Icon, total > 0 && healthy === total ? "good" : "neutral", "Healthy", healthy, `of ${total}`)}
      </KpiLink>
      <KpiLink href={hrefs.attention}>
        {tile(Alert02Icon, attention > 0 ? "bad" : "good", "Needs attention", attention, attention > 0 ? "blocking delivery" : "all clear")}
      </KpiLink>
      <KpiLink href={hrefs.approvals}>
        {tile(Task01Icon, approvals > 0 ? "bad" : "neutral", "Awaiting approval", approvals, approvals > 0 ? "plans waiting" : "none waiting")}
      </KpiLink>
      <KpiLink href={hrefs.syncs}>
        {tile(Rocket01Icon, "neutral", "Syncs, last 24 h", syncs?.total ?? "–", syncs?.successRate != null ? `${syncs.successRate.toFixed(0)}% succeeded` : "No runs yet",
          syncs && syncs.series.length > 1 && syncs.series.some((value) => value > 0) ? <Kpi.Chart data={syncs.series} color="primary" filled inline height={28} style={{ width: 72 }} aria-hidden="true" /> : null)}
      </KpiLink>
    </div>
  )
}
