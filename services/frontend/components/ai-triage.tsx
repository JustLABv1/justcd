"use client"

import Link from "next/link"
import { useCallback, useEffect, useRef, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Alert02Icon, AiMagicIcon, ArrowRight01Icon, InformationCircleIcon } from "@hugeicons/core-free-icons"
import { api, errorMessage } from "@/lib/api"
import { ago } from "@/lib/relative-time"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import type { Triage, TriageItem, TriageSeverity } from "@/lib/types"

const severityLabel: Record<TriageSeverity, string> = { critical: "Critical", high: "High", medium: "Medium", low: "Low" }
const severityVariant = { critical: "destructive-light", high: "warning-light", medium: "secondary", low: "outline" } as const
const stageLabel: Record<string, string> = { collecting: "Collecting evidence", analyzing: "Ranking with AI", saving: "Saving result" }

/** Loads the workspace triage and keeps it fresh: fast polling while a run is active, slow polling otherwise. */
export function useTriage(workspaceId: string) {
  const [triage, setTriage] = useState<Triage | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [starting, setStarting] = useState(false)
  const path = workspaceId ? `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/ai/triage` : ""
  const state = useRef<Triage["state"]>("idle")
  const load = useCallback(async (signal?: AbortSignal) => {
    if (!path) return
    try {
      const next = await api<Triage>(path, { signal })
      state.current = next.state
      setTriage(next)
      setError(null)
    } catch (cause) {
      if (!(cause instanceof Error && cause.name === "AbortError")) setError(errorMessage(cause))
    }
  }, [path])
  useEffect(() => {
    const controller = new AbortController()
    let timer: number
    const tick = async () => {
      await load(controller.signal)
      if (!controller.signal.aborted) timer = window.setTimeout(() => void tick(), state.current === "running" ? 2000 : 60000)
    }
    void tick()
    const refresh = () => void load()
    window.addEventListener("justcd:ai-policy-updated", refresh)
    return () => {
      controller.abort()
      window.clearTimeout(timer)
      window.removeEventListener("justcd:ai-policy-updated", refresh)
    }
  }, [load])
  const run = useCallback(async () => {
    if (!path) return
    setStarting(true)
    try {
      const next = await api<Triage>(path, { method: "POST" })
      state.current = next.state
      setTriage(next)
      setError(null)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setStarting(false)
    }
  }, [path])
  return { triage, error, starting, run }
}

/** Header controls: an info popover with the ranking details and the refresh action. */
export function TriageStatus({ triage, starting, onRun }: { triage: Triage | null; starting: boolean; onRun: () => void }) {
  if (!triage) return null
  const running = triage.state === "running" || starting
  const stage = `${stageLabel[triage.stage ?? "collecting"] ?? "Working"}…`
  const outdated = triage.stale && triage.source === "ai" && !running
  const rows: [string, string][] = [
    ["Ranking", triage.source === "ai" ? "AI, based on rule-derived facts" : "Rules only (no AI ranking yet)"],
    ...(triage.generatedAt ? [["Generated", `${ago(triage.generatedAt)}${triage.model ? ` · ${triage.model}` : ""}`] as [string, string]] : []),
    ["Updates", triage.autoTriage && triage.available ? "Automatically after changes settle" : triage.available ? "When you re-rank" : "AI is not enabled"],
  ]
  return (
    <div className="flex items-center justify-end gap-1.5">
      <span className="sr-only" role="status" aria-live="polite">{running ? stage : outdated ? "Ranking is out of date" : ""}</span>
      {running && <span aria-hidden="true" className="text-xs text-muted-foreground">{stage}</span>}
      <Popover>
        <PopoverTrigger render={<Button type="button" variant="ghost" size="icon-sm" aria-label="About this ranking" title="About this ranking" />}>
          <HugeiconsIcon icon={outdated ? Alert02Icon : InformationCircleIcon} strokeWidth={1.8} aria-hidden="true" className={outdated ? "text-warning-foreground dark:text-warning" : "text-muted-foreground"} />
        </PopoverTrigger>
        <PopoverContent align="end" className="w-80 gap-3 p-3">
          <p className="text-sm font-medium">How these are ranked</p>
          {outdated && <p className="rounded-md bg-warning/10 px-2.5 py-2 text-xs text-warning-foreground dark:text-warning">Applications changed since this ranking. New or worsened ones are already placed by rules; re-rank to refresh the explanations.</p>}
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-xs">
            {rows.map(([label, value]) => <div key={label} className="contents"><dt className="text-muted-foreground">{label}</dt><dd>{value}</dd></div>)}
          </dl>
          <p className="text-xs leading-5 text-muted-foreground">Down applications come first, then ones still serving while a rollout fails, then blocked or paused delivery. The model can explain and raise a ranking, never lower it.</p>
        </PopoverContent>
      </Popover>
      {triage.available && triage.attentionCount > 0 && (
        <Button type="button" variant="outline" size="sm" loading={running} loadingText="Ranking…" onClick={onRun}>
          <HugeiconsIcon icon={AiMagicIcon} strokeWidth={1.8} aria-hidden="true" className="text-primary" />
          {triage.source === "ai" ? "Re-rank" : "Rank with AI"}
        </Button>
      )}
    </div>
  )
}

