"use client"

import { useCallback, useEffect, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { AiMagicIcon } from "@hugeicons/core-free-icons"
import { api, errorMessage } from "@/lib/api"
import { diffId } from "@/components/application/helpers"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import type { ConflictGroup, ConflictGuidance } from "@/lib/types"

const recommendation = {
  adopt: { label: "Adopt", variant: "success" },
  leave: { label: "Leave alone", variant: "secondary" },
  exclude: { label: "Exclude", variant: "warning" },
  investigate: { label: "Investigate", variant: "warning" },
} as const

/** Groups conflicts by owner and says what to do with each group; adoptable groups can be selected in one click. */
export function ConflictGuidancePanel({ applicationId, claimableKeys, selectedKeys, onSelectedKeysChange, canApprove }: {
  applicationId: string
  claimableKeys: string[]
  selectedKeys: string[]
  onSelectedKeysChange: (keys: string[]) => void
  canApprove: boolean
}) {
  const [guidance, setGuidance] = useState<ConflictGuidance | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const path = `/api/v1/applications/${encodeURIComponent(applicationId)}/ownership-conflict/guidance`
  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      setGuidance(await api<ConflictGuidance>(path, { signal }))
    } catch (cause) {
      if (!(cause instanceof Error && cause.name === "AbortError")) setError(errorMessage(cause))
    }
  }, [path])
  useEffect(() => {
    const controller = new AbortController()
    void Promise.resolve().then(() => load(controller.signal))
    return () => controller.abort()
  }, [load])
  async function explain() {
    setBusy(true)
    setError(null)
    try {
      setGuidance(await api<ConflictGuidance>(path, { method: "POST" }))
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }
  if (!guidance || guidance.groups.length === 0) return error ? <p role="status" className="text-xs text-muted-foreground">Guidance is unavailable right now.</p> : null
  const claimable = new Set(claimableKeys)
  const eligibleKeys = (group: ConflictGroup) => group.identities.map(diffId).filter((key) => claimable.has(key))
  return (
    <section aria-label="Resolution guidance" className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-semibold">Suggested resolution</h3>
        {guidance.available && (
          <Button type="button" size="sm" variant="outline" loading={busy} loadingText="Analyzing…" onClick={() => void explain()}>
            <HugeiconsIcon icon={AiMagicIcon} strokeWidth={1.8} aria-hidden="true" className="text-primary" />
            {guidance.source === "ai" ? "Re-analyze" : "Explain with AI"}
          </Button>
        )}
      </div>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      {guidance.summary && <p className="text-sm">{guidance.summary}</p>}
      <ul className="space-y-2">
        {guidance.groups.map((group) => {
          const keys = eligibleKeys(group)
          const allSelected = keys.length > 0 && keys.every((key) => selectedKeys.includes(key))
          const meta = recommendation[group.recommendation]
          return (
            <li key={group.key} className="space-y-1.5 py-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge size="sm"  variant={meta.variant}>{meta.label}</Badge>
                <span className="text-sm font-medium">{group.title}</span>
                <span className="text-xs text-muted-foreground">{group.count} {group.count === 1 ? "resource" : "resources"} · {Object.entries(group.kinds).map(([kind, count]) => `${count} ${kind}`).join(", ")}</span>
                {group.safe && <Badge size="sm"  variant="success">Matches desired state</Badge>}
              </div>
              <p className="text-sm text-muted-foreground">{group.reason}</p>
              <p className="text-sm">{group.impact}</p>
              {canApprove && group.recommendation === "adopt" && keys.length > 0 && (
                <Button type="button" size="sm" variant="outline" disabled={allSelected} onClick={() => onSelectedKeysChange([...new Set([...selectedKeys, ...keys])])}>
                  {allSelected ? "Selected" : `Select these ${keys.length}`}
                </Button>
              )}
            </li>
          )
        })}
      </ul>
      {guidance.source === "ai" && <p className="text-xs text-muted-foreground">Explained by {guidance.model}. Groups owned by another application or controller are never suggested for adoption.</p>}
    </section>
  )
}
