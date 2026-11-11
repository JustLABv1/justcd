"use client"

import { useEffect, useState } from "react"
import { Button } from "@/components/panelui/button"
import { Checkbox } from "@/components/panelui/checkbox"
import { Label } from "@/components/panelui/label"
import { SettingsSection } from "@/components/settings/settings-section"
import { SettingsNotice } from "@/components/settings/layout"
import { useToast } from "@/components/toast-provider"
import { api, errorMessage } from "@/lib/api"

const choices = [7, 30, 90, 365, 0]
export function MCPPolicySettings() {
  const toast = useToast()
  const [saved, setSaved] = useState<number[] | null>(null)
  const [allowed, setAllowed] = useState<number[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    api<{ allowedLifetimes: number[] }>("/api/v1/mcp-policy", { signal: controller.signal })
      .then(policy => { setSaved(policy.allowedLifetimes); setAllowed(policy.allowedLifetimes) })
      .catch(cause => { if (!controller.signal.aborted) setError(errorMessage(cause)) })
    return () => controller.abort()
  }, [retry])
  async function save() {
    setBusy(true)
    try {
      const result = await api<{ allowedLifetimes: number[] }>("/api/v1/admin/mcp-policy", { method: "PUT", body: JSON.stringify({ allowedLifetimes: allowed }) })
      setSaved(result.allowedLifetimes)
      setAllowed(result.allowedLifetimes)
      toast.success("MCP key lifetime policy saved.")
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setBusy(false) }
  }
  const changed = saved !== null && choices.some(value => saved.includes(value) !== allowed.includes(value))
  return <SettingsSection title="MCP API keys" description="Choose which lifetimes people can use for new keys. Existing keys keep their expiry and can still be revoked from the owner’s MCP access page.">
    <div className="space-y-4 p-5">
      {error ? <SettingsNotice inline tone="destructive" title="Could not load MCP policy">{error}<Button variant="outline" onClick={() => { setError(""); setRetry(value => value + 1) }}>Retry</Button></SettingsNotice>
        : saved === null ? <p role="status" className="text-sm text-muted-foreground">Loading MCP policy…</p>
        : <>
          <fieldset disabled={busy} className="space-y-3">
            <legend className="mb-3 text-sm font-medium">Allowed key lifetimes</legend>
            {choices.map(days => <Label key={days} htmlFor={`mcp-lifetime-${days}`} className="flex items-center gap-3"><Checkbox id={`mcp-lifetime-${days}`} checked={allowed.includes(days)} onCheckedChange={checked => setAllowed(current => choices.filter(value => value === days ? checked : current.includes(value)))} />{days === 0 ? "No expiry" : days === 365 ? "1 year" : `${days} days`}</Label>)}
          </fieldset>
          <p className="text-sm text-muted-foreground">30 days is the default when allowed; otherwise the first allowed lifetime is selected. Keys without expiry remain active until revoked or account access is removed.</p>
          {allowed.length === 0 && <p role="alert" className="text-sm text-destructive">Choose at least one lifetime.</p>}
          <Button variant="outline" disabled={busy || !changed || allowed.length === 0} loading={busy} onClick={() => void save()}>Save key policy</Button>
        </>}
    </div>
  </SettingsSection>
}