export function TriageList({ triage, limit = 5, attentionHref }: { triage: Triage; limit?: number; attentionHref: string }) {
  const items = triage.items.slice(0, limit)
  return (
    <>
      {triage.source === "ai" && triage.summary && (
        <p className="flex items-start gap-2 border-b bg-muted/30 px-5 py-3 text-sm">
          <HugeiconsIcon icon={AiMagicIcon} strokeWidth={1.8} aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-primary" />
          <span>{triage.summary}</span>
        </p>
      )}
      <ol aria-label="Applications ranked by priority" className="divide-y">
        {items.map((item, index) => <TriageRow key={item.applicationId} item={item} rank={index + 1} />)}
      </ol>
      {triage.items.length > limit && (
        <p className="border-t px-5 py-3 text-sm text-muted-foreground">
          +{triage.items.length - limit} more · <Link href={attentionHref} className="text-primary underline underline-offset-4">View all</Link>
        </p>
      )}
      {triage.source === "ai" && triage.limitations.length > 0 && (
        <p className="border-t px-5 py-2 text-xs text-muted-foreground">{triage.limitations[0]}</p>
      )}
    </>
  )
}

function TriageRow({ item, rank }: { item: TriageItem; rank: number }) {
  const href = `/applications/${item.applicationId}${item.actionTab && item.actionTab !== "overview" ? `?tab=${item.actionTab}` : ""}`
  return (
    <li>
      <Link href={href} className="flex items-start gap-3 px-5 py-4 transition-colors hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:outline-none">
        <span aria-hidden="true" className="mt-0.5 grid size-6 shrink-0 place-items-center rounded-full bg-muted text-xs font-medium text-muted-foreground">{rank}</span>
        <span className="min-w-0 flex-1 space-y-1">
          <span className="flex flex-wrap items-center gap-2">
            <span className="truncate text-sm font-medium">{item.applicationName}</span>
            <Badge size="sm" radius="full" variant={severityVariant[item.severity]}>{severityLabel[item.severity]}</Badge>
          </span>
          <span className="block text-sm">{item.headline}</span>
          <span className="block text-sm text-muted-foreground">{item.impact}</span>
          <span className="block text-sm"><span className="text-muted-foreground">Next: </span>{item.action}</span>
          {item.facts && item.facts.length > 0 && <span className="block text-xs text-muted-foreground">{item.facts.slice(0, 2).join(" · ")}</span>}
        </span>
        <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={1.8} aria-hidden="true" className="mt-1 size-4 shrink-0 text-muted-foreground" />
      </Link>
    </li>
  )
}
