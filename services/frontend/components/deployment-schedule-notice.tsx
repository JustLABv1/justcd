"use client"

import { useEffect, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Alert02Icon } from "@hugeicons/core-free-icons"
import { api } from "@/lib/api"
import type { DeploymentSchedule } from "@/lib/types"
import { Alert } from "@/components/panelui/alert"
import { Button } from "@/components/panelui/button"
import { Badge } from "@/components/panelui/badge"

export function ScheduleWarning({
  reasons,
  nextAllowedAt,
  children,
}: {
  reasons: string[]
  nextAllowedAt?: string
  children?: React.ReactNode
}) {
  const frozen = reasons.some((reason) => reason.includes("deployment freeze"))
  return (
    <Alert tone="warning">
      <Alert.Icon />
      <Alert.Title>
        {frozen
          ? "Deployment freeze active"
          : "Waiting for deployment schedule"}
      </Alert.Title>
      <Alert.Description className="space-y-2">
        <p>
          {frozen
            ? "New deployments are blocked by an active freeze. Existing workloads continue running."
            : "New deployments are waiting for an allowed maintenance window."}
        </p>
        <ul className="space-y-1">
          {reasons.map((reason, index) => (
            <li key={`${index}-${reason}`} className="font-medium">
              {reason}
            </li>
          ))}
        </ul>
        <p>
          {nextAllowedAt
            ? `Next allowed start: ${new Date(nextAllowedAt).toLocaleString()} (${Intl.DateTimeFormat().resolvedOptions().timeZone})`
            : "No allowed start in the next weekly cycle. Review the deployment schedule."}
        </p>
      </Alert.Description>
      {children && <Alert.Actions>{children}</Alert.Actions>}
    </Alert>
  )
}

/** Server-evaluated availability includes inherited workspace restrictions. */
export function DeploymentScheduleNotice({
  scope,
  id,
  onViewSchedule,
  layout = "notice",
}: {
  scope: "workspaces" | "applications"
  id: string
  onViewSchedule?: () => void
  layout?: "notice" | "chip"
}) {
  const [data, setData] = useState<DeploymentSchedule | null>(null)
  const [unavailable, setUnavailable] = useState(false)
  useEffect(() => {
    let active = true
    const load = async () => {
      try {
        const result = await api<DeploymentSchedule>(
          `/api/v1/${scope}/${encodeURIComponent(id)}/deployment-schedule`
        )
        if (active) {
          setData(result)
          setUnavailable(false)
        }
      } catch {
        if (active) setUnavailable(true)
      }
    }
    void load()
    const timer = window.setInterval(() => void load(), 15_000)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [scope, id])
  if (layout === "chip") {
    if (!unavailable && (!data || data.decision.allowed)) return null
    const frozen = data?.decision.reasons.some((reason) =>
      reason.includes("deployment freeze")
    )
    const label = unavailable
      ? "Schedule unavailable"
      : frozen
        ? "Deployment freeze active"
        : "Deployments restricted"
    return (
      <Badge
        variant={unavailable ? "secondary" : "warning"}
        size="sm"
        render={
          onViewSchedule ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onViewSchedule}
              aria-label={`${label}. View deployment schedule`}
            />
          ) : undefined
        }
      >
        <HugeiconsIcon
          icon={Alert02Icon}
          strokeWidth={1.8}
          aria-hidden="true"
        />
        {label}
      </Badge>
    )
  }
  if (unavailable)
    return (
      <Alert tone="neutral" className="mb-5">
        <Alert.Icon />
        <Alert.Title>Deployment availability unavailable</Alert.Title>
        <Alert.Description>
          The deployment schedule could not be refreshed. Availability will be
          checked again automatically.
        </Alert.Description>
      </Alert>
    )
  if (!data || data.decision.allowed) return null
  return (
    <div className="mb-5">
      <ScheduleWarning
        reasons={data.decision.reasons}
        nextAllowedAt={data.decision.nextAllowedAt}
      >
        {onViewSchedule && (
          <Button variant="outline" size="sm" onClick={onViewSchedule}>
            View deployment schedule
          </Button>
        )}
      </ScheduleWarning>
    </div>
  )
}
