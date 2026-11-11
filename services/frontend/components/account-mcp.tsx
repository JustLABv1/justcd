"use client"

import { useCallback, useEffect, useState, type FormEvent } from "react"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/app-dialog"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { FormField } from "@/components/form-field"
import { FormSelect } from "@/components/form-select"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Checkbox } from "@/components/panelui/checkbox"
import { Input } from "@/components/panelui/input"
import { Label } from "@/components/panelui/label"
import { Table } from "@/components/panelui/table"
import { Textarea } from "@/components/panelui/textarea"
import { accountSettingsNav } from "@/components/settings/account-nav"
import { ScopeBadge, SettingsLayout, SettingsNotice } from "@/components/settings/layout"
import { SettingsDangerZone, SettingsPanel } from "@/components/settings/settings-section"
import { useToast } from "@/components/toast-provider"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { Workspace } from "@/lib/types"

type MCPKey = {
  id: string
  name: string
  workspaceId: string
  allowPlan: boolean
  createdAt: string
  expiresAt: string | null
  revokedAt?: string
}

export function AccountMCP() {
  const toast = useToast()
  const [keys, setKeys] = useState<MCPKey[]>([])
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [endpoint, setEndpoint] = useState("")
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [name, setName] = useState("")
  const [workspaceId, setWorkspaceId] = useState("")
  const [lifetimes, setLifetimes] = useState<number[]>([])
  const [expiry, setExpiry] = useState("30")
  const [allowPlan, setAllowPlan] = useState(false)
  const [created, setCreated] = useState<{ credential: MCPKey; token: string } | null>(null)
  const [revokeId, setRevokeId] = useState("")
  const [now, setNow] = useState(0)
  const selectedWorkspace = workspaces.find(item => item.id === workspaceId)
  const canPlan = selectedWorkspace?.role === "owner" || selectedWorkspace?.role === "deployer"
  const revocable = keys.filter(key => !key.revokedAt && (!key.expiresAt || new Date(key.expiresAt).getTime() > now))
  const revokeKey = revocable.find(key => key.id === revokeId)

  const load = useCallback(async () => {
    const [credentials, spaces, policy] = await Promise.all([
      api<{ items: MCPKey[] }>("/api/v1/mcp-credentials"),
      api<{ items: Workspace[] }>("/api/v1/workspaces"),
      api<{ allowedLifetimes: number[] }>("/api/v1/mcp-policy"),
    ])
    setLifetimes(policy.allowedLifetimes)
    setKeys(credentials.items)
    setWorkspaces(spaces.items)
    setNow(Date.now())
  }, [])

  useEffect(() => {
    void Promise.resolve().then(() => setEndpoint(`${window.location.origin}/api/v1/mcp`))
    void Promise.resolve().then(load).catch(cause => setError(errorMessage(cause))).finally(() => setLoading(false))
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [load])

  async function copy(value: string) {
    try { await navigator.clipboard.writeText(value); toast.success("Copied to clipboard.") }
    catch { toast.error("Could not copy. Select and copy the text manually.") }
  }

  function setDialogOpen(next: boolean) {
    if (busy) return
    setOpen(next)
    if (!next) setCreated(null)
  }

  function startCreate() {
    setName("")
    setWorkspaceId(workspaces[0]?.id ?? "")
    setExpiry(String(lifetimes.includes(30) ? 30 : lifetimes[0]))
    setAllowPlan(false)
    setCreated(null)
    setError("")
    setOpen(true)
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!workspaceId || !name.trim() || busy) return
    setBusy(true)
    setError("")
    try {
      const result = await apiPost<{ credential: MCPKey; token: string }>("/api/v1/mcp-credentials", {
        name: name.trim(), workspaceId, allowPlan: allowPlan && canPlan, expiresInDays: Number(expiry),
      })
      setCreated(result)
      setKeys(current => [result.credential, ...current])
      setNow(Date.now())
    } catch (cause) { setError(errorMessage(cause)) }
    finally { setBusy(false) }
  }

  const configuration = JSON.stringify({ mcpServers: { justcd: {
    url: endpoint,
    headers: { Authorization: "Bearer <YOUR_API_KEY>" },
  } } }, null, 2)

  return <SettingsLayout
    title="MCP access"
    description="Connect your assistant to JustCD to inspect applications and prepare deployment plans."
    scope={<ScopeBadge scope="account" />}
    nav={accountSettingsNav}
    activeId="mcp"
    navLabel="Account settings"
    actions={<Button disabled={loading || workspaces.length === 0} onClick={startCreate}>Create API key</Button>}
  >
    {error && !open && <SettingsNotice tone="destructive" title="Could not load MCP access">
      {error}
      <Button variant="outline" onClick={() => { setError(""); setLoading(true); void load().catch(cause => setError(errorMessage(cause))).finally(() => setLoading(false)) }}>Retry</Button>
    </SettingsNotice>}
    <SettingsPanel title="Connect an assistant" description="Use an HTTP MCP connection with a personal API key." padded>
      <div className="space-y-5">
        <FormField label="MCP server URL" htmlFor="mcp-url">
          <div className="flex min-w-0 flex-wrap gap-2">
            <Input id="mcp-url" readOnly value={endpoint} className="min-w-0 flex-1 basis-48 font-mono text-xs" onFocus={event => event.target.select()} />
            <Button variant="outline" disabled={!endpoint} onClick={() => void copy(endpoint)}>Copy URL</Button>
          </div>
        </FormField>
        <ol className="list-decimal space-y-1.5 pl-5 text-sm text-muted-foreground">
          <li>Create an API key for the workspace your assistant should access.</li>
          <li>Add the server URL to your MCP client using Streamable HTTP.</li>
          <li>Set the Authorization header to <code className="text-foreground">Bearer &lt;YOUR_API_KEY&gt;</code>.</li>
        </ol>
        <FormField label="Example client configuration" htmlFor="mcp-config" hint="Client formats vary. Replace the placeholder using your client’s secret storage; keep API keys out of Git.">
          <Textarea id="mcp-config" readOnly value={configuration} autoResize={false} rows={11} className="font-mono text-xs" onFocus={event => event.target.select()} />
        </FormField>
        <Button variant="outline" disabled={!endpoint} onClick={() => void copy(configuration)}>Copy configuration</Button>
      </div>
    </SettingsPanel>
    <SettingsPanel title="What your assistant can do" padded>
      <dl className="space-y-4 text-sm">
        <div><dt className="font-medium">Read access</dt><dd className="mt-1 text-muted-foreground">Inspect application health and revisions, read redacted deployment plans, and check your plan-generation task status.</dd></div>
        <div><dt className="font-medium">Optional plan generation</dt><dd className="mt-1 text-muted-foreground">Prepare reviewable plans with deployer or owner access. Approving and applying changes stays in JustCD.</dd></div>
        <div><dt className="font-medium">Limited to you and one workspace</dt><dd className="mt-1 text-muted-foreground">Each key follows your current workspace permissions and can be revoked at any time. Choose an expiry or, when allowed, no expiry.</dd></div>
      </dl>
    </SettingsPanel>
    <SettingsPanel title="Your API keys" description="The full key is shown only when you create it.">
      {loading ? <p className="p-5 text-sm text-muted-foreground" role="status">Loading API keys…</p>
        : keys.length === 0 ? <p className="p-5 text-sm text-muted-foreground">No API keys yet. {workspaces.length ? "Create a key to connect your assistant." : "Join or create a workspace before creating a key."}</p>
        : <Table aria-label="MCP API keys">
          <Table.Header><Table.Row><Table.Head>Name / workspace</Table.Head><Table.Head>Access</Table.Head><Table.Head>Expires</Table.Head><Table.Head>Status</Table.Head></Table.Row></Table.Header>
          <Table.Body>{keys.map(key => {
            const expired = Boolean(key.expiresAt && new Date(key.expiresAt).getTime() <= now)
            return <Table.Row key={key.id}>
              <Table.Cell><p className="max-w-64 break-words whitespace-normal font-medium">{key.name}</p><p className="mt-1 max-w-64 break-words whitespace-normal text-xs text-muted-foreground">{workspaces.find(space => space.id === key.workspaceId)?.name ?? "Workspace no longer accessible"}</p></Table.Cell>
              <Table.Cell>{key.allowPlan ? "Read + generate plans" : "Read only"}</Table.Cell>
              <Table.Cell>{key.expiresAt ? <time dateTime={key.expiresAt} title={new Date(key.expiresAt).toLocaleString()}>{new Date(key.expiresAt).toLocaleDateString()}</time> : "No expiry"}</Table.Cell>
              <Table.Cell><Badge variant={key.revokedAt ? "secondary" : expired ? "warning" : "success"}>{key.revokedAt ? "Revoked" : expired ? "Expired" : "Active"}</Badge></Table.Cell>
            </Table.Row>
          })}</Table.Body>
        </Table>}
    </SettingsPanel>
    {revocable.length > 0 && <SettingsDangerZone title="Revoke access" description="Revoking a key stops new requests. Plans already queued continue through the normal permission checks.">
      <div className="flex flex-wrap items-end gap-3 p-5">
        <div className="min-w-0 flex-1 basis-60"><FormField label="API key to revoke" htmlFor="mcp-revoke">
          <FormSelect id="mcp-revoke" value={revokeId} onValueChange={setRevokeId} placeholder="Choose an API key" items={revocable.map(key => ({ value: key.id, label: `${key.name} · ${workspaces.find(space => space.id === key.workspaceId)?.name ?? "Workspace unavailable"}` }))} />
        </FormField></div>
        <ConfirmDisclosure trigger="Revoke key" title={`Revoke ${revokeKey?.name ?? "API key"}?`} description="Your assistant will lose access through this key immediately. You can create another key later." confirmLabel="Revoke key" disabled={!revokeKey} onConfirm={async () => {
          if (!revokeKey) return
          await api(`/api/v1/mcp-credentials/${encodeURIComponent(revokeKey.id)}`, { method: "DELETE" })
          setKeys(current => current.map(key => key.id === revokeKey.id ? { ...key, revokedAt: new Date().toISOString() } : key))
          setRevokeId("")
          toast.success("API key revoked.")
        }} />
      </div>
    </SettingsDangerZone>}
    <AppDialog open={open} onOpenChange={setDialogOpen} title={created ? "Save your API key" : "Create MCP API key"} description={created ? "This key will not be shown again after you close this dialog." : "Give your assistant access to one workspace."} busy={busy} size="md">
      {created ? <div>
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground">{created.credential.name} · {created.credential.allowPlan ? "Read + generate plans" : "Read only"} · {created.credential.expiresAt ? `Expires ${new Date(created.credential.expiresAt).toLocaleDateString()}` : "No expiry"}</p>
          <FormField label="API key" htmlFor="mcp-new-key" hint="Save this key in your client’s secret storage.">
            <Input id="mcp-new-key" readOnly value={created.token} autoComplete="off" spellCheck={false} className="font-mono text-xs" onFocus={event => event.target.select()} />
          </FormField>
          <Button variant="outline" onClick={() => void copy(created.token)}>Copy API key</Button>
        </div>
        <DialogFooter><Button onClick={() => setDialogOpen(false)}>I saved my key</Button></DialogFooter>
      </div> : <form onSubmit={event => void create(event)}>
        <div className="space-y-4">
          {error && <SettingsNotice inline tone="destructive" title="Could not create API key">{error}</SettingsNotice>}
          <FormField label="Name" htmlFor="mcp-key-name"><Input id="mcp-key-name" required maxLength={100} value={name} onChange={event => setName(event.target.value)} placeholder="e.g. My coding assistant" autoFocus disabled={busy} /></FormField>
          <FormField label="Workspace" htmlFor="mcp-key-workspace"><FormSelect id="mcp-key-workspace" required value={workspaceId} disabled={busy} onValueChange={value => { setWorkspaceId(value); setAllowPlan(false) }} items={workspaces.map(space => ({ value: space.id, label: space.name }))} /></FormField>
          <FormField label="Expires after" htmlFor="mcp-key-expiry" hint="Available lifetimes are set by your instance administrator."><FormSelect id="mcp-key-expiry" value={expiry} disabled={busy} onValueChange={setExpiry} items={lifetimes.map(days => ({ value: String(days), label: days === 0 ? "No expiry" : days === 365 ? "1 year" : `${days} days` }))} /></FormField>
          <div className="space-y-2">
            <Label htmlFor="mcp-key-plan" className="flex items-center gap-3"><Checkbox id="mcp-key-plan" checked={allowPlan && canPlan} onCheckedChange={setAllowPlan} disabled={busy || !canPlan} />Allow plan generation</Label>
            <p className="text-sm text-muted-foreground">{canPlan ? "Prepare plans for review. This does not allow approval or deployment." : "Read-only access. Plan generation requires a deployer or owner role."}</p>
          </div>
        </div>
        <DialogFooter><DialogCancel disabled={busy} /><Button type="submit" loading={busy} disabled={!workspaceId || !name.trim()}>Create API key</Button></DialogFooter>
      </form>}
    </AppDialog>
  </SettingsLayout>
}
