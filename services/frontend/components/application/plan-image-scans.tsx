"use client"

import { Search01Icon, Shield01Icon } from "@hugeicons/core-free-icons"
import { HugeiconsIcon } from "@hugeicons/react"
import { useEffect, useState } from "react"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Card } from "@/components/panelui/card"
import { Input } from "@/components/panelui/input"
import { Sheet } from "@/components/panelui/sheet"
import { api, errorMessage } from "@/lib/api"

const statuses = {
  unpinned: { label: "Digest required", variant: "warning" },
  unscanned: { label: "Ready to scan", variant: "outline" },
  queued: { label: "Queued", variant: "outline" },
  submitting: { label: "Submitting", variant: "outline" },
  pending: { label: "Scanning", variant: "outline" },
  pass: { label: "Policy passed", variant: "success" },
  fail: { label: "Policy failed", variant: "destructive" },
  error: { label: "Scan error", variant: "warning" },
} as const

type Scan = {
  image: string
  status: keyof typeof statuses
  reportUrl?: string
  criticalCount: number
  highCount: number
}

type ScanResponse = { connected: boolean; items: Scan[] }

function isPending(scan: Scan) {
  return scan.status === "queued" || scan.status === "submitting" || scan.status === "pending"
}

export function PlanImageScans({ planId, canScan }: { planId: string; canScan: boolean }) {
  const path = `/api/v1/plans/${encodeURIComponent(planId)}/justscan`
  const [data, setData] = useState<ScanResponse>()
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [query, setQuery] = useState("")
  const pending = data?.items.some(isPending) ?? false

  useEffect(() => {
    if (busy) return
    const controller = new AbortController()
    async function load() {
      try {
        const result = await api<ScanResponse>(path, { signal: controller.signal })
        if (!controller.signal.aborted) {
          setData(result)
          setError(undefined)
        }
      } catch (cause) {
        if (!controller.signal.aborted) setError(errorMessage(cause))
      }
    }
    if (!pending) void load()
    const interval = pending ? window.setInterval(() => void load(), 5000) : undefined
    return () => {
      controller.abort()
      window.clearInterval(interval)
    }
  }, [path, pending, busy])

  async function start() {
    setBusy(true)
    try {
      setData(await api<ScanResponse>(path, { method: "POST" }))
      setError(undefined)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }

  if (!error && (!data?.connected || data.items.length === 0)) return null
  const items = data?.items ?? []
  const canStart = canScan && items.some((scan) => scan.status === "unscanned" || scan.status === "error")
  const counts = { unpinned: 0, unscanned: 0, queued: 0, submitting: 0, pending: 0, pass: 0, fail: 0, error: 0 }
  for (const scan of items) counts[scan.status]++
  const imageCount = `${items.length} ${items.length === 1 ? "image" : "images"}`
  const summary = [
    [counts.fail, "policy failed"],
    [counts.error, counts.error === 1 ? "scan error" : "scan errors"],
    [counts.queued + counts.submitting + counts.pending, "in progress"],
    [counts.unscanned, "ready to scan"],
    [counts.unpinned, "digest required"],
    [counts.pass, "passed"],
  ].flatMap(([count, label]) => count ? [`${count} ${label}`] : [])
  const filter = query.trim().toLowerCase()
  const visibleItems = items.filter((scan) => scan.image.toLowerCase().includes(filter))

  return (
    <Sheet side="right" onOpenChange={(open) => { if (!open) setQuery("") }}>
      <Card as="section" size="sm" aria-label="JustScan image scans">
        <Card.Content className="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-2">
          <div className="flex min-w-0 items-center gap-3">
            <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
              <HugeiconsIcon icon={Shield01Icon} className="size-4" />
            </span>
            <div className="min-w-0">
              <h3 className="text-sm font-medium">Image scans</h3>
              <p aria-live="polite" className="text-xs leading-5 text-muted-foreground">
                {items.length > 0 && [imageCount, ...summary].join(" · ")}
                {error && <span className="text-destructive">{items.length > 0 && " · "}Couldn’t load or update scans</span>}
              </p>
            </div>
          </div>
          <div className="ml-auto flex flex-wrap items-center justify-end gap-2">
            <Sheet.Trigger render={<Button size="sm" variant="outline" />}>View scans</Sheet.Trigger>
            {canStart && <Button size="sm" variant="secondary" loading={busy} onClick={start}>Scan images</Button>}
          </div>
        </Card.Content>
      </Card>
      <Sheet.Content className="w-full max-w-xl">
        <Sheet.Header>
          <Sheet.Title>JustScan image scans</Sheet.Title>
          <Sheet.Close />
          {items.length > 0 && <Sheet.Description>{imageCount} in this plan</Sheet.Description>}
        </Sheet.Header>
        {items.length > 1 && (
          <div className="shrink-0 px-6 pb-3">
            <Input.Group size="sm">
              <Input.Addon><HugeiconsIcon icon={Search01Icon} aria-hidden="true" className="size-4" /></Input.Addon>
              <Input type="search" aria-label="Search images" placeholder="Search images…" value={query} onChange={(event) => setQuery(event.target.value)} />
            </Input.Group>
          </div>
        )}
        <Sheet.Body>
          {error && <p role="alert" className="break-words py-2 text-sm text-destructive">{error}</p>}
          {filter && visibleItems.length === 0 && <p role="status" className="py-3 text-sm text-muted-foreground">No images match your search.</p>}
          <ul className="divide-y">
            {visibleItems.map((scan) => {
              const status = statuses[scan.status]
              const complete = scan.status === "pass" || scan.status === "fail"

              return (
                <li key={scan.image} className="min-w-0 space-y-2 py-3">
                  <code className="block text-xs leading-5 [overflow-wrap:anywhere]">{scan.image}</code>
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                    <Badge size="sm" variant={status.variant}>{status.label}</Badge>
                    {complete && <span className="text-xs text-muted-foreground">{scan.criticalCount} critical · {scan.highCount} high</span>}
                    {scan.reportUrl && <Button nativeButton={false} render={<a href={scan.reportUrl} target="_blank" rel="noreferrer" />} size="sm" variant="ghost" className="ms-auto">Open report</Button>}
                  </div>
                </li>
              )
            })}
          </ul>
        </Sheet.Body>
      </Sheet.Content>
    </Sheet>
  )
}
