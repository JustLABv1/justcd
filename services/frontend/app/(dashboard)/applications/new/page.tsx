"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { CheckboxCard, FormField, PageHeading, Panel, SwitchField } from "@/components/ui-kit"
import { KustomizeOptions } from "@/components/applications/kustomize-options"
import { SummaryList, Wizard, WizardFooter, useWizard } from "@/components/applications/wizard"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { api, apiPost } from "@/lib/api"
import type { Application, Cluster, GitSource, ListResponse, NamespaceBinding, Workspace } from "@/lib/types"

export default function NewApplicationPage() {
  const router = useRouter()
  const toast = useToast()
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [workspaceId, setWorkspaceId] = useState("")
  const [clusterId, setClusterId] = useState("")
  const [sources, setSources] = useState<GitSource[]>([])
  const [bindings, setBindings] = useState<NamespaceBinding[]>([])
  const [sourceId, setSourceId] = useState("")
  const [namespaces, setNamespaces] = useState<string[]>([])
  const [namespaceHelmValues, setNamespaceHelmValues] = useState<Record<string, { files: string[]; yaml: string }>>({})
  const [namespaceManifestPaths, setNamespaceManifestPaths] = useState<Record<string, string>>({})
  const [name, setName] = useState("")
  const [revision, setRevision] = useState("main")
  const [manifestPath, setManifestPath] = useState("deploy/")
  const [renderer, setRenderer] = useState("yaml")
  const [kustomizeHelmEnabled, setKustomizeHelmEnabled] = useState(false)
  const [kustomizeNamespaceOverride, setKustomizeNamespaceOverride] = useState(false)
  const [targetManifestPath, setTargetManifestPath] = useState("")
  const [helmValuesFiles, setHelmValuesFiles] = useState("")
  const [helmValuesYaml, setHelmValuesYaml] = useState("")
  const [createNamespaces, setCreateNamespaces] = useState(false)
  const [syncPolicy, setSyncPolicy] = useState("manual")
  const [pollSeconds, setPollSeconds] = useState("300")
  const [retryPolicy, setRetryPolicy] = useState({ enabled: true, maxAttempts: 5, initialDelaySeconds: 5, maxDelaySeconds: 300, jitterPercent: 20 })
  const [error, setError] = useState<unknown | null>(null)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [workspaceNotice, setWorkspaceNotice] = useState("")
  const [clusterNotice, setClusterNotice] = useState("")
  const errorRef = useRef<HTMLDivElement>(null)
  const wizard = useWizard(5, "new-app")

  useEffect(() => { if (error) errorRef.current?.scrollIntoView({ block: "nearest" }) }, [error])

  useEffect(() => {
    const queryWorkspace = new URLSearchParams(window.location.search).get("workspaceId") ?? ""
    let active = true
    api<ListResponse<Workspace>>("/api/v1/workspaces").then((workspaceResult) => {
      if (!active) return
      const writable = workspaceResult.items.filter((workspace) => workspace.role !== "viewer")
      setWorkspaces(writable)
      setWorkspaceId(writable.find((workspace) => workspace.id === queryWorkspace)?.id ?? writable[0]?.id ?? "")
    }).catch((cause) => active && setError(cause)).finally(() => active && setLoading(false))
    return () => { active = false }
  }, [])

  useEffect(() => {
    if (!workspaceId) return
    let active = true
    Promise.all([
      api<ListResponse<GitSource>>(`/api/v1/git-sources?workspaceId=${encodeURIComponent(workspaceId)}`),
      api<ListResponse<Cluster>>(`/api/v1/clusters?workspaceId=${encodeURIComponent(workspaceId)}`),
    ]).then(([sourceList, clusterList]) => {
      if (!active) return
      setSources(sourceList.items)
      setSourceId(sourceList.items[0]?.id ?? "")
      setClusters(clusterList.items)
      setClusterId(clusterList.items[0]?.id ?? "")
      setError(null)
    }).catch((cause) => active && setError(cause))
    return () => { active = false }
  }, [workspaceId])

  useEffect(() => {
    if (!workspaceId || !clusterId) return
    let active = true
    api<ListResponse<NamespaceBinding>>(`/api/v1/clusters/${encodeURIComponent(clusterId)}/bindings?workspaceId=${encodeURIComponent(workspaceId)}`).then((result) => {
      if (!active) return
      setBindings(result.items)
      setNamespaces((current) => current.filter((namespace) => result.items.some((item) => item.namespace === namespace)))
      setNamespaceHelmValues((current) => Object.fromEntries(Object.entries(current).filter(([namespace]) => result.items.some((item) => item.namespace === namespace))))
      setNamespaceManifestPaths((current) => Object.fromEntries(Object.entries(current).filter(([namespace]) => result.items.some((item) => item.namespace === namespace))))
    }).catch((cause) => active && setError(cause))
    return () => { active = false }
  }, [workspaceId, clusterId])

  const selectedWorkspace = useMemo(() => workspaces.find((workspace) => workspace.id === workspaceId), [workspaces, workspaceId])

  function missingSelection(step: number) {
    const missing = step === 1 ? (!workspaceId ? "Choose a workspace." : !sourceId ? "Choose a Git source." : "")
      : step === 3 ? (!clusterId ? "Choose a cluster." : !namespaces.length ? "Select at least one namespace." : "") : ""
    if (missing) setError(new Error(missing))
    return !missing
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!wizard.isLast) { if (wizard.next(missingSelection)) setError(null); return }
    if (!wizard.validateAll()) return
    setError(null)
    if (!workspaceId || !sourceId || !clusterId || !namespaces.length) { setError(new Error("Choose a workspace, Git source, cluster, and at least one namespace.")); return }
    setBusy(true)
    try {
      const app = await apiPost<Application>("/api/v1/applications", {
        workspaceId, name, sourceId, revision, manifestPath, renderer,
        kustomizeHelmEnabled: renderer === "kustomize" && kustomizeHelmEnabled,
        kustomizeNamespaceOverride: renderer === "kustomize" && kustomizeNamespaceOverride,
        targetManifestPath: renderer === "kustomize" ? targetManifestPath : "",
        namespaceManifestPaths: renderer === "kustomize" ? Object.fromEntries(namespaces.reduce<[string, string][]>((entries, namespace) => {
          const path = namespaceManifestPaths[namespace]?.trim() ?? ""
          if (path) entries.push([namespace, path])
          return entries
        }, [])) : {},
        helmValuesFiles: renderer === "helm" ? helmValuesFiles.split("\n").map((line) => line.trim()).filter(Boolean) : [],
        helmValuesYaml: renderer === "helm" ? helmValuesYaml : "",
        namespaceHelmValues: renderer === "helm" ? Object.fromEntries(namespaces.map((namespace) => [namespace, namespaceHelmValues[namespace] ?? { files: [], yaml: "" }])) : {},
        clusterId, namespaces, createNamespaces, syncPolicy, pollSeconds: Number(pollSeconds), retryPolicy,
      })
      toast.success("Application created.")
      router.push(`/applications/${app.id}`)
    } catch (cause) { setError(cause) } finally { setBusy(false) }
  }

  const clusterName = clusters.find((cluster) => cluster.id === clusterId)?.name ?? clusterId
  const sourceName = sources.find((source) => source.id === sourceId)?.name ?? sourceId
  const rendererLabel = renderer === "helm" ? "Helm template" : renderer === "kustomize" ? "Kustomize build" : "Plain YAML / JSON"

  return <>
    <PageHeading title="New application" description="Point JustCD at a Git revision and a repository path. We’ll render, diff, and review the changes before they reach Kubernetes." />
    <div ref={errorRef}>{error != null && <ErrorNotice error={error} />}</div>
    <form className="max-w-4xl space-y-5" onSubmit={submit} noValidate>
      <Wizard step={wizard.step} onStepChange={wizard.setStep} idPrefix="new-app" steps={[
        { title: "Source", content: <Panel title="Source" description="Select the workspace repository and the Git revision to render."><div className="grid gap-4 p-5 sm:grid-cols-2">
          <div className="space-y-2">
            <FormField label="Workspace" htmlFor="workspace"><FormSelect id="workspace" value={workspaceId} onValueChange={(value) => { if (value !== workspaceId && (sourceId || clusterId || namespaces.length)) setWorkspaceNotice("Git source, cluster and namespace selections were reset for the new workspace."); setWorkspaceId(value); setSourceId(""); setSources([]); setClusterId(""); setClusters([]); setBindings([]); setNamespaces([]); setNamespaceHelmValues({}); setNamespaceManifestPaths({}); setTargetManifestPath("") }} placeholder="Select workspace" required items={workspaces.map((workspace) => ({ value: workspace.id, label: workspace.name }))} /></FormField>
          </div>
          <FormField label="Git source" htmlFor="source"><FormSelect id="source" value={sourceId} onValueChange={(value) => { setSourceId(value); setWorkspaceNotice("") }} placeholder="Select source" required items={sources.map((source) => ({ value: source.id, label: `${source.name} · ${source.repositoryUrl}` }))} /></FormField>
          {workspaceNotice && <p role="status" className="rounded-lg border bg-muted/50 px-3 py-2 text-sm text-muted-foreground sm:col-span-2">{workspaceNotice}</p>}
          <FormField label="Application name" htmlFor="name" hint="Lowercase letters, numbers, dots, and dashes."><Input id="name" placeholder="billing-api" value={name} onChange={(event) => setName(event.target.value)} required pattern="[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?" /></FormField>
          <FormField label="Git revision" htmlFor="revision" hint="Branch, tag, or commit; JustCD pins the reviewed commit SHA."><Input id="revision" placeholder="main" value={revision} onChange={(event) => setRevision(event.target.value)} required /></FormField>
        </div></Panel> },
        { title: "Renderer", content: <Panel title="Renderer" description="Choose how the repository path is turned into Kubernetes manifests."><div className="grid gap-4 p-5 sm:grid-cols-2">
          <FormField label="Renderer" htmlFor="renderer"><FormSelect id="renderer" value={renderer} onValueChange={setRenderer} items={[{ value: "yaml", label: "Plain YAML / JSON" }, { value: "kustomize", label: "Kustomize build" }, { value: "helm", label: "Helm template" }]} /></FormField>
          <FormField label={renderer === "helm" ? "Chart path" : renderer === "kustomize" ? "Shared Kustomize path" : "Manifest path"} htmlFor="path" hint={renderer === "helm" ? "Repository-relative Helm chart directory." : renderer === "kustomize" ? "Fallback Kustomize entry point. Cluster and namespace paths can select complete overlays." : "Repository-relative directory or file."}><Input id="path" placeholder="deploy/" value={manifestPath} onChange={(event) => setManifestPath(event.target.value)} required /></FormField>
          {renderer === "kustomize" && <div className="sm:col-span-2"><KustomizeOptions idPrefix="new-app-kustomize" namespaceOverride={kustomizeNamespaceOverride} onNamespaceOverrideChange={setKustomizeNamespaceOverride} helmEnabled={kustomizeHelmEnabled} onHelmEnabledChange={setKustomizeHelmEnabled} /></div>}
          {renderer === "helm" && <div className="space-y-4 sm:col-span-2"><FormField label="Helm values files" htmlFor="helm-values-files" hint="One repository-relative YAML path per line. Later files override earlier files."><Textarea id="helm-values-files" value={helmValuesFiles} onChange={(event) => setHelmValuesFiles(event.target.value)} placeholder={"values/common.yaml\nvalues/production.yaml"} className="font-mono text-xs" /></FormField><FormField label="Helm values overrides" htmlFor="helm-values-yaml" hint="Optional JustCD values applied after the Git files."><Textarea id="helm-values-yaml" value={helmValuesYaml} onChange={(event) => setHelmValuesYaml(event.target.value)} placeholder={"agent:\n  logLevel: info"} className="min-h-32 font-mono text-xs" spellCheck={false} /></FormField></div>}
        </div></Panel> },
        { title: "Cluster & namespaces", content: <Panel title="Cluster & namespaces" description="Applications are restricted to the namespaces this workspace has access to on the cluster."><div className="space-y-5 p-5">
          <FormField label="Cluster" htmlFor="cluster"><FormSelect id="cluster" value={clusterId} onValueChange={(value) => { if (value !== clusterId && namespaces.length) setClusterNotice("Namespace selections were reset for the new cluster."); else setClusterNotice(""); setClusterId(value); setNamespaces([]); setNamespaceHelmValues({}); setNamespaceManifestPaths({}); setTargetManifestPath("") }} placeholder="Select cluster" required items={clusters.map((cluster) => ({ value: cluster.id, label: `${cluster.name} · ${cluster.apiServer}` }))} /></FormField>
          {clusterNotice && <p role="status" className="rounded-lg border bg-muted/50 px-3 py-2 text-sm text-muted-foreground">{clusterNotice}</p>}
          <div><p className="mb-2 text-sm font-medium">Namespaces</p>{bindings.length ? <div className="grid gap-2 sm:grid-cols-2">{bindings.map((binding) => <CheckboxCard key={binding.namespace} id={`new-app-ns-${binding.namespace}`} checked={namespaces.includes(binding.namespace)} title={binding.namespace} description={binding.credentialId ? "namespace credential" : "default credential"} onCheckedChange={(checked) => {
            if (checked) {
              setNamespaces((current) => [...current, binding.namespace])
              setNamespaceHelmValues((current) => ({ ...current, [binding.namespace]: current[binding.namespace] ?? { files: [], yaml: "" } }))
              setNamespaceManifestPaths((current) => ({ ...current, [binding.namespace]: current[binding.namespace] ?? "" }))
            } else {
              setNamespaces((current) => current.filter((namespace) => namespace !== binding.namespace))
              setNamespaceHelmValues((current) => { const next = { ...current }; delete next[binding.namespace]; return next })
              setNamespaceManifestPaths((current) => { const next = { ...current }; delete next[binding.namespace]; return next })
            }
          }} />)}</div> : <div className="rounded-lg border border-dashed px-4 py-5 text-sm text-muted-foreground">This workspace has no namespace access on this cluster. <Link href={`/workspaces/${workspaceId}/connections/namespaces`} className="font-medium text-primary hover:underline">Grant namespace access</Link></div>}</div>
          <CheckboxCard id="new-app-create-namespaces" checked={createNamespaces} onCheckedChange={setCreateNamespaces} title="Create missing namespaces" description="Create selected target namespaces before deploying workloads. Requires namespace creation permissions and plan approval. Namespaces are retained when the app is removed." />
          {renderer === "kustomize" && <FormField label="Cluster overlay path" htmlFor="cluster-overlay-path" hint="Optional Kustomize path for this cluster; blank uses the shared path. This overlay should include its shared base."><Input id="cluster-overlay-path" value={targetManifestPath} onChange={(event) => setTargetManifestPath(event.target.value)} placeholder="overlays/clusters/prod-eu" /></FormField>}
          {renderer === "helm" && namespaces.length > 0 && <div className="space-y-3 border-t pt-4"><p className="text-sm font-medium">Namespace Helm overrides</p>{namespaces.map((namespace) => {
            const override = namespaceHelmValues[namespace] ?? { files: [], yaml: "" }
            return <Disclosure key={namespace} className="rounded-lg border px-3 py-2.5" summary={<>{namespace} <span className="font-normal text-muted-foreground">· optional overrides</span></>}><div className="space-y-3"><FormField label="Namespace values files" htmlFor={`new-ns-files-${namespace}`} hint="Repository-relative paths, applied after shared values."><Textarea id={`new-ns-files-${namespace}`} value={override.files.join("\n")} onChange={(event) => setNamespaceHelmValues((current) => ({ ...current, [namespace]: { ...override, files: event.target.value.split("\n").map((line) => line.trim()).filter(Boolean) } }))} className="min-h-14 font-mono text-xs" /></FormField><FormField label="Namespace JustCD overrides" htmlFor={`new-ns-yaml-${namespace}`} hint="Applied after namespace Git files. Use Kubernetes Secret references for sensitive values."><Textarea id={`new-ns-yaml-${namespace}`} value={override.yaml} onChange={(event) => setNamespaceHelmValues((current) => ({ ...current, [namespace]: { ...override, yaml: event.target.value } }))} className="min-h-20 font-mono text-xs" spellCheck={false} /></FormField></div></Disclosure>
          })}</div>}
          {renderer === "kustomize" && namespaces.length > 0 && <div className="space-y-3 border-t pt-4"><p className="text-sm font-medium">Namespace overlays</p>{namespaces.map((namespace) => <FormField key={namespace} label={`${namespace} overlay path`} htmlFor={`new-ns-overlay-${namespace}`} hint="Optional Kustomize path; blank uses the cluster path. Include the base and any cluster overlay this namespace needs."><Input id={`new-ns-overlay-${namespace}`} value={namespaceManifestPaths[namespace] ?? ""} onChange={(event) => setNamespaceManifestPaths((current) => ({ ...current, [namespace]: event.target.value }))} placeholder={`overlays/namespaces/${namespace}`} /></FormField>)}</div>}
        </div></Panel> },
        { title: "Sync policy", content: <Panel title="Sync policy" description="Choose how often to check Git for changes and what may be applied automatically."><div className="space-y-4 p-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <FormField label="Sync policy" htmlFor="policy"><FormSelect id="policy" value={syncPolicy} onValueChange={setSyncPolicy} items={[{ value: "manual", label: "Manual · review each sync" }, { value: "auto-safe", label: "Auto-safe · apply non-destructive changes" }]} /></FormField>
            <FormField label="Poll interval (seconds)" htmlFor="poll" hint="Auto-safe still pauses for every deletion and cluster-wide change."><Input id="poll" type="number" min={30} max={86400} value={pollSeconds} onChange={(event) => setPollSeconds(event.target.value)} /></FormField>
          </div>
          <Disclosure summary="Advanced: retry policy" className="rounded-lg border p-3">
            <div className="space-y-3">
              <SwitchField id="new-app-retry-enabled" checked={retryPolicy.enabled} onCheckedChange={(enabled) => setRetryPolicy({ ...retryPolicy, enabled })} label="Retry transient reconciliation failures automatically" description="Only transient failures are retried. Each attempt recalculates the plan from live cluster state; authorization, validation, approval, and interrupted operations require review." />
              <div className="grid gap-3 sm:grid-cols-2">
                <FormField label="Maximum attempts" htmlFor="retry-attempts"><Input id="retry-attempts" type="number" min={1} max={20} value={retryPolicy.maxAttempts} onChange={(event) => setRetryPolicy({ ...retryPolicy, maxAttempts: Number(event.target.value) })} disabled={!retryPolicy.enabled} /></FormField>
                <FormField label="Initial backoff (seconds)" htmlFor="retry-initial"><Input id="retry-initial" type="number" min={1} max={3600} value={retryPolicy.initialDelaySeconds} onChange={(event) => setRetryPolicy({ ...retryPolicy, initialDelaySeconds: Number(event.target.value) })} disabled={!retryPolicy.enabled} /></FormField>
                <FormField label="Maximum backoff (seconds)" htmlFor="retry-max"><Input id="retry-max" type="number" min={1} max={86400} value={retryPolicy.maxDelaySeconds} onChange={(event) => setRetryPolicy({ ...retryPolicy, maxDelaySeconds: Number(event.target.value) })} disabled={!retryPolicy.enabled} /></FormField>
                <FormField label="Jitter (%)" htmlFor="retry-jitter"><Input id="retry-jitter" type="number" min={0} max={50} value={retryPolicy.jitterPercent} onChange={(event) => setRetryPolicy({ ...retryPolicy, jitterPercent: Number(event.target.value) })} disabled={!retryPolicy.enabled} /></FormField>
              </div>
            </div>
          </Disclosure>
          <div className="rounded-lg bg-muted/50 p-3 text-sm leading-5 text-muted-foreground"><span className="font-medium text-foreground">Safe by default.</span> Review a plan before syncing. Workspace or application approval rules determine who must approve syncs and deletions; cluster-scoped changes always require explicit approval.</div>
        </div></Panel> },
        { title: "Review", content: <Panel title="Review" description="Creating the application will not make cluster changes. JustCD stores the configuration and waits for you to calculate a diff."><div className="space-y-4 p-5">
          <SummaryList items={[
            { label: "Workspace", value: selectedWorkspace?.name ?? "—" },
            { label: "Git source", value: sourceName || "—" },
            { label: "Application name", value: name || "—" },
            { label: "Git revision", value: revision || "—" },
            { label: "Renderer", value: rendererLabel },
            { label: "Path", value: <span className="font-mono">{manifestPath || "—"}</span> },
            { label: "Cluster", value: clusterName || "—" },
            { label: "Namespaces", value: namespaces.length ? namespaces.join(", ") : "—" },
            { label: "Create missing namespaces", value: createNamespaces ? "Yes" : "No" },
            { label: "Sync policy", value: syncPolicy === "auto-safe" ? "Auto-safe" : "Manual" },
            { label: "Poll interval (seconds)", value: pollSeconds },
            { label: "Retry policy", value: retryPolicy.enabled ? `Up to ${retryPolicy.maxAttempts} attempts` : "Disabled" },
          ]} />
          {selectedWorkspace?.role === "viewer" && <p className="text-sm text-destructive">Your role cannot create applications.</p>}
        </div></Panel> },
      ]} />
      <WizardFooter step={wizard.step} isLast={wizard.isLast} onBack={wizard.back} onNext={() => { if (wizard.next(missingSelection)) setError(null) }} submit={<Button type="submit" loading={busy} loadingText="Creating application…" disabled={loading || !workspaceId || !sourceId || !clusterId || !namespaces.length || selectedWorkspace?.role === "viewer"}>Create application</Button>} />
    </form>
  </>
}
