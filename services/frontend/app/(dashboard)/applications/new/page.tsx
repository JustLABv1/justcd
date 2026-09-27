"use client"

import { useEffect, useMemo, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { FormField, PageHeading, Panel } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { api, apiPost, errorMessage } from "@/lib/api"
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
  const [syncPolicy, setSyncPolicy] = useState("manual")
  const [pollSeconds, setPollSeconds] = useState("300")
  const [retryPolicy, setRetryPolicy] = useState({ enabled: true, maxAttempts: 5, initialDelaySeconds: 5, maxDelaySeconds: 300, jitterPercent: 20 })
  const [error, setError] = useState<unknown | null>(null)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const queryWorkspace = new URLSearchParams(window.location.search).get("workspaceId") ?? ""
    Promise.all([
      api<ListResponse<Workspace>>("/api/v1/workspaces"),
      api<ListResponse<Cluster>>("/api/v1/clusters"),
    ]).then(([workspaceResult, clusterResult]) => {
      const writable = workspaceResult.items.filter((workspace) => workspace.role !== "viewer")
      setWorkspaces(writable)
      setWorkspaceId(writable.find((workspace) => workspace.id === queryWorkspace)?.id ?? writable[0]?.id ?? "")
      setClusters(clusterResult.items)
      setClusterId(clusterResult.items[0]?.id ?? "")
    }).catch((cause) => setError(cause)).finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (!workspaceId) return
    api<ListResponse<GitSource>>(`/api/v1/git-sources?workspaceId=${encodeURIComponent(workspaceId)}`).then((result) => {
      setSources(result.items)
      setSourceId(result.items[0]?.id ?? "")
    }).catch((cause) => setError(cause))
  }, [workspaceId])

  useEffect(() => {
    if (!workspaceId || !clusterId) return
    api<ListResponse<NamespaceBinding>>(`/api/v1/clusters/${encodeURIComponent(clusterId)}/bindings?workspaceId=${encodeURIComponent(workspaceId)}`).then((result) => {
      setBindings(result.items)
      setNamespaces((current) => current.filter((namespace) => result.items.some((item) => item.namespace === namespace)))
      setNamespaceHelmValues((current) => Object.fromEntries(Object.entries(current).filter(([namespace]) => result.items.some((item) => item.namespace === namespace))))
      setNamespaceManifestPaths((current) => Object.fromEntries(Object.entries(current).filter(([namespace]) => result.items.some((item) => item.namespace === namespace))))
    }).catch((cause) => setError(cause))
  }, [workspaceId, clusterId])

  const selectedWorkspace = useMemo(() => workspaces.find((workspace) => workspace.id === workspaceId), [workspaces, workspaceId])

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError(null)
    if (!workspaceId || !sourceId || !clusterId || !namespaces.length) { toast.error("Choose a workspace, Git source, cluster, and at least one namespace."); setBusy(false); return }
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
        clusterId, namespaces, syncPolicy, pollSeconds: Number(pollSeconds), retryPolicy,
      })
      toast.success("Application created.")
      router.push(`/applications/${app.id}`)
    } catch (cause) { toast.error(errorMessage(cause), cause) } finally { setBusy(false) }
  }

  return <>
    <PageHeading title="Create an application" description="Point JustCD at a Git revision and a repository path. We’ll render, diff, and review the changes before they reach Kubernetes." />
    {error && <ErrorNotice error={error} />}
    <form className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]" onSubmit={submit}>
      <div className="space-y-5">
        <Panel title="Source" description="Select the workspace repository and the exact Git path to render."><div className="grid gap-4 p-5 sm:grid-cols-2">
          <FormField label="Workspace" htmlFor="workspace"><FormSelect id="workspace" value={workspaceId} onValueChange={(value) => { setWorkspaceId(value); setNamespaces([]); setNamespaceHelmValues({}); setNamespaceManifestPaths({}); setTargetManifestPath("") }} placeholder="Select workspace" required items={workspaces.map((workspace) => ({ value: workspace.id, label: workspace.name }))} /></FormField>
          <FormField label="Git source" htmlFor="source"><FormSelect id="source" value={sourceId} onValueChange={setSourceId} placeholder="Select source" required items={sources.map((source) => ({ value: source.id, label: `${source.name} · ${source.repositoryUrl}` }))} /></FormField>
          <FormField label="Application name" htmlFor="name" hint="Lowercase letters, numbers, dots, and dashes."><Input id="name" placeholder="billing-api" value={name} onChange={(event) => setName(event.target.value)} required pattern="[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?" /></FormField>
          <FormField label="Git revision" htmlFor="revision" hint="Branch, tag, or commit; JustCD pins the reviewed commit SHA."><Input id="revision" placeholder="main" value={revision} onChange={(event) => setRevision(event.target.value)} required /></FormField>
          <FormField label={renderer === "helm" ? "Chart path" : renderer === "kustomize" ? "Shared Kustomize path" : "Manifest path"} htmlFor="path" hint={renderer === "helm" ? "Repository-relative Helm chart directory." : renderer === "kustomize" ? "Fallback Kustomize entry point. Cluster and namespace paths can select complete overlays." : "Repository-relative directory or file."}><Input id="path" placeholder="deploy/" value={manifestPath} onChange={(event) => setManifestPath(event.target.value)} required /></FormField>
          <FormField label="Renderer" htmlFor="renderer"><FormSelect id="renderer" value={renderer} onValueChange={setRenderer} items={[{ value: "yaml", label: "Plain YAML / JSON" }, { value: "kustomize", label: "Kustomize build" }, { value: "helm", label: "Helm template" }]} /></FormField>
          {renderer === "kustomize" && <div className="space-y-3 sm:col-span-2">
            <label className="flex items-start gap-3 rounded-lg border p-3 text-xs"><Checkbox checked={kustomizeNamespaceOverride} onCheckedChange={(checked) => setKustomizeNamespaceOverride(Boolean(checked))} /><span><span className="block font-medium">Apply namespace transform</span><span className="mt-1 block text-muted-foreground">Build once per selected namespace and set namespace metadata to that target.</span></span></label>
            <label className="flex items-start gap-3 rounded-lg border p-3 text-xs"><Checkbox checked={kustomizeHelmEnabled} onCheckedChange={(checked) => setKustomizeHelmEnabled(Boolean(checked))} /><span><span className="block font-medium">Enable Helm charts in Kustomize</span><span className="mt-1 block text-muted-foreground">Allows helmCharts from the Git revision. Helm may download pinned charts from public HTTPS repositories during rendering.</span></span></label>
          </div>}
          {renderer === "helm" && <div className="space-y-4 sm:col-span-2"><FormField label="Helm values files" htmlFor="helm-values-files" hint="One repository-relative YAML path per line. Later files override earlier files."><Textarea id="helm-values-files" value={helmValuesFiles} onChange={(event) => setHelmValuesFiles(event.target.value)} placeholder={"values/common.yaml\nvalues/production.yaml"} className="font-mono text-xs" /></FormField><FormField label="Helm values overrides" htmlFor="helm-values-yaml" hint="Optional JustCD values applied after the Git files."><Textarea id="helm-values-yaml" value={helmValuesYaml} onChange={(event) => setHelmValuesYaml(event.target.value)} placeholder={"agent:\n  logLevel: info"} className="min-h-32 font-mono text-xs" spellCheck={false} /></FormField></div>}
        </div></Panel>
        <Panel title="Cluster target" description="Applications are restricted to the namespaces already bound to this workspace."><div className="space-y-5 p-5">
          <FormField label="Cluster" htmlFor="cluster"><FormSelect id="cluster" value={clusterId} onValueChange={(value) => { setClusterId(value); setNamespaces([]); setNamespaceHelmValues({}); setNamespaceManifestPaths({}); setTargetManifestPath("") }} placeholder="Select cluster" required items={clusters.map((cluster) => ({ value: cluster.id, label: `${cluster.name} · ${cluster.apiServer}` }))} /></FormField>
          <div><p className="mb-2 text-xs font-medium">Namespace bindings</p>{bindings.length ? <div className="grid gap-2 sm:grid-cols-2">{bindings.map((binding) => <label key={binding.namespace} className={`flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2.5 text-xs transition-colors ${namespaces.includes(binding.namespace) ? "border-primary/40 bg-primary/5" : "hover:bg-muted/40"}`}><Checkbox checked={namespaces.includes(binding.namespace)} onCheckedChange={(checked) => {
            if (checked) {
              setNamespaces((current) => [...current, binding.namespace])
              setNamespaceHelmValues((current) => ({ ...current, [binding.namespace]: current[binding.namespace] ?? { files: [], yaml: "" } }))
              setNamespaceManifestPaths((current) => ({ ...current, [binding.namespace]: current[binding.namespace] ?? "" }))
            } else {
              setNamespaces((current) => current.filter((namespace) => namespace !== binding.namespace))
              setNamespaceHelmValues((current) => { const next = { ...current }; delete next[binding.namespace]; return next })
              setNamespaceManifestPaths((current) => { const next = { ...current }; delete next[binding.namespace]; return next })
            }
          }} /><span className="flex-1 font-medium">{binding.namespace}</span><span className="text-[10px] text-muted-foreground">{binding.credentialId ? "namespace credential" : "cluster default"}</span></label>)}</div> : <div className="rounded-lg border border-dashed px-4 py-5 text-xs text-muted-foreground">No namespaces are bound to this workspace on this cluster. <a href={`/workspaces/${workspaceId}/connections/namespaces`} className="font-medium text-primary hover:underline">Configure a binding</a></div>}</div>
          {renderer === "kustomize" && <FormField label="Cluster overlay path" htmlFor="cluster-overlay-path" hint="Optional Kustomize path for this cluster; blank uses the shared path. This overlay should include its shared base."><Input id="cluster-overlay-path" value={targetManifestPath} onChange={(event) => setTargetManifestPath(event.target.value)} placeholder="overlays/clusters/prod-eu" /></FormField>}
          {renderer === "helm" && namespaces.length > 0 && <div className="space-y-3 border-t pt-4"><p className="text-xs font-medium">Namespace Helm overrides</p>{namespaces.map((namespace) => {
            const override = namespaceHelmValues[namespace] ?? { files: [], yaml: "" }
            return <details key={namespace} className="rounded-lg border px-3 py-2.5"><summary className="cursor-pointer text-xs font-medium">{namespace} <span className="font-normal text-muted-foreground">· optional overrides</span></summary><div className="mt-3 space-y-3"><FormField label="Namespace values files" htmlFor={`new-ns-files-${namespace}`} hint="Repository-relative paths, applied after shared values."><Textarea id={`new-ns-files-${namespace}`} value={override.files.join("\n")} onChange={(event) => setNamespaceHelmValues((current) => ({ ...current, [namespace]: { ...override, files: event.target.value.split("\n").map((line) => line.trim()).filter(Boolean) } }))} className="min-h-14 font-mono text-xs" /></FormField><FormField label="Namespace JustCD overrides" htmlFor={`new-ns-yaml-${namespace}`} hint="Applied after namespace Git files. Use Kubernetes Secret references for sensitive values."><Textarea id={`new-ns-yaml-${namespace}`} value={override.yaml} onChange={(event) => setNamespaceHelmValues((current) => ({ ...current, [namespace]: { ...override, yaml: event.target.value } }))} className="min-h-20 font-mono text-xs" spellCheck={false} /></FormField></div></details>
          })}</div>}
          {renderer === "kustomize" && namespaces.length > 0 && <div className="space-y-3 border-t pt-4"><p className="text-xs font-medium">Namespace overlays</p>{namespaces.map((namespace) => <FormField key={namespace} label={`${namespace} overlay path`} htmlFor={`new-ns-overlay-${namespace}`} hint="Optional Kustomize path; blank uses the cluster path. Include the base and any cluster overlay this namespace needs."><Input id={`new-ns-overlay-${namespace}`} value={namespaceManifestPaths[namespace] ?? ""} onChange={(event) => setNamespaceManifestPaths((current) => ({ ...current, [namespace]: event.target.value }))} placeholder={`overlays/namespaces/${namespace}`} /></FormField>)}</div>}
        </div></Panel>
      </div>
      <div className="space-y-5">
        <Panel title="Reconciliation" description="Choose how often to check Git for changes."><div className="space-y-4 p-5">
          <FormField label="Sync policy" htmlFor="policy"><FormSelect id="policy" value={syncPolicy} onValueChange={setSyncPolicy} items={[{ value: "manual", label: "Manual · review each sync" }, { value: "auto-safe", label: "Auto-safe · apply non-destructive changes" }]} /></FormField>
          <FormField label="Poll interval (seconds)" htmlFor="poll" hint="Auto-safe still pauses for every deletion and cluster-wide change."><Input id="poll" type="number" min={30} max={86400} value={pollSeconds} onChange={(event) => setPollSeconds(event.target.value)} /></FormField>
          <div className="space-y-3 border-t pt-4">
            <label className="flex items-center gap-2 text-xs font-medium"><Checkbox checked={retryPolicy.enabled} onCheckedChange={(checked) => setRetryPolicy({ ...retryPolicy, enabled: Boolean(checked) })} />Retry transient reconciliation failures automatically</label>
            <div className="grid gap-3 sm:grid-cols-2">
              <FormField label="Maximum attempts" htmlFor="retry-attempts"><Input id="retry-attempts" type="number" min={1} max={20} value={retryPolicy.maxAttempts} onChange={(event) => setRetryPolicy({ ...retryPolicy, maxAttempts: Number(event.target.value) })} disabled={!retryPolicy.enabled} /></FormField>
              <FormField label="Initial backoff (seconds)" htmlFor="retry-initial"><Input id="retry-initial" type="number" min={1} max={3600} value={retryPolicy.initialDelaySeconds} onChange={(event) => setRetryPolicy({ ...retryPolicy, initialDelaySeconds: Number(event.target.value) })} disabled={!retryPolicy.enabled} /></FormField>
              <FormField label="Maximum backoff (seconds)" htmlFor="retry-max"><Input id="retry-max" type="number" min={1} max={86400} value={retryPolicy.maxDelaySeconds} onChange={(event) => setRetryPolicy({ ...retryPolicy, maxDelaySeconds: Number(event.target.value) })} disabled={!retryPolicy.enabled} /></FormField>
              <FormField label="Jitter (%)" htmlFor="retry-jitter"><Input id="retry-jitter" type="number" min={0} max={50} value={retryPolicy.jitterPercent} onChange={(event) => setRetryPolicy({ ...retryPolicy, jitterPercent: Number(event.target.value) })} disabled={!retryPolicy.enabled} /></FormField>
            </div>
            <p className="text-[10px] leading-4 text-muted-foreground">Only transient failures are retried. Each attempt recalculates the plan from live cluster state; authorization, validation, approval, and interrupted operations require review.</p>
          </div>
          <div className="rounded-lg bg-muted/50 p-3 text-[11px] leading-5 text-muted-foreground"><span className="font-medium text-foreground">Safe by default.</span> Review a plan before syncing. Workspace or application approval rules determine who must approve syncs and deletions; cluster-scoped changes always require explicit approval.</div>
        </div></Panel>
        <Panel title="Ready to review?" description="Creating the application will not make cluster changes."><div className="p-5"><p className="text-xs leading-5 text-muted-foreground">JustCD stores the configuration and waits for you to calculate a diff. You can review every rendered resource before applying the plan.</p>{selectedWorkspace?.role === "viewer" && <p className="mt-3 text-xs text-destructive">Your role cannot create applications.</p>}<Button className="mt-4 w-full" type="submit" loading={busy} loadingText="Creating application…" disabled={loading || !workspaceId || !sourceId || !clusterId || !namespaces.length || selectedWorkspace?.role === "viewer"}>Create application</Button></div></Panel>
      </div>
    </form>
  </>
}
