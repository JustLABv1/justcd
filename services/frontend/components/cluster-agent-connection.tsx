"use client"

import { useCallback, useEffect, useState } from "react"
import { ConnectionDialog } from "@/components/connection-dialog"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormField } from "@/components/ui-kit"
import { ErrorDetailsButton } from "@/components/error-details"
import { api, apiDelete, apiPost, errorMessage } from "@/lib/api"
import type { Cluster, Workspace } from "@/lib/types"

type Status = { mode: "direct" | "agent"; online?: boolean; agent?: { defaultProfile: string; clusterProfile: string; clusterUid: string; version: string; revoked: boolean; lastSeenAt?: string; profiles: { name: string; namespaces: string[]; clusterScope: boolean }[] } }

export function ClusterAgentConnection({ cluster, workspace, canManage, onModeChange }: { cluster: Cluster; workspace: Workspace; canManage: boolean; onModeChange: (mode: "direct" | "agent") => void }) {
  const [open, setOpen] = useState(false)
  const [status, setStatus] = useState<Status | null>(null)
  const [defaultProfile, setDefaultProfile] = useState("default")
  const [clusterProfile, setClusterProfile] = useState("")
  const [token, setToken] = useState("")
  const [expiresAt, setExpiresAt] = useState("")
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const endpoint = `/api/v1/clusters/${encodeURIComponent(cluster.id)}/agent`
  const refresh = useCallback(async () => {
    const next = await api<Status>(`${endpoint}?workspaceId=${encodeURIComponent(workspace.id)}`)
    return next
  }, [endpoint, workspace.id])
  useEffect(() => {
    if (!open) return
    let active = true
    refresh().then((next) => { if (active) { setStatus(next); setDefaultProfile(next.agent?.defaultProfile ?? "default"); setClusterProfile(next.agent?.clusterProfile ?? "") } }).catch((cause) => active && setError(cause))
    const interval = setInterval(() => void refresh().then((next) => active && setStatus(next)).catch((cause) => active && setError(cause)), 10000)
    return () => { active = false; clearInterval(interval) }
  }, [open, refresh])
  async function enroll() {
    setBusy(true); setError(null)
    try {
      const result = await apiPost<{ enrollmentToken: string; expiresAt: string }>(`${endpoint}/enrollment`, { defaultProfile, clusterProfile })
      setToken(result.enrollmentToken); setExpiresAt(result.expiresAt); onModeChange("agent"); setStatus(await refresh())
    } catch (cause) { setError(cause) }
    finally { setBusy(false) }
  }
  async function revoke(direct = false) {
    await apiDelete(`${endpoint}${direct ? "?mode=direct" : ""}`)
    setToken(""); onModeChange(direct ? "direct" : "agent"); setStatus(await refresh())
  }
  return <>
    <Button size="sm" type="button" variant="outline" onClick={() => setOpen(true)}>{cluster.connectionMode === "agent" ? "Agent" : "Set up agent"}</Button>
    <ConnectionDialog open={open} onOpenChange={(next) => { setOpen(next); if (!next) { setToken(""); setError(null) } }} busy={busy} title={`Cluster agent · ${cluster.name}`} description="Install an agent inside this cluster. It connects outbound to JustCD over HTTPS and uses local Kubernetes credentials.">
      <div className="space-y-5">
        {error != null && <div role="alert" className="flex items-start justify-between gap-3 text-sm text-destructive"><span>{errorMessage(error)}</span><ErrorDetailsButton error={error} /></div>}
        {status && <div role="status" className="rounded-lg border p-3 text-sm"><p className="font-medium">{status.mode === "direct" ? "Direct API connection" : status.agent?.revoked ? "Agent revoked" : status.online ? "Agent online" : "Agent offline · waiting for connection"}</p>{status.agent?.lastSeenAt && <p className="mt-1 text-xs text-muted-foreground">Last seen {new Date(status.agent.lastSeenAt).toLocaleString()} · v{status.agent.version}</p>}{status.agent?.clusterUid && <p className="mt-1 break-all text-xs text-muted-foreground">Cluster UID: {status.agent.clusterUid}</p>}</div>}
        {canManage && <>
          <div className="grid gap-3 sm:grid-cols-2">
            <FormField label="Namespace access profile" htmlFor={`agent-default-${cluster.id}`} hint="Must match a locally configured profile."><Input id={`agent-default-${cluster.id}`} value={defaultProfile} onChange={(event) => setDefaultProfile(event.target.value)} maxLength={64} /></FormField>
            <FormField label="Cluster-scope profile (optional)" htmlFor={`agent-cluster-${cluster.id}`} hint="Needed to create preview namespaces or manage cluster resources."><Input id={`agent-cluster-${cluster.id}`} value={clusterProfile} onChange={(event) => setClusterProfile(event.target.value)} maxLength={64} /></FormField>
          </div>
          <ConfirmDisclosure trigger={status?.mode === "agent" ? "Create new enrollment token" : "Enable agent connection"} title="Enable cluster agent" description="This switches Kubernetes requests to the agent and invalidates existing plans. A new token revokes the previous agent identity. Deployments will wait until the agent is connected." confirmLabel="Create enrollment token" triggerVariant="default" confirmVariant="default" disabled={busy || !defaultProfile} onConfirm={enroll} />
        </>}
        {token && <div className="space-y-3 rounded-lg border bg-muted/30 p-4">
          <FormField label="One-use enrollment token" htmlFor={`agent-token-${cluster.id}`} hint={`Expires ${new Date(expiresAt).toLocaleTimeString()}. Copy it now; it disappears when this dialog closes.`}><Input id={`agent-token-${cluster.id}`} type="password" value={token} readOnly autoComplete="off" /></FormField>
          <Button type="button" size="sm" variant="outline" onClick={() => void navigator.clipboard.writeText(token).catch(setError)}>Copy token</Button>
          <p className="text-sm">Create a Secret with key <code>enrollmentToken</code>, then install <code>charts/justcd-agent</code> with your HTTPS JustCD URL and that Secret’s name.</p>
          <dl className="text-xs"><dt className="text-muted-foreground">Local profile workspace ID</dt><dd className="break-all font-mono">{workspace.id}</dd></dl>
          <p className="text-xs text-muted-foreground">Set the profile’s namespaces locally. The agent enforces this allowlist and Kubernetes RBAC. Keep the identity volume when restarting or upgrading.</p>
        </div>}
        {!!status?.agent?.profiles.length && <div className="space-y-2"><h3 className="text-sm font-medium">Reported local profiles</h3>{status.agent.profiles.map((profile) => <p key={profile.name} className="break-words text-xs text-muted-foreground"><span className="font-medium text-foreground">{profile.name}</span> · {profile.clusterScope ? "Cluster scope" : profile.namespaces.join(", ") || "No namespaces"}</p>)}</div>}
        {canManage && status?.mode === "agent" && <div className="flex flex-wrap gap-2 border-t pt-4"><ConfirmDisclosure trigger="Revoke agent" title="Revoke cluster agent" description="Block new tasks for this agent. In-flight requests may finish. Agent mode remains enabled and deployments will fail until you enroll again." confirmLabel="Revoke" disabled={busy} onConfirm={() => revoke()} /><ConfirmDisclosure trigger="Use direct connection" title="Switch to direct API access" description="Revoke the agent and use this connection’s Kubernetes API endpoint and stored credentials. Existing plans must be refreshed." confirmLabel="Use direct connection" triggerVariant="outline" disabled={busy} onConfirm={() => revoke(true)} /></div>}
      </div>
    </ConnectionDialog>
  </>
}
