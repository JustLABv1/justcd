"use client"

import { useCallback, useEffect, useState } from "react"
import { Flow, type FlowColor } from "@/components/panelui/flow"
import { Spinner } from "@/components/panelui/spinner"
import { HugeiconsIcon } from "@hugeicons/react"
import { Cancel01Icon, Clock01Icon, MinusSignIcon, Tick02Icon } from "@hugeicons/core-free-icons"
import { cn } from "@/lib/utils"
import type { RunProgress, RunStage, StageStatus } from "@/lib/run-progress"

const statusLabels: Record<StageStatus, string> = {
  pending: "Not started",
  queued: "Waiting",
  running: "Running",
  complete: "Done",
  failed: "Failed",
  cancelled: "Cancelled",
  skipped: "Skipped",
}

const nodeWidth = 184
const nodeHeight = 56
const columnGap = 32
const rowGap = 28
const pad = 12

/** Time left until a deadline, as a short phrase. */
function remaining(expiresAt: string, now: number) {
  const left = Date.parse(expiresAt) - now
  if (left <= 0) return null
  const minutes = Math.ceil(left / 60000)
  if (minutes < 60) return `${minutes}m left`
  const hours = Math.floor(minutes / 60)
  return `${hours}h ${minutes % 60}m left`
}

/** The icon tile on the left of a node. A running stage spins; the others state their outcome. */
function StageIcon({ status, deadline }: { status: StageStatus; deadline?: "open" | "soon" | "over" }) {
  const tone = deadline
    ? deadline === "over" ? "bg-destructive/15 text-destructive" : deadline === "soon" ? "bg-warning/15 text-warning-foreground dark:text-warning" : "bg-info/15 text-info"
    : status === "complete"
      ? "bg-success/15 text-success"
      : status === "running"
        ? "bg-primary/15 text-primary"
        : status === "failed" || status === "cancelled"
          ? "bg-destructive/15 text-destructive"
          : "bg-muted text-muted-foreground"
  return (
    <span aria-hidden="true" className={cn("grid size-8 shrink-0 place-items-center rounded-lg", tone)}>
      {deadline ? <HugeiconsIcon icon={Clock01Icon} strokeWidth={2} className="size-4" />
        : status === "running" ? <Spinner className="size-4 motion-reduce:animate-none" />
        : status === "complete" ? <HugeiconsIcon icon={Tick02Icon} strokeWidth={2.5} className="size-4" />
        : status === "failed" || status === "cancelled" ? <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2.5} className="size-4" />
        : status === "skipped" ? <HugeiconsIcon icon={MinusSignIcon} strokeWidth={2.5} className="size-4" />
        : <span className="size-2 rounded-full bg-current opacity-60" />}
    </span>
  )
}

/** Colour and motion of the line that leads into `next`, so the flow reads as progress travelling forward. */
function edgeFor(previous: RunStage, next: RunStage): { color?: FlowColor; animated: boolean; dashed: boolean } {
  if (next.status === "failed" || next.status === "cancelled") return { color: "destructive", animated: false, dashed: false }
  if (previous.status === "complete" && next.status === "complete") return { color: "success", animated: false, dashed: false }
  if (previous.status === "complete" && (next.status === "running" || next.status === "queued")) return { color: "primary", animated: true, dashed: true }
  if (previous.status === "complete") return { color: "success", animated: false, dashed: false }
  return { animated: false, dashed: true }
}

/** What a stage says under its title: the outcome, then the time it happened. */
function stageCaption(stage: RunStage, now: number) {
  if (stage.caption) return stage.caption
  if (stage.expiresAt) {
    const left = remaining(stage.expiresAt, now)
    return left ?? "Expired"
  }
  const time = stage.time ? new Date(stage.time).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" }) : null
  return [statusLabels[stage.status], time].filter(Boolean).join(" · ")
}

/**
 * Read-only backend progress drawn as a flow of stage nodes. The stage being worked on is
 * highlighted and the line leading into it animates. Narrow containers stack the stages.
 */
