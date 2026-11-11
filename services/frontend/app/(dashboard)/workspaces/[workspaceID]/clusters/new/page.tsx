"use client"

import Link from "next/link"
import { useParams, useRouter } from "next/navigation"
import { useEffect, useMemo, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { CheckmarkCircle02Icon, Key01Icon, ServerStack02Icon, ShieldEnergyIcon } from "@hugeicons/core-free-icons"
import { ErrorNotice } from "@/components/workspace-ui"
import { Alert } from "@/components/panelui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { Section } from "@/components/section"
import { ConnectionVerificationResult } from "@/components/connections/verification-result"
import { ConnectionSteps } from "@/components/connections/steps"
import { ClusterAgentConnection } from "@/components/cluster-agent-connection"
import { Button } from "@/components/panelui/button"
import { FormSelect } from "@/components/form-select"
import { Input } from "@/components/panelui/input"
import { Textarea } from "@/components/panelui/textarea"
import { Card } from "@/components/panelui/card"
import { ChoiceCards } from "@/components/panelui/choice-cards"
import { CheckboxCard, EmptyState, FormField, PageHeading } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, apiPost } from "@/lib/api"
import type { Cluster, Credential, KubernetesPermissionReport, ListResponse, NamespaceBinding, Workspace } from "@/lib/types"

const steps = [
  { title: "Connection", description: "Create or reuse a cluster" },
  { title: "Cluster & access", description: "Endpoint and workspace credentials" },
  { title: "Namespace access", description: "Limit the deployment scope" },
  { title: "Verify", description: "Connectivity and permissions" },
]

