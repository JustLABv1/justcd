"use client"

import { useEffect, useRef, useState } from "react"
import { Badge, type BadgeProps } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Steps } from "@/components/panelui/steps"
import { HugeiconsIcon } from "@hugeicons/react"
import { Cancel01Icon, MinusSignIcon } from "@hugeicons/core-free-icons"
import type { RunProgress, StageStatus } from "@/lib/run-progress"

const statusLabels: Record<StageStatus, string> = {
  pending: "Not started",
  queued: "Waiting",
  running: "Running",
  complete: "Done",
  failed: "Failed",
  cancelled: "Cancelled",
  skipped: "Skipped",
}
const statusVariants: Record<StageStatus, BadgeProps["variant"]> = {
  pending: "secondary",
  queued: "secondary",
  running: "info",
  complete: "success",
  failed: "destructive",
  cancelled: "warning",
  skipped: "secondary",
}


/** Read-only backend progress. Native horizontal scrolling keeps every stage inspectable. */
export function RunProgressStages({ progress, label }: { progress: RunProgress; label: string }) {
  const { current, stages } = progress
  const viewport = useRef<HTMLDivElement>(null)
  const [edges, setEdges] = useState({ prev: false, next: false })
  useEffect(() => {
    const element = viewport.current
    if (!element) return
    const sync = () => setEdges({ prev: element.scrollLeft > 1, next: element.scrollLeft + element.clientWidth < element.scrollWidth - 1 })
    sync()
    element.addEventListener("scroll", sync, { passive: true })
    const resize = new ResizeObserver(sync)
    resize.observe(element)
    return () => { element.removeEventListener("scroll", sync); resize.disconnect() }
  }, [stages.length])
  useEffect(() => {
    const element = viewport.current
    const active = element?.querySelector<HTMLElement>(`[data-run-step="${current}"]`)
    if (element && active) element.scrollTo({ left: Math.max(0, active.offsetLeft - element.offsetLeft - 16), behavior: "instant" })
  }, [current])
  const move = (direction: number) => viewport.current?.scrollBy({ left: direction * viewport.current.clientWidth * 0.75, behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth" })
  return <div className="min-w-0 space-y-3">
    <div ref={viewport} role="region" aria-label={label} tabIndex={0} className="overflow-x-auto rounded-lg py-1 outline-none focus-visible:ring-2 focus-visible:ring-ring">
      <Steps value={current} className="min-w-max" aria-label={label}>
        {stages.map((stage, index) => <Steps.Item key={stage.id} step={index + 1} completed={stage.status === "complete"} loading={stage.status === "running"} data-run-step={index + 1} className="w-52 shrink-0 items-start">
          <div className="flex items-center gap-2">
            <Steps.Indicator>{stage.status === "failed" || stage.status === "cancelled" ? <HugeiconsIcon icon={Cancel01Icon} className="size-4 text-destructive" /> : stage.status === "skipped" ? <HugeiconsIcon icon={MinusSignIcon} className="size-4" /> : undefined}</Steps.Indicator>
            <Steps.Title>{stage.title}</Steps.Title>
          </div>
          <dl className="mt-3 space-y-2 pr-5 text-xs">
            <div className="flex items-center justify-between gap-3"><dt className="text-muted-foreground">Status</dt><dd><Badge size="sm" variant={statusVariants[stage.status]}>{statusLabels[stage.status]}</Badge></dd></div>
            {stage.time && <div className="flex justify-between gap-3"><dt className="text-muted-foreground">Time</dt><dd><time dateTime={stage.time}>{new Date(stage.time).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}</time></dd></div>}
            {stage.details.map((detail) => <div key={detail.label} className="flex items-baseline justify-between gap-3"><dt className="text-muted-foreground">{detail.label}</dt><dd className="min-w-0 break-words text-right">{detail.value}</dd></div>)}
          </dl>
        </Steps.Item>)}
      </Steps>
    </div>
    {(edges.prev || edges.next) && <div className="flex justify-end gap-2"><Button variant="outline" size="sm" disabled={!edges.prev} onClick={() => move(-1)}>Earlier stages</Button><Button variant="outline" size="sm" disabled={!edges.next} onClick={() => move(1)}>Later stages</Button></div>}
  </div>
}
