"use client"

import Link from "next/link"
import { useState } from "react"
import { Panel, StatusBadge } from "@/components/ui-kit"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { ErrorDetailsButton } from "@/components/error-details"
import type { SyncHistoryState } from "@/hooks/use-sync-history"
import type { ApplicationSyncActivity } from "@/lib/sync-history"
import { ago } from "@/lib/relative-time"

function SyncRows({
  applications,
  now,
}: {
  applications: ApplicationSyncActivity[]
  now: number
}) {
  return (
    <ul className="divide-y">
      {applications.map((app) => (
        <li key={app.applicationId}>
          <Button
            render={
              <Link
                href={`/applications/${encodeURIComponent(app.applicationId)}?tab=activity`}
              />
            }
            nativeButton={false}
            variant="ghost"
            className="h-auto w-full justify-start gap-3 rounded-none px-5 py-3 text-left whitespace-normal"
          >
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium">
                {app.name}
              </span>
              <span className="mt-0.5 block text-xs font-normal text-muted-foreground">
                {app.startedAt ? (
                  <>
                    {app.type === "rollback" ? "Rollback" : "Sync"} ·{" "}
                    {ago(app.startedAt, now)}
                    <time dateTime={app.startedAt} className="mt-0.5 block">
                      {new Date(app.startedAt).toLocaleString()}
                    </time>
                  </>
                ) : (
                  "No sync runs yet"
                )}
              </span>
            </span>
            <StatusBadge status={app.status || "Not run"} />
          </Button>
        </li>
      ))}
    </ul>
  )
}

export function LatestSyncs({
  state,
  now,
}: {
  state: SyncHistoryState
  now: number
}) {
  const [expanded, setExpanded] = useState(false)
  const { result, retry } = state
  const applications = result.history?.applications ?? []
  return (
    <Collapsible open={expanded} onOpenChange={setExpanded}>
      <Panel
        title="Latest syncs"
        description="Last sync or rollback per app, across all time."
        action={
          applications.length > 5 ? (
            <CollapsibleTrigger render={<Button variant="ghost" size="sm" />}>
              {expanded ? "Show less" : `View all (${applications.length})`}
            </CollapsibleTrigger>
          ) : undefined
        }
      >
        {result.loading ? (
          <div className="space-y-3 p-5" aria-label="Loading latest syncs">
            {Array.from({ length: 3 }, (_, index) => (
              <Skeleton
                key={index}
                className="h-10 w-full motion-reduce:animate-none"
              />
            ))}
          </div>
        ) : result.error ? (
          <div
            role="status"
            className="space-y-3 p-5 text-sm text-muted-foreground"
          >
            <p>Latest syncs could not be loaded.</p>
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" size="sm" onClick={retry}>
                Retry syncs
              </Button>
              <ErrorDetailsButton error={result.error} />
            </div>
          </div>
        ) : applications.length ? (
          <>
            <SyncRows applications={applications.slice(0, 5)} now={now} />
            <CollapsibleContent className="border-t">
              <SyncRows applications={applications.slice(5)} now={now} />
            </CollapsibleContent>
          </>
        ) : (
          <p className="px-5 py-8 text-center text-sm text-muted-foreground">
            Add an application to see its sync activity.
          </p>
        )}
      </Panel>
    </Collapsible>
  )
}
