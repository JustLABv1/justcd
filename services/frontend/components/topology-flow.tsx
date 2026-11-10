"use client"

import { useMemo } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/panelui/tooltip"
import { Card } from "@/components/panelui/card"
import { EmptyState } from "@/components/ui-kit"
import { flowModel, type FlowPodGroup, type FlowRow } from "@/lib/topology-flow"
import { podSummary, runtimeSignal, type RuntimeSignal } from "@/lib/topology-map"
import { parseImage } from "@/lib/container-image"
import { podLabel, podState, type PodState } from "@/lib/runtime-health"
import type { SyncState } from "@/lib/topology-view"
import type { ResourceTopology, TopologyNode } from "@/lib/types"

type Icon = Parameters<typeof HugeiconsIcon>[0]["icon"]

const signalDot: Record<RuntimeSignal, string> = { healthy: "bg-success", progressing: "bg-warning", degraded: "bg-destructive", unknown: "bg-muted-foreground/40" }
const podTone: Record<PodState, string> = {
  ready: "border-success/50 bg-success/10",
  starting: "border-warning/60 bg-warning/10",
  failing: "border-destructive/60 bg-destructive/10",
  terminating: "border-dashed border-muted-foreground/50 bg-muted/40 opacity-70",
  completed: "border-info/50 bg-info/10",
  unknown: "border-border bg-muted/30",
  sample: "border-dashed border-border bg-muted/30",
}
const podDot: Record<PodState, string> = { ready: "bg-success", starting: "bg-warning", failing: "bg-destructive", terminating: "bg-muted-foreground", completed: "bg-info", unknown: "bg-muted-foreground/40", sample: "bg-muted-foreground/40" }

type Props = {
  graph: ResourceTopology
  selectedId: string | null
  onSelect: (id: string | null) => void
  query: string
  mode: string
  focus: boolean
  related: ReadonlySet<string>
  stateFor: (node: TopologyNode) => SyncState
  syncLabels: Record<SyncState, string>
  healthFor: (node: TopologyNode) => string
  toneFor: (node: TopologyNode) => string
  iconFor: (kind: string) => Icon
  isChanged: (node: TopologyNode) => boolean
  isConflict: (node: TopologyNode) => boolean
}

function Arrow() {
  return <span aria-hidden="true" className="grid shrink-0 place-items-center text-muted-foreground/60 @max-3xl:rotate-90 @max-3xl:py-0.5"><HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={1.8} className="size-5" /></span>
}

