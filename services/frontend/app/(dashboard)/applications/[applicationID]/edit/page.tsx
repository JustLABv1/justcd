"use client"

import { useEffect, useState, type FormEvent } from "react"
import { useParams, useRouter } from "next/navigation"
import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { FormField, PageHeading, Panel } from "@/components/ui-kit"
import { api, errorMessage } from "@/lib/api"
import type { Application, Cluster, GitSource, ListResponse, NamespaceBinding, Project } from "@/lib/types"

export default function EditApplicationPage() {
  const { applicationID } = useParams<{ applicationID: string }>()
  const router = useRouter()
  const [app, setApp] = useState<Application | null>(null)
  const [project, setProject] = useState<Project | null>(null)
  const [sources, setSources] = useState<GitSource[]>([])
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [bindings, setBindings] = useState<NamespaceBinding[]>([])
  const [namespaces, setNamespaces] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    let active = true
    Promise.all([api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`), api<ListResponse<Project>>( "/api/v1/projects"), api<ListResponse<Cluster>>( "/api/v1/clusters")])
      .then(([application, projects, clusterList]) => {
        if (!active) return
        setApp(application); setNamespaces(application.namespaces.map((item) => item.namespace))
        setProject(projects.items.find((item) => item.id === application.projectId) ?? null)
        setClusters(clusterList.items)
      }).catch((cause) => active && setError(errorMessage(cause)))
    return () => { active = false }
  }, [applicationID])

  const projectId = app?.projectId
  const clusterId = app?.clusterId
  useEffect(() => {
    if (!projectId || !clusterId) return
    let active = true
    Promise.all([
      api<ListResponse<GitSource>>(`/api/v1/git-sources?projectId=${encodeURIComponent(projectId)}`),
      api<ListResponse<NamespaceBinding>>(`/api/v1/clusters/${encodeURIComponent(clusterId)}/bindings?projectId=${encodeURIComponent(projectId)}`),
    ]).then(([sourceList, bindingList]) => { if (active) { setSources(sourceList.items); setBindings(bindingList.items) } }).catch((cause) => active && setError(errorMessage(cause)))
    return () => { active = false }
  }, [projectId, clusterId])

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!app) return
    setBusy(true); setError("")
    try {
      await api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`, { method: "PUT", body: JSON.stringify({
        name: app.name, sourceId: app.sourceId, revision: app.revision, manifestPath: app.manifestPath,
        renderer: app.renderer, kustomizeHelmEnabled: app.renderer === "kustomize" && app.kustomizeHelmEnabled,
        kustomizeNamespaceOverride: app.renderer === "kustomize" && app.kustomizeNamespaceOverride,
        clusterId: app.clusterId, namespaces, syncPolicy: app.syncPolicy, pollSeconds: app.pollSeconds,
      }) })
      router.push(`/applications/${applicationID}?tab=activity`); router.refresh()
    } catch (cause) { setError(errorMessage(cause)) }
    finally { setBusy(false) }
  }

  return <>
    <PageHeading title={app ? `Edit ${app.name}` : "Edit application"} description="Changes invalidate existing plans. Review a fresh plan before the next sync." />
    {error && <p role="alert" className="mb-5 rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive">{error}</p>}
    {!app ? <p className="text-sm text-muted-foreground">Loading application…</p> : project?.role !== "owner" ? <p className="text-sm text-muted-foreground">Only project owners can edit applications.</p> : <form className="max-w-4xl space-y-5" onSubmit={save}>
      <Panel title="Source" description="The Git revision and repository path used for future plans."><div className="grid gap-4 p-5 sm:grid-cols-2">
        <FormField label="Application name" htmlFor="edit-app-name"><Input id="edit-app-name" value={app.name} onChange={(event) => setApp({ ...app, name: event.target.value })} required pattern="[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?" /></FormField>
        <FormField label="Git source" htmlFor="edit-app-source"><FormSelect id="edit-app-source" value={app.sourceId} onValueChange={(value) => setApp({ ...app, sourceId: value })} items={sources.map((source) => ({ value: source.id, label: source.name }))} /></FormField>
        <FormField label="Git revision" htmlFor="edit-app-revision"><Input id="edit-app-revision" value={app.revision} onChange={(event) => setApp({ ...app, revision: event.target.value })} required /></FormField>
        <FormField label="Manifest path" htmlFor="edit-app-path"><Input id="edit-app-path" value={app.manifestPath} onChange={(event) => setApp({ ...app, manifestPath: event.target.value })} required /></FormField>
        <FormField label="Renderer" htmlFor="edit-app-renderer"><FormSelect id="edit-app-renderer" value={app.renderer} onValueChange={(value) => setApp({ ...app, renderer: value as Application["renderer"] })} items={[{ value: "yaml", label: "Plain YAML / JSON" }, { value: "kustomize", label: "Kustomize build" }, { value: "helm", label: "Helm template" }]} /></FormField>
        {app.renderer === "kustomize" && <div className="flex flex-col gap-3 self-end text-xs"><label className="flex items-center gap-2"><Checkbox checked={app.kustomizeHelmEnabled} onCheckedChange={(checked) => setApp({ ...app, kustomizeHelmEnabled: Boolean(checked) })} />Enable Helm charts</label><label className="flex items-center gap-2"><Checkbox checked={app.kustomizeNamespaceOverride} onCheckedChange={(checked) => setApp({ ...app, kustomizeNamespaceOverride: Boolean(checked) })} />Override Git namespace with target namespace</label></div>}
      </div></Panel>
      <Panel title="Cluster target" description="Cluster and namespace changes are blocked while JustCD manages resources for this application."><div className="space-y-4 p-5">
        <FormField label="Cluster" htmlFor="edit-app-cluster"><FormSelect id="edit-app-cluster" value={app.clusterId} onValueChange={(value) => { setApp({ ...app, clusterId: value }); setNamespaces([]) }} items={clusters.map((cluster) => ({ value: cluster.id, label: cluster.name }))} /></FormField>
        <div><p className="mb-2 text-xs font-medium">Namespaces</p><div className="grid gap-2 sm:grid-cols-2">{bindings.map((binding) => <label key={binding.namespace} className="flex items-center gap-2 rounded-lg border p-3 text-xs"><Checkbox checked={namespaces.includes(binding.namespace)} onCheckedChange={(checked) => setNamespaces((current) => checked ? [...current, binding.namespace] : current.filter((item) => item !== binding.namespace))} />{binding.namespace}</label>)}</div></div>
      </div></Panel>
      <Panel title="Reconciliation"><div className="grid gap-4 p-5 sm:grid-cols-2"><FormField label="Sync policy" htmlFor="edit-app-policy"><FormSelect id="edit-app-policy" value={app.syncPolicy} onValueChange={(value) => setApp({ ...app, syncPolicy: value as Application["syncPolicy"] })} items={[{ value: "manual", label: "Manual" }, { value: "auto-safe", label: "Auto-safe" }]} /></FormField><FormField label="Poll interval (seconds)" htmlFor="edit-app-poll"><Input id="edit-app-poll" type="number" min={30} max={86400} value={app.pollSeconds} onChange={(event) => setApp({ ...app, pollSeconds: Number(event.target.value) })} /></FormField></div></Panel>
      <div className="flex justify-end gap-2"><Link href={`/applications/${applicationID}`}><Button type="button" variant="outline">Cancel</Button></Link><Button type="submit" disabled={busy || namespaces.length === 0}>{busy ? "Saving…" : "Save application"}</Button></div>
    </form>}
  </>
}
