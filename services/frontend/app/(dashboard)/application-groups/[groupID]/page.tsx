"use client"

import { useEffect, useState, type FormEvent } from "react"
import { useParams } from "next/navigation"
import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { FormField, PageHeading, Panel, StatusBadge } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { api, errorMessage } from "@/lib/api"
import type { Application, ApplicationGroup, ApplicationGroupResponse, Cluster, GitSource, ListResponse, Project } from "@/lib/types"

export default function ApplicationGroupPage() {
  const { groupID } = useParams<{ groupID: string }>()
  const toast = useToast()
  const [group, setGroup] = useState<ApplicationGroup | null>(null)
  const [applications, setApplications] = useState<Application[]>([])
  const [project, setProject] = useState<Project | null>(null)
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [sources, setSources] = useState<GitSource[]>([])
  const [revision, setRevision] = useState("")
  const [manifestPath, setManifestPath] = useState("")
  const [valuesFiles, setValuesFiles] = useState("")
  const [valuesYaml, setValuesYaml] = useState("")
  const [kustomizeHelmEnabled, setKustomizeHelmEnabled] = useState(false)
  const [kustomizeNamespaceOverride, setKustomizeNamespaceOverride] = useState(false)
  const [syncPolicy, setSyncPolicy] = useState<ApplicationGroup["syncPolicy"]>("manual")
  const [pollSeconds, setPollSeconds] = useState("300")
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown | null>(null)

  useEffect(() => {
    let active = true
    Promise.all([
      api<ApplicationGroupResponse>("/api/v1/application-groups/" + encodeURIComponent(groupID)),
      api<ListResponse<Project>>("/api/v1/projects"),
      api<ListResponse<Cluster>>("/api/v1/clusters"),
    ]).then(([result, projects, clusterList]) => {
      if (!active) return
      setGroup(result.group)
      setApplications(result.applications)
      setProject(projects.items.find((item) => item.id === result.group.projectId) ?? null)
      setClusters(clusterList.items)
      setRevision(result.group.revision)
      setManifestPath(result.group.manifestPath)
      setValuesFiles(result.group.helmValuesFiles.join("\n"))
      setValuesYaml(result.group.helmValuesYaml)
      setKustomizeHelmEnabled(result.group.kustomizeHelmEnabled)
      setKustomizeNamespaceOverride(result.group.kustomizeNamespaceOverride)
      setSyncPolicy(result.group.syncPolicy)
      setPollSeconds(String(result.group.pollSeconds))
      return api<ListResponse<GitSource>>("/api/v1/git-sources?projectId=" + encodeURIComponent(result.group.projectId))
    }).then((sourceList) => { if (active && sourceList) setSources(sourceList.items) })
      .catch((cause) => active && setError(cause))
      .finally(() => active && setLoading(false))
    return () => { active = false }
  }, [groupID])

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!group) return
    setBusy(true)
    setError(null)
    try {
      const result = await api<ApplicationGroupResponse>("/api/v1/application-groups/" + encodeURIComponent(group.id), {
        method: "PUT",
        body: JSON.stringify({
          sourceId: group.sourceId,
          revision: revision.trim(),
          manifestPath: manifestPath.trim(),
          kustomizeHelmEnabled: group.renderer === "kustomize" && kustomizeHelmEnabled,
          kustomizeNamespaceOverride: group.renderer === "kustomize" && kustomizeNamespaceOverride,
          helmValuesFiles: group.renderer === "helm" ? valuesFiles.split("\n").map((line) => line.trim()).filter(Boolean) : [],
          helmValuesYaml: group.renderer === "helm" ? valuesYaml : "",
          syncPolicy,
          pollSeconds: Number(pollSeconds),
        }),
      })
      setGroup(result.group)
      setApplications(result.applications)
      setKustomizeHelmEnabled(result.group.kustomizeHelmEnabled)
      setKustomizeNamespaceOverride(result.group.kustomizeNamespaceOverride)
      toast.success("Shared configuration saved. Target plans are stale.")
    } catch (cause) {
      setError(cause)
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusy(false)
    }
  }

  return <>
    <PageHeading title={group?.name ?? "Deployment group"} description="Shared renderer configuration and per-target application status." actions={<Link href="/applications" className="text-xs text-muted-foreground hover:text-foreground">Applications</Link>} />
    {error && <div className="mb-5"><ErrorNotice error={error} /></div>}
    {loading || !group ? <p className="text-sm text-muted-foreground">Loading deployment group…</p> : <>
      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_360px]">
        <Panel title="Targets" description="Every target has independent plans, sync history, and approval rules.">
          <div className="divide-y">
            {applications.map((app) => {
              const cluster = clusters.find((item) => item.id === app.clusterId)
              const namespaces = app.namespaces.map((item) => item.namespace).join(", ") || "No namespace"
              return <div key={app.id} className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
                <div className="min-w-0">
                  <Link href={"/applications/" + app.id} className="text-sm font-medium hover:text-primary">{app.name}</Link>
                  <p className="mt-1 truncate text-xs text-muted-foreground">{cluster?.name ?? app.clusterId} / {namespaces}</p>
                </div>
                <div className="flex items-center gap-2"><StatusBadge status={app.health} /><Link href={"/applications/" + app.id + "/edit"} className="text-xs font-medium text-primary hover:underline">Target settings</Link></div>
              </div>
            })}
          </div>
        </Panel>

        <Panel title="Shared configuration" description="Changes here update all targets and invalidate their existing plans.">
          {project?.role === "owner" ? <form className="space-y-4 p-5" onSubmit={save}>
            <FormField label="Git source" htmlFor="group-source"><FormSelect id="group-source" value={group.sourceId} onValueChange={(value) => setGroup({ ...group, sourceId: value })} items={sources.map((source) => ({ value: source.id, label: source.name }))} /></FormField>
            <FormField label="Git revision" htmlFor="group-revision"><Input id="group-revision" value={revision} onChange={(event) => setRevision(event.target.value)} required /></FormField>
            <p className="text-xs text-muted-foreground">Renderer: <span className="font-medium text-foreground">{group.renderer === "helm" ? "Helm" : "Kustomize"}</span></p>
            <FormField label={group.renderer === "helm" ? "Chart path" : "Shared Kustomize path"} htmlFor="group-path" hint={group.renderer === "helm" ? undefined : "Fallback path for targets. Cluster or namespace paths must include this base when needed."}><Input id="group-path" value={manifestPath} onChange={(event) => setManifestPath(event.target.value)} required /></FormField>
            {group.renderer === "helm" ? <>
              <FormField label="Shared values files" htmlFor="group-files" hint="One Git path per line; later files override earlier files."><Textarea id="group-files" value={valuesFiles} onChange={(event) => setValuesFiles(event.target.value)} className="font-mono text-xs" /></FormField>
              <FormField label="Shared JustCD values" htmlFor="group-yaml" hint="Applied after shared Git files and before target values. Use Kubernetes Secret references for sensitive values."><Textarea id="group-yaml" value={valuesYaml} onChange={(event) => setValuesYaml(event.target.value)} className="min-h-32 font-mono text-xs" spellCheck={false} /></FormField>
            </> : <div className="space-y-3">
              <label className="flex items-start gap-3 rounded-lg border p-3 text-xs"><Checkbox checked={kustomizeNamespaceOverride} onCheckedChange={(checked) => setKustomizeNamespaceOverride(Boolean(checked))} /><span><span className="block font-medium">Apply namespace transform</span><span className="mt-1 block text-muted-foreground">Build once per bound namespace and set namespace metadata to that target.</span></span></label>
              <label className="flex items-start gap-3 rounded-lg border p-3 text-xs"><Checkbox checked={kustomizeHelmEnabled} onCheckedChange={(checked) => setKustomizeHelmEnabled(Boolean(checked))} /><span><span className="block font-medium">Enable Helm charts in Kustomize</span><span className="mt-1 block text-muted-foreground">Allows helmCharts from the selected Git revision during Kustomize builds.</span></span></label>
            </div>}
            <div className="grid gap-3 sm:grid-cols-2">
              <FormField label="Sync policy" htmlFor="group-policy"><FormSelect id="group-policy" value={syncPolicy} onValueChange={(value) => setSyncPolicy(value as ApplicationGroup["syncPolicy"])} items={[{ value: "manual", label: "Manual" }, { value: "auto-safe", label: "Auto-safe" }]} /></FormField>
              <FormField label="Poll seconds" htmlFor="group-poll"><Input id="group-poll" type="number" min={30} max={86400} value={pollSeconds} onChange={(event) => setPollSeconds(event.target.value)} /></FormField>
            </div>
            <Button type="submit" loading={busy} loadingText="Saving shared settings…">Save shared settings</Button>
          </form> : <div className="space-y-4 p-5 text-xs">
            <p><span className="font-medium">Git revision:</span> {group.revision}</p>
            <p><span className="font-medium">Renderer:</span> {group.renderer}</p>
            <p><span className="font-medium">{group.renderer === "helm" ? "Chart path" : "Shared Kustomize path:"}</span> <span className="font-mono">{group.manifestPath}</span></p>
            {group.renderer === "helm" ? <><p className="font-medium">Git values files</p><pre className="overflow-auto rounded-lg bg-muted/50 p-3 font-mono text-[11px]">{group.helmValuesFiles.join("\n") || "None"}</pre><p className="font-medium">Shared JustCD values</p><pre className="overflow-auto rounded-lg bg-muted/50 p-3 font-mono text-[11px]">{group.helmValuesYaml || "None"}</pre></> : <p>Namespace transform: {group.kustomizeNamespaceOverride ? "enabled" : "disabled"}; Helm charts: {group.kustomizeHelmEnabled ? "enabled" : "disabled"}</p>}
          </div>}
        </Panel>
      </div>
    </>}
  </>
}
