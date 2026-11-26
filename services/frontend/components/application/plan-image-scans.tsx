"use client"

import { useEffect, useState } from "react"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Card } from "@/components/panelui/card"
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

  return <Card as="section" size="sm" className="mb-5" aria-label="JustScan image scans">
    <Card.Content>
      <div className="flex flex-wrap items-center justify-between gap-2 pb-2">
        <h3 className="text-sm font-medium">JustScan image scans</h3>
        {canStart && <Button type="button" size="sm" variant="outline" loading={busy} onClick={start}>Scan images</Button>}
      </div>
      {error && <p role="alert" className="pb-2 text-sm text-destructive">{error}</p>}
      <ul className="divide-y">
        {items.map((scan) => {
          const status = statuses[scan.status]
          const complete = scan.status === "pass" || scan.status === "fail"
          return <li key={scan.image} className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 py-2 text-sm">
            <code className="min-w-0 flex-1 basis-48 break-all">{scan.image}</code>
            {complete && <span className="text-xs text-muted-foreground">{scan.criticalCount} critical · {scan.highCount} high</span>}
            <Badge size="sm" variant={status.variant}>{status.label}</Badge>
            {scan.reportUrl && <Button nativeButton={false} render={<a href={scan.reportUrl} target="_blank" rel="noreferrer" />} size="sm" variant="ghost">Open report</Button>}
          </li>
        })}
      </ul>
    </Card.Content>
  </Card>
}