export default function ConnectClusterPage() {
  const { workspaceID } = useParams<{ workspaceID: string }>()
  const router = useRouter()
  const toast = useToast()
  const [workspace, setWorkspace] = useState<Workspace | null>(null)
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [connectionMode, setConnectionMode] = useState<"create" | "existing">("create")
  const [transportMode, setTransportMode] = useState<"direct" | "agent">("direct")
  const [selectedClusterId, setSelectedClusterId] = useState("")
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [step, setStep] = useState(1)
  const [highestStep, setHighestStep] = useState(1)
  const [loadedWorkspaceId, setLoadedWorkspaceId] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown | null>(null)
  const [credentialId, setCredentialId] = useState("")
  const [createCredential, setCreateCredential] = useState(false)
  const [credentialName, setCredentialName] = useState("")
  const [credentialKind, setCredentialKind] = useState<"kubernetes-token" | "kubeconfig">("kubernetes-token")
  const [credentialSecret, setCredentialSecret] = useState("")
  const [clusterName, setClusterName] = useState("")
  const [apiServer, setApiServer] = useState("")
  const [caDataBase64, setCaDataBase64] = useState("")
  const [insecureSkipVerify, setInsecureSkipVerify] = useState(false)
  const [cluster, setCluster] = useState<Cluster | null>(null)
  const [existingBindings, setExistingBindings] = useState<NamespaceBinding[]>([])
  const [bindingsLoadedFor, setBindingsLoadedFor] = useState("")
  const [namespace, setNamespace] = useState("default")
  const [report, setReport] = useState<KubernetesPermissionReport | null>(null)
  const loading = loadedWorkspaceId !== workspaceID

  useEffect(() => {
    let active = true
    Promise.all([
      api<ListResponse<Workspace>>("/api/v1/workspaces"),
      api<ListResponse<Credential>>(`/api/v1/credentials?workspaceId=${encodeURIComponent(workspaceID)}`),
      api<ListResponse<Cluster>>(`/api/v1/clusters?workspaceId=${encodeURIComponent(workspaceID)}`),
    ]).then(([workspaces, credentialList, clusterList]) => {
      if (!active) return
      setError(null)
      setWorkspace(workspaces.items.find((item) => item.id === workspaceID) ?? null)
      setCredentials(credentialList.items.filter((item) => item.kind === "kubernetes-token" || item.kind === "kubeconfig"))
      setClusters(clusterList.items)
      setSelectedClusterId(clusterList.items[0]?.id ?? "")
    }).catch((cause) => active && setError(cause)).finally(() => active && setLoadedWorkspaceId(workspaceID))
    return () => { active = false }
  }, [workspaceID])

  const selectedCredential = useMemo(
    () => credentials.find((item) => item.id === credentialId),
    [credentialId, credentials]
  )
  const workspaceCredentials = useMemo(
    () => credentials.filter((item) => item.workspaceId === workspaceID),
    [credentials, workspaceID]
  )
  const selectedAccessibleCluster = useMemo(
    () => clusters.find((item) => item.id === selectedClusterId),
    [clusters, selectedClusterId]
  )

  useEffect(() => {
    if (!cluster || !workspace) return
    let active = true
    api<ListResponse<NamespaceBinding>>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/bindings?workspaceId=${encodeURIComponent(workspace.id)}`)
      .then((result) => {
        if (!active) return
        setExistingBindings(result.items)
        setBindingsLoadedFor(`${workspace.id}:${cluster.id}`)
      })
      .catch((cause) => active && setError(cause))
    return () => { active = false }
  }, [cluster, workspace])

  function advance(next: number) {
    setStep(next)
    setHighestStep((current) => Math.max(current, next))
    setError(null)
  }

  async function saveCluster(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!workspace) return
    setBusy(true); setError(null)
    try {
      if ((connectionMode === "create" && transportMode === "agent") || (connectionMode === "existing" && selectedAccessibleCluster?.connectionMode === "agent")) {
        const created = connectionMode === "existing" ? selectedAccessibleCluster! : await apiPost<Cluster>("/api/v1/clusters", {workspaceId: workspace.id, name: clusterName, connectionMode: "agent"})
        setCluster(created); advance(3); return
      }
      let effectiveCredentialId = credentialId
      if (createCredential || !workspaceCredentials.length) {
        const created = await apiPost<Credential>("/api/v1/credentials", {
          workspaceId: workspace.id,
          name: credentialName,
          kind: credentialKind,
          secret: credentialKind === "kubeconfig" ? { content: credentialSecret } : { token: credentialSecret },
        })
        effectiveCredentialId = created.id
        setCredentials((items) => [...items, created])
        setCredentialId(created.id)
        setCredentialSecret("")
        toast.success("Credential encrypted and saved.")
      }
      if (!effectiveCredentialId) throw new Error("Choose or create a workspace credential to continue.")
      if (connectionMode === "existing") {
        if (!selectedAccessibleCluster) throw new Error("Choose an accessible cluster to continue.")
        await api(`/api/v1/clusters/${encodeURIComponent(selectedAccessibleCluster.id)}/workspace-credential`, {
          method: "PUT",
          body: JSON.stringify({ workspaceId: workspace.id, credentialId: effectiveCredentialId }),
        })
        setCluster(selectedAccessibleCluster)
        toast.success("Workspace access configured for the existing cluster.")
      } else {
        const created = await apiPost<Cluster>("/api/v1/clusters", {
          workspaceId: workspace.id,
          name: clusterName,
          apiServer,
          caDataBase64,
          insecureSkipVerify,
          workspaceCredentialId: effectiveCredentialId,
        })
        setCluster(created)
        toast.success("Cluster endpoint saved.")
      }
      advance(3)
    } catch (cause) { setError(cause) }
    finally { setBusy(false) }
  }

  async function saveNamespace(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!cluster || !workspace) return
    if (bindingsLoadedFor !== `${workspace.id}:${cluster.id}`) return
    setBusy(true); setError(null)
    try {
      if (!existingBindings.some((binding) => binding.namespace === namespace)) {
        await apiPost(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/bindings`, {
          workspaceId: workspace.id,
          namespace,
        })
      }
      toast.success(`${namespace} is now available to ${workspace.name}.`)
      advance(4)
    } catch (cause) { setError(cause) }
    finally { setBusy(false) }
  }

  async function verify() {
    if (!cluster || !workspace) return
    setBusy(true); setError(null)
    const progress = toast.loading("Verifying cluster access…")
    try {
      const result = await apiPost<KubernetesPermissionReport>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/test`, { workspaceId: workspace.id, namespace })
      setReport(result)
      if (result.status === "passed") toast.finish(progress, "success", "Cluster access verified.")
      else toast.finish(progress, "error", result.failure?.message ?? "Some Kubernetes permissions are missing.")
    } catch (cause) { toast.dismiss(progress); setError(cause) }
    finally { setBusy(false) }
  }

  return <>
    <PageHeading title="Connect cluster" description={`Connect a Kubernetes cluster to ${workspace?.name ?? "this workspace"} and verify access. Secret values never return to the browser.`} actions={<Button variant="outline" render={<Link href={`/workspaces/${workspaceID}/connections/clusters`} />}>Cancel</Button>} />
    {!loading && error && <div className="mb-5"><ErrorNotice error={error} /></div>}
    {loading ? <Skeleton role="status" aria-label="Loading workspace connections" className="h-44 motion-reduce:animate-none" /> : !workspace ? <Alert tone="destructive" variant="plain"><Alert.Icon /><Alert.Title>Workspace unavailable</Alert.Title><Alert.Description>Workspace not found or unavailable to your account.</Alert.Description></Alert> : workspace.role !== "owner" ? <Alert tone="warning" variant="plain"><Alert.Icon /><Alert.Title>Workspace owner access required</Alert.Title><Alert.Description>Only a workspace owner can add credentials, cluster endpoints, or namespace access. Ask an owner to connect this cluster or accept a shared connection.</Alert.Description></Alert> : <div className="grid grid-cols-1 items-start gap-7 lg:grid-cols-[240px_minmax(0,1fr)]">
      <aside className="min-w-0 lg:sticky lg:top-8"><Card size="sm"><Card.Content>
        <div className="mb-6 hidden items-center gap-4 lg:flex">
          <span aria-hidden="true" className="inline-flex shrink-0 items-center justify-center rounded-2xl bg-primary/10 p-3 cluster-icon-stack h-14 w-12 text-primary lg:h-16 lg:w-14"><HugeiconsIcon icon={ServerStack02Icon} className="size-5" /></span>
          <div><p className="text-sm font-semibold">Cluster connection</p><p className="mt-1 text-sm text-muted-foreground">Four focused steps</p></div>
        </div>
        <ConnectionSteps steps={steps} value={step} highestStep={highestStep} onValueChange={setStep} />
        <div className="mt-6 hidden text-xs leading-5 text-muted-foreground sm:block"><strong className="text-foreground">Safe by design.</strong> Verification uses discovery and permission reviews only—never workload mutations.</div>
      </Card.Content></Card></aside>
      <div className="min-w-0">
        <div hidden={step !== 1}><WorkflowPanel icon={ServerStack02Icon} eyebrow="Step 1 of 4" title="How would you like to connect?" description="Use a cluster already available to this workspace, or register a new one.">
          <div className="space-y-5">
            <div className="space-y-2">
              <ChoiceCards columns={1} className="sm:grid-cols-2" aria-label="Connection type" value={connectionMode} onValueChange={(next) => { if (next === "existing" || next === "create") setConnectionMode(next) }}>
                <ChoiceCards.Item value="existing"><ChoiceCards.Label>Use existing cluster</ChoiceCards.Label><ChoiceCards.Description>Use a cluster already available to this workspace.</ChoiceCards.Description></ChoiceCards.Item>
                <ChoiceCards.Item value="create"><ChoiceCards.Label>Add new cluster</ChoiceCards.Label><ChoiceCards.Description>Register a new Kubernetes endpoint or outbound agent.</ChoiceCards.Description></ChoiceCards.Item>
              </ChoiceCards>
              <p className="text-sm text-muted-foreground">{connectionMode === "existing" ? "Choose a private, shared, or instance-level cluster already visible here." : "Register a new Kubernetes API endpoint for this workspace."}</p>
            </div>
            {connectionMode === "existing" && <FormField label="Accessible cluster" htmlFor="wizard-existing-cluster" hint="Cluster credentials are never copied from another workspace."><FormSelect id="wizard-existing-cluster" value={selectedClusterId} onValueChange={setSelectedClusterId} emptyOption="Select a cluster" items={clusters.map((item) => ({ value: item.id, label: `${item.name}${item.shared ? ` · shared by ${item.ownerWorkspaceName}` : item.workspaceId ? " · this workspace" : " · instance"}` }))} /></FormField>}
            {connectionMode === "existing" && !clusters.length && <div className="min-w-0"><EmptyState title="No accessible clusters" description="Create a cluster here or ask its owner to share one with this workspace." /></div>}
            <WorkflowActions><Button type="button" disabled={connectionMode === "existing" && !selectedAccessibleCluster} onClick={() => advance(2)}>Continue</Button></WorkflowActions>
          </div>
        </WorkflowPanel></div>
        <div hidden={step !== 2}><WorkflowPanel icon={Key01Icon} eyebrow="Step 2 of 4" title={connectionMode === "create" ? "Add cluster details and access" : "Configure workspace access"} description={connectionMode === "create" ? "Choose direct API access or an outbound agent installed in the target cluster." : "Select a credential owned by this workspace. This is separate from the cluster owner’s authentication."}>
          <form onSubmit={saveCluster} className="space-y-5">
 {connectionMode === "create" && <FormField label="Connection method" htmlFor="wizard-transport"><FormSelect id="wizard-transport" value={transportMode} onValueChange={(value) => setTransportMode(value as "direct" | "agent")} items={[{value:"direct",label:"Direct Kubernetes API"},{value:"agent",label:"Outbound cluster agent"}]} /></FormField>}
 {transportMode === "agent" && connectionMode === "create" ? <div className="space-y-4"><FormField label="Cluster name" htmlFor="wizard-agent-name"><Input id="wizard-agent-name" value={clusterName} onChange={(event) => setClusterName(event.target.value)} required /></FormField><p className="text-sm text-muted-foreground">The target agent connects outbound over HTTPS. Install it after saving this cluster; its local profile controls workspace and namespace access.</p><WorkflowActions back={() => setStep(1)}><Button type="submit" loading={busy}>Continue</Button></WorkflowActions></div> : connectionMode === "existing" && selectedAccessibleCluster?.connectionMode === "agent" ? <div className="space-y-4"><p className="text-sm text-muted-foreground">This cluster uses an outbound agent. Its local profile must authorize workspace <code>{workspaceID}</code>.</p><WorkflowActions back={() => setStep(1)}><Button type="submit" loading={busy}>Continue</Button></WorkflowActions></div> : <>
            {connectionMode === "create" && <div className="grid grid-cols-1 gap-4 sm:grid-cols-2"><FormField label="Cluster name" htmlFor="wizard-cluster-name"><Input id="wizard-cluster-name" value={clusterName} onChange={(event) => setClusterName(event.target.value)} placeholder="prod-eu-1" required /></FormField><FormField label="Kubernetes API URL" htmlFor="wizard-api-server"><Input id="wizard-api-server" type="url" value={apiServer} onChange={(event) => setApiServer(event.target.value)} placeholder="https://api.example.com:6443" required /></FormField><div className="sm:col-span-2"><FormField label="CA certificate (base64)" htmlFor="wizard-ca" hint="Optional when the system trust store already trusts the API certificate."><Textarea id="wizard-ca" className="min-h-24 font-mono text-xs" value={caDataBase64} onChange={(event) => setCaDataBase64(event.target.value)} /></FormField></div></div>}
            {connectionMode === "create" && <CheckboxCard id="wizard-insecure-skip-verify" checked={insecureSkipVerify} onCheckedChange={setInsecureSkipVerify} title="Skip TLS certificate verification" description="Use only for temporary development clusters." />}
            {workspaceCredentials.length > 0 && !createCredential && <FormField label="Workspace Kubernetes credential" htmlFor="wizard-credential" hint="Only credentials owned by this workspace can be used here."><FormSelect id="wizard-credential" value={credentialId} onValueChange={setCredentialId} emptyOption="Select a credential" items={workspaceCredentials.map((item) => ({ value: item.id, label: item.name }))} /></FormField>}
            <Button type="button" variant="outline" onClick={() => setCreateCredential((value) => !value)}>{createCredential ? "Use an existing credential" : workspaceCredentials.length ? "Add credential" : "Create credential"}</Button>
            <p className="-mt-3 text-sm text-muted-foreground">Secret values are encrypted and never shown again.</p>
            {(createCredential || !workspaceCredentials.length) && <div className="grid grid-cols-1 gap-4 sm:grid-cols-2"><FormField label="Credential name" htmlFor="wizard-credential-name"><Input id="wizard-credential-name" value={credentialName} onChange={(event) => setCredentialName(event.target.value)} placeholder="production deploy token" required /></FormField><FormField label="Credential type" htmlFor="wizard-credential-kind"><FormSelect id="wizard-credential-kind" value={credentialKind} onValueChange={(value) => setCredentialKind(value as typeof credentialKind)} items={[{ value: "kubernetes-token", label: "Bearer token" }, { value: "kubeconfig", label: "Static kubeconfig" }]} /></FormField><div className="sm:col-span-2"><FormField label={credentialKind === "kubeconfig" ? "Kubeconfig content" : "Bearer token"} htmlFor="wizard-credential-secret"><Textarea id="wizard-credential-secret" className="min-h-28 font-mono text-xs" value={credentialSecret} onChange={(event) => setCredentialSecret(event.target.value)} required /></FormField></div></div>}
            <WorkflowActions back={() => setStep(1)}><Button type="submit" loading={busy} loadingText={createCredential ? "Securing credential…" : "Connecting cluster…"} disabled={!createCredential && workspaceCredentials.length > 0 && !credentialId}>Continue</Button></WorkflowActions>
          </>}</form>
        </WorkflowPanel></div>
        <div hidden={step !== 3}><WorkflowPanel icon={ShieldEnergyIcon} eyebrow="Step 3 of 4" title="Grant namespace access" description="Limit this workspace to an explicit Kubernetes namespace before any application can target the cluster.">
          {cluster?.connectionMode === "agent" && workspace && <div className="mb-5 space-y-3"><p className="mb-3 text-sm">Enroll and install the agent before verification.</p><ClusterAgentConnection cluster={cluster} workspace={workspace} canManage={!cluster.shared && cluster.workspaceId === workspace.id} onModeChange={(mode) => setCluster({...cluster,connectionMode:mode})} /></div>}
 <form onSubmit={saveNamespace} className="space-y-5"><FormField label="Namespace" htmlFor="wizard-namespace" hint={existingBindings.length ? `Namespaces this workspace can already use: ${existingBindings.map((binding) => binding.namespace).join(", ")}. Choose one to reuse or enter another.` : "The namespace must already exist unless you later configure approved cluster-scope access."}><Input id="wizard-namespace" value={namespace} onChange={(event) => setNamespace(event.target.value)} placeholder="production" required list="wizard-existing-namespaces" /><datalist id="wizard-existing-namespaces">{existingBindings.map((binding) => <option key={binding.namespace} value={binding.namespace} />)}</datalist></FormField><div className="text-sm leading-5 text-muted-foreground"><strong>{workspace?.name}</strong> will use <strong>{cluster?.connectionMode === "agent" ? "the agent’s local namespace profile" : selectedCredential?.name}</strong> for <strong>{cluster?.name}</strong>. Namespace-specific credentials can be assigned later.</div><WorkflowActions back={() => setStep(2)}><Button type="submit" loading={busy} loadingText="Granting access…" disabled={busy || bindingsLoadedFor !== `${workspace?.id}:${cluster?.id}`}>{bindingsLoadedFor === `${workspace?.id}:${cluster?.id}` ? "Continue" : "Checking namespace access…"}</Button></WorkflowActions></form>
        </WorkflowPanel></div>
        <div hidden={step !== 4}><WorkflowPanel icon={report?.status === "passed" ? CheckmarkCircle02Icon : ShieldEnergyIcon} eyebrow="Step 4 of 4" title={report?.status === "passed" ? "Cluster connection verified" : "Verify access safely"} description="Check TLS, API discovery, authentication, and the namespace permissions JustCD needs.">
          {!report ? <div className="space-y-5"><ReviewGrid values={[["Workspace", workspace?.name], ["Cluster", cluster?.name], ["Connection", cluster?.connectionMode === "agent" ? "Outbound agent" : cluster?.apiServer], ["Namespace", namespace], ["Credential", cluster?.connectionMode === "agent" ? "Local to agent" : selectedCredential?.name]]} /><WorkflowActions><Button onClick={() => void verify()} loading={busy} loadingText="Running safe checks…">Run verification</Button></WorkflowActions></div> : <ConnectionVerificationResult report={report} busy={busy} onRetry={() => void verify()} onFinish={() => router.push(`/workspaces/${workspaceID}/connections/clusters`)} />}
        </WorkflowPanel></div>
      </div>
    </div>}
  </>
}

function WorkflowPanel({ eyebrow, title, description, children }: { icon: typeof Key01Icon; eyebrow: string; title: string; description: string; children: React.ReactNode }) {
  return <Section surface title={title} description={description} action={<span className="text-xs font-medium text-muted-foreground">{eyebrow}</span>}>{children}</Section>
}

function WorkflowActions({ back, children }: { back?: () => void; children: React.ReactNode }) { return <div className="flex flex-wrap justify-end gap-2 pt-3">{back && <Button type="button" variant="outline" onClick={back}>Back</Button>}{children}</div> }
function ReviewGrid({ values }: { values: Array<[string, string | undefined]> }) { return <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">{values.map(([label, value]) => <div key={label} className="min-w-0"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{label}</dt><dd className="mt-1 break-all text-xs font-medium">{value || "—"}</dd></div>)}</dl> }
