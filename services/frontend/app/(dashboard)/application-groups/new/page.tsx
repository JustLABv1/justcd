"use client"

import { useEffect, useMemo, useState, type FormEvent } from "react"
import { useRouter } from "next/navigation"
import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { FormField, PageHeading, Panel } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { ApplicationGroupResponse, Cluster, GitSource, ListResponse, NamespaceBinding, Project } from "@/lib/types"

type TargetDraft = {
  id: string
  name: string
  clusterId: string
  namespaces: string[]
  manifestPath: string
  valuesFiles: string
  valuesYaml: string
  namespaceValues: Record<string, { manifestPath: string; valuesFiles: string; valuesYaml: string }>
}

const firstTarget: TargetDraft = { id: "target-0", name: "", clusterId: "", namespaces: [], manifestPath: "", valuesFiles: "", valuesYaml: "", namespaceValues: {} }
const valuesFileList = (value: string) => value.split("\n").map((line) => line.trim()).filter(Boolean)
const updateTarget = (targets: TargetDraft[], index: number, patch: Partial<TargetDraft>) =>
  targets.map((target, targetIndex) => targetIndex === index ? { ...target, ...patch } : target)

export default function NewApplicationGroupPage() {
  const router = useRouter()
  const toast = useToast()
  const [projects, setProjects] = useState<Project[]>([])
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [sources, setSources] = useState<GitSource[]>([])
  const [bindingsByCluster, setBindingsByCluster] = useState<Record<string, NamespaceBinding[]>>({})
  const [projectId, setProjectId] = useState("")
  const [sourceId, setSourceId] = useState("")
  const [name, setName] = useState("")
  const [renderer, setRenderer] = useState<"helm" | "kustomize">("helm")
  const [kustomizeHelmEnabled, setKustomizeHelmEnabled] = useState(false)
  const [kustomizeNamespaceOverride, setKustomizeNamespaceOverride] = useState(false)
  const [revision, setRevision] = useState("main")
  const [manifestPath, setManifestPath] = useState("charts/monitoring-agent")
  const [helmValuesFiles, setHelmValuesFiles] = useState("")
  const [helmValuesYaml, setHelmValuesYaml] = useState("")
  const [targets, setTargets] = useState<TargetDraft[]>([firstTarget])
  const [syncPolicy, setSyncPolicy] = useState("manual")
  const [pollSeconds, setPollSeconds] = useState("300")
  const [error, setError] = useState<unknown | null>(null)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const queryProject = new URLSearchParams(window.location.search).get("projectId") ?? ""
    Promise.all([api<ListResponse<Project>>("/api/v1/projects"), api<ListResponse<Cluster>>("/api/v1/clusters")])
      .then(([projectResult, clusterResult]) => {
        const writable = projectResult.items.filter((project) => project.role !== "viewer")
        setProjects(writable)
        setProjectId(writable.find((project) => project.id === queryProject)?.id ?? writable[0]?.id ?? "")
        setClusters(clusterResult.items)
        setTargets([{ ...firstTarget, clusterId: clusterResult.items[0]?.id ?? "" }])
      })
      .catch(setError)
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (!projectId) return
    let active = true
    api<ListResponse<GitSource>>("/api/v1/git-sources?projectId=" + encodeURIComponent(projectId))
      .then((result) => { if (active) { setSources(result.items); setSourceId(result.items[0]?.id ?? "") } })
      .catch((cause) => active && setError(cause))
    return () => { active = false }
  }, [projectId])

  useEffect(() => {
    if (!projectId || clusters.length === 0) return
    let active = true
    Promise.all(clusters.map(async (cluster) => {
      const result = await api<ListResponse<NamespaceBinding>>("/api/v1/clusters/" + encodeURIComponent(cluster.id) + "/bindings?projectId=" + encodeURIComponent(projectId))
      return [cluster.id, result.items] as const
    })).then((entries) => {
      if (!active) return
      const bindings = Object.fromEntries(entries)
      setBindingsByCluster(bindings)
      setTargets((current) => current.map((target) => ({
        ...target,
        namespaces: target.namespaces.filter((namespace) => (bindings[target.clusterId] ?? []).some((binding) => binding.namespace === namespace)),
        namespaceValues: Object.fromEntries(Object.entries(target.namespaceValues).filter(([namespace]) => (bindings[target.clusterId] ?? []).some((binding) => binding.namespace === namespace))),
      })))
    }).catch((cause) => active && setError(cause))
    return () => { active = false }
  }, [projectId, clusters])

  const selectedProject = useMemo(() => projects.find((project) => project.id === projectId), [projects, projectId])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    if (!projectId || !sourceId || !name.trim() || targets.some((target) => !target.name.trim() || !target.clusterId || !target.namespaces.length)) {
      setError(new Error("Choose a project and Git source, name the group and every target, and select at least one bound namespace for each cluster."))
      setBusy(false)
      return
    }
    try {
      const result = await apiPost<ApplicationGroupResponse>("/api/v1/application-groups", {
        projectId, name: name.trim(), sourceId, revision: revision.trim(), manifestPath: manifestPath.trim(),
        renderer, kustomizeHelmEnabled: renderer === "kustomize" && kustomizeHelmEnabled,
        kustomizeNamespaceOverride: renderer === "kustomize" && kustomizeNamespaceOverride,
        helmValuesFiles: renderer === "helm" ? valuesFileList(helmValuesFiles) : [],
        helmValuesYaml: renderer === "helm" ? helmValuesYaml : "", syncPolicy, pollSeconds: Number(pollSeconds),
        targets: targets.map((target) => ({
          name: target.name.trim(), clusterId: target.clusterId,
          targetManifestPath: renderer === "kustomize" ? target.manifestPath.trim() : "",
          namespaceManifestPaths: renderer === "kustomize" ? Object.fromEntries(target.namespaces.reduce<[string, string][]>((entries, namespace) => {
            const path = target.namespaceValues[namespace]?.manifestPath.trim() ?? ""
            if (path) entries.push([namespace, path])
            return entries
          }, [])) : {},
          targetHelmValuesFiles: renderer === "helm" ? valuesFileList(target.valuesFiles) : [], targetHelmValuesYaml: renderer === "helm" ? target.valuesYaml : "",
          namespaces: target.namespaces.map((namespace) => ({
            namespace,
            valuesFiles: renderer === "helm" ? valuesFileList(target.namespaceValues[namespace]?.valuesFiles ?? "") : [],
            valuesYaml: renderer === "helm" ? target.namespaceValues[namespace]?.valuesYaml ?? "" : "",
          })),
        })),
      })
      toast.success("Deployment group created with " + result.applications.length + " targets.")
      router.push("/application-groups/" + result.group.id)
    } catch (cause) {
      setError(cause)
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusy(false)
    }
  }

  return <>
    <PageHeading title="Deploy to multiple targets" description="Create a deployment group with one independently planned application per cluster, each covering selected namespaces." actions={<Link href="/applications" className="text-xs text-muted-foreground hover:text-foreground">Back to applications</Link>} />
    {error && <div className="mb-5"><ErrorNotice error={error} /></div>}
    <form className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]" onSubmit={submit}>
      <div className="space-y-5">
        <Panel title="Shared source" description="Targets share a Git source and revision. Choose Helm values layering or Kustomize overlays."><div className="grid gap-4 p-5 sm:grid-cols-2">
          <FormField label="Project" htmlFor="group-project"><FormSelect id="group-project" value={projectId} onValueChange={(value) => { setProjectId(value); setTargets((current) => current.map((target) => ({ ...target, namespaces: [], namespaceValues: {} }))) }} placeholder="Select project" required items={projects.map((project) => ({ value: project.id, label: project.name }))} /></FormField>
          <FormField label="Git source" htmlFor="group-source"><FormSelect id="group-source" value={sourceId} onValueChange={setSourceId} placeholder="Select source" required items={sources.map((source) => ({ value: source.id, label: source.name + " · " + source.repositoryUrl }))} /></FormField>
          <FormField label="Renderer" htmlFor="group-renderer"><FormSelect id="group-renderer" value={renderer} onValueChange={(value) => { const next = value as "helm" | "kustomize"; setRenderer(next); setHelmValuesFiles(""); setHelmValuesYaml(""); if (next === "helm") { setKustomizeHelmEnabled(false); setKustomizeNamespaceOverride(false) } setTargets((current) => current.map((target) => ({ ...target, valuesFiles: "", valuesYaml: "", manifestPath: "", namespaceValues: {} }))) }} items={[{ value: "helm", label: "Helm" }, { value: "kustomize", label: "Kustomize" }]} /></FormField>
          <FormField label="Group name" htmlFor="group-name" hint="A stable name for this set of deployments."><Input id="group-name" value={name} onChange={(event) => setName(event.target.value)} placeholder="monitoring-agent" required pattern="[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?" /></FormField>
          <FormField label="Git revision" htmlFor="group-revision" hint="Branch, tag, or commit; JustCD pins the reviewed commit SHA."><Input id="group-revision" value={revision} onChange={(event) => setRevision(event.target.value)} required /></FormField>
          <FormField label={renderer === "helm" ? "Chart path" : "Shared Kustomize path"} htmlFor="group-path" hint={renderer === "helm" ? "Repository-relative Helm chart directory." : "Repository-relative shared base or Kustomize overlay directory."}><Input id="group-path" value={manifestPath} onChange={(event) => setManifestPath(event.target.value)} required /></FormField>
        </div></Panel>

        {renderer === "helm" ? <Panel title="Shared Helm values" description="The chart's values.yaml is the base. Add optional Git files in order, then a shared JustCD YAML override."><div className="space-y-4 p-5">
          <FormField label="Shared values files" htmlFor="group-values-files" hint="One repository-relative YAML path per line. Later files override earlier values."><Textarea id="group-values-files" value={helmValuesFiles} onChange={(event) => setHelmValuesFiles(event.target.value)} placeholder={"values/common.yaml\nvalues/production.yaml"} className="min-h-20 font-mono text-xs" /></FormField>
          <FormField label="Shared JustCD overrides" htmlFor="group-values-yaml" hint="Applied after the shared Git files. Use Kubernetes Secret references for sensitive values."><Textarea id="group-values-yaml" value={helmValuesYaml} onChange={(event) => setHelmValuesYaml(event.target.value)} placeholder={"agent:\n  logLevel: info\n  imageTag: \"1.4.2\""} className="min-h-36 font-mono text-xs" spellCheck={false} /></FormField>
        </div></Panel> : <Panel title="Kustomize options" description="Put shared defaults and patches in a base overlay in Git. Cluster and namespace paths select complete entry points that can include the base."><div className="space-y-4 p-5">
          <label className="flex items-start gap-3 rounded-lg border p-3 text-xs"><Checkbox checked={kustomizeNamespaceOverride} onCheckedChange={(checked) => setKustomizeNamespaceOverride(Boolean(checked))} /><span><span className="block font-medium">Apply namespace transform</span><span className="mt-1 block text-muted-foreground">Build the selected entry point once per bound namespace and set namespace metadata to the target namespace.</span></span></label>
          <label className="flex items-start gap-3 rounded-lg border p-3 text-xs"><Checkbox checked={kustomizeHelmEnabled} onCheckedChange={(checked) => setKustomizeHelmEnabled(Boolean(checked))} /><span><span className="block font-medium">Enable Helm charts in Kustomize</span><span className="mt-1 block text-muted-foreground">Allows helmCharts from the Git revision during Kustomize builds.</span></span></label>
        </div></Panel>}

        <Panel title="Deployment targets" description="Each cluster becomes one JustCD application that can render into several selected namespaces." action={<Button type="button" size="sm" variant="outline" onClick={() => setTargets((current) => [...current, { ...firstTarget, id: "target-" + Date.now(), clusterId: clusters.find((cluster) => !current.some((target) => target.clusterId === cluster.id))?.id ?? "" }])} disabled={targets.length >= clusters.length}>Add cluster</Button>}>
          <div className="space-y-4 p-5">
            {targets.map((target, index) => <div key={target.id} className="rounded-xl border bg-muted/15 p-4">
              <div className="mb-4 flex items-center justify-between"><p className="text-xs font-semibold">Cluster {index + 1}</p>{targets.length > 1 && <Button type="button" size="sm" variant="ghost" onClick={() => setTargets((current) => current.filter((item) => item.id !== target.id))}>Remove</Button>}</div>
              <div className="grid gap-4 sm:grid-cols-2">
                <FormField label="Application name" htmlFor={"target-name-" + target.id}><Input id={"target-name-" + target.id} value={target.name} onChange={(event) => setTargets((current) => updateTarget(current, index, { name: event.target.value }))} placeholder={(name || "agent") + "-" + (index + 1)} required pattern="[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?" /></FormField>
                <FormField label="Cluster" htmlFor={"target-cluster-" + target.id}><FormSelect id={"target-cluster-" + target.id} value={target.clusterId} onValueChange={(value) => setTargets((current) => updateTarget(current, index, { clusterId: value, namespaces: [], namespaceValues: {} }))} placeholder="Select cluster" required items={clusters.filter((cluster) => cluster.id === target.clusterId || !targets.some((other, otherIndex) => otherIndex !== index && other.clusterId === cluster.id)).map((cluster) => ({ value: cluster.id, label: cluster.name }))} /></FormField>
                <div className="sm:col-span-2"><p className="mb-2 text-xs font-medium">Bound namespaces</p>{(bindingsByCluster[target.clusterId] ?? []).length ? <div className="grid gap-2 sm:grid-cols-2">{(bindingsByCluster[target.clusterId] ?? []).map((binding) => <label key={binding.namespace} className={`flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2.5 text-xs transition-colors ${target.namespaces.includes(binding.namespace) ? "border-primary/40 bg-primary/5" : "hover:bg-muted/40"}`}><Checkbox checked={target.namespaces.includes(binding.namespace)} onCheckedChange={(checked) => setTargets((current) => current.map((item, itemIndex) => {
                  if (itemIndex !== index) return item
                  if (checked) return { ...item, namespaces: [...item.namespaces, binding.namespace], namespaceValues: { ...item.namespaceValues, [binding.namespace]: item.namespaceValues[binding.namespace] ?? { manifestPath: "", valuesFiles: "", valuesYaml: "" } } }
                  const namespaceValues = { ...item.namespaceValues }; delete namespaceValues[binding.namespace]
                  return { ...item, namespaces: item.namespaces.filter((namespace) => namespace !== binding.namespace), namespaceValues }
                }))} /><span className="flex-1 font-medium">{binding.namespace}</span><span className="text-[10px] text-muted-foreground">{binding.credentialId ? "namespace credential" : "cluster default"}</span></label>)}</div> : <div className="rounded-lg border border-dashed px-4 py-5 text-xs text-muted-foreground">No namespaces are bound to this project on this cluster. Configure a namespace binding in project connections.</div>}</div>
                {renderer === "kustomize" ? <div className="sm:col-span-2"><FormField label="Cluster overlay path" htmlFor={`target-overlay-${target.id}`} hint="Optional Kustomize path for this cluster; blank uses the shared path. Include the shared base in this entry point."><Input id={`target-overlay-${target.id}`} value={target.manifestPath} onChange={(event) => setTargets((current) => updateTarget(current, index, { manifestPath: event.target.value }))} placeholder="overlays/clusters/prod-eu" /></FormField></div> : <><div className="sm:col-span-2"><FormField label="Cluster values files" htmlFor={"target-files-" + target.id} hint="Optional repository YAML files applied after shared values for every namespace in this cluster."><Textarea id={"target-files-" + target.id} value={target.valuesFiles} onChange={(event) => setTargets((current) => updateTarget(current, index, { valuesFiles: event.target.value }))} placeholder="values/clusters/prod-eu.yaml" className="min-h-16 font-mono text-xs" /></FormField></div><div className="sm:col-span-2"><FormField label="Cluster JustCD overrides" htmlFor={"target-values-" + target.id} hint="These values take precedence over shared values for every namespace in this cluster. Use Kubernetes Secret references for sensitive values."><Textarea id={"target-values-" + target.id} value={target.valuesYaml} onChange={(event) => setTargets((current) => updateTarget(current, index, { valuesYaml: event.target.value }))} placeholder={"agent:\n  region: eu"} className="min-h-24 font-mono text-xs" spellCheck={false} /></FormField></div></>}
                {target.namespaces.length > 0 && <div className="space-y-3 sm:col-span-2"><p className="text-xs font-medium">Namespace overrides</p>{target.namespaces.map((namespace) => {
                  const override = target.namespaceValues[namespace] ?? { manifestPath: "", valuesFiles: "", valuesYaml: "" }
                  return <details key={namespace} className="rounded-lg border bg-background px-3 py-2.5"><summary className="cursor-pointer text-xs font-medium">{namespace} <span className="font-normal text-muted-foreground">· optional override</span></summary><div className="mt-3 space-y-3">{renderer === "kustomize" ? <FormField label="Namespace overlay path" htmlFor={`namespace-overlay-${target.id}-${namespace}`} hint="Optional Kustomize path; blank uses the cluster path. Include the base and any needed cluster overlay in this entry point."><Input id={`namespace-overlay-${target.id}-${namespace}`} value={override.manifestPath} onChange={(event) => setTargets((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, namespaceValues: { ...item.namespaceValues, [namespace]: { ...override, manifestPath: event.target.value } } } : item))} placeholder={`overlays/namespaces/${namespace}`} /></FormField> : <><FormField label="Namespace values files" htmlFor={`namespace-files-${target.id}-${namespace}`} hint="Repository-relative paths, applied after cluster values."><Textarea id={`namespace-files-${target.id}-${namespace}`} value={override.valuesFiles} onChange={(event) => setTargets((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, namespaceValues: { ...item.namespaceValues, [namespace]: { ...override, valuesFiles: event.target.value } } } : item))} className="min-h-14 font-mono text-xs" /></FormField><FormField label="Namespace JustCD overrides" htmlFor={`namespace-yaml-${target.id}-${namespace}`} hint="Applied after namespace Git files."><Textarea id={`namespace-yaml-${target.id}-${namespace}`} value={override.valuesYaml} onChange={(event) => setTargets((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, namespaceValues: { ...override, valuesYaml: event.target.value } } : item))} placeholder={`agent:\n  namespace: ${namespace}`} className="min-h-20 font-mono text-xs" spellCheck={false} /></FormField></>}</div></details>
                })}</div>}
              </div>
            </div>)}
          </div>
        </Panel>
      </div>

      <div className="space-y-5">
        <Panel title="Reconciliation" description="All targets inherit these defaults."><div className="space-y-4 p-5">
          <FormField label="Sync policy" htmlFor="group-policy"><FormSelect id="group-policy" value={syncPolicy} onValueChange={setSyncPolicy} items={[{ value: "manual", label: "Manual · review each sync" }, { value: "auto-safe", label: "Auto-safe · apply non-destructive changes" }]} /></FormField>
          <FormField label="Poll interval (seconds)" htmlFor="group-poll"><Input id="group-poll" type="number" min={30} max={86400} value={pollSeconds} onChange={(event) => setPollSeconds(event.target.value)} /></FormField>
        </div></Panel>
        <Panel title="Before creating" description="This creates configuration only. It does not change any cluster."><div className="p-5"><p className="text-xs leading-5 text-muted-foreground">JustCD creates {targets.length} applications, one per cluster. Each has a separate reviewed plan and sync history and can cover multiple namespaces. {renderer === "helm" ? "Helm values layer from chart defaults, shared Git and JustCD values, cluster Git and JustCD values, then namespace Git and JustCD values. Use Kubernetes Secret references for sensitive values." : "Kustomize renders the shared overlay or selected cluster and namespace overlays. Identical cluster-scoped output is coalesced; conflicts stop planning."}</p>{selectedProject?.role !== "owner" && <p className="mt-3 text-xs text-destructive">Only project owners can create deployment groups.</p>}<Button className="mt-4 w-full" type="submit" loading={busy} loadingText="Creating targets…" disabled={loading || busy || !projectId || !sourceId || selectedProject?.role !== "owner"}>Create deployment group</Button></div></Panel>
      </div>
    </form>
  </>
}
