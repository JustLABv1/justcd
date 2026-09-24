"use client"

import Link from "next/link"
import { useParams } from "next/navigation"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { DataGridList } from "@/components/data-grid-table"
import { EmptyState, FormField, PageHeading, Panel, StatusBadge } from "@/components/ui-kit"
import { api, apiPost } from "@/lib/api"
import type { Application, ListResponse, Project } from "@/lib/types"

export default function ProjectDetailPage() {
  const { projectID } = useParams<{ projectID: string }>()
  const [project, setProject] = useState<Project | null>(null)
  const [applications, setApplications] = useState<Application[]>([])
  const [members, setMembers] = useState<{ id: string; email: string; displayName: string; role: string }[]>([])
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(true)
  const [memberEmail, setMemberEmail] = useState("")
  const [memberRole, setMemberRole] = useState("viewer")
  const [memberBusy, setMemberBusy] = useState(false)

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
        setProject(projects.items.find((item) => item.id === projectID) ?? null)
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

  return <>
    <PageHeading eyebrow="Project" title={project?.name ?? (loading ? "Loading project…" : "Project not found")} description={project?.description || "Manage Git-driven applications and access scoped to this project."} actions={project && <><Link href={`/settings?projectId=${project.id}`}><Button variant="outline">Project connections</Button></Link><Link href={`/applications/new?projectId=${project.id}`}><Button disabled={project.role === "viewer"}><span aria-hidden="true">＋</span> New application</Button></Link></>} />
    {error && <div role="alert" className="mb-5 rounded-lg border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-destructive">{error}</div>}
    {project && <div className="mb-6 flex flex-wrap gap-2 text-[11px]"><span className="rounded-full border bg-card px-3 py-1.5 capitalize text-muted-foreground">Your role: <strong className="font-medium text-foreground">{project.role}</strong></span><span className="rounded-full border bg-card px-3 py-1.5 text-muted-foreground">{applications.length} application{applications.length === 1 ? "" : "s"}</span><span className="rounded-full border bg-card px-3 py-1.5 text-muted-foreground">{members.length} member{members.length === 1 ? "" : "s"}</span></div>}
    <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_330px]">
      <Panel title="Applications" description="Deployments reconciled by this project">
        {loading ? <div className="p-8 text-sm text-muted-foreground">Loading applications…</div> : applications.length ? <div className="p-4"><DataGridList rows={applications} columns={[
          { id: "name", title: "Application", cell: (app) => <Link href={`/applications/${app.id}`} className="font-medium hover:text-primary">{app.name}<span className="mt-0.5 block text-[10px] font-normal text-muted-foreground">{app.renderer} · {app.manifestPath}</span></Link> },
          { id: "target", title: "Target namespaces", cell: (app) => <span className="text-xs text-muted-foreground">{app.namespaces.map((item) => item.namespace).join(", ")}</span> },
          { id: "revision", title: "Revision", cell: (app) => <span className="font-mono text-[11px] text-muted-foreground">{app.revision}</span> },
          { id: "health", title: "Health", cell: (app) => <StatusBadge status={app.health} /> },
        ]} empty="No applications." /></div> : <EmptyState title="No apps in this project" description="Connect a source and namespace credential, then create an application from a Git path." href={`/settings?projectId=${projectID}`} action="Configure project" />}
      </Panel>
      <div className="space-y-5">
        <Panel title="Members" description="Users with access to this delivery scope">
          {project?.role === "owner" && <form className="grid gap-3 border-b p-4 sm:grid-cols-[1fr_140px_auto] sm:items-end" onSubmit={addMember}>
            <FormField label="User email" htmlFor="member-email" hint="They must have signed in to JustCD at least once."><Input id="member-email" type="email" placeholder="teammate@example.com" value={memberEmail} onChange={(event) => setMemberEmail(event.target.value)} required /></FormField>
            <FormField label="Project role" htmlFor="member-role"><select id="member-role" className="h-8 w-full rounded-lg border bg-background px-2 text-xs" value={memberRole} onChange={(event) => setMemberRole(event.target.value)}><option value="viewer">Viewer</option><option value="deployer">Deployer</option><option value="owner">Owner</option></select></FormField>
            <Button size="sm" type="submit" disabled={memberBusy}>{memberBusy ? "Saving…" : "Add member"}</Button>
          </form>}
          {members.length ? <div className="divide-y">{members.map((member) => <div key={member.id} className="flex items-center gap-3 px-5 py-3"><span className="grid size-8 place-items-center rounded-full bg-muted text-[10px] font-semibold uppercase">{(member.displayName || member.email).slice(0, 2)}</span><span className="min-w-0 flex-1"><span className="block truncate text-xs font-medium">{member.displayName || member.email}</span><span className="block truncate text-[10px] text-muted-foreground">{member.email}</span></span><span className="text-[10px] capitalize text-muted-foreground">{member.role}</span></div>)}</div> : <p className="px-5 py-6 text-xs text-muted-foreground">Project owner is the only member so far.</p>}
        </Panel>
        <div className="rounded-xl border bg-card p-5"><p className="text-xs font-semibold">Scoped by design</p><p className="mt-2 text-xs leading-5 text-muted-foreground">Git credentials, cluster targets, and namespace bindings are attached to this project. Applications cannot deploy outside those namespace bindings.</p><Link href={`/settings?projectId=${projectID}`} className="mt-3 inline-block text-xs font-medium text-primary hover:underline">Review project access →</Link></div>
      </div>
    </div>
  </>
}
