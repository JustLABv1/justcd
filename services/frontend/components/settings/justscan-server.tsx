"use client"

import { useEffect, useState, type FormEvent } from "react"
import { Button } from "@/components/panelui/button"
import { Input } from "@/components/panelui/input"
import { SettingsPanel } from "@/components/settings/settings-section"
import { LoadErrorNotice, SectionSkeleton } from "@/components/settings/shared"
import { useToast } from "@/components/toast-provider"
import { FormField } from "@/components/ui-kit"
import { api, errorMessage } from "@/lib/api"

const path = "/api/v1/admin/justscan"
type Settings = { serverUrl: string }

export function JustScanServerPanel() {
  const toast = useToast()
  const [settings, setSettings] = useState<Settings>()
  const [url, setUrl] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    void api<Settings>(path, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return
      setSettings(result)
      setUrl(result.serverUrl)
    }).catch((cause) => {
      if (!controller.signal.aborted) setError(cause)
    })
    return () => controller.abort()
  }, [attempt])

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    try {
      const result = await api<Settings>(path, {
        method: "PUT",
        body: JSON.stringify({ serverUrl: url }),
      })
      setSettings(result)
      setUrl(result.serverUrl)
      toast.success(result.serverUrl ? "JustScan server saved." : "JustScan disabled.")
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusy(false)
    }
  }

  if (error) return <LoadErrorNotice error={error} title="Could not load JustScan settings" onRetry={() => { setError(null); setAttempt(attempt + 1) }} />
  if (!settings) return <SectionSkeleton label="Loading JustScan settings" />

  return <SettingsPanel title="JustScan server" description="This server is available to all workspaces." padded>
    <form className="max-w-xl space-y-4" onSubmit={save}>
      <FormField label="Server URL" htmlFor="instance-justscan-url" hint="Use HTTPS in production. Leave blank to pause scanning.">
        <Input id="instance-justscan-url" type="url" placeholder="https://scan.example.com" value={url} onChange={(event) => setUrl(event.target.value)} disabled={busy} />
      </FormField>
      {settings.serverUrl && url.trim() && url.trim().replace(/\/+$/, "") !== settings.serverUrl && <p className="text-sm text-muted-foreground">Workspace owners will need to reconnect for the new server.</p>}
      <Button type="submit" loading={busy} disabled={url.trim() === settings.serverUrl}>Save server</Button>
    </form>
  </SettingsPanel>
}
