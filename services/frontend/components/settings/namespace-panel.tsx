"use client"

import { useEffect, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { PlusSignIcon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { RowActions } from "@/components/action-menu"
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
  NamespaceBinding,
  Workspace,
} from "@/lib/types"
import {
  CopyCode,
  deleteItem,
  FieldError,
  InventoryPanel,
  LoadErrorNotice,
  SectionEmpty,
  statusLabel,
  statusVariant,
  useSaving,
  type ActionRunner,
} from "@/components/settings/shared"

export function NamespacePanel({
  workspace,
  cluster,
  clusters,
  activeClusterId,
  onClusterChange,
  credentials,
  bindings,
  busy,
  action,
  createOpen,
  onCreateOpenChange,
  onCreated,
  onUpdated,
  onDeleted,
}: {
  workspace: Workspace
  cluster?: Cluster
  clusters: Cluster[]
  activeClusterId: string
  onClusterChange: (value: string) => void
  credentials: Credential[]
  bindings: NamespaceBinding[]
  busy: boolean
  action: ActionRunner
  createOpen: boolean
  onCreateOpenChange: (open: boolean) => void
  onCreated: (value: NamespaceBinding) => void
  onUpdated: (value: NamespaceBinding) => void
  onDeleted: (namespace: string) => void
}) {
  const toast = useToast()
  const isOwner = workspace.role === "owner"
  const [editing, setEditing] = useState<NamespaceBinding | null>(null)
  const [workspaceDefault, setWorkspaceDefault] = useState(false)
  const [reports, setReports] = useState<Record<string, KubernetesPermissionTest>>({})
  const [statusLoaded, setStatusLoaded] = useState(false)
  const [statusError, setStatusError] = useState<unknown | null>(null)
  const [statusTick, setStatusTick] = useState(0)
  const [testingNamespace, setTestingNamespace] = useState("")
  const [includeClusterScope, setIncludeClusterScope] = useState(false)

  useEffect(() => {
    if (!cluster) return
    let active = true
    Promise.all([
      api<{ credentialId: string | null }>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/workspace-credential?workspaceId=${encodeURIComponent(workspace.id)}`),
      api<ListResponse<KubernetesPermissionTest>>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/tests?workspaceId=${encodeURIComponent(workspace.id)}`),
    ])
      .then(([credential, tests]) => {
        if (!active) return
        setWorkspaceDefault(!!credential.credentialId)
        setReports(Object.fromEntries(tests.items.map((item) => [item.namespace, item])))
        setStatusLoaded(true)
      })
      .catch((cause) => { if (active) setStatusError(cause) })
    return () => { active = false }
  }, [workspace, cluster, statusTick])

  function retryStatus() {
    setStatusError(null)
    setStatusTick((tick) => tick + 1)
  }

  async function runSelfTest(targetNamespace: string, clusterScope = false) {
    if (!cluster) return
    setTestingNamespace(targetNamespace)
    try {
      await action(
        () => apiPost<KubernetesPermissionReport>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/test`, {
          workspaceId: workspace.id,
          namespace: targetNamespace,
          includeClusterScope: clusterScope,
        }),
        (report) => report.status === "passed"
          ? `Kubernetes permissions passed in ${report.namespace}.`
          : (report.failure?.message ?? `Kubernetes permission checks: ${statusLabel(report.status).toLowerCase()} in ${report.namespace}.`),
        (report) => setReports((current) => ({
          ...current,
          [report.namespace]: { workspaceId: workspace.id, clusterId: cluster.id, namespace: report.namespace, report, checkedAt: report.checkedAt },
        }))
      )
    } finally {
      setTestingNamespace("")
    }
  }

  const clusterScopeHint = [
    "Also checks namespace get, list, create, apply, and delete permissions with the cluster-scope credential, or the namespace credential if none is set. Read-only: nothing is changed.",
    !cluster?.clusterScopeCredentialId ? "Without a cluster-scope credential these checks use your namespace or default credential and do not enable cluster-wide deployments." : "",
    !isOwner ? "Only workspace owners can request cluster-wide checks." : "",
  ].filter(Boolean).join(" ")

  return (
    <div className="space-y-6">
      {statusError != null && <LoadErrorNotice error={statusError} title="Could not load self-test results and the default credential" onRetry={retryStatus} />}
      <InventoryPanel
        title="Namespace access"
        description="Namespaces this workspace can deploy to on the selected cluster."
        count={bindings.length}
        toolbar={
          <div className="max-w-xs">
            <FormField label="Cluster" htmlFor="active-cluster">
              <FormSelect id="active-cluster" value={activeClusterId} onValueChange={onClusterChange} emptyOption="Select cluster" items={clusters.map((item) => ({ value: item.id, label: item.name }))} />
            </FormField>
          </div>
        }
      >
        {bindings.length ? (
          <ul className="divide-y">
            {bindings.map((binding) => {
              const record = reports[binding.namespace]
              const report = record?.report
              const credentialName = binding.credentialId ? credentials.find((item) => item.id === binding.credentialId)?.name || "Namespace credential" : null
              return (
                <ConnectionRow
                  key={binding.namespace}
                  icon={<WorkspaceIcon name="server" className="size-4" />}
                  title={<span className="font-mono">{binding.namespace}</span>}
                  subtitle={cluster?.connectionMode === "agent" ? "Agent local namespace profile" : credentialName ? `Credential: ${credentialName}` : "Uses the default credential"}
                  badges={report && <Badge variant={statusVariant(report.status)} radius="full">Self-test: {statusLabel(report.status)}</Badge>}
                  meta={
                    record
                      ? `Last self-test ${new Date(record.checkedAt).toLocaleString()}`
                      : statusError != null
                        ? "Self-test history unavailable."
                        : statusLoaded
                          ? "No self-test saved yet. New namespace access is tested automatically."
                          : <Skeleton className="h-4 w-48" />
                  }
                  actions={
                    <RowActions
                      label={binding.namespace}
                      primary={
                        <Button
                          size="sm"
                          variant="outline"
                          type="button"
                          aria-label={`Run self-test for ${binding.namespace}`}
                          loading={testingNamespace === binding.namespace}
                          loadingText="Testing…"
                          disabled={busy || !cluster}
                          onClick={() => void runSelfTest(binding.namespace, includeClusterScope)}
                        >
                          Run self-test
                        </Button>
                      }
                      items={isOwner && cluster ? [
                        { label: "Edit access", onSelect: () => setEditing(binding), disabled: busy },
                        deleteItem({
                          label: "Remove access",
                          name: `${binding.namespace} namespace access`,
                          description: "This removes this workspace's access to the namespace from JustCD. The Kubernetes namespace and its resources remain. Applications that use this access must be removed first.",
                          confirmLabel: "Remove access",
                          endpoint: `/api/v1/clusters/${encodeURIComponent(cluster.id)}/bindings/${encodeURIComponent(binding.namespace)}?workspaceId=${encodeURIComponent(workspace.id)}`,
                          successMessage: `Access to ${binding.namespace} removed.`,
                          onDeleted: () => {
                            if (editing?.namespace === binding.namespace) setEditing(null)
                            onDeleted(binding.namespace)
                          },
                          toast,
                          disabled: busy,
                        }),
                      ] : []}
                    />
                  }
                >
                  {report && (
                    <Disclosure summary="View permission checks and suggested RBAC">
                      <PermissionReportDetails report={report} />
                    </Disclosure>
                  )}
                </ConnectionRow>
              )
            })}
          </ul>
        ) : (
          <SectionEmpty
            title="No namespace access yet"
            description="Grant access to a namespace so applications in this workspace can deploy to it."
            action={isOwner && cluster ? (
              <Button type="button" onClick={() => onCreateOpenChange(true)}>
                <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} aria-hidden="true" />
                Grant namespace access
              </Button>
            ) : undefined}
            hint="Ask a workspace owner to grant namespace access."
          />
        )}
      </InventoryPanel>
      <SwitchField
        id="include-cluster-scope"
        checked={includeClusterScope}
        onCheckedChange={setIncludeClusterScope}
        disabled={busy || !isOwner || !cluster}
        label="Include optional cluster-wide checks"
        description={clusterScopeHint}
      />
      {(createOpen || editing) && cluster && (
        <NamespaceDialog
          key={editing?.namespace ?? "new"}
          workspace={workspace}
          cluster={cluster}
          credentials={credentials}
          workspaceDefault={workspaceDefault}
          action={action}
          editing={editing ?? undefined}
          onClose={() => { setEditing(null); onCreateOpenChange(false) }}
          onSaved={(binding, created) => {
            if (created) onCreated(binding)
            else onUpdated(binding)
            void runSelfTest(binding.namespace)
          }}
        />
      )}
    </div>
  )
}

function NamespaceDialog({ workspace, cluster, credentials, workspaceDefault, action, editing, onClose, onSaved }: {
  workspace: Workspace
  cluster: Cluster
  credentials: Credential[]
  workspaceDefault: boolean
  action: ActionRunner
  editing?: NamespaceBinding
  onClose: () => void
  onSaved: (binding: NamespaceBinding, created: boolean) => void
}) {
  const { saving, track } = useSaving()
  const [namespace, setNamespace] = useState(editing?.namespace ?? "")
  const [credentialId, setCredentialId] = useState(editing?.credentialId ?? "")
  const [attempted, setAttempted] = useState(false)
  const namespaceError = namespace.trim() ? "" : "Enter a namespace."
  const agent = cluster.connectionMode === "agent"
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (namespaceError) {
      setAttempted(true)
      return
    }
    let saved: NamespaceBinding | undefined
    const ok = await track(action(
      () => editing
        ? api<NamespaceBinding>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/bindings/${encodeURIComponent(editing.namespace)}`, {
            method: "PUT",
            body: JSON.stringify({ workspaceId: workspace.id, credentialId: credentialId || null }),
          })
        : apiPost<NamespaceBinding>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/bindings`, {
            workspaceId: workspace.id,
            namespace,
            credentialId: credentialId || undefined,
          }),
      editing ? "Namespace access updated." : "Namespace access granted.",
      (binding) => { saved = binding }
    ))
    if (ok && saved) {
      onClose()
      onSaved(saved, !editing)
    }
  }
  return (
    <AppDialog
      open
      onOpenChange={(open) => { if (!open) onClose() }}
      busy={saving}
      title={editing ? "Edit namespace access" : "Grant namespace access"}
      description={`Choose the namespace and credential this workspace uses on ${cluster.name}.`}
    >
      <form className="space-y-5" onSubmit={submit} noValidate>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label="Namespace" htmlFor="namespace-name" hint="Kubernetes namespace, up to 63 characters.">
            <Input id="namespace-name" placeholder="payments" value={namespace} onChange={(event) => setNamespace(event.target.value)} maxLength={63} readOnly={!!editing} aria-invalid={attempted && !!namespaceError} aria-describedby={attempted && namespaceError ? "namespace-name-error" : undefined} />
            {attempted && <FieldError id="namespace-name-error">{namespaceError}</FieldError>}
          </FormField>
          {agent
            ? <p className="self-center text-sm text-muted-foreground">Access uses the agent’s local namespace profile.</p>
            : (
              <FormField label="Credential for this namespace" htmlFor="namespace-credential">
                <FormSelect
                  id="namespace-credential"
                  value={credentialId}
                  onValueChange={setCredentialId}
                  emptyOption="Use default credential"
                  items={credentials.map((credential) => ({ value: credential.id, label: credential.name }))}
                />
              </FormField>
            )}
        </div>
        {!agent && !cluster.defaultCredentialId && !workspaceDefault && !credentialId && (
          <p className="text-sm text-warning-foreground">Choose a namespace credential, or set a default credential for this cluster first.</p>
        )}
        <DialogFooter>
          <DialogCancel disabled={saving} />
          <Button type="submit" loading={saving} loadingText={editing ? "Saving…" : "Granting…"} disabled={workspace.role !== "owner"}>
            {editing ? "Save changes" : "Grant access"}
          </Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}

function PermissionReportDetails({ report }: { report: KubernetesPermissionReport }) {
  const checks = [...report.checks, ...(report.clusterScope?.checks ?? [])]
  const suggestions = [
    ["Role", report.suggestions.roleYaml],
    ["RoleBinding", report.suggestions.roleBindingYaml],
    ["ClusterRole", report.suggestions.clusterRoleYaml],
    ["ClusterRoleBinding", report.suggestions.clusterRoleBindingYaml],
  ].filter((item): item is [string, string] => Boolean(item[1]))
  return (
    <div className="space-y-3 rounded-lg border bg-muted/20 p-3">
      <p className="text-sm text-muted-foreground">
        {report.serverVersion ? `Kubernetes ${report.serverVersion} · ` : ""}
        Checks ask Kubernetes whether the credential is allowed to act and do not touch deployment resources.
        <span className="block font-mono text-xs">Uses SelfSubjectAccessReview</span>
      </p>
      {report.failure && (
        <div className="rounded-md border border-destructive/20 bg-destructive/5 p-3 text-sm">
          <p className="font-medium">{report.failure.category}: {report.failure.message}</p>
          <p className="mt-1 text-muted-foreground">{report.failure.remediation}</p>
        </div>
      )}
      {report.clusterScope?.failure && (
        <div className="rounded-md border border-destructive/20 bg-destructive/5 p-3 text-sm">
          <p className="font-medium">Cluster-wide check: {report.clusterScope.failure.message}</p>
          <p className="mt-1 text-muted-foreground">{report.clusterScope.failure.remediation}</p>
        </div>
      )}
      {report.clusterScope?.status === "not_configured" && (
        <p className="text-sm text-muted-foreground">Cluster-wide checks were requested, but no cluster-scope credential is configured.</p>
      )}
      <ul className="grid gap-1 sm:grid-cols-2">
        {checks.map((check) => (
          <li key={`${check.scope}-${check.id}`} className="flex items-center justify-between gap-3 rounded-md bg-muted/40 px-2 py-1.5 text-sm">
            <span>{check.title}</span>
            <Badge variant={statusVariant(check.status)} radius="full">{statusLabel(check.status)}</Badge>
          </li>
        ))}
      </ul>
      {suggestions.length > 0 && (
        <div className="space-y-2">
          <h4 className="text-sm font-semibold">Suggested RBAC for missing permissions</h4>
          {suggestions.map(([title, value]) => (
            <div key={title} className="space-y-1">
              <p className="text-sm text-muted-foreground">{title}</p>
              <CopyCode block value={value} label={`${title} YAML`} />
            </div>
          ))}
          <p className="text-sm text-muted-foreground">Review and replace the placeholder subject before applying these suggestions.</p>
        </div>
      )}
    </div>
  )
}
