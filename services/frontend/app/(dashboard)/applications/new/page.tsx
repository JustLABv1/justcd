"use client"

import { useEffect, useMemo, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormField, PageHeading, Panel } from "@/components/ui-kit"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { Application, Cluster, GitSource, ListResponse, NamespaceBinding, Project } from "@/lib/types"

export default function NewApplicationPage() {
  const router = useRouter()
  const [projects, setProjects] = useState<Project[]>([])
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [projectId, setProjectId] = useState("")
  const [clusterId, setClusterId] = useState("")
  const [sources, setSources] = useState<GitSource[]>([])
  const [bindings, setBindings] = useState<NamespaceBinding[]>([])
  const [sourceId, setSourceId] = useState("")
  const [namespaces, setNamespaces] = useState<string[]>([])
  const [name, setName] = useState("")
  const [revision, setRevision] = useState("main")
  const [manifestPath, setManifestPath] = useState("deploy/")
  const [renderer, setRenderer] = useState("yaml")
  const [syncPolicy, setSyncPolicy] = useState("manual")
  const [pollSeconds, setPollSeconds] = useState("300")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const queryProject = new URLSearchParams(window.location.search).get("projectId") ?? ""
    Promise.all([
      api<ListResponse<Project>>("/api/v1/projects"),
      api<ListResponse<Cluster>>("/api/v1/clusters"),
    ]).then(([projectResult, clusterResult]) => {
      const writable = projectResult.items.filter((project) => project.role !== "viewer")
      setProjects(writable)
      setProjectId(writable.find((project) => project.id === queryProject)?.id ?? writable[0]?.id ?? "")
      setClusters(clusterResult.items)
      setClusterId(clusterResult.items[0]?.id ?? "")
    }).catch((cause) => setError(errorMessage(cause))).finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (!projectId) return
    api<ListResponse<GitSource>>(`/api/v1/git-sources?projectId=${encodeURIComponent(projectId)}`).then((result) => {
      setSources(result.items)
      setSourceId(result.items[0]?.id ?? "")
    }).catch((cause) => setError(errorMessage(cause)))
  }, [projectId])

  useEffect(() => {
    if (!projectId || !clusterId) return
    api<ListResponse<NamespaceBinding>>(`/api/v1/clusters/${encodeURIComponent(clusterId)}/bindings?projectId=${encodeURIComponent(projectId)}`).then((result) => {
      setBindings(result.items)
      setNamespaces((current) => current.filter((namespace) => result.items.some((item) => item.namespace === namespace)))
    }).catch((cause) => setError(errorMessage(cause)))
  }, [projectId, clusterId])

  const selectedProject = useMemo(() => projects.find((project) => project.id === projectId), [projects, projectId])

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError("")
    try {
      const app = await apiPost<Application>("/api/v1/applications", { projectId, name, sourceId, revision, manifestPath, renderer, clusterId, namespaces, syncPolicy, pollSeconds: Number(pollSeconds) })
      router.push(`/applications/${app.id}`)
    } catch (cause) { setError(errorMessage(cause)) } finally { setBusy(false) }
  }

  return <>
    <PageHeading eyebrow="Application setup" title="Create an application" description="Point JustCD at a Git revision and a repository path. We’ll render, diff, and review the changes before they reach Kubernetes." />
    {error && <div role="alert" className="mb-5 rounded-lg border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-destructive">{error}</div>}
    <form className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]" onSubmit={submit}>
      <div className="space-y-5">
        <Panel title="Source" description="Select the project repository and the exact Git path to render."><div className="grid gap-4 p-5 sm:grid-cols-2">
          <FormField label="Project" htmlFor="project"><select id="project" className="h-9 w-full rounded-lg border bg-background px-3 text-sm" value={projectId} onChange={(event) => setProjectId(event.target.value)} required><option value="">Select project</option>{projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></FormField>
          <FormField label="Git source" htmlFor="source"><select id="source" className="h-9 w-full rounded-lg border bg-background px-3 text-sm" value={sourceId} onChange={(event) => setSourceId(event.target.value)} required><option value="">Select source</option>{sources.map((source) => <option key={source.id} value={source.id}>{source.name} · {source.repositoryUrl}</option>)}</select></FormField>
          <FormField label="Application name" htmlFor="name" hint="Lowercase letters, numbers, dots, and dashes."><Input id="name" placeholder="billing-api" value={name} onChange={(event) => setName(event.target.value)} required pattern="[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?" /></FormField>
          <FormField label="Git revision" htmlFor="revision" hint="Branch, tag, or commit; JustCD pins the reviewed commit SHA."><Input id="revision" placeholder="main" value={revision} onChange={(event) => setRevision(event.target.value)} required /></FormField>
          <FormField label="Manifest path" htmlFor="path" hint="Repository-relative directory or file."><Input id="path" placeholder="deploy/" value={manifestPath} onChange={(event) => setManifestPath(event.target.value)} required /></FormField>
          <FormField label="Renderer" htmlFor="renderer"><select id="renderer" className="h-9 w-full rounded-lg border bg-background px-3 text-sm" value={renderer} onChange={(event) => setRenderer(event.target.value)}><option value="yaml">Plain YAML / JSON</option><option value="kustomize">Kustomize build</option><option value="helm">Helm template</option></select></FormField>
        </div></Panel>
        <Panel title="Cluster target" description="Applications are restricted to the namespaces already bound to this project."><div className="space-y-5 p-5">
          <FormField label="Cluster" htmlFor="cluster"><select id="cluster" className="h-9 w-full rounded-lg border bg-background px-3 text-sm" value={clusterId} onChange={(event) => { setClusterId(event.target.value); setNamespaces([]) }} required><option value="">Select cluster</option>{clusters.map((cluster) => <option key={cluster.id} value={cluster.id}>{cluster.name} · {cluster.apiServer}</option>)}</select></FormField>
          <div><p className="mb-2 text-xs font-medium">Namespace bindings</p>{bindings.length ? <div className="grid gap-2 sm:grid-cols-2">{bindings.map((binding) => <label key={binding.namespace} className={`flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2.5 text-xs transition-colors ${namespaces.includes(binding.namespace) ? "border-primary/40 bg-primary/5" : "hover:bg-muted/40"}`}><input type="checkbox" className="accent-primary" checked={namespaces.includes(binding.namespace)} onChange={(event) => setNamespaces((current) => event.target.checked ? [...current, binding.namespace] : current.filter((namespace) => namespace !== binding.namespace))} /><span className="flex-1 font-medium">{binding.namespace}</span><span className="text-[10px] text-muted-foreground">{binding.credentialId ? "namespace credential" : "cluster default"}</span></label>)}</div> : <div className="rounded-lg border border-dashed px-4 py-5 text-xs text-muted-foreground">No namespaces are bound to this project on this cluster. <a href={`/settings?projectId=${projectId}`} className="font-medium text-primary hover:underline">Configure a binding</a></div>}</div>
        </div></Panel>
      </div>
      <div className="space-y-5">
        <Panel title="Reconciliation" description="Choose how often to check Git for changes."><div className="space-y-4 p-5">
          <FormField label="Sync policy" htmlFor="policy"><select id="policy" className="h-9 w-full rounded-lg border bg-background px-3 text-sm" value={syncPolicy} onChange={(event) => setSyncPolicy(event.target.value)}><option value="manual">Manual · review each sync</option><option value="auto-safe">Auto-safe · apply non-destructive changes</option></select></FormField>
          <FormField label="Poll interval (seconds)" htmlFor="poll" hint="Auto-safe still pauses for every deletion and cluster-wide change."><Input id="poll" type="number" min={30} max={86400} value={pollSeconds} onChange={(event) => setPollSeconds(event.target.value)} /></FormField>
          <div className="rounded-lg bg-muted/50 p-3 text-[11px] leading-5 text-muted-foreground"><span className="font-medium text-foreground">Safe by default.</span> A plan is required before any sync. Deletions and cluster-scoped changes require owner approval.</div>
        </div></Panel>
        <Panel title="Ready to review?" description="Creating the application will not make cluster changes."><div className="p-5"><p className="text-xs leading-5 text-muted-foreground">JustCD stores the configuration and waits for you to calculate a diff. You can review every rendered resource before applying the plan.</p>{selectedProject?.role === "viewer" && <p className="mt-3 text-xs text-destructive">Your role cannot create applications.</p>}<Button className="mt-4 w-full" type="submit" disabled={busy || loading || !projectId || !sourceId || !clusterId || !namespaces.length || selectedProject?.role === "viewer"}>{busy ? "Creating…" : "Create application"}</Button></div></Panel>
      </div>
    </form>
  </>
}
