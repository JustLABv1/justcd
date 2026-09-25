"use client"

import { useEffect, useState, type FormEvent } from "react"
import { useParams, useRouter } from "next/navigation"
import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { ApprovalRuleEditor } from "@/components/approval-rule-editor"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { FormField, PageHeading, Panel } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { api, errorMessage } from "@/lib/api"
import type { Application, ApprovalPolicyOverride, Cluster, GitSource, ListResponse, NamespaceBinding, Project, ProjectMember } from "@/lib/types"

export default function EditApplicationPage() {
  const { applicationID } = useParams<{ applicationID: string }>()
  const router = useRouter()
  const toast = useToast()
  const [app, setApp] = useState<Application | null>(null)
  const [project, setProject] = useState<Project | null>(null)
  const [sources, setSources] = useState<GitSource[]>([])
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [bindings, setBindings] = useState<NamespaceBinding[]>([])
  const [members, setMembers] = useState<ProjectMember[]>([])
  const [namespaces, setNamespaces] = useState<string[]>([])
  const [approvalPolicyOverride, setApprovalPolicyOverride] = useState<ApprovalPolicyOverride>({})
  const [busy, setBusy] = useState(false)
  const [approvalBusy, setApprovalBusy] = useState(false)
  const [error, setError] = useState<unknown | null>(null)

  useEffect(() => {
    let active = true
    Promise.all([api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`), api<ListResponse<Project>>( "/api/v1/projects"), api<ListResponse<Cluster>>( "/api/v1/clusters")])
      .then(([application, projects, clusterList]) => {
        if (!active) return
        setApp(application); setNamespaces(application.namespaces.map((item) => item.namespace)); setApprovalPolicyOverride(application.approvalPolicyOverride ?? {})
        setProject(projects.items.find((item) => item.id === application.projectId) ?? null)
        setClusters(clusterList.items)
      }).catch((cause) => active && setError(cause))
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
      api<ListResponse<ProjectMember>>(`/api/v1/projects/${encodeURIComponent(projectId)}/members`),
    ]).then(([sourceList, bindingList, memberList]) => { if (active) { setSources(sourceList.items); setBindings(bindingList.items); setMembers(memberList.items) } }).catch((cause) => active && setError(cause))
    return () => { active = false }
  }, [projectId, clusterId])

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!app) return
    setBusy(true); setError(null)
    try {
      await api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`, { method: "PUT", body: JSON.stringify({
        name: app.name, sourceId: app.sourceId, revision: app.revision, manifestPath: app.manifestPath,
        renderer: app.renderer, kustomizeHelmEnabled: app.renderer === "kustomize" && app.kustomizeHelmEnabled,
        kustomizeNamespaceOverride: app.renderer === "kustomize" && app.kustomizeNamespaceOverride,
        targetManifestPath: app.renderer === "kustomize" ? app.targetManifestPath : "",
        namespaceManifestPaths: app.renderer === "kustomize" ? Object.fromEntries(namespaces.reduce<[string, string][]>((entries, namespace) => {
          const path = app.namespaceManifestPaths?.[namespace]?.trim() ?? ""
          if (path) entries.push([namespace, path])
          return entries
        }, [])) : {},
        helmValuesFiles: app.renderer === "helm" ? app.helmValuesFiles : [],
        helmValuesYaml: app.renderer === "helm" ? app.helmValuesYaml : "",
        targetHelmValuesFiles: app.renderer === "helm" ? app.targetHelmValuesFiles : [],
        targetHelmValuesYaml: app.renderer === "helm" ? app.targetHelmValuesYaml : "",
        namespaceHelmValues: app.renderer === "helm" ? Object.fromEntries(namespaces.map((namespace) => [namespace, app.namespaceHelmValues?.[namespace] ?? { files: [], yaml: "" }])) : {},
        clusterId: app.clusterId, namespaces, syncPolicy: app.syncPolicy, pollSeconds: app.pollSeconds,
      }) })
      toast.success("Application updated.")
      router.push(`/applications/${applicationID}?tab=activity`); router.refresh()
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setBusy(false) }
  }

  async function saveApprovalOverrides() {
    setApprovalBusy(true); setError(null)
    try {
      const override = Object.values(approvalPolicyOverride).some(Boolean) ? approvalPolicyOverride : null
      const result = await api<{ approvalPolicyOverride: ApprovalPolicyOverride | null }>(`/api/v1/applications/${encodeURIComponent(applicationID)}/approval-policy`, { method: "PUT", body: JSON.stringify({ override }) })
      setApprovalPolicyOverride(result.approvalPolicyOverride ?? {})
      setApp((current) => current ? { ...current, approvalPolicyOverride: result.approvalPolicyOverride ?? undefined } : current)
      toast.success("Application approval rules saved. Existing plans are stale.")
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setApprovalBusy(false) }
  }

  function toggleOverride(kind: "sync" | "deletion", enabled: boolean) {
    setApprovalPolicyOverride((current) => {
      const next = { ...current }
      if (enabled) {
        const inherited = project?.approvalPolicy[kind]
        if (inherited) next[kind] ??= { ...inherited, approverRoles: [...inherited.approverRoles], approverUserIds: [...inherited.approverUserIds] }
      } else {
        delete next[kind]
      }
      return next
    })
  }

  return <>
    <PageHeading title={app ? `Edit ${app.name}` : "Edit application"} description="Changes invalidate existing plans. Review a fresh plan before the next sync." />
    {error && <ErrorNotice error={error} />}
    {!app ? <p className="text-sm text-muted-foreground">Loading application…</p> : project?.role !== "owner" ? <p className="text-sm text-muted-foreground">Only project owners can edit applications.</p> : <form className="max-w-4xl space-y-5" onSubmit={save}>
      <Panel title="Source" description="The Git revision and repository path used for future plans."><div className="grid gap-4 p-5 sm:grid-cols-2">
        <FormField label="Application name" htmlFor="edit-app-name"><Input id="edit-app-name" value={app.name} onChange={(event) => setApp({ ...app, name: event.target.value })} required pattern="[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?" /></FormField>
        <FormField label="Git source" htmlFor="edit-app-source"><FormSelect id="edit-app-source" value={app.sourceId} onValueChange={(value) => setApp({ ...app, sourceId: value })} disabled={Boolean(app.applicationGroupId)} items={sources.map((source) => ({ value: source.id, label: source.name }))} /></FormField>
        <FormField label="Git revision" htmlFor="edit-app-revision"><Input id="edit-app-revision" value={app.revision} onChange={(event) => setApp({ ...app, revision: event.target.value })} required disabled={Boolean(app.applicationGroupId)} /></FormField>
        <FormField label={app.renderer === "helm" ? "Chart path" : app.renderer === "kustomize" ? "Shared Kustomize path" : "Manifest path"} htmlFor="edit-app-path" hint={app.renderer === "kustomize" ? "Fallback Kustomize entry point. Cluster and namespace paths can select complete overlays." : undefined}><Input id="edit-app-path" value={app.manifestPath} onChange={(event) => setApp({ ...app, manifestPath: event.target.value })} required disabled={Boolean(app.applicationGroupId)} /></FormField>
        <FormField label="Renderer" htmlFor="edit-app-renderer"><FormSelect id="edit-app-renderer" value={app.renderer} onValueChange={(value) => setApp({ ...app, renderer: value as Application["renderer"] })} disabled={Boolean(app.applicationGroupId)} items={[{ value: "yaml", label: "Plain YAML / JSON" }, { value: "kustomize", label: "Kustomize build" }, { value: "helm", label: "Helm template" }]} /></FormField>
        {app.renderer === "kustomize" && <div className="flex flex-col gap-3 self-end text-xs">{app.applicationGroupId && <p className="text-muted-foreground">Shared Kustomize settings are managed by the <Link href={`/application-groups/${app.applicationGroupId}`} className="font-medium text-primary hover:underline">deployment group</Link>.</p>}<label className="flex items-center gap-2"><Checkbox checked={app.kustomizeHelmEnabled} disabled={Boolean(app.applicationGroupId)} onCheckedChange={(checked) => setApp({ ...app, kustomizeHelmEnabled: Boolean(checked) })} />Enable Helm charts</label><label className="flex items-center gap-2"><Checkbox checked={app.kustomizeNamespaceOverride} disabled={Boolean(app.applicationGroupId)} onCheckedChange={(checked) => setApp({ ...app, kustomizeNamespaceOverride: Boolean(checked) })} />Apply namespace transform</label></div>}
        {app.renderer === "helm" && <div className="space-y-4 sm:col-span-2">
          {app.applicationGroupId ? <div className="rounded-lg border bg-muted/30 p-3 text-xs">Shared Helm values are managed by this deployment group. <Link href={"/application-groups/" + app.applicationGroupId} className="font-medium text-primary hover:underline">Edit shared values →</Link></div> : <><FormField label="Helm values files" htmlFor="edit-helm-files" hint="One repository-relative path per line; later files override earlier files."><Textarea id="edit-helm-files" value={app.helmValuesFiles.join("\n")} onChange={(event) => setApp({ ...app, helmValuesFiles: event.target.value.split("\n").map((line) => line.trim()).filter(Boolean) })} className="font-mono text-xs" /></FormField><FormField label="Helm values overrides" htmlFor="edit-helm-yaml" hint="Optional JustCD values applied after the files. Use Kubernetes Secret references for sensitive values."><Textarea id="edit-helm-yaml" value={app.helmValuesYaml} onChange={(event) => setApp({ ...app, helmValuesYaml: event.target.value })} className="min-h-28 font-mono text-xs" spellCheck={false} /></FormField></>}
          <FormField label={app.applicationGroupId ? "Cluster Helm values files" : "Additional cluster values files"} htmlFor="edit-target-helm-files" hint="Optional repository files applied after shared values for every namespace in this cluster."><Textarea id="edit-target-helm-files" value={app.targetHelmValuesFiles.join("\n")} onChange={(event) => setApp({ ...app, targetHelmValuesFiles: event.target.value.split("\n").map((line) => line.trim()).filter(Boolean) })} className="font-mono text-xs" /></FormField>
          <FormField label={app.applicationGroupId ? "Cluster JustCD overrides" : "Additional cluster overrides"} htmlFor="edit-target-helm-yaml" hint="Values for this cluster override shared values and apply to all selected namespaces."><Textarea id="edit-target-helm-yaml" value={app.targetHelmValuesYaml} onChange={(event) => setApp({ ...app, targetHelmValuesYaml: event.target.value })} className="min-h-28 font-mono text-xs" spellCheck={false} /></FormField>
        </div>}
      </div></Panel>
      <Panel title="Cluster target" description="Cluster and namespace changes are blocked while JustCD manages resources for this application."><div className="space-y-4 p-5">
        <FormField label="Cluster" htmlFor="edit-app-cluster"><FormSelect id="edit-app-cluster" value={app.clusterId} onValueChange={(value) => { setApp({ ...app, clusterId: value, namespaceHelmValues: {}, namespaceManifestPaths: {}, targetManifestPath: "" }); setNamespaces([]) }} items={clusters.map((cluster) => ({ value: cluster.id, label: cluster.name }))} /></FormField>
        {app.renderer === "kustomize" && <FormField label="Cluster overlay path" htmlFor="edit-target-overlay" hint="Optional Kustomize path for this cluster; blank uses the shared path. This overlay should include its shared base."><Input id="edit-target-overlay" value={app.targetManifestPath ?? ""} onChange={(event) => setApp({ ...app, targetManifestPath: event.target.value })} placeholder="overlays/clusters/prod-eu" /></FormField>}
        <div><p className="mb-2 text-xs font-medium">Namespaces</p><div className="grid gap-2 sm:grid-cols-2">{bindings.map((binding) => <label key={binding.namespace} className="flex items-center gap-2 rounded-lg border p-3 text-xs"><Checkbox checked={namespaces.includes(binding.namespace)} onCheckedChange={(checked) => {
          if (checked) {
            setNamespaces((current) => [...current, binding.namespace])
            setApp((current) => current ? { ...current, namespaceHelmValues: { ...current.namespaceHelmValues, [binding.namespace]: current.namespaceHelmValues?.[binding.namespace] ?? { files: [], yaml: "" } } } : current)
          } else {
            setNamespaces((current) => current.filter((item) => item !== binding.namespace))
            setApp((current) => {
              if (!current) return current
              const namespaceHelmValues = { ...current.namespaceHelmValues }; delete namespaceHelmValues[binding.namespace]
              const namespaceManifestPaths = { ...current.namespaceManifestPaths }; delete namespaceManifestPaths[binding.namespace]
              return { ...current, namespaceHelmValues, namespaceManifestPaths }
            })
          }
        }} />{binding.namespace}</label>)}</div></div>
        {app.renderer === "kustomize" && namespaces.length > 0 && <div className="space-y-3 border-t pt-4"><p className="text-xs font-medium">Namespace overlays</p>{namespaces.map((namespace) => <FormField key={namespace} label={`${namespace} overlay path`} htmlFor={`edit-ns-overlay-${namespace}`} hint="Optional Kustomize path; blank uses the cluster path. Include the base and any cluster overlay this namespace needs."><Input id={`edit-ns-overlay-${namespace}`} value={app.namespaceManifestPaths?.[namespace] ?? ""} onChange={(event) => setApp({ ...app, namespaceManifestPaths: { ...app.namespaceManifestPaths, [namespace]: event.target.value } })} placeholder={`overlays/namespaces/${namespace}`} /></FormField>)}</div>}
        {app.renderer === "helm" && namespaces.length > 0 && <div className="space-y-3 border-t pt-4"><p className="text-xs font-medium">Namespace Helm overrides</p>{namespaces.map((namespace) => {
          const override = app.namespaceHelmValues?.[namespace] ?? { files: [], yaml: "" }
          return <details key={namespace} className="rounded-lg border px-3 py-2.5"><summary className="cursor-pointer text-xs font-medium">{namespace} <span className="font-normal text-muted-foreground">· optional overrides</span></summary><div className="mt-3 space-y-3"><FormField label="Namespace values files" htmlFor={`edit-ns-files-${namespace}`} hint="Repository-relative paths, applied after cluster values."><Textarea id={`edit-ns-files-${namespace}`} value={override.files.join("\n")} onChange={(event) => setApp({ ...app, namespaceHelmValues: { ...app.namespaceHelmValues, [namespace]: { ...override, files: event.target.value.split("\n").map((line) => line.trim()).filter(Boolean) } } })} className="min-h-14 font-mono text-xs" /></FormField><FormField label="Namespace JustCD overrides" htmlFor={`edit-ns-yaml-${namespace}`} hint="Applied after namespace Git files. Use Kubernetes Secret references for sensitive values."><Textarea id={`edit-ns-yaml-${namespace}`} value={override.yaml} onChange={(event) => setApp({ ...app, namespaceHelmValues: { ...app.namespaceHelmValues, [namespace]: { ...override, yaml: event.target.value } } })} className="min-h-20 font-mono text-xs" spellCheck={false} /></FormField></div></details>
        })}</div>}
      </div></Panel>
      <Panel title="Reconciliation"><div className="grid gap-4 p-5 sm:grid-cols-2"><FormField label="Sync policy" htmlFor="edit-app-policy"><FormSelect id="edit-app-policy" value={app.syncPolicy} onValueChange={(value) => setApp({ ...app, syncPolicy: value as Application["syncPolicy"] })} disabled={Boolean(app.applicationGroupId)} items={[{ value: "manual", label: "Manual" }, { value: "auto-safe", label: "Auto-safe" }]} /></FormField><FormField label="Poll interval (seconds)" htmlFor="edit-app-poll"><Input id="edit-app-poll" type="number" min={30} max={86400} value={app.pollSeconds} onChange={(event) => setApp({ ...app, pollSeconds: Number(event.target.value) })} disabled={Boolean(app.applicationGroupId)} /></FormField>{app.applicationGroupId && <p className="text-xs text-muted-foreground sm:col-span-2">Shared reconciliation settings come from the deployment group.</p>}</div></Panel>
      <Panel title="Approval rules" description="Each rule inherits the project default until you turn on an application override.">
        <div className="space-y-4 p-5">
          <div className="space-y-3">
            <label className="flex items-center gap-2 text-xs font-medium"><Checkbox checked={Boolean(approvalPolicyOverride.sync)} disabled={approvalBusy} onCheckedChange={(checked) => toggleOverride("sync", Boolean(checked))} />Override project sync rule</label>
            {approvalPolicyOverride.sync && project && <ApprovalRuleEditor id="application-sync-approvals" title="Application sync approvals" rule={approvalPolicyOverride.sync} members={members} disabled={approvalBusy} onChange={(sync) => setApprovalPolicyOverride((current) => ({ ...current, sync }))} />}
          </div>
          <div className="space-y-3 border-t pt-4">
            <label className="flex items-center gap-2 text-xs font-medium"><Checkbox checked={Boolean(approvalPolicyOverride.deletion)} disabled={approvalBusy} onCheckedChange={(checked) => toggleOverride("deletion", Boolean(checked))} />Override project deletion rule</label>
            {approvalPolicyOverride.deletion && project && <ApprovalRuleEditor id="application-deletion-approvals" title="Application deletion approvals" rule={approvalPolicyOverride.deletion} members={members} deletion disabled={approvalBusy} onChange={(deletion) => setApprovalPolicyOverride((current) => ({ ...current, deletion }))} />}
          </div>
          <div className="flex justify-end"><Button type="button" size="sm" variant="outline" loading={approvalBusy} loadingText="Saving rules…" onClick={() => void saveApprovalOverrides()}>Save approval overrides</Button></div>
        </div>
      </Panel>
      <div className="flex justify-end gap-2"><Link href={`/applications/${applicationID}`}><Button type="button" variant="outline">Cancel</Button></Link><Button type="submit" loading={busy} loadingText="Saving application…" disabled={namespaces.length === 0}>Save application</Button></div>
    </form>}
  </>
}
