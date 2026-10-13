"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { HugeiconsIcon } from "@hugeicons/react"
import { GitBranchIcon, ServerStack01Icon, Delete02Icon } from "@hugeicons/core-free-icons"
import { RowActions } from "@/components/action-menu"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { Disclosure } from "@/components/ui/collapsible"
import { InventoryPanel, LoadErrorNotice } from "@/components/settings/shared"
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
  offerOpen,
  onOfferOpenChange,
}: {
  workspace: Workspace
  workspaces: Workspace[]
  clusters: Cluster[]
  sources: GitSource[]
  credentials: Credential[]
  offerOpen: boolean
  onOfferOpenChange: (open: boolean) => void
}) {
  const toast = useToast()
  const [shares, setShares] = useState<WorkspaceConnectionShare[]>([])
  const [kind, setKind] = useState<ShareKind>(clusters.some((item) => item.workspaceId === workspace.id) ? "cluster" : "git-source")
  const [resourceId, setResourceId] = useState("")
  const [targetWorkspaceId, setTargetWorkspaceId] = useState("")
  const [credentialSelection, setCredentialSelection] = useState<Record<string, string>>({})
  const [busyId, setBusyId] = useState("")
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
      onOfferOpenChange(false)
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
        body: JSON.stringify({ workspaceId: workspace.id, credentialId: (credentialSelection[share.id] ?? share.credentialId) || null }),
      })
      toast.success("Workspace-specific Git credential saved.")
      await loadShares()
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusyId("")
    }
  }

  function shareRow(share: WorkspaceConnectionShare) {
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
      badges={<><Badge size="sm" radius="full" variant="outline">{share.kind === "cluster" ? "Kubernetes cluster" : "Git source"}</Badge><Badge size="sm" radius="full" variant={share.status === "accepted" ? "success-light" : share.status === "pending" ? "warning-light" : share.status === "declined" || share.status === "revoked" ? "destructive-light" : "outline"} className="capitalize">{share.status}</Badge></>}
      meta={<><p className="truncate text-sm">{outgoing ? `Offered to ${share.targetWorkspaceName}` : `Shared by ${share.ownerWorkspaceName}`}{share.repositoryUrl ? ` · ${share.repositoryUrl}` : ""}</p><p className="mt-0.5 text-sm">{share.kind === "git-source" ? outgoing ? "Recipient must provide its own Git credential after accepting." : share.status === "accepted" ? `Workspace credential: ${savedCredentialName ?? "not configured"}` : "Credential access is not included." : "Kubernetes credentials are never included in cluster shares."}</p></>}
      actions={actions}
    >
      {!outgoing && share.status === "accepted" && share.kind === "git-source" && canOffer && <div className="grid items-end gap-3 sm:grid-cols-[minmax(0,1fr)_auto]"><FormField label="Credential owned by this workspace" htmlFor={`shared-git-credential-${share.id}`} hint="Choose a private credential or leave blank for a public repository."><FormSelect id={`shared-git-credential-${share.id}`} value={currentCredential} onValueChange={(value) => setCredentialSelection((current) => ({ ...current, [share.id]: value }))} emptyOption="Public repository / no credential" items={ownGitCredentials.map((item) => ({ value: item.id, label: item.name }))} /></FormField><Button size="sm" variant="outline" aria-label={`Save credential for ${share.resourceName}`} disabled={busyId !== "" || currentCredential === (share.credentialId ?? "")} loading={busyId === share.id} onClick={() => void saveGitCredential(share)}>Save credential</Button></div>}
    </ConnectionRow>
  }

  const canOffer = workspace.role === "owner"
  const groups = [
    { title: "Incoming offers", description: "Another workspace is offering access. An owner of this workspace must accept or decline.", items: shares.filter((item) => item.ownerWorkspaceId !== workspace.id && item.status === "pending") },
    { title: "Shared with this workspace", description: "Connections this workspace has accepted. Configure private credentials here when needed.", items: shares.filter((item) => item.ownerWorkspaceId !== workspace.id && item.status === "accepted") },
    { title: "Shared with other workspaces", description: "Connections owned by this workspace that have been offered or accepted elsewhere.", items: shares.filter((item) => item.ownerWorkspaceId === workspace.id && ["pending", "accepted"].includes(item.status)) },
  ]
  const history = shares.filter((item) => ["declined", "revoked"].includes(item.status))
  return <div className="space-y-6">
    <AppDialog open={offerOpen} onOpenChange={onOfferOpenChange} busy={busyId === "offer"} title="Offer a connection" description="The receiving workspace owner must accept. Credentials stay private.">
      <form className="space-y-4" onSubmit={(event) => void offer(event)}>
        <FormField label="Connection type" htmlFor="share-kind"><FormSelect id="share-kind" value={kind} onValueChange={(value) => { setKind(value as ShareKind); setResourceId("") }} items={[{ value: "cluster", label: "Kubernetes cluster" }, { value: "git-source", label: "Git source" }]} /></FormField>
        <FormField label={kind === "cluster" ? "Workspace-owned cluster" : "Workspace-owned Git source"} htmlFor="share-resource"><FormSelect id="share-resource" value={resourceId} onValueChange={setResourceId} emptyOption="Select a connection" items={resources.map((item) => ({ value: item.id, label: item.name }))} /></FormField>
        <FormField label="Offer to workspace" htmlFor="share-target"><FormSelect id="share-target" value={targetWorkspaceId} onValueChange={setTargetWorkspaceId} emptyOption="Select a workspace" items={otherWorkspaces.map((item) => ({ value: item.id, label: item.name }))} /></FormField>
        {!resources.length && <p className="text-sm text-muted-foreground">No workspace-owned {kind === "cluster" ? "clusters" : "Git sources"} are available to share. {kind === "cluster" ? <InlineLink href={`/workspaces/${workspace.id}/clusters/new`}>Connect cluster</InlineLink> : <InlineLink href={`/workspaces/${workspace.id}/git-sources/new`}>Connect Git source</InlineLink>}</p>}
        {!otherWorkspaces.length && <p className="text-sm text-muted-foreground">You need access to another workspace before you can offer a connection.</p>}
        <DialogFooter><DialogCancel disabled={busyId === "offer"} /><Button type="submit" loading={busyId === "offer"} disabled={!resourceId || !targetWorkspaceId || busyId !== ""}>Send offer</Button></DialogFooter>
      </form>
    </AppDialog>

    <InventoryPanel title="Shared connections" description="Git sources and clusters can be shared between workspaces. Credentials are never included." count={shares.length}>
      {loading ? <div role="status" aria-label="Loading shared connections" className="space-y-3 p-5"><Skeleton className="h-14 rounded-lg" /><Skeleton className="h-14 rounded-lg" /></div>
        : error != null ? <div className="p-5"><LoadErrorNotice error={error} title="Could not load shared connections" onRetry={() => { setLoading(true); void loadShares() }} /></div>
        : shares.length === 0 ? <EmptyState title="No shared connections yet" description={canOffer ? "Offer a Git source or cluster to another workspace. Its owner must accept, and credentials stay private." : "Ask a workspace owner to offer a connection or accept an incoming offer. Credentials stay private."} />
        : <div className="divide-y">
          {groups.every((group) => group.items.length === 0) && <EmptyState title="No active shared connections" description="Previous offers and revoked access are recorded below. New offers require an owner to accept them." />}
          {groups.filter((group) => group.items.length > 0).map((group) => <section key={group.title} aria-label={group.title}>
            <header className="bg-muted/30 px-5 py-3"><h3 className="text-sm font-medium">{group.title} <span className="ml-2 text-muted-foreground">{group.items.length}</span></h3><p className="mt-1 text-sm text-muted-foreground">{group.description}</p></header>
            <ul className="divide-y">{group.items.map(shareRow)}</ul>
          </section>)}
          {history.length > 0 && <Disclosure summary={`Past offers and revoked access · ${history.length}`} className="p-5"><ul className="divide-y rounded-lg border">{history.map(shareRow)}</ul></Disclosure>}
        </div>}
    </InventoryPanel>
  </div>
}
