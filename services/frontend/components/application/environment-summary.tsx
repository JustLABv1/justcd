"use client"

import { useEffect, useState } from "react"
import { Badge } from "@/components/reui/badge"
import { api } from "@/lib/api"
import { ago } from "@/lib/relative-time"
import type { ApplicationEnvironmentInfo } from "@/lib/types"

const statusBadge = {
  success: { variant: "success-light", label: "Deployed" },
  failed: { variant: "destructive-light", label: "Failed" },
  running: { variant: "info-light", label: "Running" },
  canceled: { variant: "outline", label: "Canceled" },
} as const

/**
 * The GitLab environment a Git-managed application is reported to and the
 * outcome of its latest report. Renders nothing unless reporting is enabled,
 * and stays quiet on load errors because the data is supplementary.
 */
export function EnvironmentSummary({
  applicationId,
}: {
  applicationId: string
}) {
  const [info, setInfo] = useState<ApplicationEnvironmentInfo | null>(null)

  useEffect(() => {
    let active = true
    api<ApplicationEnvironmentInfo>(
      `/api/v1/applications/${encodeURIComponent(applicationId)}/environment`
    )
      .then((value) => {
        if (active) setInfo(value)
      })
      .catch(() => {
        if (active) setInfo(null)
      })
    return () => {
      active = false
    }
  }, [applicationId])

  if (!info?.reporting || !info.environment) return null
  const { environment, latestDeployment: latest } = info
  const status = latest ? statusBadge[latest.status] : null
  return (
    <div className="border-t px-5 py-4">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-semibold">GitLab environment</h3>
        <Badge size="sm" radius="full" variant="outline">
          {environment.tier}
        </Badge>
        {status && (
          <Badge size="sm" radius="full" variant={status.variant}>
            {status.label}
          </Badge>
        )}
      </div>
      <p className="mt-1 text-sm break-all">
        {environment.url ? (
          <a
            className="text-primary underline underline-offset-4"
            href={environment.url}
            target="_blank"
            rel="noreferrer"
          >
            {environment.name}
          </a>
        ) : (
          environment.name
        )}
        {!info.declared && (
          <span className="text-muted-foreground">
            {" "}
            · derived from cluster and namespace
          </span>
        )}
      </p>
      {latest ? (
        <p className="mt-1 text-sm text-muted-foreground">
          Last reported {ago(latest.reportedAt)}
          {latest.sha ? (
            <>
              {" "}
              · <span className="font-mono text-xs">{latest.sha.slice(0, 12)}</span>
            </>
          ) : null}
        </p>
      ) : (
        <p className="mt-1 text-sm text-muted-foreground">
          Nothing reported yet. The next finished sync is recorded in GitLab.
        </p>
      )}
      {latest?.error && (
        <p role="alert" className="mt-1 text-sm break-words text-destructive">
          Could not report to GitLab: {latest.error}
        </p>
      )}
    </div>
  )
}
