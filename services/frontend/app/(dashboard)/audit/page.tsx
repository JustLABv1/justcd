"use client"

import { useEffect, useRef, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  ArrowRight01Icon,
  Download01Icon,
  Refresh01Icon,
} from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { AuditDetailPanel } from "@/components/audit-detail-panel"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Input } from "@/components/ui/input"
import { EmptyState, PageHeading } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { api } from "@/lib/api"
import { useWorkspaceSelection } from "@/hooks/workspace-selection"
import {
  actionLabel,
  actorLabel,
  dateLabel,
  eventStatus,
  words,
  type AuditEvent,
  type AuditResponse,
  type EventFilter,
} from "@/lib/audit"

const filters: { id: EventFilter; label: string }[] = [
  { id: "all", label: "All activity" },
  { id: "changes", label: "Configuration" },
  { id: "deployments", label: "Deployments" },
  { id: "access", label: "Access" },
]

export default function AuditPage() {
  const toast = useToast()
  const { isAdmin, workspaces, workspaceId } = useWorkspaceSelection()
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [selected, setSelected] = useState<AuditEvent | null>(null)
  const [filter, setFilter] = useState<EventFilter>("all")
  const [query, setQuery] = useState("")
  const [search, setSearch] = useState("")
  const [workspaceChoice, setWorkspace] = useState<string | null>(null)
  const workspace = workspaceChoice ?? (isAdmin ? "" : workspaceId)
  const [resource, setResource] = useState("")
  const [hasMore, setHasMore] = useState(false)
  const [loadedKey, setLoadedKey] = useState("")
  const pagination = useRef<AbortController | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<unknown | null>(null)
  const [version, setVersion] = useState(0)
  const [retentionOpen, setRetentionOpen] = useState(false)
  const [retention, setRetention] = useState("0")
  const [saving, setSaving] = useState(false)
  const [retentionLoading, setRetentionLoading] = useState(false)
  const params = new URLSearchParams({ group: filter })
  if (search) params.set("q", search)
  if (workspace) params.set("workspaceId", workspace)
  if (resource) params.set("resourceId", resource)
  const filterKey = params.toString()
  const requestKey = `${filterKey}|${version}`
  const scopeAvailable = isAdmin || !!workspace
  const loading =
    scopeAvailable && (loadedKey !== requestKey || query.trim() !== search)

  useEffect(() => {
    const timer = setTimeout(() => setSearch(query.trim()), 250)
    return () => clearTimeout(timer)
  }, [query])
  useEffect(() => {
    const controller = new AbortController()
    const id = new URL(window.location.href).searchParams.get("event")
    if (id && /^\d+$/.test(id))
      api<AuditEvent>(`/api/v1/audit/${id}`, { signal: controller.signal })
        .then(setSelected)
        .catch((cause) => {
          if (!controller.signal.aborted) setError(cause)
        })
    return () => controller.abort()
  }, [])
  useEffect(() => {
    if (!isAdmin && !workspace) return
    const controller = new AbortController()
    api<AuditResponse>(`/api/v1/audit?${filterKey}`, {
      signal: controller.signal,
    })
      .then((result) => {
        if (controller.signal.aborted) return
        setError(null)
        setEvents(result.items)
        setHasMore(result.hasMore)
      })
      .catch((cause) => {
        if (!controller.signal.aborted) {
          setEvents([])
          setHasMore(false)
          setError(cause)
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoadedKey(requestKey)
      })
    return () => {
      controller.abort()
      pagination.current?.abort()
    }
  }, [filterKey, requestKey, isAdmin, workspace])

  async function loadMore() {
    const before = events.at(-1)?.id
    if (!before || loadingMore || loading) return
    const controller = new AbortController()
    pagination.current = controller
    setLoadingMore(true)
    setError(null)
    try {
      const result = await api<AuditResponse>(
        `/api/v1/audit?${filterKey}&before=${before}`,
        { signal: controller.signal }
      )
      if (controller.signal.aborted) return
      setEvents((current) => [...current, ...result.items])
      setHasMore(result.hasMore)
    } catch (cause) {
      if (!controller.signal.aborted) setError(cause)
    } finally {
      setLoadingMore(false)
    }
  }
  async function openRetention() {
    setRetentionOpen(true)
    setRetentionLoading(true)
    try {
      const result = await api<{ days: number }>(
        `/api/v1/workspaces/${workspace}/audit-retention`
      )
      setRetention(String(result.days))
    } catch (cause) {
      toast.error("Could not load retention settings.", cause)
      setRetentionOpen(false)
    } finally {
      setRetentionLoading(false)
    }
  }
  async function saveRetention() {
    setSaving(true)
    try {
      await api(`/api/v1/workspaces/${workspace}/audit-retention`, {
        method: "PUT",
        body: JSON.stringify({ days: Number(retention) }),
      })
      setRetentionOpen(false)
      toast.success("Audit retention updated.")
    } catch (cause) {
      toast.error("Could not save audit retention.", cause)
    } finally {
      setSaving(false)
    }
  }
  function closePanel() {
    setSelected(null)
    const url = new URL(window.location.href)
    url.searchParams.delete("event")
    window.history.replaceState(null, "", url)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeading
        title="Audit trail"
        description="Follow configuration changes, access decisions, and deployment outcomes."
        badge={<Badge variant="outline">Versioned evidence</Badge>}
        actions={
          <>
            <Button
              variant="outline"
              disabled={loading || loadingMore}
              onClick={() => setVersion((value) => value + 1)}
            >
              <HugeiconsIcon icon={Refresh01Icon} />
              Refresh
            </Button>
            <Button
              variant="outline"
              disabled={loading || !scopeAvailable}
              render={
                <a
                  href={`/api/v1/audit/export?${filterKey}&format=csv`}
                  download
                />
              }
            >
              <HugeiconsIcon icon={Download01Icon} />
              CSV
            </Button>
            <Button
              disabled={loading || !scopeAvailable}
              render={
                <a
                  href={`/api/v1/audit/export?${filterKey}&format=json`}
                  download
                />
              }
            >
              <HugeiconsIcon icon={Download01Icon} />
              Export JSON
            </Button>
          </>
        }
      />
      {error !== null && <ErrorNotice error={error} />}
      <div className="mb-4 space-y-4 rounded-xl border bg-card p-4 sm:p-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <ToggleGroup
            value={[filter]}
            onValueChange={(next) => {
              if (next[0]) setFilter(next[0] as EventFilter)
            }}
            aria-label="Filter audit events"
            className="flex-wrap"
            disabled={loadingMore}
          >
            {filters.map((item) => (
              <ToggleGroupItem key={item.id} value={item.id}>
                {item.label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <Input
            className="w-full sm:max-w-72"
            aria-label="Search audit events"
            placeholder="Search actions, actors, resources…"
            value={query}
            maxLength={500}
            disabled={loadingMore}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t pt-4">
          <div className="flex w-full flex-wrap items-center gap-2 sm:w-auto">
            <div className="w-full sm:w-64">
              <FormSelect
                value={workspace}
                onValueChange={setWorkspace}
                items={workspaces.map((item) => ({
                  value: item.id,
                  label: item.name,
                }))}
                emptyOption={
                  isAdmin ? "All workspaces (administrators)" : undefined
                }
                ariaLabel="Audit workspace"
                disabled={loadingMore}
              />
            </div>
            {workspace &&
              (isAdmin ||
                workspaces.find((item) => item.id === workspace)?.role ===
                  "owner") && (
                <Button variant="ghost" onClick={() => void openRetention()}>
                  Retention settings
                </Button>
              )}
            {resource && (
              <Button
                variant="outline"
                disabled={loadingMore}
                onClick={() => setResource("")}
              >
                Clear resource filter
              </Button>
            )}
          </div>
          <p className="text-xs text-muted-foreground">
            {loading
              ? "Loading…"
              : `${events.length} events loaded${hasMore ? " · More available" : ""}`}{" "}
            · Exports include all matches
          </p>
        </div>
      </div>
      <section
        aria-label="Audit events"
        aria-busy={loading}
        className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border bg-card"
      >
        <div className="flex items-center justify-between border-b px-5 py-3">
          <h2 className="text-sm font-semibold">Activity</h2>
          <span className="text-xs text-muted-foreground">
            Newest first · Local time
          </span>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          {loading ? (
            <div
              role="status"
              aria-label="Loading audit events"
              className="divide-y"
            >
              {[0, 1, 2, 3].map((item) => (
                <div key={item} className="space-y-3 p-5">
                  <Skeleton className="h-4 w-1/3" />
                  <Skeleton className="h-3 w-2/3" />
                </div>
              ))}
            </div>
          ) : events.length ? (
            <div className="divide-y">
              {events.map((event) => {
                const status = eventStatus(event.action)
                return (
                  <Button
                    key={event.id}
                    variant="ghost"
                    onClick={() => setSelected(event)}
                    aria-label={`View event ${event.id}: ${actionLabel(event.action)} by ${actorLabel(event)} at ${dateLabel(event.createdAt)}`}
                    className="h-auto w-full justify-start gap-4 rounded-none px-5 py-4 text-left whitespace-normal sm:px-6"
                  >
                    <span
                      className="grid size-9 shrink-0 place-items-center rounded-full border bg-muted/30 font-mono text-xs text-muted-foreground"
                      aria-hidden="true"
                    >
                      {actorLabel(event).slice(0, 2).toUpperCase()}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="flex flex-wrap items-center gap-2">
                        <span className="text-sm font-medium">
                          {actionLabel(event.action)}
                        </span>
                        {status && (
                          <Badge variant={status.variant}>{status.label}</Badge>
                        )}
                      </span>
                      <span className="mt-1.5 block truncate text-xs font-normal text-muted-foreground">
                        {actorLabel(event)}
                        <span className="mx-2">·</span>
                        {event.resourceName || words(event.resourceType)}
                        <span className="mx-2">·</span>
                        <span className="font-mono">
                          {event.resourceId || "Instance"}
                        </span>
                      </span>
                      <span className="mt-1.5 block text-xs font-normal text-muted-foreground sm:hidden">
                        <time dateTime={event.createdAt}>
                          {dateLabel(event.createdAt)}
                        </time>
                      </span>
                      {typeof event.details?.message === "string" && (
                        <span className="mt-2 line-clamp-1 block text-xs font-normal text-muted-foreground">
                          {event.details.message}
                        </span>
                      )}
                    </span>
                    <span className="hidden shrink-0 text-right text-xs font-normal text-muted-foreground sm:block">
                      <time dateTime={event.createdAt}>
                        {dateLabel(event.createdAt)}
                      </time>
                      <span className="mt-1.5 block font-mono">
                        EVT-{event.id}
                      </span>
                    </span>
                    <HugeiconsIcon
                      icon={ArrowRight01Icon}
                      className="size-4 shrink-0 text-muted-foreground"
                    />
                  </Button>
                )
              })}
            </div>
          ) : (
            <EmptyState
              title={
                error
                  ? "Audit events could not be loaded"
                  : "No matching activity"
              }
              description={
                error
                  ? "Try refreshing the page."
                  : "Change your filters or search to see other events."
              }
            />
          )}
        </div>
        {hasMore && (
          <div className="shrink-0 border-t p-3 text-center">
            <Button
              variant="outline"
              loading={loadingMore}
              onClick={() => void loadMore()}
              disabled={loading || !scopeAvailable}
            >
              {loadingMore ? "Loading…" : "Load older events"}
            </Button>
          </div>
        )}
      </section>
      <AuditDetailPanel
        event={selected}
        events={events}
        onSelect={setSelected}
        onClose={closePanel}
        onHistory={(id) => {
          setFilter("all")
          setQuery("")
          setSearch("")
          setResource(id)
          closePanel()
        }}
      />
      <AppDialog
        open={retentionOpen}
        onOpenChange={setRetentionOpen}
        title="Audit retention"
        description="Choose how long this workspace keeps audit evidence. The default keeps records indefinitely."
        busy={saving}
        size="sm"
      >
        <div className="space-y-5">
          <label className="block space-y-2">
            <span className="text-sm font-medium">Keep records for</span>
            <FormSelect
              value={retention}
              onValueChange={setRetention}
              disabled={retentionLoading || saving}
              ariaLabel="Retention period"
              items={[
                ...new Set(["0", "30", "90", "180", "365", "2555", retention]),
              ].map((value) => ({
                value,
                label:
                  value === "0" ? "Indefinitely (default)" : `${value} days`,
              }))}
            />
          </label>
          <p className="text-sm text-muted-foreground">
            {retention === "0"
              ? "No audit records will expire automatically."
              : "Records older than this period are permanently deleted by the hourly cleanup. Export evidence you need to keep before reducing retention."}
          </p>
          <DialogFooter>
            <DialogCancel disabled={saving} />
            <Button
              onClick={() => void saveRetention()}
              loading={saving}
              disabled={retentionLoading}
              variant={retention === "0" ? "default" : "destructive"}
            >
              Save retention
            </Button>
          </DialogFooter>
        </div>
      </AppDialog>
    </div>
  )
}
