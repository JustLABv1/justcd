"use client"

import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormSelect } from "@/components/ui/form-select"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { StatusBadge } from "@/components/ui-kit"
import { resourceLabel, sameIdentity } from "@/components/application/helpers"
import { syncRunProgress } from "@/lib/run-progress"
import type { Identity, Operation, PlanRecord } from "@/lib/types"

export function RolloutPanel({ operation, plan, elapsed, onReviewResource }: {
  operation: Operation
  plan?: PlanRecord
  elapsed: (startedAt: string, finishedAt?: string) => string
  onReviewResource: (identity: Identity) => void
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [status, setStatus] = useState("all")
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const resources = operation.progress?.resources ?? []
  const key = (identity: Identity) => JSON.stringify([identity.clusterId ?? "", identity.apiVersion, identity.kind, identity.namespace, identity.name])
  const filtered = resources.filter((resource) =>
    (status === "all" || resource.status === status) &&
    resourceLabel(resource.identity).toLowerCase().includes(query.toLowerCase())
  )
  const selected = filtered.find((resource) => key(resource.identity) === selectedKey) ?? filtered.find((resource) => resource.status === "failed" || resource.status === "running" || resource.status === "retrying") ?? filtered[0]
  const index = selected ? filtered.indexOf(selected) : -1
  const succeeded = resources.filter((resource) => resource.status === "succeeded").length
  const failed = resources.filter((resource) => resource.status === "failed").length
  const skipped = resources.filter((resource) => resource.status === "skipped").length
  const progress = syncRunProgress(operation, plan)
  return <Sheet open={open} onOpenChange={setOpen}>
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-muted/20 p-3">
      <div className="space-y-1">
        <p className="text-sm font-medium">Resource rollout <span className="text-muted-foreground">· {resources.length} resources</span></p>
        <p className="text-xs text-muted-foreground">{succeeded} succeeded{failed > 0 && <span className="font-medium text-destructive"> · {failed} failed</span>} · {resources.length - succeeded - failed - skipped} pending{skipped > 0 && ` · ${skipped} skipped`}</p>
      </div>
      <SheetTrigger render={<Button variant="outline" size="sm" />}>View rollout details</SheetTrigger>
    </div>
    <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl motion-reduce:transition-none sm:my-3 sm:mr-3 sm:rounded-xl sm:border sm:data-[side=right]:h-[calc(100%-1.5rem)]">
      <SheetHeader className="shrink-0 border-b p-5 pr-12">
        <p className="mb-2 truncate font-mono text-xs text-muted-foreground" title={operation.id}>{operation.id}</p>
        <SheetTitle className="flex flex-wrap items-center gap-2">{operation.type === "rollback" ? "Rollback" : "Sync"} rollout <StatusBadge status={operation.status} /></SheetTitle>
        <SheetDescription>{succeeded} / {resources.length} resources succeeded · Elapsed {elapsed(operation.startedAt, operation.finishedAt)}</SheetDescription>
      </SheetHeader>
      <Tabs defaultValue="resources" className="flex min-h-0 flex-1 flex-col">
        <TabsList className="mb-0 shrink-0 px-5 pt-2" aria-label="Rollout views">
          <TabsTrigger value="resources">Resources ({resources.length})</TabsTrigger>
          <TabsTrigger value="pipeline">Sync details</TabsTrigger>
        </TabsList>
        <TabsContent value="resources" className="min-h-0 flex-1 overflow-y-auto p-5">
          <div className="mb-4 grid gap-2 sm:grid-cols-[1fr_160px]">
            <Input aria-label="Search rollout resources" placeholder="Search resources…" value={query} onChange={(event) => setQuery(event.target.value)} />
            <FormSelect ariaLabel="Filter resource status" value={status} onValueChange={setStatus} items={[{ value: "all", label: "All statuses" }, ...["failed", "running", "retrying", "queued", "succeeded", "skipped"].map((value) => ({ value, label: value[0].toUpperCase() + value.slice(1) }))]} />
          </div>
          {selected && <section aria-label="Selected resource" className="mb-5 rounded-lg border bg-muted/20 p-4">
            <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
              <StatusBadge status={selected.status} />
              <div className="flex items-center gap-2">
                <span className="text-xs text-muted-foreground">{index + 1} of {filtered.length}</span>
                <Button size="sm" variant="ghost" aria-label="Previous resource" disabled={index <= 0} onClick={() => setSelectedKey(key(filtered[index - 1].identity))}>Previous</Button>
                <Button size="sm" variant="ghost" aria-label="Next resource" disabled={index >= filtered.length - 1} onClick={() => setSelectedKey(key(filtered[index + 1].identity))}>Next</Button>
              </div>
            </div>
            <h3 className="text-sm font-semibold break-all">{selected.identity.name}</h3>
            <p className="mt-1 text-xs text-muted-foreground">{selected.identity.kind} · {selected.identity.namespace || "Cluster scoped"}</p>
            <dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
              <dt className="text-muted-foreground">Action</dt><dd>{selected.action.replaceAll("_", " ")}</dd>
              <dt className="text-muted-foreground">Stage</dt><dd>{selected.stage || "Queued"}</dd>
              <dt className="text-muted-foreground">Attempt</dt><dd>{selected.attemptCount || "Not attempted"}</dd>
              <dt className="text-muted-foreground">Duration</dt><dd>{selected.startedAt ? elapsed(selected.startedAt, selected.finishedAt) : "Not started"}</dd>
            </dl>
            {selected.message && <p className={`mt-4 text-sm break-words ${selected.status === "failed" ? "text-destructive" : "text-muted-foreground"}`}>{selected.message}</p>}
            {plan && plan.id === operation.planId && plan.plan.changes.some((change) => sameIdentity(change.identity, selected.identity)) && <Button className="mt-4" size="sm" variant="outline" onClick={() => { setOpen(false); onReviewResource(selected.identity) }}>Review resource diff</Button>}
          </section>}
          <ul aria-label="Resource progress" className="space-y-1">
            {filtered.map((resource) => <li key={key(resource.identity)}>
              <Button variant="ghost" className={`h-auto w-full justify-start gap-3 px-3 py-3 text-left ${resource === selected ? "bg-muted" : ""}`} aria-pressed={resource === selected} onClick={() => setSelectedKey(key(resource.identity))}>
                <StatusBadge status={resource.status} />
                <span className="min-w-0 flex-1"><span className="block truncate">{resource.identity.name}</span><span className="block truncate text-xs font-normal text-muted-foreground">{resource.identity.kind} · {resource.identity.namespace || "Cluster scoped"}</span></span>
              </Button>
            </li>)}
          </ul>
          {!filtered.length && <p className="py-8 text-center text-sm text-muted-foreground">No resources match your search and status filter.</p>}
        </TabsContent>
        <TabsContent value="pipeline" className="min-h-0 flex-1 space-y-5 overflow-y-auto p-5">
          <ol aria-label="Sync stages" className="divide-y rounded-lg border px-4">
            {progress.stages.map((stage) => <li key={stage.id} className="py-3">
              <div className="flex items-center justify-between gap-3"><span className="font-medium">{stage.title}</span><StatusBadge status={stage.status} /></div>
              {stage.details.map((detail) => <p key={detail.label} className="mt-1 text-xs text-muted-foreground">{detail.label}: {detail.value}</p>)}
            </li>)}
          </ol>
          <p className="text-sm text-muted-foreground">{progress.summary}</p>
          {operation.message && <p className="text-sm break-words">{operation.message}</p>}
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-3 text-sm">
            <dt className="text-muted-foreground">Started</dt><dd>{new Date(operation.startedAt).toLocaleString()}</dd>
            <dt className="text-muted-foreground">Attempt</dt><dd>{operation.attemptCount || 1}</dd>
            {operation.finishedAt && <><dt className="text-muted-foreground">Finished</dt><dd>{new Date(operation.finishedAt).toLocaleString()}</dd></>}
            {operation.planId && <><dt className="text-muted-foreground">Plan</dt><dd className="break-all font-mono text-xs">{operation.planId}</dd></>}
          </dl>
        </TabsContent>
      </Tabs>
    </SheetContent>
  </Sheet>
}
