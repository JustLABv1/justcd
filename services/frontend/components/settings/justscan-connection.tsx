"use client"

import { useEffect, useState, type FormEvent } from "react"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { Button } from "@/components/panelui/button"
import { Input } from "@/components/panelui/input"
import { SectionSkeleton } from "@/components/settings/shared"
import { SettingsDangerZone, SettingsPanel, SettingsRow } from "@/components/settings/settings-section"
import { useToast } from "@/components/toast-provider"
import { FormField } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { api, errorMessage } from "@/lib/api"

type Connection = {
  serverUrl: string
  organizationId: string
  connected: boolean
}

export function JustScanConnection({ workspaceId }: { workspaceId: string }) {
  const path = `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/justscan`
  const toast = useToast()
  const [connection, setConnection] = useState<Connection>()
  const [organizationId, setOrganizationId] = useState("")
  const [token, setToken] = useState("")
  const [saving, setSaving] = useState(false)
  const [loadError, setLoadError] = useState<unknown>(null)

  useEffect(() => {
    const controller = new AbortController()
    void api<Connection>(path, { signal: controller.signal }).then((value) => {
      if (controller.signal.aborted) return
      setConnection(value)
      setOrganizationId(value.organizationId)
    }).catch((cause) => {
      if (!controller.signal.aborted) setLoadError(cause)
    })
    return () => controller.abort()
  }, [path])

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSaving(true)
    try {
      const result = await api<Connection>(path, {
        method: "PUT",
        body: JSON.stringify({ serverUrl: connection?.serverUrl, organizationId, token }),
      })
      setConnection(result)
      setOrganizationId(result.organizationId)
      setToken("")
      toast.success("JustScan connection saved.")
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
    } finally {
      setSaving(false)
    }
  }

  async function remove() {
    await api<void>(path, { method: "DELETE" })
    setConnection((value) => value && { ...value, organizationId: "", connected: false })
    setOrganizationId("")
    setToken("")
    toast.success("JustScan connection removed.")
  }

  if (loadError) return <ErrorNotice error={loadError} />
  if (!connection) return <SectionSkeleton label="Loading JustScan connection" />

  return <>
    <SettingsPanel title="JustScan connection" padded>
      {connection.serverUrl ? <form onSubmit={save} className="max-w-xl space-y-4">
        <div className="text-sm">
          <p className="font-medium">Server</p>
          <p className="break-all text-muted-foreground">{connection.serverUrl}</p>
        </div>
        {connection.organizationId && !connection.connected && <p className="text-sm text-muted-foreground">The server changed. Enter a pipeline token for the new server to reconnect.</p>}
        <FormField label="Organization ID" htmlFor="justscan-organization">
          <Input id="justscan-organization" value={organizationId} onChange={(event) => setOrganizationId(event.target.value)} autoComplete="off" disabled={saving} required />
        </FormField>
        <FormField label="Pipeline token" htmlFor="justscan-token" hint="Use a JustScan organization token with the pipeline_scan scope.">
          <Input id="justscan-token" type="password" value={token} onChange={(event) => setToken(event.target.value)} autoComplete="new-password" disabled={saving} required />
        </FormField>
        {connection.organizationId && organizationId.trim().toLowerCase() !== connection.organizationId && <p className="text-sm text-muted-foreground">Changing the organization clears existing plan scan results in this workspace.</p>}
        <Button type="submit" loading={saving}>{connection.connected ? "Update connection" : "Connect JustScan"}</Button>
      </form> : <p className="text-sm text-muted-foreground">Ask an instance administrator to configure the JustScan server in instance settings.</p>}
    </SettingsPanel>
    {connection.organizationId && <SettingsDangerZone>
      <SettingsRow label="Remove JustScan connection" description="Removes the saved credential and plan scan results from this workspace." action={
        <ConfirmDisclosure trigger="Remove connection" title="Remove JustScan connection?" description="Queued scans and scan results will be removed from JustCD." confirmLabel="Remove connection" onConfirm={remove} disabled={saving} />
      } />
    </SettingsDangerZone>}
  </>
}
