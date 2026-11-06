"use client"

import { useMemo } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Button } from "@/components/ui/button"
import { EmptyState } from "@/components/ui-kit"
import { packedTopologyScene, supportingTopologyResource, topologyScope } from "@/lib/topology-map"
import type { SyncState } from "@/lib/topology-view"
import type { ResourceTopology, TopologyNode } from "@/lib/types"

type Icon = Parameters<typeof HugeiconsIcon>[0]["icon"]

/** Readable default view: one card per workload group with a health roll-up. */
export function TopologyOverview({ graph, selectedId, onSelect, query, mode, isChanged, isConflict, stateFor, syncLabels, healthFor, toneFor, iconFor, onShowInGraph }: {
  graph: ResourceTopology
  selectedId: string | null
  onSelect: (id: string | null) => void
  query: string
  mode: string
  isChanged: (node: TopologyNode) => boolean
  isConflict: (node: TopologyNode) => boolean
  stateFor: (node: TopologyNode) => SyncState
  syncLabels: Record<SyncState, string>
  healthFor: (node: TopologyNode) => string
  toneFor: (node: TopologyNode) => string
  iconFor: (kind: string) => Icon
  onShowInGraph: (text: string) => void
}) {
  const needle = query.trim().toLowerCase()
  const groups = useMemo(() => {
    const matches = (node: TopologyNode) =>
      (!needle || `${node.identity.kind} ${node.identity.namespace} ${node.identity.name}`.toLowerCase().includes(needle)) &&
      (mode === "all" || (mode === "observed" ? ["kubernetes", "sample"].includes(node.source) : mode === "conflicts" ? isConflict(node) : isChanged(node)))
    const candidates = [...new Map([...topologyScope(graph, null, false), ...graph.nodes.filter(supportingTopologyResource)].map((node) => [node.id, node])).values()].filter(matches)
    const byId = new Map(candidates.map((node) => [node.id, node]))
    const scene = packedTopologyScene(graph, candidates.map((node) => node.id), { width: 1200, height: 600 })
    const placed = new Set<string>()
    const result = scene.groups.map((group) => {
      const nodes = group.resourceIds.map((id) => byId.get(id)).filter((node): node is TopologyNode => Boolean(node))
      nodes.forEach((node) => placed.add(node.id))
      return { id: group.id, title: group.title, nodes }
    }).filter((group) => group.nodes.length)
    const rest = candidates.filter((node) => !placed.has(node.id))
    if (rest.length) result.push({ id: "__rest", title: "Other resources", nodes: rest })
    return result
  }, [graph, needle, mode, isChanged, isConflict])

  if (!groups.length) return <div className="min-w-0 p-6"><EmptyState title={graph.nodes.length ? "No matching resources" : "No resources to show yet"} description={graph.nodes.length ? "Try another filter or search term." : "Build a plan to discover resources and their relationships."} /></div>

  return <div className="grid min-w-0 gap-4 p-4 [grid-template-columns:repeat(auto-fill,minmax(280px,1fr))]">
    {groups.map((group) => {
      const problems = group.nodes.filter((node) => isConflict(node) || stateFor(node) === "failed" || toneFor(node).includes("text-destructive")).length
      const changes = group.nodes.filter(isChanged).length
      return <section key={group.id} aria-label={group.title} className="flex min-w-0 flex-col rounded-xl border bg-card">
        <header className="flex items-start justify-between gap-3 border-b px-4 py-3">
          <div className="min-w-0">
            <h3 className="truncate text-sm font-semibold" title={group.title}>{group.title}</h3>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {group.nodes.length} {group.nodes.length === 1 ? "resource" : "resources"}
              {changes > 0 && ` · ${changes} to change`}
              {problems > 0 && <span className="text-destructive"> · {problems} need attention</span>}
            </p>
          </div>
          {!group.id.startsWith("__") && <Button type="button" size="xs" variant="ghost" aria-label={`Show ${group.title} in graph`} onClick={() => onShowInGraph(group.title)}>Graph</Button>}
        </header>
        <ul className="divide-y">
          {group.nodes.map((node) => {
            const state = stateFor(node)
            const note = isConflict(node) ? "Owned elsewhere" : state === "unknown" || state === "observed" ? toneFor(node).includes("text-destructive") ? healthFor(node) : "" : syncLabels[state]
            return <li key={node.id}>
              <Button type="button" variant={selectedId === node.id ? "secondary" : "ghost"} aria-pressed={selectedId === node.id} className="h-auto w-full items-start justify-start gap-3 rounded-none px-4 py-2 text-left whitespace-normal focus-visible:ring-inset" onClick={() => onSelect(node.id)}>
                <HugeiconsIcon icon={iconFor(node.identity.kind)} strokeWidth={1.6} className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium break-all">{node.identity.name}</span>
                  <span className="block text-xs text-muted-foreground">{node.identity.kind}</span>
                  {note && <span title={note} className={`mt-0.5 block text-xs break-words line-clamp-3 ${toneFor(node).includes("text-destructive") || isConflict(node) ? "text-destructive" : "text-muted-foreground"}`}>{note}</span>}
                </span>
              </Button>
            </li>
          })}
        </ul>
      </section>
    })}
  </div>
}
