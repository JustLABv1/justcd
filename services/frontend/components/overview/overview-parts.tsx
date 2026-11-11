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

type SummaryProps = {
  total: number
  healthy: number
  attention: number
  approvals: number
  syncRange?: "24h" | "7d"
  syncs: { total: number; successRate: number | null; series: number[] } | null
  hrefs: { healthy: string; attention: string; approvals: string; syncs: string }
}

/** The four numbers that matter first, each with an icon, a caption or a sparkline so the page has something to look at. */
export function SummaryKpis({ total, healthy, attention, approvals, syncs, syncRange = "24h", hrefs }: SummaryProps) {
  const tile = (href: string, icon: typeof Task01Icon, tone: "good" | "bad" | "neutral", title: string, value: ReactNode, caption: ReactNode, chart?: ReactNode) => (
    <Kpi surface render={<Link href={href} />} className="h-full gap-3 outline-none transition-shadow hover:shadow-md focus-visible:ring-2 focus-visible:ring-ring">
      <Kpi.Header><Kpi.Icon tone={tone}><HugeiconsIcon icon={icon} strokeWidth={1.8} /></Kpi.Icon><Kpi.Title>{title}</Kpi.Title></Kpi.Header>
      <Kpi.Content layout={chart ? "inline" : "below"}>
        <Kpi.Stat className="w-full"><Kpi.Value>{value}</Kpi.Value><Kpi.Footer className="w-full">{caption}</Kpi.Footer></Kpi.Stat>
        {chart}
      </Kpi.Content>
    </Kpi>
  )
  const showSeries = Boolean(syncs && syncs.series.length > 1 && syncs.series.some((value) => value > 0))
  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      {tile(hrefs.healthy, CheckmarkCircle02Icon, total > 0 && healthy === total ? "good" : "neutral", "Healthy", healthy, <span className="flex w-full items-center gap-3"><span className="shrink-0">of {total}</span><Kpi.Progress className="min-w-0 flex-1" value={healthy} maxValue={Math.max(total, 1)} color={total > 0 && healthy === total ? "success" : healthy === 0 && total > 0 ? "destructive" : "warning"} aria-label={`${healthy} of ${total} applications healthy`} /></span>)}
      {tile(hrefs.attention, Alert02Icon, attention > 0 ? "bad" : "good", "Needs attention", attention, attention > 0 ? "blocking delivery" : "all clear")}
      {tile(hrefs.approvals, Task01Icon, approvals > 0 ? "bad" : "neutral", "Awaiting approval", approvals, approvals > 0 ? "plans waiting" : "none waiting")}
      {tile(hrefs.syncs, Rocket01Icon, "neutral", `Syncs, last ${syncRange === "7d" ? "7 days" : "24 h"}`, syncs?.total ?? "–", syncs?.successRate != null ? `${syncs.successRate.toFixed(0)}% succeeded` : "No runs yet",
        showSeries && syncs ? <Kpi.Chart data={syncs.series} color="primary" filled inline height={40} style={{ width: 96 }} aria-hidden="true" /> : undefined)}
    </div>
  )
}
