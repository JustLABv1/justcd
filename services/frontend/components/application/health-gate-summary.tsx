"use client"

import { useEffect, useState } from "react"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { ago } from "@/lib/relative-time"
import type { ApplicationHealthGateInfo } from "@/lib/types"

const statusBadge = {
  pending: { variant: "info-light", label: "Verifying" },
  passed: { variant: "success-light", label: "Passed" },
  failed: { variant: "destructive-light", label: "Failed" },
} as const

/**
 * The post-deploy health gate declared in justcd.yaml and the outcome of its
 * latest check. Renders nothing when the application declares no gate and
 * stays quiet on load errors because the data is supplementary.
 */
export function HealthGateSummary({
  applicationId,
  onOpenActivity,
}: {
  applicationId: string
  onOpenActivity: () => void
}) {
  const [info, setInfo] = useState<ApplicationHealthGateInfo | null>(null)

  useEffect(() => {
    let active = true
    const load = () =>
      api<ApplicationHealthGateInfo>(
        `/api/v1/applications/${encodeURIComponent(applicationId)}/health-gate`
      )
        .then((value) => {
          if (active) setInfo(value)
        })
        .catch(() => {
          if (active) setInfo(null)
        })
    void load()
    // A pending check resolves within minutes; keep it fresh while visible.
    const timer = setInterval(() => void load(), 15000)
    return () => {
      active = false
      clearInterval(timer)
    }
  }, [applicationId])

  if (!info?.enabled || !info.gate) return null
  const latest = info.latest
  const status = latest ? statusBadge[latest.status] : null
  return (
    <div className="border-t px-5 py-4">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-semibold">Post-deploy health gate</h3>
        {status && (
          <Badge size="sm" radius="full" variant={status.variant}>
            {status.label}
          </Badge>
        )}
      </div>
      <p className="mt-1 text-sm text-muted-foreground">
        Each sync must report Healthy within {Math.round(info.gate.timeoutSeconds / 60) || 1}{" "}
        min or it is marked failed
        {info.gate.onFailure === "prepare-rollback"
          ? ", and a rollback plan is prepared for owner approval."
          : "."}
      </p>
      {latest && (
        <p className="mt-1 text-sm text-muted-foreground">
          {latest.status === "pending"
            ? `Verifying since ${ago(latest.startedAt)}.`
            : `Checked ${ago(latest.finishedAt ?? latest.startedAt)}.`}
          {latest.revision ? (
            <>
              {" "}
              <span className="font-mono text-xs">
                {latest.revision.slice(0, 12)}
              </span>
            </>
          ) : null}
        </p>
      )}
      {latest?.status === "failed" && latest.message && (
        <p role="alert" className="mt-1 text-sm break-words text-destructive">
          {latest.message}
        </p>
      )}
      {latest?.status === "failed" && latest.rollbackPlanId && (
        <div className="mt-2 flex flex-wrap items-center gap-3 text-sm">
          <span>
            A rollback plan was prepared. It still needs owner approval and
            expires after 15 minutes.
          </span>
          <Button type="button" size="sm" variant="outline" onClick={onOpenActivity}>
            Review rollback
          </Button>
        </div>
      )}
    </div>
  )
}
