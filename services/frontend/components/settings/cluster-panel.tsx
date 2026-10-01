"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { Tabs } from "@base-ui/react/tabs"
import { HugeiconsIcon } from "@hugeicons/react"
import { PlusSignIcon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { RowActions, type ActionItem } from "@/components/action-menu"
import { ClusterAgentActivity } from "@/components/cluster-agent-activity"
import { ClusterAgentConnection } from "@/components/cluster-agent-connection"
import { ConnectionRow, FormField, SwitchField } from "@/components/ui-kit"
import { WorkspaceIcon } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { api, apiPost } from "@/lib/api"
import type {
  Cluster,
  Credential,
  KubernetesPermissionReport,
  KubernetesPermissionTest,
  ListResponse,
  User,
  Workspace,
} from "@/lib/types"
import {
  deleteItem,
  FieldError,
  FieldsSkeleton,
  InventoryPanel,
  LoadErrorNotice,
  monoTextareaClass,
  SectionEmpty,
  statusLabel,
  statusVariant,
  useSaving,
  type ActionRunner,
} from "@/components/settings/shared"

type WorkspaceCredentialResponse = { credentialId: string | null }

const tabClass = "flex shrink-0 items-center gap-2 border-b-2 border-transparent px-1 pb-3 text-sm font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-[active]:border-primary data-[active]:text-foreground"

function workspaceCredentialUrl(clusterId: string, workspaceId: string) {
  return `/api/v1/clusters/${encodeURIComponent(clusterId)}/workspace-credential?workspaceId=${encodeURIComponent(workspaceId)}`
}

function intRangeError(value: string, min: number, max: number) {
  const number = Number(value)
  return value.trim() !== "" && Number.isInteger(number) && number >= min && number <= max ? "" : `Enter a whole number from ${min} to ${max}.`
}

function isHttpUrl(value: string) {
  try {
    const url = new URL(value.trim())
    return url.protocol === "https:" || url.protocol === "http:"
  } catch {
    return false
  }
}

export function ClusterPanel({
  user,
  workspace,
  clusters,
  globalCredentials,
  workspaceCredentials,
  busy,
  action,
  onUpdated,
  onDeleted,
}: {
  user: User | null
  workspace: Workspace
  clusters: Cluster[]
  globalCredentials: Credential[]
  workspaceCredentials: Credential[]
  busy: boolean
  action: ActionRunner
  onUpdated: (value: Cluster) => void
  onDeleted: (id: string) => void
}) {
  const toast = useToast()
  const [clusterQuery, setClusterQuery] = useState("")
  const visibleClusters = clusters.filter(cluster => `${cluster.name} ${cluster.connectionMode === "agent" ? "agent" : "direct"}`.toLowerCase().includes(clusterQuery.toLowerCase()))
  const isOwner = workspace.role === "owner"
  const [credentialCluster, setCredentialCluster] = useState<Cluster | null>(null)
  const [editing, setEditing] = useState<Cluster | null>(null)
  const [testingClusterId, setTestingClusterId] = useState("")
  const [testFailed, setTestFailed] = useState<Record<string, boolean>>({})
  const [latestTests, setLatestTests] = useState<Record<string, KubernetesPermissionTest>>({})
  const [credentialByCluster, setCredentialByCluster] = useState<Record<string, string | null>>({})
  const [statusLoaded, setStatusLoaded] = useState(false)
  const [statusError, setStatusError] = useState<unknown | null>(null)
  const [statusTick, setStatusTick] = useState(0)
  const [agentClusterId, setAgentClusterId] = useState<string | null>(null)
  const agentCluster = clusters.find((item) => item.id === agentClusterId) ?? null

  useEffect(() => {
    let active = true
    Promise.all(clusters.map(async (cluster) => {
      const [testResult, credentialResult] = await Promise.all([
        api<ListResponse<KubernetesPermissionTest>>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/tests?workspaceId=${encodeURIComponent(workspace.id)}`),
        api<WorkspaceCredentialResponse>(workspaceCredentialUrl(cluster.id, workspace.id)),
      ])
      const latest = [...testResult.items].sort((left, right) => right.checkedAt.localeCompare(left.checkedAt))[0]
      return [cluster.id, latest, credentialResult.credentialId] as const
    }))
      .then((results) => {
        if (!active) return
        setLatestTests(Object.fromEntries(results.flatMap(([id, latest]) => latest ? [[id, latest]] : [])))
        setCredentialByCluster(Object.fromEntries(results.map((result) => [result[0], result[2]])))
        setStatusLoaded(true)
      })
      .catch((cause) => { if (active) setStatusError(cause) })
    return () => { active = false }
  }, [clusters, workspace, statusTick])

  function retryStatus() {
    setStatusError(null)
    setStatusLoaded(false)
    setStatusTick((tick) => tick + 1)
  }

  async function test(cluster: Cluster) {
    setTestingClusterId(cluster.id)
    try {
      const ok = await action(
        () => apiPost<KubernetesPermissionReport>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/test`, { workspaceId: workspace.id }),
        (result) => {
          const missing = result.checks.filter((check) => check.status === "missing").length
          return result.status === "passed"
            ? `${cluster.name}: all Kubernetes permissions passed in ${result.namespace}.`
            : result.status === "partial"
              ? `${cluster.name}: ${missing} Kubernetes permission${missing === 1 ? "" : "s"} missing in ${result.namespace}.`
              : `${cluster.name}: ${result.failure?.message ?? "Kubernetes permission test failed."}`
        },
        (result) => setLatestTests((current) => ({ ...current, [cluster.id]: { workspaceId: workspace.id, clusterId: cluster.id, namespace: result.namespace, report: result, checkedAt: result.checkedAt } }))
      )
      setTestFailed((current) => ({ ...current, [cluster.id]: !ok }))
    } finally {
      setTestingClusterId("")
    }
  }

  return (
    <div className="space-y-6">
      {statusError != null && <LoadErrorNotice error={statusError} title="Could not load test results and default credentials" onRetry={retryStatus} />}
      <InventoryPanel
        title="Connected clusters"
        description={isOwner ? undefined : "Only workspace owners can change cluster connections."}
        count={clusters.length}
        toolbar={<Input aria-label="Search clusters" placeholder="Search clusters or connection type…" value={clusterQuery} onChange={event => setClusterQuery(event.target.value)} className="max-w-sm" />}
      >
        {visibleClusters.length ? (
          <ul className="divide-y">
            {visibleClusters.map((cluster) => {
              const record = latestTests[cluster.id]
              const credentialId = credentialByCluster[cluster.id]
              const canEdit = isOwner && (cluster.workspaceId === workspace.id || (!cluster.workspaceId && !!user?.isAdmin))
              const canDelete = !cluster.shared && (cluster.workspaceId === workspace.id ? isOwner : !cluster.workspaceId && !!user?.isAdmin)
              const items: ActionItem[] = [
                {
                  label: cluster.connectionMode === "agent" ? "Agent connection" : "Set up agent connection",
                  onSelect: () => setAgentClusterId(cluster.id),
                },
                ...(cluster.connectionMode !== "agent" && isOwner
                  ? [{ label: "Default credential", onSelect: () => setCredentialCluster(cluster), disabled: busy }]
                  : []),
                ...(canEdit ? [{ label: "Edit", onSelect: () => setEditing(cluster), disabled: busy }] : []),
                ...(canDelete
                  ? [deleteItem({
                      name: cluster.name,
                      description: "This removes the cluster connection and its namespace access from JustCD. Resources in Kubernetes remain. Applications and active shares must be removed first.",
                      confirmLabel: "Delete cluster",
                      endpoint: `/api/v1/clusters/${encodeURIComponent(cluster.id)}`,
                      successMessage: `${cluster.name} deleted.`,
                      onDeleted: () => {
                        if (editing?.id === cluster.id) setEditing(null)
                        onDeleted(cluster.id)
                      },
                      toast,
                      disabled: busy,
                    })]
                  : []),
              ]
              return (
                <ConnectionRow
                  key={cluster.id}
                  icon={<WorkspaceIcon name="server" className="size-4" />}
                  title={cluster.name}
                  subtitle={cluster.connectionMode === "agent" ? "Outbound cluster agent" : cluster.apiServer}
                  badges={
                    <>
                      {cluster.shared
                        ? <Badge variant="info-light" radius="full">Shared by {cluster.ownerWorkspaceName ?? "another workspace"}</Badge>
                        : <Badge variant="outline" radius="full">{cluster.workspaceId ? "Private to this workspace" : "Instance-owned"}</Badge>}
                      {credentialId
                        ? <Badge variant="success-light" radius="full">Default credential: {workspaceCredentials.find((item) => item.id === credentialId)?.name ?? "configured"}</Badge>
                        : cluster.connectionMode === "agent"
                          ? <Badge variant="outline" radius="full">Agent local credentials</Badge>
                          : cluster.shared
                            ? <Badge variant="warning-light" radius="full">Credential needed</Badge>
                            : cluster.defaultCredentialId
                              ? <Badge variant="outline" radius="full">Instance default credential</Badge>
                              : statusLoaded && <Badge variant="warning-light" radius="full">No default credential</Badge>}
                      {testFailed[cluster.id]
                        ? <Badge variant="destructive-light" radius="full">Test could not run</Badge>
                        : record && <Badge variant={statusVariant(record.report.status)} radius="full">Test: {statusLabel(record.report.status)}</Badge>}
                    </>
                  }
                  meta={
                    testFailed[cluster.id]
                      ? "The last test could not run. See the error message for details."
                      : record
                        ? `Last tested ${new Date(record.checkedAt).toLocaleString()} in ${record.namespace}`
                        : statusError != null
                          ? "Test history unavailable."
                          : statusLoaded
                            ? "Not tested yet"
                            : <Skeleton className="h-4 w-40" />
                  }
                  actions={
                    <RowActions
                      label={cluster.name}
                      primary={
                        <Button
                          size="sm"
                          variant="outline"
                          type="button"
                          aria-label={`Test ${cluster.name}`}
                          loading={testingClusterId === cluster.id}
                          loadingText="Testing…"
                          disabled={busy}
                          onClick={() => void test(cluster)}
                        >
                          Test
                        </Button>
                      }
                      items={items}
                    />
                  }
                >
                  {cluster.connectionMode === "agent" && <ClusterAgentActivity key={`${workspace.id}:${cluster.id}`} cluster={cluster} workspaceId={workspace.id} />}
                </ConnectionRow>
              )
            })}
          </ul>
        ) : (
          <SectionEmpty
            title={clusters.length ? "No matching clusters" : "No clusters connected yet"}
            description={clusters.length ? "Try another cluster name or connection type." : "Connect a cluster, or ask another workspace owner to share one. Credentials stay private to each workspace."}
            action={!clusters.length && isOwner ? (
              <Button nativeButton={false} render={<Link href={`/workspaces/${workspace.id}/clusters/new`} />}>
                <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} aria-hidden="true" />
                Connect cluster
              </Button>
            ) : undefined}
            hint={clusters.length ? undefined : "Ask a workspace owner to connect a cluster."}
          />
        )}
      </InventoryPanel>
      {credentialCluster && (
        <DefaultCredentialDialog
          cluster={credentialCluster}
          workspace={workspace}
          workspaceCredentials={workspaceCredentials}
          action={action}
          onClose={() => setCredentialCluster(null)}
          onSaved={(credentialId) => setCredentialByCluster((current) => ({ ...current, [credentialCluster.id]: credentialId }))}
        />
      )}
      {editing && (
        <EditClusterDialog
          cluster={editing}
          workspace={workspace}
          user={user}
          globalCredentials={globalCredentials}
          workspaceCredentials={workspaceCredentials}
          action={action}
          onClose={() => setEditing(null)}
          onSaved={(cluster, credentialId) => {
            onUpdated(cluster)
            setCredentialByCluster((current) => ({ ...current, [cluster.id]: credentialId }))
          }}
        />
      )}
      {agentCluster && (
        <ClusterAgentConnection
          open
          onOpenChange={(next) => { if (!next) setAgentClusterId(null) }}
          cluster={agentCluster}
          workspace={workspace}
          canManage={isOwner && (agentCluster.workspaceId === workspace.id || (!agentCluster.workspaceId && !!user?.isAdmin))}
          onModeChange={(mode) => onUpdated({ ...agentCluster, connectionMode: mode })}
        />
      )}
    </div>
  )
}

/** Loads the workspace default credential id for a cluster, with retry. */
function useWorkspaceCredential(clusterId: string, workspaceId: string) {
  const [credentialId, setCredentialId] = useState("")
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState<unknown | null>(null)
  const [tick, setTick] = useState(0)
  useEffect(() => {
    let active = true
    api<WorkspaceCredentialResponse>(workspaceCredentialUrl(clusterId, workspaceId))
      .then((result) => {
        if (!active) return
        setCredentialId(result.credentialId ?? "")
        setLoaded(true)
      })
      .catch((cause) => { if (active) setError(cause) })
    return () => { active = false }
  }, [clusterId, workspaceId, tick])
  function retry() {
    setError(null)
    setTick((value) => value + 1)
  }
  return { credentialId, setCredentialId, loaded, error, retry }
}

function DefaultCredentialDialog({ cluster, workspace, workspaceCredentials, action, onClose, onSaved }: {
  cluster: Cluster
  workspace: Workspace
  workspaceCredentials: Credential[]
  action: ActionRunner
  onClose: () => void
  onSaved: (credentialId: string | null) => void
}) {
  const { saving, track } = useSaving()
  const { credentialId, setCredentialId, loaded, error, retry } = useWorkspaceCredential(cluster.id, workspace.id)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const ok = await track(action(
      () => api<WorkspaceCredentialResponse>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/workspace-credential`, {
        method: "PUT",
        body: JSON.stringify({ workspaceId: workspace.id, credentialId: credentialId || null }),
      }),
      "Default credential saved.",
      (result) => onSaved(result.credentialId)
    ))
    if (ok) onClose()
  }
  return (
    <AppDialog
      open
      onOpenChange={(open) => { if (!open) onClose() }}
      busy={saving}
      size="md"
      title="Default credential"
      description={`Used for all namespace access on ${cluster.name} unless a namespace has its own credential. Its Kubernetes RBAC decides which namespaces it can reach. Private to this workspace.`}
    >
      <form className="space-y-5" onSubmit={submit}>
        {error != null
          ? <LoadErrorNotice error={error} title="Could not load the current default credential" onRetry={retry} />
          : !loaded
            ? <FieldsSkeleton label="Loading default credential" />
            : (
              <FormField label="Default credential" htmlFor="workspace-cluster-credential">
                <FormSelect
                  id="workspace-cluster-credential"
                  value={credentialId}
                  onValueChange={setCredentialId}
                  emptyOption={cluster.shared || cluster.workspaceId ? "No default credential" : "Use instance default credential"}
                  items={workspaceCredentials.map((credential) => ({ value: credential.id, label: credential.name }))}
                />
              </FormField>
            )}
        <DialogFooter>
          <DialogCancel disabled={saving} />
          <Button type="submit" loading={saving} loadingText="Saving…" disabled={!loaded || workspace.role !== "owner"}>Save changes</Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}

type TabId = "general" | "auth" | "limits"

function EditClusterDialog({ cluster, workspace, user, globalCredentials, workspaceCredentials, action, onClose, onSaved }: {
  cluster: Cluster
  workspace: Workspace
  user: User | null
  globalCredentials: Credential[]
  workspaceCredentials: Credential[]
  action: ActionRunner
  onClose: () => void
  onSaved: (cluster: Cluster, credentialId: string | null) => void
}) {
  const { saving, track } = useSaving()
  const credential = useWorkspaceCredential(cluster.id, workspace.id)
  const [tab, setTab] = useState<TabId>("general")
  const [attempted, setAttempted] = useState(false)
  const [name, setName] = useState(cluster.name)
  const [apiServer, setApiServer] = useState(cluster.apiServer)
  const [caDataBase64, setCaDataBase64] = useState("")
  const [insecure, setInsecure] = useState(cluster.insecureSkipVerify)
  const [defaultCredentialId, setDefaultCredentialId] = useState(cluster.defaultCredentialId ?? "")
  const [clusterScopeCredentialId, setClusterScopeCredentialId] = useState(cluster.clusterScopeCredentialId ?? "")
  const [maxConcurrent, setMaxConcurrent] = useState(String(cluster.maxConcurrentOperations || 2))
  const [perMinute, setPerMinute] = useState(String(cluster.operationsPerMinute || 30))

  const nameError = name.trim() ? "" : "Enter a cluster name."
  const apiError = !apiServer.trim() ? "Enter the Kubernetes API URL." : isHttpUrl(apiServer) ? "" : "Enter a valid URL, for example https://api.example.com:6443."
  const maxError = intRangeError(maxConcurrent, 1, 20)
  const rateError = intRangeError(perMinute, 1, 1000)
  const generalInvalid = Boolean(nameError || apiError)
  const limitsInvalid = Boolean(maxError || rateError)
  const showInstanceCredentials = !cluster.workspaceId && !!user?.isAdmin && globalCredentials.length > 0

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (generalInvalid || limitsInvalid) {
      setAttempted(true)
      setTab(generalInvalid ? "general" : "limits")
      return
    }
    const ok = await track(action(
      async () => {
        const updated = await api<Cluster>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}`, {
          method: "PUT",
          body: JSON.stringify({
            name,
            apiServer,
            ...(caDataBase64 ? { caDataBase64 } : {}),
            insecureSkipVerify: insecure,
            defaultCredentialId: defaultCredentialId || null,
            clusterScopeCredentialId: clusterScopeCredentialId || null,
            maxConcurrentOperations: Number(maxConcurrent),
            operationsPerMinute: Number(perMinute),
          }),
        })
        const authentication = await api<WorkspaceCredentialResponse>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/workspace-credential`, {
          method: "PUT",
          body: JSON.stringify({ workspaceId: workspace.id, credentialId: credential.credentialId || null }),
        })
        return { updated, credentialId: authentication.credentialId }
      },
      "Cluster updated. The CA certificate was kept unless you replaced it.",
      (result) => onSaved(result.updated, result.credentialId)
    ))
    if (ok) onClose()
  }

  const errorDot = (visible: boolean) => visible && <span role="img" aria-label="Contains errors" className="size-1.5 rounded-full bg-destructive" />
  return (
    <AppDialog
      open
      onOpenChange={(open) => { if (!open) onClose() }}
      busy={saving}
      title={`Edit cluster · ${cluster.name}`}
      description="Update the endpoint, authentication and rate limits for this cluster connection."
    >
      <form className="space-y-5" onSubmit={submit} noValidate>
        {credential.error != null && <LoadErrorNotice error={credential.error} title="Could not load the default credential" onRetry={credential.retry} />}
        <Tabs.Root value={tab} onValueChange={(value) => setTab(value as TabId)}>
          <Tabs.List aria-label="Cluster settings" className="mb-5 flex gap-6 overflow-x-auto border-b" activateOnFocus>
            <Tabs.Tab value="general" className={tabClass}>General{errorDot(attempted && generalInvalid)}</Tabs.Tab>
            <Tabs.Tab value="auth" className={tabClass}>Authentication & TLS</Tabs.Tab>
            <Tabs.Tab value="limits" className={tabClass}>Limits{errorDot(attempted && limitsInvalid)}</Tabs.Tab>
          </Tabs.List>
          <Tabs.Panel value="general" className="space-y-5 outline-none sm:min-h-72">
            <FormField label="Cluster name" htmlFor="cluster-name">
              <Input id="cluster-name" placeholder="prod-eu-1" value={name} onChange={(event) => setName(event.target.value)} aria-invalid={attempted && !!nameError} aria-describedby={attempted && nameError ? "cluster-name-error" : undefined} />
              {attempted && <FieldError id="cluster-name-error">{nameError}</FieldError>}
            </FormField>
            <FormField label="Kubernetes API URL" htmlFor="cluster-api">
              <Input id="cluster-api" type="url" placeholder="https://api.example.com:6443" value={apiServer} onChange={(event) => setApiServer(event.target.value)} aria-invalid={attempted && !!apiError} aria-describedby={attempted && apiError ? "cluster-api-error" : undefined} />
              {attempted && <FieldError id="cluster-api-error">{apiError}</FieldError>}
            </FormField>
          </Tabs.Panel>
          <Tabs.Panel value="auth" className="space-y-5 outline-none sm:min-h-72">
            <FormField label="CA certificate (base64)" htmlFor="cluster-ca" hint="Leave blank to keep the current CA certificate.">
              <Textarea id="cluster-ca" className={monoTextareaClass} value={caDataBase64} onChange={(event) => setCaDataBase64(event.target.value)} />
            </FormField>
            <FormField label="Default credential for this workspace" htmlFor="edit-workspace-credential" hint="Used for all namespace access without its own credential. A cluster-wide ServiceAccount token can be selected here.">
              {credential.loaded
                ? <FormSelect id="edit-workspace-credential" value={credential.credentialId} onValueChange={credential.setCredentialId} emptyOption="No default credential" items={workspaceCredentials.map((item) => ({ value: item.id, label: item.name }))} />
                : credential.error != null ? <p className="text-sm text-muted-foreground">Unavailable until the default credential loads.</p> : <FieldsSkeleton label="Loading default credential" />}
            </FormField>
            {showInstanceCredentials && (
              <div className="space-y-3 rounded-lg border p-3">
                <div>
                  <h3 className="text-sm font-semibold">Instance credentials</h3>
                  <p className="mt-0.5 text-sm text-muted-foreground">Optional. These apply to every workspace; workspace credentials stay private to each workspace.</p>
                </div>
                <div className="grid gap-3 sm:grid-cols-2">
                  <FormField label="Instance default credential" htmlFor="cluster-default">
                    <FormSelect id="cluster-default" value={defaultCredentialId} onValueChange={setDefaultCredentialId} emptyOption="None" items={globalCredentials.map((item) => ({ value: item.id, label: item.name }))} />
                  </FormField>
                  <FormField label="Cluster-scope credential" htmlFor="cluster-scope" hint="Only used for optional cluster-wide permission checks.">
                    <FormSelect id="cluster-scope" value={clusterScopeCredentialId} onValueChange={setClusterScopeCredentialId} emptyOption="Disabled (recommended)" items={globalCredentials.map((item) => ({ value: item.id, label: item.name }))} />
                  </FormField>
                </div>
              </div>
            )}
            <SwitchField
              id="cluster-insecure"
              tone="warning"
              checked={insecure}
              onCheckedChange={setInsecure}
              label="Skip TLS certificate verification"
              description="Unsafe: JustCD will not check the cluster's identity, so traffic could be intercepted. Use only for temporary development clusters."
            />
          </Tabs.Panel>
          <Tabs.Panel value="limits" className="space-y-5 outline-none sm:min-h-72">
            <p className="text-sm text-muted-foreground">Limits protect the cluster from too many simultaneous changes from JustCD.</p>
            <div className="grid gap-3 sm:grid-cols-2">
              <FormField label="Max concurrent operations" htmlFor="cluster-max-concurrent" hint="Simultaneous operations against this cluster (1–20).">
                <Input id="cluster-max-concurrent" type="number" min={1} max={20} value={maxConcurrent} onChange={(event) => setMaxConcurrent(event.target.value)} aria-invalid={!!maxError} aria-describedby={maxError ? "cluster-max-concurrent-error" : undefined} />
                <FieldError id="cluster-max-concurrent-error">{maxError}</FieldError>
              </FormField>
              <FormField label="Operations per minute" htmlFor="cluster-operations-per-minute" hint="Operation starts are spaced to stay under this rate (1–1000).">
                <Input id="cluster-operations-per-minute" type="number" min={1} max={1000} value={perMinute} onChange={(event) => setPerMinute(event.target.value)} aria-invalid={!!rateError} aria-describedby={rateError ? "cluster-operations-per-minute-error" : undefined} />
                <FieldError id="cluster-operations-per-minute-error">{rateError}</FieldError>
              </FormField>
            </div>
          </Tabs.Panel>
        </Tabs.Root>
        <DialogFooter>
          <DialogCancel disabled={saving} />
          <Button type="submit" loading={saving} loadingText="Saving…" disabled={!credential.loaded}>Save changes</Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}
