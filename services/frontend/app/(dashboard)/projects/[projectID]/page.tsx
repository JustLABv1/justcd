"use client"

import Link from "next/link"
import { useParams, useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { Tabs } from "@base-ui/react/tabs"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { ApplicationCollection } from "@/components/application-collection"
import { ActionLink, CollectionSkeleton } from "@/components/workspace-ui"
import { FormField, PageHeading, Panel } from "@/components/ui-kit"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { Application, ListResponse, Project } from "@/lib/types"

export default function ProjectDetailPage() {
  const { projectID } = useParams<{ projectID: string }>()
  const router = useRouter()
  const [project, setProject] = useState<Project | null>(null)
  const [applications, setApplications] = useState<Application[]>([])
  const [members, setMembers] = useState<{ id: string; email: string; displayName: string; role: string }[]>([])
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(true)
  const [memberEmail, setMemberEmail] = useState("")
  const [memberRole, setMemberRole] = useState("viewer")
  const [memberBusy, setMemberBusy] = useState(false)
  const [editName, setEditName] = useState("")
  const [editDescription, setEditDescription] = useState("")
  const [projectBusy, setProjectBusy] = useState(false)
  const [deletePolicy, setDeletePolicy] = useState("keep")
  const [deletionPlans, setDeletionPlans] = useState<{ applicationId: string; applicationName: string; managedResources: number }[]>([])
  const [activeTab, setActiveTab] = useState("applications")

  useEffect(() => {
    let active = true
    async function load() {
      try {
        const [projects, apps, projectMembers] = await Promise.all([
          api<ListResponse<Project>>("/api/v1/projects"),
          api<ListResponse<Application>>(`/api/v1/applications?projectId=${encodeURIComponent(projectID)}`),
          api<ListResponse<{ id: string; email: string; displayName: string; role: string }>>(`/api/v1/projects/${encodeURIComponent(projectID)}/members`),
        ])
        if (!active) return
        const selected = projects.items.find((item) => item.id === projectID) ?? null
        setProject(selected)
        setEditName(selected?.name ?? "")
        setEditDescription(selected?.description ?? "")
        setApplications(apps.items)
        setMembers(projectMembers.items)
      } catch (cause) {
        if (active) setError(cause instanceof Error ? cause.message : "Could not load project")
      } finally { if (active) setLoading(false) }
    }
    void load()
    return () => { active = false }
  }, [projectID])

  async function addMember(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setMemberBusy(true); setError("")
    try {
      await apiPost(`/api/v1/projects/${encodeURIComponent(projectID)}/members`, { email: memberEmail, role: memberRole })
      const result = await api<{ items: typeof members }>(`/api/v1/projects/${encodeURIComponent(projectID)}/members`)
      setMembers(result.items); setMemberEmail("")
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Could not update project membership") }
    finally { setMemberBusy(false) }
  }

  async function saveProject(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setProjectBusy(true); setError("")
    try {
      await api(`/api/v1/projects/${encodeURIComponent(projectID)}`, { method: "PUT", body: JSON.stringify({ name: editName, description: editDescription }) })
      setProject((current) => current ? { ...current, name: editName.trim(), description: editDescription.trim() } : current)
    } catch (cause) { setError(errorMessage(cause)) }
    finally { setProjectBusy(false) }
  }

  async function deleteProject() {
    const result = await api<{ deleted: boolean; plans?: typeof deletionPlans }>(`/api/v1/projects/${encodeURIComponent(projectID)}?resources=${deletePolicy}`, { method: "DELETE" })
    if (result.deleted) { router.push("/projects"); router.refresh(); return }
    setDeletionPlans(result.plans ?? [])
    setError("")
  }

  return <>
    <PageHeading title={project?.name ?? (loading ? "Loading project…" : "Project not found")} description={project?.description || "Manage Git-driven applications and access scoped to this project."} actions={project && <><ActionLink href={`/settings?projectId=${project.id}`} secondary>Project connections</ActionLink>{project.role !== "viewer" && <ActionLink href={`/applications/new?projectId=${project.id}`}><span aria-hidden="true">＋</span> New application</ActionLink>}</>} />
    {error && <div role="alert" className="mb-5 rounded-lg border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-destructive">{error}</div>}
    {deletionPlans.length > 0 && <div role="status" className="mb-5 rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><p className="font-medium">Deletion plans are ready for review.</p><p className="mt-1 text-xs">Approve and apply each application plan. Once all managed resources are gone, choose “Delete resources” here again to remove the project record.</p><ul className="mt-3 space-y-1">{deletionPlans.map((item) => <li key={item.applicationId}><Link href={`/applications/${item.applicationId}?tab=changes`} className="underline underline-offset-4">{item.applicationName} · {item.managedResources} resources →</Link></li>)}</ul></div>}
    {project && <div className="mb-6 flex flex-wrap gap-2 text-[11px]"><span className="rounded-full border bg-card px-3 py-1.5 capitalize text-muted-foreground">Your role: <strong className="font-medium text-foreground">{project.role}</strong></span><span className="rounded-full border bg-card px-3 py-1.5 text-muted-foreground">{applications.length} application{applications.length === 1 ? "" : "s"}</span><span className="rounded-full border bg-card px-3 py-1.5 text-muted-foreground">{members.length} member{members.length === 1 ? "" : "s"}</span></div>}
    <Tabs.Root value={activeTab} onValueChange={(value) => setActiveTab(String(value))}>
      <Tabs.List aria-label="Project views" className="mb-6 flex gap-6 overflow-x-auto border-b" activateOnFocus>
        {[["applications", "Applications", applications.length], ["members", "Members", members.length], ...(project?.role === "owner" ? [["settings", "Project settings", null]] : [])].map(([value, label, count]) => <Tabs.Tab key={String(value)} value={String(value)} className="flex shrink-0 items-center gap-2 border-b-2 border-transparent px-1 pb-3 text-xs font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-[active]:border-primary data-[active]:text-foreground">{label}{count !== null && <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] tabular-nums text-muted-foreground">{count}</span>}</Tabs.Tab>)}
      </Tabs.List>
      <Tabs.Panel value="applications" className="outline-none">
        <Panel surface="flat" title="Applications" description="Deployments reconciled by this project">
          {loading ? <CollectionSkeleton /> : <ApplicationCollection applications={applications} projectRole={project?.role} createHref={`/applications/new?projectId=${projectID}`} canCreate={project?.role !== "viewer"} onDeleted={(id) => setApplications((current) => current.filter((app) => app.id !== id))} />}
        </Panel>
      </Tabs.Panel>
      <Tabs.Panel value="members" className="max-w-3xl outline-none">
        <Panel title="Members" description="Users with access to this delivery scope">
          {project?.role === "owner" && <form className="grid gap-3 border-b p-4" onSubmit={addMember}>
            <FormField label="User email" htmlFor="member-email" hint="They must have signed in to JustCD at least once."><Input id="member-email" type="email" placeholder="teammate@example.com" value={memberEmail} onChange={(event) => setMemberEmail(event.target.value)} required /></FormField>
            <FormField label="Project role" htmlFor="member-role"><FormSelect id="member-role" size="sm" value={memberRole} onValueChange={setMemberRole} items={[{ value: "viewer", label: "Viewer" }, { value: "deployer", label: "Deployer" }, { value: "owner", label: "Owner" }]} /></FormField>
            <Button size="sm" type="submit" className="w-fit" disabled={memberBusy}>{memberBusy ? "Saving…" : "Add member"}</Button>
          </form>}
          {members.length ? <div className="divide-y">{members.map((member) => <div key={member.id} className="flex items-center gap-3 px-5 py-3"><span className="grid size-8 place-items-center rounded-full bg-muted text-[10px] font-semibold uppercase">{(member.displayName || member.email).slice(0, 2)}</span><span className="min-w-0 flex-1"><span className="block truncate text-xs font-medium">{member.displayName || member.email}</span><span className="block truncate text-[10px] text-muted-foreground">{member.email}</span></span><span className="text-[10px] capitalize text-muted-foreground">{member.role}</span></div>)}</div> : <p className="px-5 py-6 text-xs text-muted-foreground">Project owner is the only member so far.</p>}
        </Panel>
      </Tabs.Panel>
      {project?.role === "owner" && <Tabs.Panel value="settings" className="max-w-3xl space-y-5 outline-none">
        <Panel title="Project settings" description="Change the project name or remove this delivery scope."><div className="space-y-5 p-5"><form className="space-y-3" onSubmit={saveProject}><FormField label="Project name" htmlFor="edit-project-name"><Input id="edit-project-name" value={editName} onChange={(event) => setEditName(event.target.value)} required maxLength={100} /></FormField><FormField label="Description" htmlFor="edit-project-description"><Textarea id="edit-project-description" value={editDescription} onChange={(event) => setEditDescription(event.target.value)} maxLength={500} /></FormField><Button size="sm" type="submit" disabled={projectBusy}>{projectBusy ? "Saving…" : "Save project"}</Button></form><div className="border-t pt-4"><p className="mb-3 text-xs text-muted-foreground">Deleting this project removes its applications and project-scoped connections from JustCD.</p><ConfirmDisclosure trigger="Delete project" title={`Delete ${project.name}?`} description="Choose what happens to resources managed by applications in this project. This cannot be undone in JustCD." confirmLabel="Continue" onConfirm={deleteProject}><FormField label="Managed cluster resources" htmlFor="project-delete-policy"><FormSelect id="project-delete-policy" value={deletePolicy} onValueChange={setDeletePolicy} items={[{ value: "keep", label: "Keep resources in Kubernetes" }, { value: "delete", label: "Delete resources through reviewed plans" }]} /></FormField>{deletePolicy === "delete" && <p className="mt-2 text-xs text-muted-foreground">JustCD prepares deletion plans for each application with managed resources. The project is removed only after all plans have been approved, applied, and the inventory is empty.</p>}</ConfirmDisclosure></div></div></Panel>
        <div className="rounded-xl border bg-card p-5"><p className="text-xs font-semibold">Scoped by design</p><p className="mt-2 text-xs leading-5 text-muted-foreground">Git credentials, cluster targets, and namespace bindings are attached to this project. Applications cannot deploy outside those namespace bindings.</p><Link href={`/settings?projectId=${projectID}`} className="mt-3 inline-block text-xs font-medium text-primary hover:underline">Review project access →</Link></div>
      </Tabs.Panel>}
    </Tabs.Root>
  </>
}
