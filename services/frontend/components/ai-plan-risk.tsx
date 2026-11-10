"use client"

import { useCallback, useEffect, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { AiMagicIcon } from "@hugeicons/core-free-icons"
import { api, errorMessage } from "@/lib/api"
import { ago } from "@/lib/relative-time"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Disclosure } from "@/components/ui/collapsible"
import type { PlanReview } from "@/lib/types"

const levelText = { high: "High risk", medium: "Medium risk", low: "Low risk" } as const
const levelVariant = { high: "destructive", medium: "warning", low: "secondary" } as const

/** Approval briefing: rule-derived review points always, plus an optional AI explanation and checklist. */
export function PlanRiskCard({ workspaceId, planId, digest }: { workspaceId: string; planId: string; digest: string }) {
  const [review, setReview] = useState<PlanReview | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [elapsed, setElapsed] = useState(0)
  const path = `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/ai/plan-review/${encodeURIComponent(planId)}`
  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      setReview(await api<PlanReview>(path, { signal }))
      setError(null)
    } catch (cause) {
      if (!(cause instanceof Error && cause.name === "AbortError")) setError(errorMessage(cause))
    }
  }, [path])
  useEffect(() => {
    const controller = new AbortController()
    void Promise.resolve().then(() => load(controller.signal))
    // A background run can attach a briefing after the page opens.
    const timer = window.setInterval(() => { if (!document.hidden) void load() }, 30000)
    return () => { controller.abort(); window.clearInterval(timer) }
  }, [load, digest])
  useEffect(() => {
    if (!busy) return
    const timer = window.setInterval(() => setElapsed((value) => value + 1), 1000)
    return () => window.clearInterval(timer)
  }, [busy])
  async function generate() {
    setBusy(true)
    setElapsed(0)
    setError(null)
    try {
      setReview(await api<PlanReview>(path, { method: "POST" }))
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }
  if (!review) return error ? <p role="status" className="mb-4 text-xs text-muted-foreground">Review points are unavailable right now.</p> : null
  const { counts } = review
  const outdated = review.source === "ai" && review.stale
  return (
    <section aria-label="Approval briefing" className="mb-4 overflow-hidden rounded-2xl bg-muted/30">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b px-4 py-3">
        <h3 className="text-sm font-semibold">Approval briefing</h3>
        <Badge size="sm"  variant={levelVariant[review.level]}>{levelText[review.level]}</Badge>
        <span className="text-xs text-muted-foreground">{[counts.create && `${counts.create} create`, counts.update && `${counts.update} update`, counts.delete && `${counts.delete} delete`].filter(Boolean).join(" · ")}</span>
        {review.available && (
          <Button type="button" size="sm" variant="outline" className="ml-auto" loading={busy} loadingText={`Briefing… ${elapsed}s`} onClick={() => void generate()}>
            <HugeiconsIcon icon={AiMagicIcon} strokeWidth={1.8} aria-hidden="true" className="text-primary" />
            {review.source === "ai" ? "Refresh briefing" : "Brief with AI"}
          </Button>
        )}
      </div>
      <div className="space-y-4 px-4 py-3">
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        {review.source === "ai" && review.summary && (
          <p className="flex items-start gap-2 text-sm">
            <HugeiconsIcon icon={AiMagicIcon} strokeWidth={1.8} aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-primary" />
            <span>{review.summary}</span>
          </p>
        )}
        {outdated && <p className="text-xs text-warning-foreground dark:text-warning">This plan changed after the briefing was written. Refresh it before relying on it.</p>}
        {review.risks.length === 0
          ? <p className="text-sm text-muted-foreground">No elevated-risk changes detected by the review rules.</p>
          : <ul aria-label="Review points" className="space-y-2">
            {review.risks.slice(0, 8).map((risk, index) => (
              <li key={index} className="flex items-start gap-2 text-sm">
                <Badge size="sm"  variant={levelVariant[risk.level]} className="mt-0.5 shrink-0 capitalize">{risk.level}</Badge>
                <span className="min-w-0"><span className="font-medium">{risk.title}</span>{risk.resource && <span className="text-muted-foreground"> · {risk.resource}</span>}<span className="block text-muted-foreground">{risk.detail}</span></span>
              </li>
            ))}
            {review.risks.length > 8 && <li className="text-xs text-muted-foreground">+{review.risks.length - 8} more review points</li>}
          </ul>}
        {review.concerns.length > 0 && (
          <div className="space-y-2">
            <h4 className="text-xs font-medium text-muted-foreground">Additional concerns from AI</h4>
            <ul className="space-y-2">
              {review.concerns.map((concern, index) => <li key={index} className="text-sm"><span className="font-medium">{concern.title}</span><span className="block text-muted-foreground">{concern.detail}</span></li>)}
            </ul>
          </div>
        )}
        {review.checks.length > 0 && (
          <div className="space-y-1.5">
            <h4 className="text-xs font-medium text-muted-foreground">Before approving</h4>
            <ol className="list-decimal space-y-1 pl-5 text-sm">{review.checks.map((check, index) => <li key={index}>{check}</li>)}</ol>
          </div>
        )}
        {review.images.length > 0 && (
          <Disclosure summary={`Image changes (${review.images.length})`}>
            <ul className="space-y-1 font-mono text-xs break-all text-muted-foreground">{review.images.map((image) => <li key={image}>{image}</li>)}</ul>
          </Disclosure>
        )}
        {review.source === "ai" && review.generatedAt && (
          <p className="text-xs text-muted-foreground">Briefing by {review.model} {ago(review.generatedAt)}. {review.limitations[0]}</p>
        )}
      </div>
    </section>
  )
}
