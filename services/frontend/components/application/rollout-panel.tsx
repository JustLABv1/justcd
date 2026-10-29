"use client"

import { useEffect, useId, useRef, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  Cancel01Icon,
  CircleDashedIcon,
  MinusSignIcon,
  Tick02Icon,
} from "@hugeicons/core-free-icons"
import {
  Timeline,
  TimelineContent,
  TimelineHeader,
  TimelineIndicator,
  TimelineItem,
  TimelineSeparator,
  TimelineTitle,
} from "@/components/reui/timeline"
import { Switch } from "@/components/ui/switch"
import {
  rolloutFocusResource,
  rolloutStatus,
  rolloutSummary,
  resourceStagesFor,
  resourceStageStatus,
  rolloutResourceKey,
  type RolloutResource,
} from "@/lib/rollout-timeline"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormSelect } from "@/components/ui/form-select"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Badge } from "@/components/reui/badge"
import { StatusBadge } from "@/components/ui-kit"
import { resourceLabel, sameIdentity } from "@/components/application/helpers"
import { syncRunProgress } from "@/lib/run-progress"
import type { Identity, Operation, PlanRecord } from "@/lib/types"

const resourceTone: Record<RolloutResource["status"], string> = {
  succeeded:
    "border-success/30 bg-success/10 text-success-foreground dark:text-success",
  running:
    "border-warning/30 bg-warning/10 text-warning-foreground dark:text-warning",
  retrying:
    "border-warning/30 bg-warning/10 text-warning-foreground dark:text-warning",
  failed: "border-destructive/30 bg-destructive/10 text-destructive",
  queued: "border-border bg-background text-muted-foreground",
  skipped: "border-border bg-muted text-muted-foreground",
}

function ResourceIndicator({
  status,
  position,
}: {
  status: RolloutResource["status"]
  position: number
}) {
  const icon =
    status === "succeeded"
      ? Tick02Icon
      : status === "failed"
        ? Cancel01Icon
        : status === "skipped"
          ? MinusSignIcon
          : status === "running" || status === "retrying"
            ? CircleDashedIcon
            : null
  return (
    <TimelineIndicator
      className={`flex size-6 items-center justify-center border bg-popover! group-data-completed/timeline-item:border-success/30 group-data-[orientation=vertical]/timeline:top-3 ${resourceTone[status]}`}
    >
      <span
        className={`flex size-full items-center justify-center rounded-full ${resourceTone[status]}`}
      >
        {icon ? (
          <HugeiconsIcon
            icon={icon}
            strokeWidth={2}
            className={`size-3.5 ${status === "running" || status === "retrying" ? "animate-spin motion-reduce:animate-none" : ""}`}
          />
        ) : (
          <span className="text-[10px] font-medium tabular-nums">
            {position}
          </span>
        )}
      </span>
    </TimelineIndicator>
  )
}

