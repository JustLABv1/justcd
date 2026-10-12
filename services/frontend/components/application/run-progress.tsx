"use client"

import { useEffect, useRef, useState, type PointerEvent } from "react"
import { Badge, type BadgeProps } from "@/components/reui/badge"
import {
  Stepper,
  StepperIndicator,
  StepperItem,
  StepperNav,
  StepperSeparator,
  StepperTitle,
} from "@/components/reui/stepper"
import { cn } from "cn"

import { Button } from "@/components/ui/button"
import {
  Carousel,
  CarouselContent,
  CarouselItem,
  type CarouselApi,
} from "@/components/ui/carousel"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  Cancel01Icon,
  CircleDashedIcon,
  MinusSignIcon,
  Tick02Icon,
} from "@hugeicons/core-free-icons"

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
  running: "info-light",
  complete: "success-light",
  failed: "destructive-light",
  cancelled: "warning-light",
  skipped: "secondary",
}

function Pending({ spin = false }: { spin?: boolean }) {
  return (
    <span className="flex size-5.5 items-center justify-center rounded-full bg-ring/80 text-primary-foreground">
      <HugeiconsIcon
        icon={CircleDashedIcon}
        strokeWidth={2}
        className={cn(
          "size-3.5",
          spin && "animate-spin motion-reduce:animate-none"
        )}
        aria-hidden="true"
      />
    </span>
  )
}

function Skipped() {
  return (
    <span className="flex size-5.5 items-center justify-center rounded-full bg-muted text-muted-foreground">
      <HugeiconsIcon
        icon={MinusSignIcon}
        strokeWidth={2}
        className="size-3.5"
        aria-hidden="true"
      />
    </span>
  )
}

// The 26px ring stays transparent and the 22px dot inside it carries the
// state color, so the 1px gap between them shows whatever surface is behind.
const indicators = {
  completed: (
    <span className="flex size-5.5 items-center justify-center rounded-full bg-success text-primary-foreground">
      <HugeiconsIcon
        icon={Tick02Icon}
        strokeWidth={2}
        className="size-3.5"
        aria-hidden="true"
      />
    </span>
  ),
  inactive: <Pending />,
  loading: <Pending spin />,
}

// A flex item with no text has its baseline at its bottom edge, so in an
// items-baseline row this dashed rule lands on the text baseline.
function Leader({ neutral = false }: { neutral?: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "min-w-2 flex-1 border-b border-dashed",
        neutral ? "border-muted-foreground/30" : "border-ring/30"
      )}
    />
  )
}

// 4px dots spaced edge to edge by mask-repeat: space, so a connector at least
// one tile wide never shows a clipped dot. Tiles are in rem, like the box.
const dots =
  "m-0 bg-transparent bg-linear-to-r mask-[radial-gradient(circle_closest-side,#000_90%,transparent)] mask-size-[0.4375rem_0.25rem] mask-repeat-space rtl:bg-linear-to-l group-data-[orientation=horizontal]/stepper-nav:h-1"

// Matches the fade width: a mouse resting this close to a faded edge
// steps the row that way, one stage per beat.
const EDGE_PX = 40
const STEP_MS = 600

const reducedMotion = () =>
  window.matchMedia("(prefers-reduced-motion: reduce)").matches