function Lane({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="flex min-w-0 flex-col gap-1.5"><h4 className="text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">{label}</h4>{children}</div>
}

function Placeholder({ children }: { children: React.ReactNode }) {
  return <p className="py-1 text-xs text-muted-foreground">{children}</p>
}

/** Left-to-right pipeline per workload: traffic → services → replica sets and their pods. */
export function TopologyFlow(props: Props) {
  const { graph, selectedId, onSelect, query, mode, focus, related, stateFor, syncLabels, healthFor, toneFor, iconFor, isChanged, isConflict } = props
  const model = useMemo(() => flowModel(graph), [graph])
  const needle = query.trim().toLowerCase()
  const matches = (node: TopologyNode) =>
    (!needle || `${node.identity.kind} ${node.identity.namespace} ${node.identity.name}`.toLowerCase().includes(needle)) &&
    (mode === "all" || (mode === "observed" ? ["kubernetes", "sample"].includes(node.source) : mode === "conflicts" ? isConflict(node) : isChanged(node)))
  const nodeIndex = useMemo(() => new Map(graph.nodes.map((node) => [node.id, node])), [graph])
  const filtering = Boolean(needle) || mode !== "all"
  const rowVisible = (row: FlowRow) => {
    if (focus && selectedId) return row.members.has(selectedId) || [...row.members].some((id) => related.has(id))
    return !filtering || [...row.members].some((id) => { const node = nodeIndex.get(id); return node && matches(node) })
  }
  const rows = model.rows.filter(rowVisible)
  const other = focus && selectedId ? [] : model.other.filter((node) => !filtering || matches(node))

  const chip = (node: TopologyNode) => {
    const bad = isConflict(node) || toneFor(node).includes("text-destructive")
    return <Button key={node.id} type="button" variant="secondary" aria-pressed={selectedId === node.id} className={`h-auto w-full items-start justify-start gap-2.5 px-3 py-2 text-left whitespace-normal ${selectedId === node.id ? "ring-2 ring-primary" : ""}`} onClick={() => onSelect(node.id)}>
      <HugeiconsIcon icon={iconFor(node.identity.kind)} strokeWidth={1.6} className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1.5"><i aria-hidden="true" className={`size-2 shrink-0 rounded-full ${signalDot[runtimeSignal(node)]}`} /><span className="min-w-0 text-sm font-medium break-words">{node.identity.name}</span></span>
        <span className="block text-xs text-muted-foreground">{node.identity.kind}{stateFor(node) !== "unknown" && stateFor(node) !== "observed" && ` · ${syncLabels[stateFor(node)]}`}</span>
        <span className={`block text-xs break-words line-clamp-2 ${bad ? "text-destructive" : "text-muted-foreground"}`}>{isConflict(node) ? "Owned elsewhere" : healthFor(node)}</span>
      </span>
    </Button>
  }

  const podTile = (pod: TopologyNode, prefix: string) => {
    const state = podState(pod)
    const short = pod.identity.name.startsWith(prefix) ? pod.identity.name.slice(prefix.length).replace(/^-/, "") || pod.identity.name : pod.identity.name
    const restarts = pod.healthSummary?.restartCount ?? 0
    const containers = pod.healthSummary ? `${pod.healthSummary.readyContainers ?? 0}/${pod.healthSummary.totalContainers ?? 0} containers ready` : ""
    return <Tooltip key={pod.id}>
      <TooltipTrigger render={<Button type="button" variant="ghost" aria-pressed={selectedId === pod.id} aria-label={`Pod ${pod.identity.name}: ${podLabel(pod)}`} onClick={() => onSelect(pod.id)} className={`h-auto min-w-28 flex-col items-start gap-0.5 rounded-lg border px-2.5 py-1.5 text-left whitespace-normal ${podTone[state]} ${selectedId === pod.id ? "ring-2 ring-info" : ""}`} />}>
        <span className="flex w-full items-center gap-1.5"><i aria-hidden="true" className={`size-2 shrink-0 rounded-full ${podDot[state]} ${state === "starting" ? "animate-pulse" : ""}`} /><span className="truncate font-mono text-xs font-medium">{short}</span></span>
        <span className={`text-[11px] leading-4 ${state === "failing" ? "font-medium text-destructive" : "text-muted-foreground"}`}>{podLabel(pod)}{restarts > 0 && ` · ${restarts}↻`}</span>
      </TooltipTrigger>
      <TooltipContent>{pod.identity.name} · {podLabel(pod)}{containers && ` · ${containers}`}{restarts > 0 && ` · ${restarts} restarts`}</TooltipContent>
    </Tooltip>
  }

  const podGroup = (group: FlowPodGroup, row: FlowRow) => {
    const prefix = group.replicaSet?.identity.name ?? row.workload.identity.name
    const hidden = group.pods.length === 0
    const versions = [...new Set((group.replicaSet?.healthSummary?.containers ?? group.pods[0]?.healthSummary?.containers ?? []).filter((container) => !container.init && container.image).map((container) => parseImage(container.image).version))].join(", ")
    return <div key={group.id} className={`flex min-w-0 flex-col gap-1.5 ${hidden ? "opacity-60" : ""}`}>
      {group.replicaSet && <Button variant="ghost" type="button" onClick={() => onSelect(group.replicaSet!.id)} className="w-fit rounded text-left text-[11px] text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"><span className="font-mono">{group.replicaSet.identity.name.slice(row.workload.identity.name.length).replace(/^-/, "") || group.replicaSet.identity.name}</span> · {group.pods.length === 0 ? "scaled down" : `${group.pods.length} ${group.pods.length === 1 ? "pod" : "pods"}`}{versions && <> · <span className="font-mono text-foreground">{versions}</span></>}</Button>}
      {group.pods.length > 0 && <div className="flex flex-wrap gap-2">{group.pods.map((pod) => podTile(pod, prefix))}</div>}
    </div>
  }

  const flowRow = (row: FlowRow) => {
    const { workload } = row
    const summary = podSummary(row.pods)
    const bad = isConflict(workload) || toneFor(workload).includes("text-destructive")
    const showGroups = row.groups.filter((group) => group.pods.length > 0 || row.groups.length === 1)
    const scaledDown = row.groups.length - showGroups.length
    return <Card size="sm" key={row.id} aria-label={workload.identity.name} className="@container min-w-0">
      <Card.Header>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
        <Button variant="ghost" type="button" onClick={() => onSelect(workload.id)} className={`-mx-2 flex h-8 min-w-0 items-center gap-2 rounded px-2 text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none ${selectedId === workload.id ? "text-info" : ""}`}>
          <HugeiconsIcon icon={iconFor(workload.identity.kind)} strokeWidth={1.6} className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <i aria-hidden="true" className={`size-2 shrink-0 rounded-full ${signalDot[runtimeSignal(workload)]}`} />
          <h3 className="truncate text-sm font-semibold">{workload.identity.name}</h3>
          <span className="text-xs text-muted-foreground">{workload.identity.kind}</span>
        </Button>
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge size="sm" variant={bad ? "destructive" : ({ healthy: "success", progressing: "warning", degraded: "destructive", unknown: "secondary" } as const)[runtimeSignal(workload)]} dot>{isConflict(workload) ? "Owned elsewhere" : summary ?? healthFor(workload)}</Badge>
          {stateFor(workload) !== "unknown" && stateFor(workload) !== "observed" && <Badge size="sm" variant="outline">{syncLabels[stateFor(workload)]}</Badge>}
        </div>
      </div>
      </Card.Header>
      <Card.Content>
      <div className="grid gap-4 @3xl:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)_auto_minmax(0,1fr)] @3xl:items-start">
        <Lane label="Traffic">{row.traffic.length ? row.traffic.map(chip) : <Placeholder>No ingress or route</Placeholder>}</Lane>
        <Arrow />
        <Lane label="Service">{row.services.length ? row.services.map(chip) : <Placeholder>No service selects these pods</Placeholder>}</Lane>
        <Arrow />
        <Lane label={workload.identity.kind === "Pod" ? "Pod" : "Pods"}>
          {showGroups.length ? <div className="flex flex-col gap-3">{showGroups.map((group) => podGroup(group, row))}{scaledDown > 0 && <p className="text-[11px] text-muted-foreground">{scaledDown} older {scaledDown === 1 ? "revision" : "revisions"} scaled to zero</p>}</div> : <Placeholder>{workload.uid ? "No pods running" : "Not observed in the cluster yet"}</Placeholder>}
        </Lane>
      </div>
      {row.config.length > 0 && <div className="flex flex-wrap items-center gap-2 pt-4"><span className="text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">Uses</span>{row.config.map((node) => <Button key={node.id} type="button" size="sm" variant={selectedId === node.id ? "secondary" : "outline"} aria-pressed={selectedId === node.id} onClick={() => onSelect(node.id)}><i aria-hidden="true" className={`size-1.5 rounded-full ${signalDot[runtimeSignal(node)]}`} />{node.identity.name}<span className="text-muted-foreground">{node.identity.kind}</span></Button>)}</div>}
      </Card.Content>
    </Card>
  }

  if (!rows.length && !other.length) return <div className="min-w-0 p-6"><EmptyState title={graph.nodes.length ? "No matching resources" : "No resources to show yet"} description={graph.nodes.length ? "Try another filter or search term." : "Build a plan to discover resources and their relationships."} /></div>

  const workloadsNeedingAttention = rows.filter((row) => [row.workload, ...row.pods].some((node) => runtimeSignal(node) === "degraded" || isConflict(node))).length
  const podsReady = rows.flatMap((row) => row.pods).filter((pod) => podState(pod) === "ready" || podState(pod) === "completed").length
  const podTotal = rows.reduce((total, row) => total + row.pods.length, 0)
  const toChange = graph.nodes.filter(isChanged).length
  return <div className="flex min-w-0 flex-col gap-4">
    {rows.length > 1 && <div role="status" className="flex flex-wrap items-center gap-2">
      <Badge variant="secondary">{rows.length} {rows.length === 1 ? "workload" : "workloads"}</Badge>
      <Badge variant="outline" className="bg-background">{podsReady}/{podTotal} pods ready</Badge>
      {workloadsNeedingAttention > 0 ? <Badge variant="destructive" dot>{workloadsNeedingAttention} need attention</Badge> : <Badge variant="success" dot>None need attention</Badge>}
      {toChange > 0 && <Badge variant="warning" dot>{toChange} to change</Badge>}
    </div>}
    {rows.map(flowRow)}
    {other.length > 0 && <Card size="sm" aria-label="Other resources" className="min-w-0">
      <Card.Header><Card.Title as="h3" className="text-sm">Other resources</Card.Title><Card.Description>Not attached to a workload.</Card.Description></Card.Header>
      <Card.Content><div className="grid gap-2 [grid-template-columns:repeat(auto-fill,minmax(min(100%,280px),1fr))]">{other.map(chip)}</div></Card.Content>
    </Card>}
  </div>
}