export function RolloutPanel({
  operation,
  plan,
  elapsed,
  onReviewResource,
}: {
  operation: Operation
  plan?: PlanRecord
  elapsed: (startedAt: string, finishedAt?: string) => string
  onReviewResource: (identity: Identity) => void
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [status, setStatus] = useState("all")
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const [following, setFollowing] = useState(true)
  const [tab, setTab] = useState("resources")
  const [scrollRequest, setScrollRequest] = useState(0)
  const [viewport, setViewport] = useState<HTMLDivElement | null>(null)
  const followId = useId()
  const rows = useRef(new Map<string, HTMLElement>())
  const lastScroll = useRef<{
    signature: string
    request: number
    resource: string
    viewport: HTMLElement
  } | null>(null)
  const resources = operation.progress?.resources ?? []
  const key = rolloutResourceKey
  // Retain the reported sequence. Status changes never reorder timeline rows.
  const filtered = resources.filter(
    (resource) =>
      (status === "all" ||
        (status === "partial"
          ? resource.status === "failed" && resource.mutationApplied
          : resource.status === status)) &&
      resourceLabel(resource.identity)
        .toLowerCase()
        .includes(query.toLowerCase())
  )
  const focus = rolloutFocusResource(operation)
  const wantedKey = following && focus ? key(focus.identity) : selectedKey
  const selected =
    filtered.find((resource) => key(resource.identity) === wantedKey) ??
    filtered[0]
  const selectedResourceKey = selected ? key(selected.identity) : null
  const index = selected ? filtered.indexOf(selected) : -1
  const position = selected ? resources.indexOf(selected) + 1 : 0
  const live = operation.status === "running" || operation.status === "queued"

  function selectResource(resource: RolloutResource) {
    viewport?.scrollTo({ top: viewport.scrollTop, behavior: "instant" })
    setSelectedKey(key(resource.identity))
    setFollowing(false)
  }
  function pauseFollowing() {
    if (!following) return
    viewport?.scrollTo({ top: viewport.scrollTop, behavior: "instant" })
    setSelectedKey(selectedResourceKey)
    setFollowing(false)
  }
  function changeFollowing(next: boolean) {
    if (!next) {
      pauseFollowing()
      return
    }
    setQuery("")
    setStatus("all")
    setSelectedKey(null)
    setScrollRequest((current) => current + 1)
    setFollowing(true)
  }
  function changeOpen(next: boolean) {
    setOpen(next)
    if (next) changeFollowing(true)
  }

  useEffect(() => {
    if (!open || tab !== "resources" || !viewport) {
      lastScroll.current = null
      return
    }
    if (!selectedResourceKey) return
    const signature = JSON.stringify([
      operation.id,
      selectedResourceKey,
      query,
      status,
      scrollRequest,
    ])
    // Pausing on a wheel/touch event must not itself snap the list back.
    if (
      lastScroll.current?.signature === signature &&
      lastScroll.current.viewport === viewport
    )
      return
    const frame = requestAnimationFrame(() => {
      const row = rows.current.get(selectedResourceKey)
      if (!row) return
      const previous = lastScroll.current
      const reduced = window.matchMedia(
        "(prefers-reduced-motion: reduce)"
      ).matches
      viewport.scrollTo({
        top: Math.max(
          0,
          viewport.scrollTop +
            row.getBoundingClientRect().top -
            viewport.getBoundingClientRect().top -
            12
        ),
        behavior:
          following &&
          previous &&
          previous.resource !== selectedResourceKey &&
          previous.request === scrollRequest &&
          !reduced
            ? "smooth"
            : "instant",
      })
      lastScroll.current = {
        signature,
        request: scrollRequest,
        resource: selectedResourceKey,
        viewport,
      }
    })
    return () => cancelAnimationFrame(frame)
  }, [
    open,
    tab,
    viewport,
    operation.id,
    selectedResourceKey,
    following,
    query,
    status,
    scrollRequest,
  ])
  const progress = syncRunProgress(operation, plan)
  return (
    <Sheet open={open} onOpenChange={changeOpen}>
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-muted/20 p-3">
        <div className="space-y-1">
          <p className="text-sm font-medium">
            Resource rollout{" "}
            <span className="text-muted-foreground">
              · {resources.length} resources
            </span>
          </p>
          <p className="text-xs text-muted-foreground">
            {rolloutSummary(resources)}
          </p>
        </div>
        <SheetTrigger render={<Button variant="outline" size="sm" />}>
          View rollout details
        </SheetTrigger>
      </div>
      <SheetContent className="gap-0 data-[side=right]:w-full motion-reduce:transition-none sm:my-3 sm:mr-3 sm:rounded-xl sm:border sm:data-[side=right]:h-[calc(100%-1.5rem)] data-[side=right]:sm:max-w-xl">
        <SheetHeader className="shrink-0 border-b p-5 pr-12">
          <p
            className="mb-2 truncate font-mono text-xs text-muted-foreground"
            title={operation.id}
          >
            {operation.id}
          </p>
          <SheetTitle className="flex flex-wrap items-center gap-2">
            {operation.type === "rollback" ? "Rollback" : "Sync"} rollout{" "}
            <StatusBadge status={operation.status} />
          </SheetTitle>
          <SheetDescription>
            {rolloutSummary(resources)} · Elapsed{" "}
            {elapsed(operation.startedAt, operation.finishedAt)}
          </SheetDescription>
        </SheetHeader>
        <Tabs
          value={tab}
          onValueChange={(value) => setTab(String(value))}
          className="flex min-h-0 flex-1 flex-col"
        >
          <TabsList
            className="mb-0 shrink-0 px-5 pt-2"
            aria-label="Rollout views"
          >
            <TabsTrigger value="resources">
              Resources ({resources.length})
            </TabsTrigger>
            <TabsTrigger value="pipeline">Sync details</TabsTrigger>
          </TabsList>
          <TabsContent
            value="resources"
            className="flex min-h-0 flex-1 flex-col"
          >
            <div className="shrink-0 space-y-3 border-b p-5">
              <div className="grid gap-2 sm:grid-cols-[1fr_160px]">
                <Input
                  aria-label="Search rollout resources"
                  placeholder="Search resources…"
                  value={query}
                  onChange={(event) => {
                    const value = event.target.value
                    pauseFollowing()
                    setQuery(value)
                  }}
                />
                <FormSelect
                  ariaLabel="Filter resource status"
                  value={status}
                  onValueChange={(value) => {
                    pauseFollowing()
                    setStatus(value)
                  }}
                  items={[
                    { value: "all", label: "All statuses" },
                    { value: "partial", label: "Applied · follow-up failed" },
                    ...[
                      "failed",
                      "running",
                      "retrying",
                      "queued",
                      "succeeded",
                      "skipped",
                    ].map((value) => ({
                      value,
                      label: value[0].toUpperCase() + value.slice(1),
                    })),
                  ]}
                />
              </div>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <label
                  htmlFor={followId}
                  className="flex items-center gap-2 text-sm"
                >
                  <Switch
                    id={followId}
                    checked={following}
                    onCheckedChange={changeFollowing}
                  />
                  Follow rollout
                </label>
                <div className="flex items-center gap-1">
                  {selected && (
                    <span className="mr-1 text-xs text-muted-foreground">
                      {position} / {resources.length}
                    </span>
                  )}
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-label="Previous resource"
                    disabled={index <= 0}
                    onClick={() => selectResource(filtered[index - 1])}
                  >
                    Previous
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-label="Next resource"
                    disabled={index < 0 || index >= filtered.length - 1}
                    onClick={() => selectResource(filtered[index + 1])}
                  >
                    Next
                  </Button>
                </div>
              </div>
              <p role="status" className="text-xs text-muted-foreground">
                {following
                  ? live
                    ? "Following the active resource"
                    : "Showing the final result"
                  : "Following paused · inspect any resource or turn following back on"}
              </p>
              <p aria-live="polite" className="sr-only">
                {following && selected
                  ? `${selected.identity.name}: ${rolloutStatus(selected)}`
                  : ""}
              </p>
            </div>
            <div
              ref={setViewport}
              data-slot="rollout-timeline-viewport"
              className="relative min-h-0 flex-1 overflow-y-auto overscroll-contain p-5"
              onWheel={pauseFollowing}
              onFocusCapture={pauseFollowing}
              onTouchMove={pauseFollowing}
              onPointerDown={pauseFollowing}
              onKeyDownCapture={(event) => {
                if (
                  [
                    "ArrowUp",
                    "ArrowDown",
                    "PageUp",
                    "PageDown",
                    "Home",
                    "End",
                    " ",
                  ].includes(event.key)
                )
                  pauseFollowing()
              }}
            >
              <Timeline
                render={<ol />}
                value={0}
                aria-label="Resource rollout timeline"
                className="ml-1"
              >
                {filtered.map((resource) => {
                  const resourceKey = key(resource.identity)
                  const ordinal = resources.indexOf(resource) + 1
                  const active = focus && key(focus.identity) === resourceKey
                  const expanded = resource === selected
                  return (
                    <TimelineItem
                      render={<li />}
                      key={resourceKey}
                      step={ordinal}
                      data-completed={
                        resource.status === "succeeded" || undefined
                      }
                      data-resource-key={resourceKey}
                      ref={(node) => {
                        if (node) rows.current.set(resourceKey, node)
                        else rows.current.delete(resourceKey)
                      }}
                      className="min-w-0 group-data-[orientation=vertical]/timeline:not-last:pb-3"
                    >
                      <TimelineSeparator
                        className={`group-data-[orientation=vertical]/timeline:top-10 group-data-[orientation=vertical]/timeline:h-[calc(100%-2rem)] group-data-[orientation=vertical]/timeline:translate-y-0 ${
                          resource.status === "succeeded"
                            ? "bg-success/40!"
                            : "bg-border!"
                        }`}
                      />
                      <ResourceIndicator
                        status={resource.status}
                        position={ordinal}
                      />
                      <Button
                        variant="ghost"
                        data-resource-select
                        aria-pressed={expanded}
                        aria-current={active ? "step" : undefined}
                        onClick={() => selectResource(resource)}
                        className={`h-auto min-h-16 w-full justify-start gap-2 rounded-lg px-3 py-3 text-left ${expanded ? "bg-muted/60" : ""}`}
                      >
                        <TimelineHeader
                          render={<span />}
                          className="min-w-0 flex-1"
                        >
                          <TimelineTitle
                            render={<span />}
                            className="block truncate"
                            title={resource.identity.name}
                          >
                            {resource.identity.name}
                          </TimelineTitle>
                          <TimelineContent
                            render={<span />}
                            className="mt-1 block truncate text-xs font-normal"
                          >
                            {resource.identity.kind} ·{" "}
                            {resource.identity.namespace || "Cluster scoped"}
                          </TimelineContent>
                        </TimelineHeader>
                        <span className="shrink-0">
                          {resource.status === "failed" &&
                          resource.mutationApplied ? (
                            <Badge
                              variant="warning-light"
                              className="max-w-36 text-right whitespace-normal"
                            >
                              {rolloutStatus(resource)}
                            </Badge>
                          ) : (
                            <StatusBadge status={resource.status} />
                          )}
                        </span>
                      </Button>
                      {expanded && selected && (
                        <section
                          aria-label="Selected resource"
                          className="mt-2 rounded-lg border bg-muted/20 p-4"
                        >
                          <h3 className="sr-only">
                            {selected.identity.name} details
                          </h3>
                          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                            <dt className="text-muted-foreground">Action</dt>
                            <dd>{selected.action.replaceAll("_", " ")}{selected.propagationPolicy ? ` · ${selected.propagationPolicy}` : ""}</dd>
                            <dt className="text-muted-foreground">Stage</dt>
                            <dd>
                              {resourceStagesFor(selected).find(
                                (stage) => stage.id === selected.stage
                              )?.label ??
                                selected.stage ??
                                "Queued"}
                            </dd>
                            <dt className="text-muted-foreground">Attempt</dt>
                            <dd>{selected.attemptCount || "Not attempted"}</dd>
                            <dt className="text-muted-foreground">Duration</dt>
                            <dd>
                              {selected.startedAt
                                ? elapsed(
                                    selected.startedAt,
                                    selected.finishedAt
                                  )
                                : "Not started"}
                            </dd>
                          </dl>
                          {(selected.action === "update" || selected.action === "recreate") && (
                            <ol
                              aria-label="Resource steps"
                              className="mt-4 space-y-2 text-sm"
                            >
                              {resourceStagesFor(selected).map((stage) => (
                                <li
                                  key={stage.id}
                                  className="flex justify-between gap-3"
                                >
                                  <span>{stage.label}</span>
                                  <span
                                    className={
                                      resourceStageStatus(
                                        selected,
                                        stage.id
                                      ) === "failed"
                                        ? "text-destructive"
                                        : "text-muted-foreground"
                                    }
                                  >
                                    {resourceStageStatus(selected, stage.id)}
                                  </span>
                                </li>
                              ))}
                            </ol>
                          )}
                          {selected.mutationApplied &&
                            selected.status === "failed" && (
                              <p className="mt-4 text-sm font-medium">
                                Kubernetes accepted the changes. Follow-up did
                                not finish; no automatic rollback was attempted.
                              </p>
                            )}
                          {selected.reasonCode && (
                            <p className="mt-3 text-sm text-muted-foreground">
                              {selected.reasonCode ===
                              "live_state_changed_after_apply"
                                ? "Live configuration or field ownership changed between apply and cleanup."
                                : selected.reasonCode ===
                                    "ownership_cleanup_failed"
                                  ? "Field ownership cleanup could not finish."
                                  : selected.reasonCode === "operation_stopped"
                                    ? "The operation stopped before this resource was attempted."
                                    : selected.reasonCode.replaceAll("_", " ")}
                            </p>
                          )}
                          {Boolean(selected.changedPaths?.length) && (
                            <div className="mt-3 text-sm">
                              <p className="font-medium">Changed fields</p>
                              <ul className="mt-1 space-y-1">
                                {selected.changedPaths!.map((path) => (
                                  <li
                                    key={path}
                                    className="font-mono text-xs break-all"
                                  >
                                    {path}
                                  </li>
                                ))}
                              </ul>
                            </div>
                          )}
                          {selected.message && (
                            <p
                              className={`mt-4 text-sm break-words ${selected.status === "failed" ? "text-destructive" : "text-muted-foreground"}`}
                            >
                              {selected.message}
                            </p>
                          )}
                          {plan &&
                            plan.id === operation.planId &&
                            plan.plan.changes.some((change) =>
                              sameIdentity(change.identity, selected.identity)
                            ) && (
                              <Button
                                className="mt-4"
                                size="sm"
                                variant="outline"
                                onClick={() => {
                                  setOpen(false)
                                  onReviewResource(selected.identity)
                                }}
                              >
                                Review resource diff
                              </Button>
                            )}
                        </section>
                      )}
                    </TimelineItem>
                  )
                })}
              </Timeline>
              {!filtered.length && (
                <p className="py-8 text-center text-sm text-muted-foreground">
                  No resources match your search and status filter.
                </p>
              )}
            </div>
          </TabsContent>
          <TabsContent
            value="pipeline"
            className="min-h-0 flex-1 space-y-5 overflow-y-auto p-5"
          >
            <ol
              aria-label="Sync stages"
              className="divide-y rounded-lg border px-4"
            >
              {progress.stages.map((stage) => (
                <li key={stage.id} className="py-3">
                  <div className="flex items-center justify-between gap-3">
                    <span className="font-medium">{stage.title}</span>
                    <StatusBadge
                      status={
                        stage.status === "complete" ? "succeeded" : stage.status
                      }
                    />
                  </div>
                  {stage.details.map((detail) => (
                    <p
                      key={detail.label}
                      className="mt-1 text-xs text-muted-foreground"
                    >
                      {detail.label}: {detail.value}
                    </p>
                  ))}
                </li>
              ))}
            </ol>
            <p className="text-sm text-muted-foreground">{progress.summary}</p>
            {operation.message && (
              <p className="text-sm break-words">{operation.message}</p>
            )}
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-3 text-sm">
              <dt className="text-muted-foreground">Started</dt>
              <dd>{new Date(operation.startedAt).toLocaleString()}</dd>
              <dt className="text-muted-foreground">Attempt</dt>
              <dd>{operation.attemptCount || 1}</dd>
              {operation.finishedAt && (
                <>
                  <dt className="text-muted-foreground">Finished</dt>
                  <dd>{new Date(operation.finishedAt).toLocaleString()}</dd>
                </>
              )}
              {operation.planId && (
                <>
                  <dt className="text-muted-foreground">Plan</dt>
                  <dd className="font-mono text-xs break-all">
                    {operation.planId}
                  </dd>
                </>
              )}
            </dl>
          </TabsContent>
        </Tabs>
      </SheetContent>
    </Sheet>
  )
}
