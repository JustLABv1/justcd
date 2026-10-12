"use client"

import { useEffect, useState } from "react"
import { Switch } from "@/components/ui/switch"
import { Panel } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, errorMessage } from "@/lib/api"
import { LoadErrorNotice, SectionSkeleton } from "./shared"

type Settings = { maintenanceEnabled: boolean }

export function MaintenancePanel() {
  const toast = useToast()
  const [settings, setSettings] = useState<Settings | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [tick, setTick] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    api<Settings>("/api/v1/admin/instance-settings", {
      signal: controller.signal,
    })
      .then(setSettings)
      .catch((cause) => {
        if (!controller.signal.aborted) setError(cause)
      })
    return () => controller.abort()
  }, [tick])

  async function change(enabled: boolean) {
    setBusy(true)
    try {
      const result = await api<Settings>("/api/v1/admin/instance-settings", {
        method: "PUT",
        body: JSON.stringify({ maintenanceEnabled: enabled }),
      })
      setSettings(result)
      window.dispatchEvent(new Event("justcd:maintenance-updated"))
      toast.success(
        enabled
          ? "Maintenance mode enabled. Administrators retain access."
          : "Maintenance mode disabled. Everyone can access JustCD."
      )
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusy(false)
    }
  }

  if (error)
    return (
      <LoadErrorNotice
        error={error}
        title="Could not load maintenance settings"
        onRetry={() => {
          setError(null)
          setTick(tick + 1)
        }}
      />
    )
  if (!settings)
    return <SectionSkeleton rows={2} label="Loading maintenance settings" />
  return (
    <Panel
      title="Maintenance mode"
      description="Temporarily restrict access while you work on this instance."
    >
      <div className="space-y-5 p-5">
        <div className="flex items-start justify-between gap-6">
          <div className="min-w-0">
            <label
              htmlFor="maintenance-enabled"
              className="text-sm font-medium"
            >
              Enable maintenance mode
            </label>
            <p
              id="maintenance-description"
              className="mt-1 text-sm leading-6 text-muted-foreground"
            >
              Visitors and regular users see the maintenance page. Signed-in
              instance administrators automatically bypass it and can turn it
              off here.
            </p>
          </div>
          <Switch
            id="maintenance-enabled"
            checked={settings.maintenanceEnabled}
            onCheckedChange={(value) => void change(value)}
            disabled={busy}
            aria-describedby="maintenance-description"
            aria-busy={busy || undefined}
            className="mt-1 shrink-0"
          />
        </div>
        <p className="rounded-lg border bg-muted/30 p-3 text-sm leading-6 text-muted-foreground">
          Existing deployments, background jobs, webhooks, and cluster agents
          continue running. Maintenance restricts regular users’ authenticated
          API access; administrators can still sign in.
        </p>
      </div>
    </Panel>
  )
}
