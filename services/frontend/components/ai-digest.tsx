"use client"

import Link from "next/link"
import { useCallback, useEffect, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { AiMagicIcon } from "@hugeicons/core-free-icons"
import { api, errorMessage } from "@/lib/api"
import { ago } from "@/lib/relative-time"
import { Button } from "@/components/panelui/button"
import { Frame } from "@/components/panelui/frame"
import { Tabs } from "@/components/panelui/tabs"
import type { Digest } from "@/lib/types"

const sectionLabel = { deployed: "Deployed", failed: "Failed", attention: "Needs attention", waiting: "Waiting for you" } as const

/** Activity digest for the workspace: plain counts always, an AI narrative on demand or automatically. */
export function WorkspaceDigest({ workspaceId }: { workspaceId: string }) {
  const [period, setPeriod] = useState<"day" | "week">("day")
  const [digest, setDigest] = useState<Digest | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const path = `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/ai/digest?period=${period}`
  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      setDigest(await api<Digest>(path, { signal }))
      setError(null)
    } catch (cause) {
      if (!(cause instanceof Error && cause.name === "AbortError")) setError(errorMessage(cause))
    }
  }, [path])
  useEffect(() => {
    const controller = new AbortController()
    void Promise.resolve().then(() => load(controller.signal))
    return () => controller.abort()
  }, [load])
  async function generate() {
    setBusy(true)
    setError(null)
    try {
      setDigest(await api<Digest>(path, { method: "POST" }))
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }
  const stats = digest?.stats
  const window = period === "day" ? "24 hours" : "7 days"
  const grouped = (["failed", "attention", "waiting", "deployed"] as const)
    .map((section) => [section, digest?.items.filter((item) => item.section === section) ?? []] as const)
    .filter(([, items]) => items.length > 0)
  return (
    <Frame variant="plain">
      <Frame.Header>
        <Frame.Title as="h2">Activity digest</Frame.Title>
        <Frame.Description>{`What shipped, what failed and what needs a decision in the last ${window}.`}</Frame.Description>
        <Frame.Action>
          <div className="flex flex-wrap items-center justify-end gap-2">
            <Tabs value={period} onValueChange={(next) => { if (next === "day" || next === "week") setPeriod(next) }}>
              <Tabs.List aria-label="Digest period">
                <Tabs.Trigger value="day">24 hours</Tabs.Trigger>
                <Tabs.Trigger value="week">7 days</Tabs.Trigger>
              </Tabs.List>
            </Tabs>
            {digest?.available && (
              <Button type="button" size="sm" variant="outline" loading={busy} onClick={() => void generate()}>
                <HugeiconsIcon icon={AiMagicIcon} strokeWidth={1.8} aria-hidden="true" className="text-primary" />
                {busy ? "Writing…" : digest.source === "ai" ? "Refresh" : "Write with AI"}
              </Button>
            )}
          </div>
        </Frame.Action>
      </Frame.Header>
      <Frame.Panel>
      <div className="space-y-4 p-5">
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        {!digest && !error && <p role="status" className="text-sm text-muted-foreground">Loading digest…</p>}
        {stats && (
          <>
            <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              {([["Deployments", stats.operations], ["Succeeded", stats.succeeded], ["Failed", stats.failed], ["Rollbacks", stats.rollbacks]] as const).map(([label, value]) => (
                <div key={label} className="rounded-lg border px-3 py-2"><dt className="text-xs text-muted-foreground">{label}</dt><dd className={`text-xl font-semibold tabular-nums ${label === "Failed" && value > 0 ? "text-destructive" : ""}`}>{value}</dd></div>
              ))}
            </dl>
            {digest.source === "ai" && digest.summary ? (
              <p className="flex items-start gap-2 text-sm">
                <HugeiconsIcon icon={AiMagicIcon} strokeWidth={1.8} aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-primary" />
                <span>{digest.summary}</span>
              </p>
            ) : stats.operations === 0 && stats.waiting.length === 0 && stats.attention.length === 0 ? (
              <p className="text-sm text-muted-foreground">Nothing happened in this period.</p>
            ) : null}
            {grouped.map(([section, items]) => (
              <div key={section} className="space-y-2">
                <h3 className="text-xs font-medium text-muted-foreground">{sectionLabel[section]}</h3>
                <ul className="space-y-2">
                  {items.map((item, index) => (
                    <li key={index} className="text-sm">
                      {item.applicationId
                        ? <Link href={`/applications/${item.applicationId}${section === "waiting" ? "?tab=changes" : section === "failed" ? "?tab=activity" : ""}`} className="font-medium underline-offset-4 hover:underline">{item.title}</Link>
                        : <span className="font-medium">{item.title}</span>}
                      <span className="block text-muted-foreground">{item.detail}</span>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
            {digest.source !== "ai" && stats.applications.some((entry) => entry.failed > 0) && (
              <div className="space-y-2">
                <h3 className="text-xs font-medium text-muted-foreground">Failed deployments</h3>
                <ul className="space-y-1.5">
                  {stats.applications.filter((entry) => entry.failed > 0).slice(0, 5).map((entry) => (
                    <li key={entry.applicationId} className="text-sm"><Link href={`/applications/${entry.applicationId}?tab=activity`} className="font-medium underline-offset-4 hover:underline">{entry.name}</Link><span className="text-muted-foreground"> · {entry.failed} failed{entry.lastMessage ? ` · ${entry.lastMessage}` : ""}</span></li>
                  ))}
                </ul>
              </div>
            )}
            {digest.source === "ai" && digest.generatedAt && (
              <p className="text-xs text-muted-foreground">Written by {digest.model} {ago(digest.generatedAt)}{digest.stale ? " · counts above are live; refresh to update the text" : ""}.</p>
            )}
          </>
        )}
      </div>
    </Frame.Panel>
    </Frame>
  )
}
