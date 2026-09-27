"use client"

import { useRef, useState, type ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormSelect } from "@/components/ui/form-select"
import { EmptyState } from "@/components/ui-kit"
import type { Change } from "@/lib/types"

export function PlanReview({ changes, ignored, selected, onSelect, identityKey, isExcluded, renderChange }: {
  changes: Change[]; ignored: Change[]; selected: string; onSelect: (key: string) => void
  identityKey: (change: Change) => string
  isExcluded: (change: Change) => boolean
  renderChange: (change: Change, ignored: boolean) => ReactNode
}) {
  const [query, setQuery] = useState("")
  const [filter, setFilter] = useState("all")
  const [fullscreenError, setFullscreenError] = useState("")
  const root = useRef<HTMLElement>(null)
  const entries = [...changes.map((change) => ({ change, ignored: false })), ...ignored.map((change) => ({ change, ignored: true }))]
  const key = (entry: typeof entries[number]) => `${entry.ignored ? "ignored:" : ""}${identityKey(entry.change)}`
  const matching = entries.filter((entry) => {
    const id = entry.change.identity
    return (filter === "all" || (filter === "ignored" ? entry.ignored || isExcluded(entry.change) : !entry.ignored && entry.change.kind === filter)) && `${id.kind} ${id.namespace} ${id.name}`.toLowerCase().includes(query.toLowerCase())
  })
  const active = matching.find((entry) => key(entry) === selected) ?? matching[0]
  async function toggleFullscreen() {
    try {
      if (document.fullscreenElement) await document.exitFullscreen()
      else await root.current?.requestFullscreen()
    } catch { setFullscreenError("Fullscreen is unavailable in this browser. The review remains available below.") }
  }
  return <section ref={root} aria-label="Resource review" className="min-w-0 bg-card fullscreen:overflow-auto fullscreen:p-5">
    <div className="mb-3 flex flex-wrap items-center justify-between gap-3"><h3 className="text-base font-semibold">Review resources <span className="font-normal text-muted-foreground">({entries.length})</span></h3><Button type="button" variant="outline" onClick={() => void toggleFullscreen()}>Toggle fullscreen</Button></div>
    {fullscreenError && <p role="status" className="mb-3 text-sm text-muted-foreground">{fullscreenError}</p>}
    <div className="grid min-w-0 gap-4 lg:grid-cols-[260px_minmax(0,1fr)]">
      <nav aria-label="Plan resources" className="min-w-0 rounded-lg border bg-muted/10 p-3 lg:self-start">
        <Input aria-label="Search plan resources" placeholder="Find a resource…" value={query} onChange={(event) => setQuery(event.target.value)} />
        <FormSelect ariaLabel="Filter changes" value={filter} onValueChange={setFilter} className="my-2" items={[{ value: "all", label: "All resources" }, { value: "create", label: "Create" }, { value: "update", label: "Update" }, { value: "delete", label: "Delete" }, { value: "ignored", label: "Excluded" }]} />
        <p className="mb-2 text-xs text-muted-foreground" role="status">{matching.length} of {entries.length} resources</p>
        <div className="h-48 overflow-auto lg:h-[60svh]">
          {matching.map((entry) => <Button type="button" key={key(entry)} variant={active === entry ? "secondary" : "ghost"} aria-current={active === entry ? "true" : undefined} className="mb-1 h-auto w-full justify-start whitespace-normal px-3 py-3 text-left" onClick={() => onSelect(key(entry))}>
            <span className="min-w-0"><span className="block break-all text-sm font-medium">{entry.change.identity.name}</span><span className="mt-1 block text-xs text-muted-foreground">{entry.change.identity.kind} · {entry.change.identity.namespace || "cluster"}</span><span className={`mt-2 block text-xs capitalize ${entry.change.kind === "delete" ? "text-rose-600 dark:text-rose-300" : entry.change.kind === "create" ? "text-emerald-700 dark:text-emerald-300" : "text-blue-600 dark:text-blue-300"}`}>{entry.change.kind}{entry.ignored ? " · Excluded" : isExcluded(entry.change) ? " · Exclusion draft" : ""}{entry.change.takeover ? " · Takeover" : ""}</span></span>
          </Button>)}
        </div>
      </nav>
      <div className="min-w-0">{active ? renderChange(active.change, active.ignored) : <div className="rounded-xl border border-dashed"><EmptyState title="No matching resources" description="Try a different resource filter or search term." /></div>}</div>
    </div>
  </section>
}
