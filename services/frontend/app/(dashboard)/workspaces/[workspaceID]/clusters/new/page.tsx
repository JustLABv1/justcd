"use client"

import Link from "next/link"
import { useParams, useRouter } from "next/navigation"
import { useEffect, useMemo, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { CheckmarkCircle02Icon, Key01Icon, ServerStack02Icon, ShieldEnergyIcon } from "@hugeicons/core-free-icons"
import { ErrorDetailsButton } from "@/components/error-details"
import { IconStack } from "@/components/reui/icon-stack"
import {
  Stepper, StepperContent, StepperDescription, StepperIndicator, StepperItem,
  StepperNav, StepperPanel, StepperSeparator, StepperTitle, StepperTrigger,
} from "@/components/reui/stepper"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { FormField, PageHeading } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { Cluster, Credential, KubernetesPermissionReport, ListResponse, NamespaceBinding, Workspace } from "@/lib/types"

const steps = [
  { title: "Connection", description: "Create or reuse a cluster" },
  { title: "Cluster & access", description: "Endpoint and workspace credentials" },
  { title: "Namespace", description: "Limit the deployment scope" },
  { title: "Verify", description: "Connectivity and permissions" },
]

export default function ConnectClusterPage() {
  const { workspaceID } = useParams<{ workspaceID: string }>()
  const router = useRouter()
  const toast = useToast()
  const [workspace, setWorkspace] = useState<Workspace | null>(null)
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [connectionMode, setConnectionMode] = useState<"create" | "existing">("create")
  const [selectedClusterId, setSelectedClusterId] = useState("")
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [step, setStep] = useState(1)
  const [highestStep, setHighestStep] = useState(1)
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

  useEffect(() => {
    let active = true
    Promise.all([
      api<ListResponse<Workspace>>("/api/v1/workspaces"),
      api<ListResponse<Credential>>(`/api/v1/credentials?workspaceId=${encodeURIComponent(workspaceID)}`),
      api<ListResponse<Cluster>>(`/api/v1/clusters?workspaceId=${encodeURIComponent(workspaceID)}`),
    ]).then(([workspaces, credentialList, clusterList]) => {
      if (!active) return
      setWorkspace(workspaces.items.find((item) => item.id === workspaceID) ?? null)
      setCredentials(credentialList.items.filter((item) => item.kind === "kubernetes-token" || item.kind === "kubeconfig"))
      setClusters(clusterList.items)
      setSelectedClusterId(clusterList.items[0]?.id ?? "")
    }).catch((cause) => active && setError(cause))
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
    try {
      const result = await apiPost<KubernetesPermissionReport>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/test`, { workspaceId: workspace.id, namespace })
      setReport(result)
      if (result.status === "passed") toast.success("Cluster access verified.")
      else toast.error(result.failure?.message ?? "Some Kubernetes permissions are missing.")
    } catch (cause) { setError(cause) }
    finally { setBusy(false) }
  }

  return <>
    <PageHeading title="Connect a Kubernetes cluster" description={`A guided, verifiable connection for ${workspace?.name ?? "this workspace"}. Existing credentials stay reusable and secret values never return to the browser.`} actions={<Link href={`/workspaces/${workspaceID}?tab=connections`}><Button variant="outline">Exit workflow</Button></Link>} />
    {error && <div role="alert" className="mb-5 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive"><span>{errorMessage(error)}</span><ErrorDetailsButton error={error} /></div>}
    {workspace && workspace.role !== "owner" ? <div role="alert" className="rounded-2xl border bg-card p-6"><h2 className="text-sm font-semibold">Workspace owner access required</h2><p className="mt-2 text-xs leading-5 text-muted-foreground">Only a workspace owner can add credentials, cluster endpoints, or namespace bindings. Ask an owner to connect this cluster or accept a share.</p></div> : <Stepper orientation="vertical" value={step} onValueChange={(value) => { if (value <= highestStep) setStep(value) }} className="grid items-start gap-7 lg:grid-cols-[240px_minmax(0,1fr)]">
      <aside className="rounded-2xl border bg-card p-4 sm:p-5 lg:sticky lg:top-8">
        <div className="mb-4 flex items-center gap-4 lg:mb-6">
          <IconStack className="cluster-icon-stack h-14 w-12 text-primary lg:h-16 lg:w-14"><HugeiconsIcon icon={ServerStack02Icon} className="size-5" /></IconStack>
          <div><p className="text-sm font-semibold">Cluster connection</p><p className="mt-1 text-[11px] text-muted-foreground">Four focused steps</p></div>
        </div>
        <StepperNav className="grid w-full grid-cols-4 gap-1 lg:flex lg:grid-cols-none lg:gap-0">
          {steps.map((item, index) => <StepperItem key={item.title} step={index + 1} disabled={index + 1 > highestStep} className="items-center! justify-start! lg:items-start!">
            <StepperTrigger className="w-full flex-col items-center rounded-lg px-1 py-2 text-center transition-[background-color,transform] duration-150 motion-reduce:transition-none active:scale-[0.98] motion-reduce:active:scale-100 data-[state=active]:bg-muted lg:flex-row lg:items-start lg:px-2 lg:text-left">
              <StepperIndicator>{index + 1}</StepperIndicator>
              <span className="min-w-0"><StepperTitle className="text-[10px] lg:text-sm">{item.title}</StepperTitle><StepperDescription className="mt-1 hidden text-[11px] leading-4 lg:block">{item.description}</StepperDescription></span>
            </StepperTrigger>
            {index < steps.length - 1 && <StepperSeparator className="ml-[19px] hidden h-6! lg:block" />}
          </StepperItem>)}
        </StepperNav>
        <div className="mt-6 hidden rounded-xl bg-muted/50 p-3 text-[11px] leading-5 text-muted-foreground sm:block"><strong className="text-foreground">Safe by design.</strong> Verification uses discovery and permission reviews only—never workload mutations.</div>
      </aside>
      <StepperPanel className="min-w-0">
        <StepperContent value={1}><WorkflowPanel icon={ServerStack02Icon} eyebrow="Step 1 of 4" title="How would you like to connect?" description="Use a cluster already available to this workspace, or register a new endpoint. Shared clusters still need credentials owned by this workspace.">
          <div className="space-y-5">
            <div className="grid gap-3 sm:grid-cols-2">
              <button type="button" aria-pressed={connectionMode === "existing"} onClick={() => setConnectionMode("existing")} className={`rounded-xl border p-4 text-left transition-[border-color,background-color,transform] duration-150 motion-reduce:transition-none hover:border-primary/40 active:scale-[0.99] motion-reduce:active:scale-100 ${connectionMode === "existing" ? "border-primary bg-primary/5" : "bg-card"}`}><span className="block text-sm font-semibold">Use an accessible cluster</span><span className="mt-1 block text-xs leading-5 text-muted-foreground">Choose a private, shared, or legacy cluster already visible here.</span></button>
              <button type="button" aria-pressed={connectionMode === "create"} onClick={() => setConnectionMode("create")} className={`rounded-xl border p-4 text-left transition-[border-color,background-color,transform] duration-150 motion-reduce:transition-none hover:border-primary/40 active:scale-[0.99] motion-reduce:active:scale-100 ${connectionMode === "create" ? "border-primary bg-primary/5" : "bg-card"}`}><span className="block text-sm font-semibold">Create a cluster</span><span className="mt-1 block text-xs leading-5 text-muted-foreground">Register a new Kubernetes API endpoint for this workspace.</span></button>
            </div>
            {connectionMode === "existing" && <FormField label="Accessible cluster" htmlFor="wizard-existing-cluster" hint="Cluster credentials are never copied from another workspace."><FormSelect id="wizard-existing-cluster" value={selectedClusterId} onValueChange={setSelectedClusterId} emptyOption="Select a cluster" items={clusters.map((item) => ({ value: item.id, label: `${item.name}${item.shared ? ` · shared by ${item.ownerWorkspaceName}` : item.workspaceId ? " · this workspace" : " · instance"}` }))} /></FormField>}
            {connectionMode === "existing" && !clusters.length && <div className="rounded-xl border border-dashed p-4 text-xs text-muted-foreground">No clusters are accessible yet. You can create one here, or ask its owner to share a cluster with this workspace.</div>}
            <WorkflowActions><Button type="button" disabled={connectionMode === "existing" && !selectedAccessibleCluster} onClick={() => advance(2)}>Continue</Button></WorkflowActions>
          </div>
        </WorkflowPanel></StepperContent>
        <StepperContent value={2}><WorkflowPanel icon={Key01Icon} eyebrow="Step 2 of 4" title={connectionMode === "create" ? "Add cluster details and access" : "Configure workspace access"} description={connectionMode === "create" ? "The endpoint belongs to this workspace. Its Kubernetes credential stays private to this workspace too." : "Select a credential owned by this workspace. This is separate from the cluster owner’s authentication."}>
          <form onSubmit={saveCluster} className="space-y-5">
            {connectionMode === "create" && <div className="grid gap-4 sm:grid-cols-2"><FormField label="Cluster name" htmlFor="wizard-cluster-name"><Input id="wizard-cluster-name" value={clusterName} onChange={(event) => setClusterName(event.target.value)} placeholder="prod-eu-1" required /></FormField><FormField label="Kubernetes API URL" htmlFor="wizard-api-server"><Input id="wizard-api-server" type="url" value={apiServer} onChange={(event) => setApiServer(event.target.value)} placeholder="https://api.example.com:6443" required /></FormField><div className="sm:col-span-2"><FormField label="CA certificate (base64)" htmlFor="wizard-ca" hint="Optional when the system trust store already trusts the API certificate."><Textarea id="wizard-ca" className="min-h-24 font-mono text-xs" value={caDataBase64} onChange={(event) => setCaDataBase64(event.target.value)} /></FormField></div></div>}
            {connectionMode === "create" && <label className="flex items-start gap-3 rounded-xl border p-4 text-xs"><input type="checkbox" className="mt-0.5" checked={insecureSkipVerify} onChange={(event) => setInsecureSkipVerify(event.target.checked)} /><span><strong className="block">Skip TLS certificate verification</strong><span className="mt-1 block text-muted-foreground">Use only for temporary development clusters.</span></span></label>}
            {workspaceCredentials.length > 0 && !createCredential && <FormField label="Workspace Kubernetes credential" htmlFor="wizard-credential" hint="Only credentials owned by this workspace can be used here."><FormSelect id="wizard-credential" value={credentialId} onValueChange={setCredentialId} emptyOption="Select a credential" items={workspaceCredentials.map((item) => ({ value: item.id, label: item.name }))} /></FormField>}
            <button type="button" onClick={() => setCreateCredential((value) => !value)} className="w-full rounded-xl border border-dashed p-4 text-left transition-[border-color,background-color,transform] duration-150 motion-reduce:transition-none hover:border-primary/40 hover:bg-primary/3 active:scale-[0.99] motion-reduce:active:scale-100"><span className="block text-xs font-semibold">{createCredential ? "Use an existing workspace credential" : workspaceCredentials.length ? "Add a workspace credential" : "Create a workspace credential"}</span><span className="mt-1 block text-[11px] text-muted-foreground">Secret values are encrypted and never shown again.</span></button>
            {(createCredential || !workspaceCredentials.length) && <div className="grid gap-4 rounded-xl bg-muted/30 p-4 sm:grid-cols-2"><FormField label="Credential name" htmlFor="wizard-credential-name"><Input id="wizard-credential-name" value={credentialName} onChange={(event) => setCredentialName(event.target.value)} placeholder="production deploy token" required /></FormField><FormField label="Credential type" htmlFor="wizard-credential-kind"><FormSelect id="wizard-credential-kind" value={credentialKind} onValueChange={(value) => setCredentialKind(value as typeof credentialKind)} items={[{ value: "kubernetes-token", label: "Bearer token" }, { value: "kubeconfig", label: "Static kubeconfig" }]} /></FormField><div className="sm:col-span-2"><FormField label={credentialKind === "kubeconfig" ? "Kubeconfig content" : "Bearer token"} htmlFor="wizard-credential-secret"><Textarea id="wizard-credential-secret" className="min-h-28 font-mono text-xs" value={credentialSecret} onChange={(event) => setCredentialSecret(event.target.value)} required /></FormField></div></div>}
            <WorkflowActions back={() => setStep(1)}><Button type="submit" loading={busy} loadingText={createCredential ? "Securing credential…" : "Connecting cluster…"} disabled={!createCredential && workspaceCredentials.length > 0 && !credentialId}>{connectionMode === "create" ? "Save cluster and continue" : "Save access and continue"}</Button></WorkflowActions>
          </form>
        </WorkflowPanel></StepperContent>
        <StepperContent value={3}><WorkflowPanel icon={ShieldEnergyIcon} eyebrow="Step 3 of 4" title="Bind a namespace" description="Constrain this workspace to an explicit Kubernetes namespace before any application can target the cluster.">
          <form onSubmit={saveNamespace} className="space-y-5"><FormField label="Namespace" htmlFor="wizard-namespace" hint={existingBindings.length ? `Existing workspace bindings: ${existingBindings.map((binding) => binding.namespace).join(", ")}. Choose one to reuse or enter another.` : "The namespace must already exist unless you later configure approved cluster-scope access."}><Input id="wizard-namespace" value={namespace} onChange={(event) => setNamespace(event.target.value)} placeholder="production" required list="wizard-existing-namespaces" /><datalist id="wizard-existing-namespaces">{existingBindings.map((binding) => <option key={binding.namespace} value={binding.namespace} />)}</datalist></FormField><div className="rounded-xl border bg-muted/30 p-4 text-xs leading-5"><strong>{workspace?.name}</strong> will use <strong>{selectedCredential?.name}</strong> for <strong>{cluster?.name}</strong>. Namespace-specific credentials can be assigned later.</div><WorkflowActions><Button type="submit" loading={busy} loadingText="Binding namespace…" disabled={busy || bindingsLoadedFor !== `${workspace?.id}:${cluster?.id}`}>{bindingsLoadedFor === `${workspace?.id}:${cluster?.id}` ? "Bind and continue" : "Checking existing bindings…"}</Button></WorkflowActions></form>
        </WorkflowPanel></StepperContent>
        <StepperContent value={4}><WorkflowPanel icon={report?.status === "passed" ? CheckmarkCircle02Icon : ShieldEnergyIcon} eyebrow="Step 4 of 4" title={report?.status === "passed" ? "Cluster connection verified" : "Verify access safely"} description="Check TLS, API discovery, authentication, and the namespace permissions JustCD needs.">
          {!report ? <div className="space-y-5"><ReviewGrid values={[["Workspace", workspace?.name], ["Cluster", cluster?.name], ["Endpoint", cluster?.apiServer], ["Namespace", namespace], ["Credential", selectedCredential?.name]]} /><WorkflowActions><Button onClick={() => void verify()} loading={busy} loadingText="Running safe checks…">Run verification</Button></WorkflowActions></div> : <div className="space-y-5"><div className={`rounded-xl border p-5 ${report.status === "passed" ? "border-emerald-500/30 bg-emerald-500/5" : "border-amber-500/30 bg-amber-500/5"}`}><div className="flex items-center gap-3"><span className="grid size-9 place-items-center rounded-full bg-background"><HugeiconsIcon icon={report.status === "passed" ? CheckmarkCircle02Icon : ShieldEnergyIcon} className="size-5" /></span><div><p className="text-sm font-semibold capitalize">{report.status}</p><p className="mt-1 text-xs text-muted-foreground">{report.status === "passed" ? `All required namespace checks passed on ${report.serverVersion ?? "the Kubernetes API"}.` : report.failure?.message ?? `${report.checks.filter((item) => item.status === "missing").length} required permissions are missing.`}</p></div></div>{report.failure?.remediation && <p className="mt-4 border-t pt-3 text-xs leading-5"><strong>How to fix:</strong> {report.failure.remediation}</p>}</div><ReviewGrid values={[["Checks passed", `${report.checks.filter((item) => item.status === "passed").length} / ${report.checks.length}`], ["Namespace", report.namespace], ["Read Pods", report.canReadPods ? "Allowed" : "Not allowed"]]} /><WorkflowActions><Button variant="outline" onClick={() => void verify()} loading={busy}>Run again</Button><Button onClick={() => router.push(`/workspaces/${workspaceID}?tab=connections`)}>{report.status === "passed" ? "Finish connection" : "Finish with warnings"}</Button></WorkflowActions></div>}
        </WorkflowPanel></StepperContent>
      </StepperPanel>
    </Stepper>}
  </>
}

function WorkflowPanel({ icon, eyebrow, title, description, children }: { icon: typeof Key01Icon; eyebrow: string; title: string; description: string; children: React.ReactNode }) {
  return <section className="workflow-panel overflow-hidden rounded-2xl border bg-card"><header className="flex items-start gap-4 border-b p-5 sm:p-6"><span className="grid size-10 shrink-0 place-items-center rounded-xl bg-primary/10 text-primary"><HugeiconsIcon icon={icon} className="size-5" /></span><div><p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{eyebrow}</p><h2 className="mt-1 text-lg font-semibold tracking-tight">{title}</h2><p className="mt-1 max-w-2xl text-xs leading-5 text-muted-foreground">{description}</p></div></header><div className="p-5 sm:p-6">{children}</div></section>
}

function WorkflowActions({ back, children }: { back?: () => void; children: React.ReactNode }) { return <div className="flex flex-wrap justify-end gap-2 border-t pt-5">{back && <Button type="button" variant="outline" onClick={back}>Back</Button>}{children}</div> }
function ReviewGrid({ values }: { values: Array<[string, string | undefined]> }) { return <dl className="grid gap-px overflow-hidden rounded-xl border bg-border sm:grid-cols-2">{values.map(([label, value]) => <div key={label} className="bg-card p-4"><dt className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</dt><dd className="mt-1 break-all text-xs font-medium">{value || "—"}</dd></div>)}</dl> }
