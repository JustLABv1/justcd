"use client"

import { useEffect, useMemo, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Cancel01Icon, Clock01Icon, Refresh01Icon, Tick02Icon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Input } from "@/components/ui/input"
import { EmptyState, PageHeading } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { api } from "@/lib/api"

type AuditEvent = {
  id: number
  actorId?: string
  actorName: string
  action: string
  resourceType: string
  resourceId: string
  details: Record<string, unknown> | null
  createdAt: string
}
type AuditResponse = { items: AuditEvent[]; hasMore: boolean }
type EventFilter = "all" | "changes" | "deployments" | "access"

const filters: { id: EventFilter; label: string }[] = [
  { id: "all", label: "All activity" },
  { id: "changes", label: "Configuration" },
  { id: "deployments", label: "Deployments" },
  { id: "access", label: "Access" },
]

function eventGroup(action: string): Exclude<EventFilter, "all"> {
  if (
    /^(sync|rollback|auto_sync|plan)\./.test(action) ||
    action.includes("decommission") ||
    action.includes("rollback") ||
    action.includes("resource_adopted")
  )
    return "deployments"
  if (
    /^(user|workspace_member|oidc|credential)\./.test(action) ||
    action.includes("approval_policy")
  )
    return "access"
  return "changes"
}

function words(value: string) {
  return value.replaceAll("_", " ").replace(/([a-z])([A-Z])/g, "$1 $2")
}

function actionLabel(action: string) {
  const [scope, ...rest] = action.split(".")
  return (words(scope) + " " + words(rest.join(" "))).replace(/^\w/, (letter) =>
    letter.toUpperCase()
  )
}

function actorLabel(event: AuditEvent) {
  if (event.actorId === "justcd-system") return "JustCD automation"
  return event.actorName || (event.actorId ? "Unknown user" : "System")
}

function eventStatus(action: string) {
  if (action.endsWith("failed")) return { label: "Failed", icon: Cancel01Icon, className: "text-destructive" }
  if (action.endsWith("succeeded")) return { label: "Succeeded", icon: Tick02Icon, className: "text-success-foreground dark:text-success" }
  if (action.endsWith("queued")) return { label: "Queued", icon: Clock01Icon, className: "text-warning-foreground dark:text-warning" }
  return null
}

function dateLabel(value: string) {
  return new Date(value).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  })
}

function valueText(value: unknown) {
  if (value === null || value === undefined) return "—"
  if (typeof value === "boolean") return value ? "Yes" : "No"
  if (typeof value === "string" || typeof value === "number")
    return String(value)
  return JSON.stringify(value, null, 2)
}

function AuditDetail({ event }: { event: AuditEvent }) {
  const details = Object.entries(event.details || {})
  return (
    <section
      className="min-w-0 rounded-xl border bg-card"
      aria-label="Event details"
    >
      <div className="border-b px-5 py-5 sm:px-6">
        <p className="text-sm font-medium text-muted-foreground">
          EVENT #{event.id}
        </p>
        <h2 className="mt-2 text-lg font-semibold tracking-tight">
          {actionLabel(event.action)}
        </h2>
        <p className="mt-1 text-sm text-muted-foreground">
          {dateLabel(event.createdAt)}
        </p>
      </div>
      <dl className="grid gap-4 border-b px-5 py-5 text-sm sm:grid-cols-2 sm:px-6">
        <div className="min-w-0">
          <dt className="text-xs text-muted-foreground">Performed by</dt>
          <dd className="mt-1 font-medium">{actorLabel(event)}</dd>
          {event.actorId && event.actorId !== "justcd-system" && (
            <dd className="mt-0.5 font-mono text-xs break-all text-muted-foreground">
              {event.actorId}
            </dd>
          )}
        </div>
        <div className="min-w-0">
          <dt className="text-xs text-muted-foreground">Resource</dt>
          <dd className="mt-1 font-medium capitalize">
            {words(event.resourceType)}
          </dd>
          <dd className="mt-0.5 font-mono text-xs break-all text-muted-foreground">
            {event.resourceId || "—"}
          </dd>
        </div>
      </dl>
      <div className="px-5 py-5 sm:px-6">
        <h3 className="text-sm font-semibold">Change details</h3>
        {details.length ? (
          <dl className="mt-4 divide-y rounded-lg border">
            {details.map(([key, value]) => {
              const structured = typeof value === "object" && value !== null
              return (
                <div
                  key={key}
                  className="grid min-w-0 gap-1 px-3 py-3 text-sm sm:grid-cols-[140px_minmax(0,1fr)] sm:gap-3"
                >
                  <dt className="text-xs text-muted-foreground">
                    {words(key)}
                  </dt>
                  <dd className="min-w-0">
                    {structured ? (
                      <pre className="max-h-64 overflow-auto rounded-md bg-muted/50 p-3 font-mono text-xs leading-5 break-all whitespace-pre-wrap">
                        {valueText(value)}
                      </pre>
                    ) : (
                      <span className="font-mono text-xs leading-5 break-all">
                        {valueText(value)}
                      </span>
                    )}
                  </dd>
                </div>
              )
            })}
          </dl>
        ) : (
          <p className="mt-3 text-sm text-muted-foreground">
            No additional details were recorded for this event.
          </p>
        )}
      </div>
    </section>
  )
}

