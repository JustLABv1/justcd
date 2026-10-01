"use client"

import { useEffect, useMemo, useRef, useState, type FormEvent } from "react"
import { useRouter } from "next/navigation"
import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { HugeiconsIcon } from "@hugeicons/react"
import { Add01Icon, ArrowLeft01Icon, Delete02Icon } from "@hugeicons/core-free-icons"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { CheckboxCard, FormField, PageHeading, Panel } from "@/components/ui-kit"
import { KustomizeOptions } from "@/components/applications/kustomize-options"
import { SummaryList, Wizard, WizardFooter, useWizard } from "@/components/applications/wizard"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { api, apiPost } from "@/lib/api"
import type { ApplicationGroupResponse, Cluster, GitSource, ListResponse, NamespaceBinding, Workspace } from "@/lib/types"

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
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [sources, setSources] = useState<GitSource[]>([])
  const [bindingsByCluster, setBindingsByCluster] = useState<Record<string, NamespaceBinding[]>>({})
  const [workspaceId, setWorkspaceId] = useState("")
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
  const [workspaceNotice, setWorkspaceNotice] = useState("")
  const errorRef = useRef<HTMLDivElement>(null)
  const wizard = useWizard(5, "new-group")

  useEffect(() => { if (error) errorRef.current?.scrollIntoView({ block: "nearest" }) }, [error])

  useEffect(() => {
    const queryWorkspace = new URLSearchParams(window.location.search).get("workspaceId") ?? ""
    let active = true
    api<ListResponse<Workspace>>("/api/v1/workspaces")
      .then((workspaceResult) => {
        if (!active) return
        const writable = workspaceResult.items.filter((workspace) => workspace.role !== "viewer")
        setWorkspaces(writable)
        setWorkspaceId(writable.find((workspace) => workspace.id === queryWorkspace)?.id ?? writable[0]?.id ?? "")
      })
      .catch((cause) => active && setError(cause))
      .finally(() => active && setLoading(false))
    return () => { active = false }
  }, [])

  useEffect(() => {
    if (!workspaceId) return
    let active = true
    Promise.all([
      api<ListResponse<GitSource>>("/api/v1/git-sources?workspaceId=" + encodeURIComponent(workspaceId)),
      api<ListResponse<Cluster>>("/api/v1/clusters?workspaceId=" + encodeURIComponent(workspaceId)),
    ]).then(([sourceList, clusterList]) => {
      if (!active) return
      setSources(sourceList.items)
      setSourceId(sourceList.items[0]?.id ?? "")
      setClusters(clusterList.items)
      setTargets((current) => current.map((target) => ({ ...target, clusterId: clusterList.items.some((cluster) => cluster.id === target.clusterId) ? target.clusterId : clusterList.items[0]?.id ?? "", namespaces: [], namespaceValues: {} })))
      setError(null)
    }).catch((cause) => active && setError(cause))
    return () => { active = false }
  }, [workspaceId])

  useEffect(() => {
    if (!workspaceId || clusters.length === 0) return
    let active = true
    Promise.all(clusters.map(async (cluster) => {
      const result = await api<ListResponse<NamespaceBinding>>("/api/v1/clusters/" + encodeURIComponent(cluster.id) + "/bindings?workspaceId=" + encodeURIComponent(workspaceId))
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
  }, [workspaceId, clusters])

  const selectedWorkspace = useMemo(() => workspaces.find((workspace) => workspace.id === workspaceId), [workspaces, workspaceId])

  function missingSelection(step: number) {
    const missing = step === 1 ? (!workspaceId ? "Choose a workspace." : !sourceId ? "Choose a Git source." : "")
      : step === 3 ? (targets.some((target) => !target.clusterId) ? "Choose a cluster for every target." : targets.some((target) => !target.namespaces.length) ? "Select at least one namespace for every target." : "") : ""
    if (missing) setError(new Error(missing))
    return !missing
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!wizard.isLast) { if (wizard.next(missingSelection)) setError(null); return }
    if (!wizard.validateAll()) return
    setError(null)
    if (!workspaceId || !sourceId || !name.trim() || targets.some((target) => !target.name.trim() || !target.clusterId || !target.namespaces.length)) {
      setError(new Error("Choose a workspace and Git source, name the group and every target, and select at least one namespace for each cluster."))
      return
    }
    setBusy(true)
    try {
      const result = await apiPost<ApplicationGroupResponse>("/api/v1/application-groups", {
        workspaceId, name: name.trim(), sourceId, revision: revision.trim(), manifestPath: manifestPath.trim(),
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
    } finally {
      setBusy(false)
    }
  }

  const rendererLabel = renderer === "helm" ? "Helm" : "Kustomize"
  const sourceName = sources.find((source) => source.id === sourceId)?.name ?? sourceId

  return <>
    <PageHeading title="New deployment group" description="Deploy to multiple targets: one independently planned application per cluster, each covering selected namespaces." actions={<Button render={<Link href="/applications" />} variant="ghost" size="sm" nativeButton={false}><HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={1.8} aria-hidden="true" />Applications</Button>} />
    <div ref={errorRef}>{error != null && <ErrorNotice error={error} />}</div>
    <form className="max-w-4xl space-y-5" onSubmit={submit} noValidate>
      <Wizard step={wizard.step} onStepChange={wizard.setStep} idPrefix="new-group" steps={[
        { title: "Source", content: <Panel title="Shared source" description="Targets share a Git source and revision."><div className="grid gap-4 p-5 sm:grid-cols-2">
          <FormField label="Workspace" htmlFor="group-workspace"><FormSelect id="group-workspace" value={workspaceId} onValueChange={(value) => { if (value !== workspaceId && (sourceId || targets.some((target) => target.clusterId || target.namespaces.length))) setWorkspaceNotice("Git source, cluster and namespace selections were reset for the new workspace."); setWorkspaceId(value); setSourceId(""); setSources([]); setClusters([]); setBindingsByCluster({}); setTargets((current) => current.map((target) => ({ ...target, clusterId: "", namespaces: [], namespaceValues: {} }))) }} placeholder="Select workspace" required items={workspaces.map((workspace) => ({ value: workspace.id, label: workspace.name }))} /></FormField>
          <FormField label="Git source" htmlFor="group-source"><FormSelect id="group-source" value={sourceId} onValueChange={(value) => { setSourceId(value); setWorkspaceNotice("") }} placeholder="Select source" required items={sources.map((source) => ({ value: source.id, label: source.name + " · " + source.repositoryUrl }))} /></FormField>
          {workspaceNotice && <p role="status" className="rounded-lg border bg-muted/50 px-3 py-2 text-sm text-muted-foreground sm:col-span-2">{workspaceNotice}</p>}
          <FormField label="Group name" htmlFor="group-name" hint="A stable name for this set of deployments."><Input id="group-name" value={name} onChange={(event) => setName(event.target.value)} placeholder="monitoring-agent" required pattern="[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?" /></FormField>
          <FormField label="Git revision" htmlFor="group-revision" hint="Branch, tag, or commit; JustCD pins the reviewed commit SHA."><Input id="group-revision" value={revision} onChange={(event) => setRevision(event.target.value)} required /></FormField>
        </div></Panel> },
        { title: "Renderer options", content: <>
          <Panel title="Renderer" description="Choose Helm values layering or Kustomize overlays."><div className="grid gap-4 p-5 sm:grid-cols-2">
            <FormField label="Renderer" htmlFor="group-renderer"><FormSelect id="group-renderer" value={renderer} onValueChange={(value) => { const next = value as "helm" | "kustomize"; setRenderer(next); setHelmValuesFiles(""); setHelmValuesYaml(""); if (next === "helm") { setKustomizeHelmEnabled(false); setKustomizeNamespaceOverride(false) } setTargets((current) => current.map((target) => ({ ...target, valuesFiles: "", valuesYaml: "", manifestPath: "", namespaceValues: {} }))) }} items={[{ value: "helm", label: "Helm" }, { value: "kustomize", label: "Kustomize" }]} /></FormField>
            <FormField label={renderer === "helm" ? "Chart path" : "Shared Kustomize path"} htmlFor="group-path" hint={renderer === "helm" ? "Repository-relative Helm chart directory." : "Repository-relative shared base or Kustomize overlay directory."}><Input id="group-path" value={manifestPath} onChange={(event) => setManifestPath(event.target.value)} required /></FormField>
          </div></Panel>
          {renderer === "helm" ? <Panel title="Shared Helm values" description="The chart's values.yaml is the base. Add optional Git files in order, then a shared JustCD YAML override."><div className="space-y-4 p-5">
            <FormField label="Shared values files" htmlFor="group-values-files" hint="One repository-relative YAML path per line. Later files override earlier values."><Textarea id="group-values-files" value={helmValuesFiles} onChange={(event) => setHelmValuesFiles(event.target.value)} placeholder={"values/common.yaml\nvalues/production.yaml"} className="min-h-20 font-mono text-xs" /></FormField>
            <FormField label="Shared JustCD overrides" htmlFor="group-values-yaml" hint="Applied after the shared Git files. Use Kubernetes Secret references for sensitive values."><Textarea id="group-values-yaml" value={helmValuesYaml} onChange={(event) => setHelmValuesYaml(event.target.value)} placeholder={"agent:\n  logLevel: info\n  imageTag: \"1.4.2\""} className="min-h-36 font-mono text-xs" spellCheck={false} /></FormField>
          </div></Panel> : <Panel title="Kustomize options" description="Put shared defaults and patches in a base overlay in Git. Cluster and namespace paths select complete entry points that can include the base."><div className="p-5">
            <KustomizeOptions idPrefix="new-group-kustomize" namespaceOverride={kustomizeNamespaceOverride} onNamespaceOverrideChange={setKustomizeNamespaceOverride} helmEnabled={kustomizeHelmEnabled} onHelmEnabledChange={setKustomizeHelmEnabled} />
          </div></Panel>}
        </> },
        { title: "Targets", content: <Panel title="Deployment targets" description="Each cluster becomes one JustCD application that can render into several selected namespaces." action={<Button type="button" size="sm" variant="outline" onClick={() => setTargets((current) => [...current, { ...firstTarget, id: "target-" + Date.now(), clusterId: clusters.find((cluster) => !current.some((target) => target.clusterId === cluster.id))?.id ?? "" }])} disabled={targets.length >= clusters.length}><HugeiconsIcon icon={Add01Icon} strokeWidth={1.8} aria-hidden="true" />Add target</Button>}>
          <div className="space-y-4 p-5">
            {targets.map((target, index) => <div key={target.id} className="rounded-xl border bg-muted/15 p-4">
              <div className="mb-4 flex items-center justify-between"><h3 className="text-sm font-semibold">Target {index + 1}{target.name && <span className="font-normal text-muted-foreground"> · {target.name}</span>}</h3>{targets.length > 1 && <Button type="button" size="icon" variant="ghost" aria-label={`Remove target ${target.name || index + 1}`} onClick={() => setTargets((current) => current.filter((item) => item.id !== target.id))}><HugeiconsIcon icon={Delete02Icon} strokeWidth={1.8} aria-hidden="true" /></Button>}</div>
              <div className="grid gap-4 sm:grid-cols-2">
                <FormField label="Application name" htmlFor={"target-name-" + target.id}><Input id={"target-name-" + target.id} value={target.name} onChange={(event) => setTargets((current) => updateTarget(current, index, { name: event.target.value }))} placeholder={(name || "agent") + "-" + (index + 1)} required pattern="[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?" /></FormField>
                <FormField label="Cluster" htmlFor={"target-cluster-" + target.id}><FormSelect id={"target-cluster-" + target.id} value={target.clusterId} onValueChange={(value) => setTargets((current) => updateTarget(current, index, { clusterId: value, namespaces: [], namespaceValues: {} }))} placeholder="Select cluster" required items={clusters.filter((cluster) => cluster.id === target.clusterId || !targets.some((other, otherIndex) => otherIndex !== index && other.clusterId === cluster.id)).map((cluster) => ({ value: cluster.id, label: cluster.name }))} /></FormField>
                <div className="sm:col-span-2"><p className="mb-2 text-sm font-medium">Namespaces</p>{(bindingsByCluster[target.clusterId] ?? []).length ? <div className="grid gap-2 sm:grid-cols-2">{(bindingsByCluster[target.clusterId] ?? []).map((binding) => <CheckboxCard key={binding.namespace} id={`${target.id}-ns-${binding.namespace}`} title={binding.namespace} description={binding.credentialId ? "namespace credential" : "default credential"} checked={target.namespaces.includes(binding.namespace)} onCheckedChange={(checked) => setTargets((current) => current.map((item, itemIndex) => {
                  if (itemIndex !== index) return item
                  if (checked) return { ...item, namespaces: [...item.namespaces, binding.namespace], namespaceValues: { ...item.namespaceValues, [binding.namespace]: item.namespaceValues[binding.namespace] ?? { manifestPath: "", valuesFiles: "", valuesYaml: "" } } }
                  const namespaceValues = { ...item.namespaceValues }; delete namespaceValues[binding.namespace]
                  return { ...item, namespaces: item.namespaces.filter((namespace) => namespace !== binding.namespace), namespaceValues }
                }))} />)}</div> : <div className="rounded-lg border border-dashed px-4 py-5 text-sm text-muted-foreground">This workspace has no namespace access on this cluster. Grant namespace access in workspace connections.</div>}</div>
                {renderer === "kustomize" ? <div className="sm:col-span-2"><FormField label="Cluster overlay path" htmlFor={`target-overlay-${target.id}`} hint="Optional Kustomize path for this cluster; blank uses the shared path. Include the shared base in this entry point."><Input id={`target-overlay-${target.id}`} value={target.manifestPath} onChange={(event) => setTargets((current) => updateTarget(current, index, { manifestPath: event.target.value }))} placeholder="overlays/clusters/prod-eu" /></FormField></div> : <><div className="sm:col-span-2"><FormField label="Cluster values files" htmlFor={"target-files-" + target.id} hint="Optional repository YAML files applied after shared values for every namespace in this cluster."><Textarea id={"target-files-" + target.id} value={target.valuesFiles} onChange={(event) => setTargets((current) => updateTarget(current, index, { valuesFiles: event.target.value }))} placeholder="values/clusters/prod-eu.yaml" className="min-h-16 font-mono text-xs" /></FormField></div><div className="sm:col-span-2"><FormField label="Cluster JustCD overrides" htmlFor={"target-values-" + target.id} hint="These values take precedence over shared values for every namespace in this cluster. Use Kubernetes Secret references for sensitive values."><Textarea id={"target-values-" + target.id} value={target.valuesYaml} onChange={(event) => setTargets((current) => updateTarget(current, index, { valuesYaml: event.target.value }))} placeholder={"agent:\n  region: eu"} className="min-h-24 font-mono text-xs" spellCheck={false} /></FormField></div></>}
                {target.namespaces.length > 0 && <div className="space-y-3 sm:col-span-2"><p className="text-sm font-medium">Namespace overrides</p>{target.namespaces.map((namespace) => {
                  const override = target.namespaceValues[namespace] ?? { manifestPath: "", valuesFiles: "", valuesYaml: "" }
                  return <Disclosure key={namespace} className="rounded-lg border bg-background px-3 py-2.5" summary={<>{namespace} <span className="font-normal text-muted-foreground">· optional override</span></>}><div className="space-y-3">{renderer === "kustomize" ? <FormField label="Namespace overlay path" htmlFor={`namespace-overlay-${target.id}-${namespace}`} hint="Optional Kustomize path; blank uses the cluster path. Include the base and any needed cluster overlay in this entry point."><Input id={`namespace-overlay-${target.id}-${namespace}`} value={override.manifestPath} onChange={(event) => setTargets((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, namespaceValues: { ...item.namespaceValues, [namespace]: { ...override, manifestPath: event.target.value } } } : item))} placeholder={`overlays/namespaces/${namespace}`} /></FormField> : <><FormField label="Namespace values files" htmlFor={`namespace-files-${target.id}-${namespace}`} hint="Repository-relative paths, applied after cluster values."><Textarea id={`namespace-files-${target.id}-${namespace}`} value={override.valuesFiles} onChange={(event) => setTargets((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, namespaceValues: { ...item.namespaceValues, [namespace]: { ...override, valuesFiles: event.target.value } } } : item))} className="min-h-14 font-mono text-xs" /></FormField><FormField label="Namespace JustCD overrides" htmlFor={`namespace-yaml-${target.id}-${namespace}`} hint="Applied after namespace Git files."><Textarea id={`namespace-yaml-${target.id}-${namespace}`} value={override.valuesYaml} onChange={(event) => setTargets((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, namespaceValues: { ...item.namespaceValues, [namespace]: { ...override, valuesYaml: event.target.value } } } : item))} placeholder={`agent:\n  namespace: ${namespace}`} className="min-h-20 font-mono text-xs" spellCheck={false} /></FormField></>}</div></Disclosure>
                })}</div>}
              </div>
            </div>)}
          </div>
        </Panel> },
        { title: "Reconciliation", content: <Panel title="Reconciliation" description="All targets inherit these defaults."><div className="grid gap-4 p-5 sm:grid-cols-2">
          <FormField label="Sync policy" htmlFor="group-policy"><FormSelect id="group-policy" value={syncPolicy} onValueChange={setSyncPolicy} items={[{ value: "manual", label: "Manual · review each sync" }, { value: "auto-safe", label: "Auto-safe · apply non-destructive changes" }]} /></FormField>
          <FormField label="Poll interval (seconds)" htmlFor="group-poll"><Input id="group-poll" type="number" min={30} max={86400} value={pollSeconds} onChange={(event) => setPollSeconds(event.target.value)} /></FormField>
        </div></Panel> },
        { title: "Review", content: <Panel title="Review" description="This creates configuration only. It does not change any cluster."><div className="space-y-4 p-5">
          <SummaryList items={[
            { label: "Workspace", value: selectedWorkspace?.name ?? "—" },
            { label: "Group name", value: name || "—" },
            { label: "Git source", value: sourceName || "—" },
            { label: "Git revision", value: revision || "—" },
            { label: "Renderer", value: rendererLabel },
            { label: renderer === "helm" ? "Chart path" : "Shared Kustomize path", value: <span className="font-mono">{manifestPath || "—"}</span> },
            { label: "Sync policy", value: syncPolicy === "auto-safe" ? "Auto-safe" : "Manual" },
            { label: "Poll interval (seconds)", value: pollSeconds },
            ...targets.map((target, index) => ({ label: `Target ${index + 1}`, value: <>{target.name || "—"} <span className="font-normal text-muted-foreground">· {clusters.find((cluster) => cluster.id === target.clusterId)?.name ?? "no cluster"} / {target.namespaces.join(", ") || "no namespace"}</span></> })),
          ]} />
          <p className="text-sm leading-5 text-muted-foreground">JustCD creates {targets.length} {targets.length === 1 ? "application" : "applications"}, one per cluster. Each has a separate reviewed plan and sync history and can cover multiple namespaces. {renderer === "helm" ? "Helm values layer from chart defaults, shared Git and JustCD values, cluster Git and JustCD values, then namespace Git and JustCD values. Use Kubernetes Secret references for sensitive values." : "Kustomize renders the shared overlay or selected cluster and namespace overlays. Identical cluster-scoped output is coalesced; conflicts stop planning."}</p>
          {selectedWorkspace?.role !== "owner" && <p className="text-sm text-destructive">Only workspace owners can create deployment groups.</p>}
        </div></Panel> },
      ]} />
      <WizardFooter step={wizard.step} isLast={wizard.isLast} onBack={wizard.back} onNext={() => { if (wizard.next(missingSelection)) setError(null) }} submit={<Button type="submit" loading={busy} loadingText="Creating targets…" disabled={loading || busy || !workspaceId || !sourceId || selectedWorkspace?.role !== "owner"}>Create deployment group</Button>} />
    </form>
  </>
}
