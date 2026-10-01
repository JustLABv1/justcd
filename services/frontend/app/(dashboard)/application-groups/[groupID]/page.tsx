"use client"

import { useEffect, useRef, useState, type FormEvent } from "react"
import { useParams } from "next/navigation"
import Link from "next/link"
import { Button } from "@/components/ui/button"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowLeft01Icon, Edit02Icon, ViewIcon, Layers01Icon } from "@hugeicons/core-free-icons"
import { RowActions } from "@/components/action-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { KustomizeOptions } from "@/components/applications/kustomize-options"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { ConnectionRow, FormField, PageHeading, Panel, StatusBadge } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { api } from "@/lib/api"
import type { Application, ApplicationGroup, ApplicationGroupResponse, Cluster, GitSource, ListResponse, Workspace } from "@/lib/types"

export default function ApplicationGroupPage() {
  const { groupID } = useParams<{ groupID: string }>()
  const toast = useToast()
  const [group, setGroup] = useState<ApplicationGroup | null>(null)
  const [applications, setApplications] = useState<Application[]>([])
  const [workspace, setWorkspace] = useState<Workspace | null>(null)
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
  const savedGroup = useRef<ApplicationGroup | null>(null)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown | null>(null)

  useEffect(() => {
    let active = true
    Promise.all([
      api<ApplicationGroupResponse>("/api/v1/application-groups/" + encodeURIComponent(groupID)),
      api<ListResponse<Workspace>>("/api/v1/workspaces"),
    ]).then(async ([result, workspaces]) => {
      const clusterList = await api<ListResponse<Cluster>>(`/api/v1/clusters?workspaceId=${encodeURIComponent(result.group.workspaceId)}`)
      if (!active) return
      savedGroup.current = result.group
      setGroup(result.group)
      setApplications(result.applications)
      setWorkspace(workspaces.items.find((item) => item.id === result.group.workspaceId) ?? null)
      setClusters(clusterList.items)
      setRevision(result.group.revision)
      setManifestPath(result.group.manifestPath)
      setValuesFiles(result.group.helmValuesFiles.join("\n"))
      setValuesYaml(result.group.helmValuesYaml)
      setKustomizeHelmEnabled(result.group.kustomizeHelmEnabled)
      setKustomizeNamespaceOverride(result.group.kustomizeNamespaceOverride)
      setSyncPolicy(result.group.syncPolicy)
      setPollSeconds(String(result.group.pollSeconds))
      return api<ListResponse<GitSource>>("/api/v1/git-sources?workspaceId=" + encodeURIComponent(result.group.workspaceId))
    }).then((sourceList) => { if (active && sourceList) setSources(sourceList.items) })
      .catch((cause) => active && setError(cause))
      .finally(() => active && setLoading(false))
    return () => { active = false }
  }, [groupID])

  function resetFields(source: ApplicationGroup) {
    setRevision(source.revision)
    setManifestPath(source.manifestPath)
    setValuesFiles(source.helmValuesFiles.join("\n"))
    setValuesYaml(source.helmValuesYaml)
    setKustomizeHelmEnabled(source.kustomizeHelmEnabled)
    setKustomizeNamespaceOverride(source.kustomizeNamespaceOverride)
    setSyncPolicy(source.syncPolicy)
    setPollSeconds(String(source.pollSeconds))
  }

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
      savedGroup.current = result.group
      setGroup(result.group)
      setApplications(result.applications)
      setKustomizeHelmEnabled(result.group.kustomizeHelmEnabled)
      setKustomizeNamespaceOverride(result.group.kustomizeNamespaceOverride)
      toast.success("Shared configuration saved. Target plans are stale.")
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }

  const owner = workspace?.role === "owner"

  return <>
    <PageHeading title={group?.name ?? "Deployment group"} description="Shared renderer configuration and per-target application status." actions={<Button render={<Link href="/applications" />} nativeButton={false} variant="ghost" size="sm"><HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={1.8} aria-hidden="true" />Applications</Button>} />
    {error != null && <ErrorNotice error={error} />}
    {loading || !group ? (error != null ? null : <div role="status" aria-label="Loading deployment group" className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_360px]"><Skeleton className="h-64 rounded-xl" /><Skeleton className="h-96 rounded-xl" /></div>) : <>
      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_360px]">
        <Panel title="Targets" description="Every target has independent plans, sync history, and approval rules.">
          <ul className="divide-y">
            {applications.map((app) => {
              const cluster = clusters.find((item) => item.id === app.clusterId)
              const namespaces = app.namespaces.map((item) => item.namespace).join(", ") || "No namespace"
              return <ConnectionRow
                key={app.id}
                icon={<HugeiconsIcon icon={Layers01Icon} strokeWidth={1.8} className="size-4" />}
                title={<Link href={"/applications/" + app.id} className="hover:text-primary">{app.name}</Link>}
                subtitle={`${cluster?.name ?? app.clusterId} / ${namespaces}`}
                badges={<StatusBadge status={app.health} />}
                actions={<RowActions label={app.name} items={[
                  { label: "View application", icon: ViewIcon, href: "/applications/" + app.id },
                  { label: "Target settings", icon: Edit02Icon, href: "/applications/" + app.id + "/edit" },
                ]} />}
              />
            })}
          </ul>
        </Panel>

        <Panel title="Shared configuration" description={owner ? "Changes here update all targets and invalidate their existing plans." : "Only workspace owners can change shared settings."}>
          <form className="space-y-4 p-5" onSubmit={save}>
            <FormField label="Git source" htmlFor="group-source"><FormSelect id="group-source" value={group.sourceId} onValueChange={(value) => setGroup({ ...group, sourceId: value })} disabled={!owner} items={sources.map((source) => ({ value: source.id, label: source.name }))} /></FormField>
            <FormField label="Git revision" htmlFor="group-revision"><Input id="group-revision" value={revision} onChange={(event) => setRevision(event.target.value)} required disabled={!owner} /></FormField>
            <FormField label="Renderer" htmlFor="group-renderer"><FormSelect id="group-renderer" value={group.renderer} onValueChange={() => {}} disabled items={[{ value: "helm", label: "Helm" }, { value: "kustomize", label: "Kustomize" }]} /></FormField>
            <FormField label={group.renderer === "helm" ? "Chart path" : "Shared Kustomize path"} htmlFor="group-path" hint={group.renderer === "helm" ? undefined : "Fallback path for targets. Cluster or namespace paths must include this base when needed."}><Input id="group-path" value={manifestPath} onChange={(event) => setManifestPath(event.target.value)} required disabled={!owner} /></FormField>
            {group.renderer === "helm" ? <>
              <FormField label="Shared values files" htmlFor="group-files" hint="One Git path per line; later files override earlier files."><Textarea id="group-files" value={valuesFiles} onChange={(event) => setValuesFiles(event.target.value)} className="font-mono text-xs" disabled={!owner} /></FormField>
              <FormField label="Shared JustCD values" htmlFor="group-yaml" hint="Applied after shared Git files and before target values. Use Kubernetes Secret references for sensitive values."><Textarea id="group-yaml" value={valuesYaml} onChange={(event) => setValuesYaml(event.target.value)} className="min-h-32 font-mono text-xs" spellCheck={false} disabled={!owner} /></FormField>
            </> : <KustomizeOptions idPrefix="group-kustomize" disabled={!owner} namespaceOverride={kustomizeNamespaceOverride} onNamespaceOverrideChange={setKustomizeNamespaceOverride} helmEnabled={kustomizeHelmEnabled} onHelmEnabledChange={setKustomizeHelmEnabled} />}
            <div className="grid gap-3 sm:grid-cols-2">
              <FormField label="Sync policy" htmlFor="group-policy"><FormSelect id="group-policy" value={syncPolicy} onValueChange={(value) => setSyncPolicy(value as ApplicationGroup["syncPolicy"])} disabled={!owner} items={[{ value: "manual", label: "Manual" }, { value: "auto-safe", label: "Auto-safe" }]} /></FormField>
              <FormField label="Poll interval (seconds)" htmlFor="group-poll"><Input id="group-poll" type="number" min={30} max={86400} value={pollSeconds} onChange={(event) => setPollSeconds(event.target.value)} disabled={!owner} /></FormField>
            </div>
            {owner && <div className="flex justify-end gap-2"><Button type="button" variant="outline" disabled={busy} onClick={() => { const saved = savedGroup.current ?? group; setGroup(saved); resetFields(saved); setError(null) }}>Cancel</Button><Button type="submit" loading={busy} loadingText="Saving changes…">Save changes</Button></div>}
          </form>
        </Panel>
      </div>
    </>}
  </>
}
