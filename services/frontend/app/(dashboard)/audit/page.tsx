"use client"

import { useEffect, useState } from "react"
import { DataGridList } from "@/components/data-grid-table"
import { EmptyState, PageHeading, Panel } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { api } from "@/lib/api"
import type { ListResponse } from "@/lib/types"

type AuditEvent = {
  id: number
  actorId?: string
  action: string
  resourceType: string
  resourceId: string
  details: Record<string, unknown>
  createdAt: string
}

export default function AuditPage() {
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [error, setError] = useState<unknown | null>(null)
  useEffect(() => { api<ListResponse<AuditEvent>>("/api/v1/audit").then((result) => setEvents(result.items)).catch((cause) => setError(cause)) }, [])
  return <>
    <PageHeading title="Audit trail" description="Recent changes to users, access, connections, plans, and sync operations." />
    {error && <ErrorNotice error={error} />}
    <Panel surface="flat" title="Recent activity" description="The audit trail is available to instance administrators.">
      {events.length ? <div className="min-w-0"><DataGridList rows={events} columns={[
        { id: "when", title: "When", cell: (event) => <span className="text-[10px] text-muted-foreground">{new Date(event.createdAt).toLocaleString()}</span> },
        { id: "action", title: "Action", cell: (event) => <span className="font-medium">{event.action.replaceAll(".", " · ")}</span> },
        { id: "resource", title: "Resource", cell: (event) => <span className="text-xs text-muted-foreground">{event.resourceType}<span className="block font-mono text-[9px]">{event.resourceId.slice(0, 12)}</span></span> },
        { id: "actor", title: "Actor", cell: (event) => <span className="font-mono text-[10px] text-muted-foreground">{event.actorId?.slice(0, 12) ?? "system"}</span> },
        { id: "details", title: "Details", cell: (event) => <details className="max-w-[300px]"><summary className="cursor-pointer text-[10px] text-primary">View details</summary><pre className="mt-2 max-h-40 overflow-auto rounded bg-muted p-2 font-mono text-[9px]">{JSON.stringify(event.details, null, 2)}</pre></details> },
      ]} /></div> : <EmptyState title="No activity recorded" description="Administrative and deployment actions will appear here as they happen." />}
    </Panel>
  </>
}