// Adapted from ReUI c-stepper-18. Backend state owns progress; the row only
// scrolls for inspection and never advances or reapplies a deployment.
export function RunProgressStages({
  progress,
  label,
}: {
  progress: RunProgress
  label: string
}) {
  const { current, stages } = progress
  const [api, setApi] = useState<CarouselApi>()
  const [edges, setEdges] = useState({ prev: false, next: false })
  const activeIndex = Math.min(current, stages.length) - 1
  const hover = useRef({ dir: 0, timer: 0 })
  const previousButton = useRef<HTMLButtonElement>(null)
  const nextButton = useRef<HTMLButtonElement>(null)
  const activeStatus = stages[current - 1]?.status
  const stopped = activeStatus === "failed" || activeStatus === "cancelled"
  const carouselOptions = {
    align: "start",
    dragFree: true,
    inViewThreshold: 1,
    startIndex: activeIndex,
  } as const

  // Each faded edge shows only while there is more row past it, so a
  // row that fits renders as a plain stepper: no fade, nothing to drag.
  useEffect(() => {
    if (!api) return
    const sync = () => {
      if (
        !api.canScrollPrev() &&
        document.activeElement === previousButton.current &&
        api.canScrollNext()
      )
        nextButton.current?.focus()
      if (
        !api.canScrollNext() &&
        document.activeElement === nextButton.current &&
        api.canScrollPrev()
      )
        previousButton.current?.focus()
      setEdges((prev) => {
        const next = { prev: api.canScrollPrev(), next: api.canScrollNext() }
        return prev.prev === next.prev && prev.next === next.next ? prev : next
      })
    }
    sync()
    api.on("scroll", sync).on("reInit", sync)
    return () => {
      api.off("scroll", sync).off("reInit", sync)
    }
  }, [api])

  // Previous and Next live outside the row, so bring the active stage in
  // when it is not fully visible. Snaps are trimmed at the end, so a late
  // stage shares the last snap.
  useEffect(() => {
    if (!api) return
    const revealActive = () => {
      if (api.slidesInView().includes(activeIndex)) return
      const snap = Math.min(activeIndex, api.scrollSnapList().length - 1)
      api.scrollTo(snap, reducedMotion())
    }
    revealActive()
    api.on("reInit", revealActive)
    return () => {
      api.off("reInit", revealActive)
    }
  }, [api, activeIndex])

  useEffect(() => {
    const state = hover.current
    return () => window.clearInterval(state.timer)
  }, [])

  function hoverEdge(dir: number) {
    if (!api || dir === hover.current.dir) return
    window.clearInterval(hover.current.timer)
    hover.current.dir = dir
    hover.current.timer = 0
    if (!dir) return
    const step = () => {
      if (dir < 0 ? !api.canScrollPrev() : !api.canScrollNext()) {
        return hoverEdge(0)
      }
      if (dir < 0) api.scrollPrev(reducedMotion())
      else api.scrollNext(reducedMotion())
    }
    step()
    hover.current.timer = window.setInterval(step, STEP_MS)
  }

  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    if (reducedMotion() || event.pointerType !== "mouse" || event.buttons)
      return hoverEdge(0)
    const { left, right } = event.currentTarget.getBoundingClientRect()
    hoverEdge(
      event.clientX < left + EDGE_PX && edges.prev
        ? -1
        : event.clientX > right - EDGE_PX && edges.next
          ? 1
          : 0
    )
  }

  return (
    <Stepper
      value={current}
      indicators={{
        ...indicators,
        inactive: stopped ? <Skipped /> : indicators.inactive,
        active: stopped ? (
          <span className="flex size-5.5 items-center justify-center rounded-full bg-destructive text-primary-foreground">
            <HugeiconsIcon
              icon={Cancel01Icon}
              className="size-3.5"
              aria-hidden="true"
            />
          </span>
        ) : (
          <Pending />
        ),
      }}
      className="flex min-w-0 flex-col gap-4"
      role="group"
      aria-label={label}
    >
      <StepperNav>
        {/* Stages are read-only; slides retain the example's responsive sizing. */}
        <Carousel
          setApi={setApi}
          opts={carouselOptions}
          role={undefined}
          aria-roledescription={undefined}
          onKeyDownCapture={undefined}
          onPointerMove={onPointerMove}
          onPointerLeave={() => hoverEdge(0)}
          data-prev={edges.prev || undefined}
          data-next={edges.next || undefined}
          className="-m-1 min-w-0 flex-1 data-next:mask-r-from-[calc(100%-2.5rem)] data-prev:mask-l-from-[calc(100%-2.5rem)]"
        >
          <CarouselContent className="ml-0 py-1">
            {stages.map((stage, index) => {
              const step = index + 1
              const last = step === stages.length
              const nextStatus = stages[index + 1]?.status
              const connectorStart =
                stage.status === "complete"
                  ? "from-success"
                  : stage.status === "failed" || stage.status === "cancelled"
                    ? "from-destructive"
                    : stage.status === "skipped"
                      ? "from-muted-foreground/35"
                      : "from-ring/80"
              const connectorEnd =
                nextStatus === "complete"
                  ? "to-success"
                  : nextStatus === "failed" || nextStatus === "cancelled"
                    ? "to-destructive"
                    : nextStatus === "skipped"
                      ? "to-muted-foreground/35"
                      : "to-ring/80"

              return (
                <CarouselItem
                  key={stage.id}
                  role={undefined}
                  aria-roledescription={undefined}
                  className={cn(
                    "basis-auto pl-1",
                    last ? "min-w-32 pr-1" : "grow"
                  )}
                >
                  <StepperItem
                    step={step}
                    loading={stage.status === "running"}
                    role="group"
                    aria-label={`${stage.title}: ${statusLabels[stage.status]}`}
                    aria-current={step === current ? "step" : undefined}
                    className="items-stretch justify-start gap-2 group-data-[orientation=horizontal]/stepper-nav:flex-col"
                  >
                    <div className="flex items-center gap-1">
                      <StepperIndicator
                        className={cn(
                          "size-6.5 border border-ring/80 bg-transparent data-[state=active]:bg-transparent data-[state=completed]:border-success data-[state=completed]:bg-transparent",
                          stopped && step === current && "border-destructive",
                          stage.status === "skipped" &&
                            "border-muted-foreground/40"
                        )}
                      />

                      {/* Each connector endpoint follows its stage's status. */}
                      {step < stages.length ? (
                        <StepperSeparator
                          className={cn(
                            dots,
                            "to-70%",
                            connectorStart,
                            connectorEnd
                          )}
                        />
                      ) : (
                        <StepperSeparator
                          className={cn(dots, connectorStart, "to-transparent")}
                        />
                      )}
                    </div>

                    {/* The title and the detail labels share one auto column
                        through subgrid, so every leader in a stage starts at the
                        same x and the column follows the font, not a fixed width. */}
                    <div
                      className={cn(
                        "grid grid-cols-[auto_1fr] items-baseline gap-x-1 gap-y-1.5 ps-0.5",
                        last ? "pe-1" : "pe-5"
                      )}
                    >
                      <StepperTitle>{stage.title}</StepperTitle>
                      <div className="flex items-baseline gap-1">
                        <Leader neutral={stage.status === "skipped"} />
                        <span className="text-xs whitespace-nowrap text-muted-foreground">
                          {stage.time ? (
                            <time dateTime={stage.time}>
                              {new Date(stage.time).toLocaleTimeString(
                                undefined,
                                { hour: "2-digit", minute: "2-digit" }
                              )}
                            </time>
                          ) : (
                            statusLabels[stage.status]
                          )}
                        </span>
                      </div>

                      <dl className="col-span-2 grid grid-cols-subgrid gap-y-1.5">
                        {(
                          [
                            {
                              label: "Status",
                              value: statusLabels[stage.status],
                              badge: statusVariants[stage.status],
                            },
                            ...stage.details,
                          ] as {
                            label: string
                            value: string
                            badge?: BadgeProps["variant"]
                          }[]
                        ).map((detail) => (
                          <div
                            key={detail.label}
                            className="col-span-2 grid grid-cols-subgrid items-baseline"
                          >
                            <dt className="text-xs font-medium">
                              {detail.label}
                            </dt>
                            <dd className="flex items-baseline gap-1">
                              <Leader neutral={stage.status === "skipped"} />
                              {detail.badge ? (
                                <Badge variant={detail.badge} radius="full">
                                  {detail.value}
                                </Badge>
                              ) : (
                                <span className="max-w-48 text-right text-xs break-words text-muted-foreground">
                                  {detail.value}
                                </span>
                              )}
                            </dd>
                          </div>
                        ))}
                      </dl>
                    </div>
                  </StepperItem>
                </CarouselItem>
              )
            })}
          </CarouselContent>
        </Carousel>
      </StepperNav>

      {(edges.prev || edges.next) && (
        <div className="flex justify-end gap-2">
          <Button
            ref={previousButton}
            variant="outline"
            size="sm"
            disabled={!edges.prev}
            onClick={() => api?.scrollPrev(reducedMotion())}
          >
            Earlier stages
          </Button>
          <Button
            ref={nextButton}
            variant="outline"
            size="sm"
            disabled={!edges.next}
            onClick={() => api?.scrollNext(reducedMotion())}
          >
            Later stages
          </Button>
        </div>
      )}
    </Stepper>
  )
}