export default function AuditPage() {
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [filter, setFilter] = useState<EventFilter>("all")
  const [query, setQuery] = useState("")
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<unknown | null>(null)

  async function refresh() {
    setLoading(true)
    setError(null)
    try {
      const result = await api<AuditResponse>("/api/v1/audit")
      setEvents(result.items)
      setHasMore(result.hasMore)
      setSelectedId(result.items[0]?.id ?? null)
    } catch (cause) {
      setError(cause)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    let active = true
    api<AuditResponse>("/api/v1/audit")
      .then((result) => {
        if (!active) return
        setEvents(result.items)
        setHasMore(result.hasMore)
        setSelectedId(result.items[0]?.id ?? null)
      })
      .catch((cause) => {
        if (active) setError(cause)
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [])

  async function loadMore() {
    const before = events.at(-1)?.id
    if (!before || loadingMore || loading) return
    setLoadingMore(true)
    setError(null)
    try {
      const result = await api<AuditResponse>("/api/v1/audit?before=" + before)
      setEvents((current) => [...current, ...result.items])
      setHasMore(result.hasMore)
    } catch (cause) {
      setError(cause)
    } finally {
      setLoadingMore(false)
    }
  }

  const visible = useMemo(() => {
    const term = query.trim().toLowerCase()
    return events.filter((event) => {
      if (filter !== "all" && eventGroup(event.action) !== filter) return false
      if (!term) return true
      return [
        event.action,
        actionLabel(event.action),
        event.resourceType,
        event.resourceId,
        event.actorName,
        event.actorId || "",
        JSON.stringify(event.details),
      ].some((part) => part.toLowerCase().includes(term))
    })
  }, [events, filter, query])
  const selected =
    visible.find((event) => event.id === selectedId) ?? visible[0]

  return (
    <div className="lg:flex lg:min-h-0 lg:flex-1 lg:flex-col">
      <PageHeading
        title="Audit trail"
        description="A record of configuration changes, access decisions, plans, and deployments."
        badge={<Badge variant="outline" radius="full">Instance administrators only</Badge>}
        actions={
          <Button
            variant="outline"
            onClick={() => void refresh()}
            disabled={loading || loadingMore}
          >
            <HugeiconsIcon icon={Refresh01Icon} strokeWidth={1.8} aria-hidden="true" />
            {loading ? "Refreshing…" : "Refresh"}
          </Button>
        }
      />
      {error !== null && <ErrorNotice error={error} />}
      <div className="mb-5 flex flex-col gap-4 rounded-xl border bg-card p-4 sm:p-5">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <div>
            <h2 className="text-sm font-semibold">Activity</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              {events.length} events loaded
              {hasMore ? "; older events available" : ""}. Times are shown in
              your local time zone.
            </p>
          </div>
        </div>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <ToggleGroup
            value={[filter]}
            onValueChange={(next) => { if (next[0]) setFilter(next[0] as EventFilter) }}
            aria-label="Filter audit events"
            className="flex-wrap"
          >
            {filters.map((item) => (
              <ToggleGroupItem key={item.id} value={item.id}>
                {item.label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <Input
            className="w-full sm:max-w-64"
            aria-label="Search loaded audit events"
            placeholder="Search loaded events…"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
      </div>
      {loading && events.length === 0 ? (
        <div role="status" aria-label="Loading audit events" className="space-y-px overflow-hidden rounded-xl border bg-card">
          {[0, 1, 2, 3].map((item) => <div key={item} className="space-y-2 p-5"><Skeleton className="h-4 w-1/3" /><Skeleton className="h-3 w-2/3" /></div>)}
        </div>
      ) : events.length === 0 && error ? (
        <div className="rounded-xl border bg-card px-5 py-10 text-center text-sm text-muted-foreground">
          Audit events could not be loaded. Try refreshing the page.
        </div>
      ) : events.length === 0 ? (
        <div className="rounded-xl border bg-card">
          <EmptyState
            title="No activity recorded"
            description="Administrative and deployment actions will appear here as they happen."
          />
        </div>
      ) : (
        <div className="grid min-w-0 gap-5 lg:min-h-0 lg:flex-1 lg:overflow-hidden lg:grid-cols-[minmax(360px,0.95fr)_minmax(0,1fr)]">
          <section
            className="min-w-0 overflow-hidden rounded-xl border bg-card lg:order-2 lg:flex lg:min-h-0 lg:flex-col"
            aria-label="Audit events"
          >
            {visible.length ? (
              <div className="divide-y lg:min-h-0 lg:flex-1 lg:overflow-y-auto lg:overscroll-contain">
                {visible.map((event) => {
                  const active = selected?.id === event.id
                  const status = eventStatus(event.action)
                  return (
                    <div key={event.id}>
                      <button
                        type="button"
                        onClick={() => setSelectedId(event.id)}
                        aria-current={active ? "true" : undefined}
                        className={
                          "flex w-full min-w-0 items-start gap-3 px-4 py-4 text-left transition-colors hover:bg-muted/40 focus-visible:relative focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-primary sm:px-5 " +
                          (active ? "bg-muted/50" : "")
                        }
                      >
                        {status ? (
                          <span className={"mt-0.5 grid size-5 shrink-0 place-items-center " + status.className}>
                            <HugeiconsIcon icon={status.icon} strokeWidth={2} className="size-4" aria-hidden="true" />
                            <span className="sr-only">{status.label}: </span>
                          </span>
                        ) : (
                          <span className="mt-0.5 grid size-5 shrink-0 place-items-center" aria-hidden="true"><span className="size-2 rounded-full bg-muted-foreground/50" /></span>
                        )}
                        <span className="min-w-0 flex-1">
                          <span className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                            <span className="font-medium">
                              {actionLabel(event.action)}
                            </span>
                            <time
                              dateTime={event.createdAt}
                              className="shrink-0 text-xs text-muted-foreground"
                            >
                              {dateLabel(event.createdAt)}
                            </time>
                          </span>
                          <span className="mt-1 block truncate text-xs text-muted-foreground">
                            {actorLabel(event)} · {words(event.resourceType)} ·{" "}
                            {event.resourceId || "—"}
                          </span>
                          {typeof event.details?.message === "string" && (
                            <span className="mt-2 line-clamp-2 block text-xs text-muted-foreground">
                              {event.details.message}
                            </span>
                          )}
                        </span>
                      </button>
                      {active && (
                        <div className="border-t p-3 lg:hidden">
                          <AuditDetail event={event} />
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            ) : (
              <div className="px-5 py-10 text-center text-sm text-muted-foreground lg:flex-1">
                No loaded events match these filters.
              </div>
            )}
            {hasMore && (
              <div className="border-t p-4 text-center">
                <Button
                  variant="outline"
                  onClick={() => void loadMore()}
                  disabled={loadingMore}
                >
                  {loadingMore ? "Loading…" : "Load older events"}
                </Button>
              </div>
            )}
          </section>
          <div className="hidden min-w-0 lg:order-1 lg:block lg:min-h-0 lg:overflow-y-auto lg:overscroll-contain">
            {selected && <AuditDetail event={selected} />}
          </div>
        </div>
      )}
    </div>
  )
}
