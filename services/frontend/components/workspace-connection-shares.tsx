"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { HugeiconsIcon } from "@hugeicons/react"
import { GitBranchIcon, ServerStack01Icon, Delete02Icon } from "@hugeicons/core-free-icons"
import { RowActions } from "@/components/action-menu"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { ConnectionRow, EmptyState, FormField, InlineLink } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, errorMessage } from "@/lib/api"
import type { Cluster, Credential, GitSource, ListResponse, Workspace, WorkspaceConnectionShare } from "@/lib/types"

type ShareKind = "cluster" | "git-source"

export function WorkspaceConnectionShares({
  workspace,
  workspaces,
  clusters,
  sources,
  credentials,
}: {
  workspace: Workspace
  workspaces: Workspace[]
  clusters: Cluster[]
  sources: GitSource[]
  credentials: Credential[]
}) {
  const toast = useToast()
  const [shares, setShares] = useState<WorkspaceConnectionShare[]>([])
  const [kind, setKind] = useState<ShareKind>("cluster")
  const [resourceId, setResourceId] = useState("")
  const [targetWorkspaceId, setTargetWorkspaceId] = useState("")
  const [credentialSelection, setCredentialSelection] = useState<Record<string, string>>({})
  const [busyId, setBusyId] = useState("")
  const [offerOpen, setOfferOpen] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)

  const ownClusters = useMemo(() => clusters.filter((item) => item.workspaceId === workspace.id), [clusters, workspace.id])
  const ownSources = useMemo(() => sources.filter((item) => item.workspaceId === workspace.id), [sources, workspace.id])
  const ownGitCredentials = useMemo(() => credentials.filter((item) => item.workspaceId === workspace.id && (item.kind === "git-https" || item.kind === "git-ssh")), [credentials, workspace.id])
  const otherWorkspaces = useMemo(() => workspaces.filter((item) => item.id !== workspace.id), [workspaces, workspace.id])
  const resources = kind === "cluster" ? ownClusters : ownSources

  const loadShares = useCallback(async () => {
    try {
      const result = await api<ListResponse<WorkspaceConnectionShare>>(`/api/v1/workspace-connection-shares?workspaceId=${encodeURIComponent(workspace.id)}`)
      setShares(result.items)
      setError(null)
    } catch (cause) {
      setError(cause)
    } finally {
      setLoading(false)
    }
  }, [workspace.id])

  useEffect(() => { void Promise.resolve().then(loadShares) }, [loadShares])

  async function offer(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (workspace.role !== "owner" || !resourceId || !targetWorkspaceId) return
    setBusyId("offer")
    try {
      const endpoint = kind === "cluster"
        ? `/api/v1/clusters/${encodeURIComponent(resourceId)}/shares`
        : `/api/v1/git-sources/${encodeURIComponent(resourceId)}/shares`
      await api(endpoint, { method: "POST", body: JSON.stringify({ targetWorkspaceId }) })
      toast.success("Connection offer sent. The receiving workspace owner must accept it.")
      setResourceId("")
      setTargetWorkspaceId("")
      setOfferOpen(false)
      await loadShares()
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusyId("")
    }
  }

  async function decide(share: WorkspaceConnectionShare, accept: boolean) {
    setBusyId(share.id)
    try {
      await api(`/api/v1/workspace-connection-shares/${encodeURIComponent(share.id)}/${accept ? "accept" : "decline"}`, {
        method: "POST",
        body: JSON.stringify({ workspaceId: workspace.id }),
      })
      toast.success(accept ? "Connection accepted." : "Connection offer declined.")
      await loadShares()
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusyId("")
    }
  }

  async function revoke(share: WorkspaceConnectionShare) {
    setBusyId(share.id)
    try {
      await api(`/api/v1/workspace-connection-shares/${encodeURIComponent(share.id)}?workspaceId=${encodeURIComponent(workspace.id)}`, { method: "DELETE" })
      toast.success("Access revoked.")
      await loadShares()
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusyId("")
    }
  }

  async function saveGitCredential(share: WorkspaceConnectionShare) {
    setBusyId(share.id)
    try {
      await api(`/api/v1/workspace-connection-shares/${encodeURIComponent(share.id)}/credential`, {
        method: "PUT",
        body: JSON.stringify({ workspaceId: workspace.id, credentialId: credentialSelection[share.id] || null }),
      })
      toast.success("Workspace-specific Git credential saved.")
      await loadShares()
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusyId("")
    }
  }

  const canOffer = workspace.role === "owner"
  return <div className="space-y-6">
    {error != null && <div role="alert" className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive"><span>{errorMessage(error)}</span><Button type="button" size="sm" variant="outline" onClick={() => void loadShares()}>Retry</Button></div>}
    <AppDialog open={offerOpen} onOpenChange={setOfferOpen} busy={busyId === "offer"} title="Offer a connection" description="The receiving workspace owner must accept. Credentials stay private.">
      <form className="space-y-4" onSubmit={(event) => void offer(event)}>
        <FormField label="Connection type" htmlFor="share-kind"><FormSelect id="share-kind" value={kind} onValueChange={(value) => { setKind(value as ShareKind); setResourceId("") }} items={[{ value: "cluster", label: "Kubernetes cluster" }, { value: "git-source", label: "Git source" }]} /></FormField>
        <FormField label={kind === "cluster" ? "Workspace-owned cluster" : "Workspace-owned Git source"} htmlFor="share-resource"><FormSelect id="share-resource" value={resourceId} onValueChange={setResourceId} emptyOption="Select a connection" items={resources.map((item) => ({ value: item.id, label: item.name }))} /></FormField>
        <FormField label="Offer to workspace" htmlFor="share-target"><FormSelect id="share-target" value={targetWorkspaceId} onValueChange={setTargetWorkspaceId} emptyOption="Select a workspace" items={otherWorkspaces.map((item) => ({ value: item.id, label: item.name }))} /></FormField>
        {!resources.length && <p className="text-sm text-muted-foreground">No workspace-owned {kind === "cluster" ? "clusters" : "Git sources"} are available to share. {kind === "cluster" ? <InlineLink href={`/workspaces/${workspace.id}/clusters/new`}>Connect cluster</InlineLink> : <InlineLink href={`/workspaces/${workspace.id}/git-sources/new`}>Connect Git source</InlineLink>}</p>}
        {!otherWorkspaces.length && <p className="text-sm text-muted-foreground">You need access to another workspace before you can offer a connection.</p>}
        <DialogFooter><DialogCancel disabled={busyId === "offer"} /><Button type="submit" loading={busyId === "offer"} disabled={!resourceId || !targetWorkspaceId || busyId !== ""}>Send offer</Button></DialogFooter>
      </form>
    </AppDialog>

    <section className="overflow-hidden rounded-xl border bg-card">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4"><div><h2 className="text-sm font-semibold">Connection offers and access</h2><p className="mt-1 text-sm text-muted-foreground">Private offers, accepted shares, and revoked access for {workspace.name}.</p></div><div className="flex shrink-0 items-center gap-2"><Badge size="sm" variant="secondary" aria-label={`${shares.length} shared connections`}>{shares.length}</Badge>{canOffer && <Button size="sm" type="button" onClick={() => setOfferOpen(true)}>Offer connection</Button>}</div></header>
      {loading ? <div role="status" aria-label="Loading shared connections" className="space-y-3 p-5"><Skeleton className="h-14 rounded-lg" /><Skeleton className="h-14 rounded-lg" /></div> : shares.length === 0 ? <EmptyState title="No shared connections yet" description={canOffer ? "Connections remain private until explicitly offered and accepted. Use “Offer connection” to share one." : "Connections remain private until explicitly offered and accepted. Ask a workspace owner to offer or accept connections."} /> : <ul className="divide-y">
        {shares.map((share) => {
          const outgoing = share.ownerWorkspaceId === workspace.id
          const pendingForThisWorkspace = !outgoing && share.status === "pending"
          const canRevoke = (outgoing || share.status === "accepted") && canOffer && share.status !== "revoked" && share.status !== "declined"
          const currentCredential = credentialSelection[share.id] ?? share.credentialId ?? ""
          const savedCredentialName = ownGitCredentials.find((item) => item.id === share.credentialId)?.name
          const actions = pendingForThisWorkspace && canOffer
            ? <div className="flex shrink-0 items-center gap-2"><Button size="sm" variant="outline" aria-label={`Decline offer of ${share.resourceName}`} disabled={busyId !== ""} loading={busyId === share.id} onClick={() => void decide(share, false)}>Decline</Button><Button size="sm" aria-label={`Accept offer of ${share.resourceName}`} disabled={busyId !== ""} loading={busyId === share.id} onClick={() => void decide(share, true)}>Accept</Button></div>
            : canRevoke
              ? <RowActions label={share.resourceName} items={[{ label: "Revoke access", icon: Delete02Icon, destructive: true, disabled: busyId !== "", confirm: { title: `Revoke access to ${share.resourceName}?`, description: "Applications in the receiving workspace will no longer be able to use this connection. Existing credentials stay separate.", confirmLabel: "Revoke access", onConfirm: () => revoke(share) } }]} />
              : undefined
          return <ConnectionRow
            key={share.id}
            icon={<HugeiconsIcon icon={share.kind === "cluster" ? ServerStack01Icon : GitBranchIcon} strokeWidth={1.8} className="size-4" />}
            title={share.resourceName}
            badges={<Badge size="sm" radius="full" variant={share.status === "accepted" ? "success-light" : share.status === "pending" ? "warning-light" : share.status === "declined" || share.status === "revoked" ? "destructive-light" : "outline"} className="capitalize">{share.status}</Badge>}
            meta={<><p className="truncate text-sm">{outgoing ? `Offered to ${share.targetWorkspaceName}` : `Shared by ${share.ownerWorkspaceName}`}{share.repositoryUrl ? ` · ${share.repositoryUrl}` : ""}</p><p className="mt-0.5 text-sm">{share.kind === "git-source" ? outgoing ? "Recipient must provide its own Git credential after accepting." : share.status === "accepted" ? `Workspace credential: ${savedCredentialName ?? "not configured"}` : "Credential access is not included." : "Kubernetes credentials are never included in cluster shares."}</p></>}
            actions={actions}
          >
            {!outgoing && share.status === "accepted" && share.kind === "git-source" && canOffer && <div className="grid items-end gap-3 sm:grid-cols-[minmax(0,1fr)_auto]"><FormField label="Credential owned by this workspace" htmlFor={`shared-git-credential-${share.id}`} hint="Choose a private credential or leave blank for a public repository."><FormSelect id={`shared-git-credential-${share.id}`} value={currentCredential} onValueChange={(value) => setCredentialSelection((current) => ({ ...current, [share.id]: value }))} emptyOption="Public repository / no credential" items={ownGitCredentials.map((item) => ({ value: item.id, label: item.name }))} /></FormField><Button size="sm" variant="outline" aria-label={`Save credential for ${share.resourceName}`} disabled={busyId !== ""} loading={busyId === share.id} onClick={() => void saveGitCredential(share)}>Save credential</Button></div>}
          </ConnectionRow>
        })}
      </ul>}
    </section>
  </div>
}