export function RunProgressStages({ progress, label, onStagePress, pressable = [] }: { progress: RunProgress; label: string; onStagePress?: (id: string) => void; /** Stages that open details when pressed. */ pressable?: string[] }) {
  const { stages } = progress
  const [now, setNow] = useState(() => Date.now())
  const hasDeadline = stages.some((stage) => stage.expiresAt)
  useEffect(() => {
    if (!hasDeadline) return
    const timer = window.setInterval(() => setNow(Date.now()), 15000)
    return () => window.clearInterval(timer)
  }, [hasDeadline])
  const deadlineState = (stage: RunStage) => !stage.expiresAt ? undefined : Date.parse(stage.expiresAt) <= now ? "over" as const : Date.parse(stage.expiresAt) - now < 5 * 60000 ? "soon" as const : "open" as const
  const [width, setWidth] = useState(0)
  const watch = useCallback((node: HTMLElement | null) => {
    if (!node) return
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width))
    observer.observe(node)
    return () => observer.disconnect()
  }, [])
  const horizontalWidth = stages.length * nodeWidth + (stages.length - 1) * columnGap + pad * 2
  // Row when the stages fit without shrinking much, otherwise a column.
  const wide = width === 0 || width >= horizontalWidth * 0.75
  const place = (index: number) => wide
    ? { x: pad + index * (nodeWidth + columnGap), y: pad }
    : { x: pad, y: pad + index * (nodeHeight + rowGap) }
  const height = wide ? nodeHeight + pad * 2 : stages.length * nodeHeight + (stages.length - 1) * rowGap + pad * 2
  return <div ref={watch} className="min-w-0">
    <div className="overflow-hidden rounded-2xl" style={{ height }}>
      <Flow
        key={`${wide}-${stages.length}`}
        fitViewOnMount
        fitViewPadding={0}
        maxZoom={1}
        panOnDrag={false}
        zoomOnPinch={false}
        label={label}
        className="h-full cursor-default border-0 bg-transparent active:cursor-default"
      >
        {/* A quiet dot grid marks this as a pipeline. No spotlight: the strip cannot be panned. */}
        <Flow.Background variant="dots" gap={16} className="size-full text-muted-foreground/30" />
        {stages.map((stage, index) => <Flow.Node
          key={stage.id}
          id={stage.id}
          position={place(index)}
          draggable={false}
          onPress={onStagePress && pressable.includes(stage.id) ? () => onStagePress(stage.id) : undefined}
          selected={stage.status === "running" || (onStagePress && pressable.includes(stage.id) && stage.status === "queued")}
          data-stage-status={stage.status}
          style={{ width: nodeWidth, height: nodeHeight }}
          className={cn(
            (onStagePress && pressable.includes(stage.id) ? "group cursor-pointer hover:border-primary/50" : "cursor-default active:cursor-default") + " flex items-center gap-2.5 px-2.5 transition-[opacity,box-shadow,border-color] duration-300",
            (stage.status === "pending" || stage.status === "skipped") && "opacity-60",
            (stage.status === "failed" || stage.status === "cancelled" || deadlineState(stage) === "over") && "border-destructive/50",
            stage.status === "running" && "border-primary/50 shadow-md motion-safe:animate-pulse"
          )}
        >
          <StageIcon status={stage.status} deadline={deadlineState(stage)} />
          <div className="min-w-0">
            <Flow.NodeTitle className="whitespace-nowrap text-sm">{stage.title}</Flow.NodeTitle>
            <Flow.NodeDescription className="truncate text-xs">{stage.action && onStagePress ? <span className="font-medium text-primary">{stage.action}</span> : onStagePress && stage.status === "failed" && pressable.includes(stage.id) ? <span className="font-medium text-destructive">Click to view error</span> : onStagePress && pressable.includes(stage.id) ? <>
              <span className="group-hover:hidden group-focus-visible:hidden">{stageCaption(stage, now)}</span>
              <span className="hidden font-medium text-primary group-hover:inline group-focus-visible:inline">{stage.id === "approval" && stage.status === "queued" ? "Click to approve" : "Click for details"}</span>
            </> : stageCaption(stage, now)}</Flow.NodeDescription>
          </div>
        </Flow.Node>)}
        {stages.slice(1).map((stage, index) => <Flow.Edge
          key={stage.id}
          from={stages[index].id}
          to={stage.id}
          variant="smoothstep"
          gap={10}
          arrow
          fromSide={wide ? "right" : "bottom"}
          toSide={wide ? "left" : "top"}
          {...(stage.expiresAt ? { color: deadlineState(stage) === "over" ? "destructive" as const : "info" as const, animated: deadlineState(stage) !== "over", dashed: true } : edgeFor(stages[index], stage))}
        />)}
      </Flow>
    </div>
    <ol aria-label={label} className="sr-only">
      {stages.map((stage) => <li key={stage.id}>
        {stage.title}: {stage.expiresAt ? stageCaption(stage, now) : statusLabels[stage.status]}
        {stage.time ? `, ${new Date(stage.time).toLocaleTimeString()}` : ""}
        {stage.details.map((detail) => `, ${detail.label} ${detail.value}`).join("")}
      </li>)}
    </ol>
  </div>
}
