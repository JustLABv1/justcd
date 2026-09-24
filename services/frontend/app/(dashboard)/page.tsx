"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { DataGridList } from "@/components/data-grid-table"
import { EmptyState, PageHeading, Panel, StatCard, StatusBadge } from "@/components/ui-kit"
import { api } from "@/lib/api"
import type { Application, ListResponse, Project } from "@/lib/types"

type AppRow = Application & { projectName: string }

export default function OverviewPage() {
  const [projects, setProjects] = useState<Project[]>([])
  const [applications, setApplications] = useState<AppRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")

  useEffect(() => {
    let active = true
    async function load() {
      try {
        const result = await api<ListResponse<Project>>("/api/v1/projects")
        const grouped = await Promise.all(result.items.map(async (project) => {
          const response = await api<ListResponse<Application>>(`/api/v1/applications?projectId=${encodeURIComponent(project.id)}`)
          return response.items.map((app) => ({ ...app, projectName: project.name }))
        }))
        if (active) {
          setProjects(result.items)
          setApplications(grouped.flat())
        }
      } catch (cause) {
        if (active) setError(cause instanceof Error ? cause.message : "Could not load workspace")
      } finally {
        if (active) setLoading(false)
      }
    }
    void load()
    return () => { active = false }
  }, [])

  const drifted = applications.filter((app) => ["out_of_sync", "deletion_pending", "degraded"].includes(app.health)).length
  const synced = applications.filter((app) => app.health === "synced").length

  return (
    <>
      <PageHeading
        title="Good deployments start with a clear plan."
        description="A live view of your Git-managed Kubernetes applications, their health, and changes that need your attention."
        actions={<><Link href="/projects/new"><Button variant="outline">New project</Button></Link><Link href="/applications/new"><Button><span aria-hidden="true">＋</span> New application</Button></Link></>}
      />
      {error && <div role="alert" className="mb-5 rounded-lg border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-destructive">{error}</div>}
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Projects" value={loading ? "—" : projects.length} note="Organized delivery scopes" icon="▦" />
        <StatCard label="Applications" value={loading ? "—" : applications.length} note="Git sources tracked" icon="◈" accent="bg-violet-500/10 text-violet-600" />
        <StatCard label="In sync" value={loading ? "—" : synced} note={applications.length ? `${Math.round((synced / applications.length) * 100)}% of applications` : "No applications yet"} icon="✓" accent="bg-emerald-500/10 text-emerald-600" />
        <StatCard label="Needs review" value={loading ? "—" : drifted} note="Diff, drift, or deletion approval" icon="!" accent="bg-amber-500/10 text-amber-700" />
      </div>

      <div className="mt-7 grid gap-5 xl:grid-cols-[minmax(0,1fr)_330px]">
        <Panel surface="flat" title="Applications" description="Recent health and reconciliation state" action={<Link href="/applications/new" className="text-xs font-medium text-primary hover:underline">Create app →</Link>}>
          {loading ? <div className="px-5 py-12 text-center text-sm text-muted-foreground">Loading applications…</div> : applications.length ? (
            <div className="min-w-0">
              <DataGridList
                rows={applications}
                columns={[
                  { id: "app", title: "Application", size: 265, cell: (app) => <Link href={`/applications/${app.id}`} title={app.name} className="block min-w-0 font-medium text-foreground hover:text-primary"><span className="block truncate">{app.name}</span><span className="mt-0.5 block truncate font-normal text-[10px] text-muted-foreground">{app.projectName} · {app.renderer}</span></Link> },
                  { id: "target", title: "Target", size: 130, cell: (app) => <span className="text-xs text-muted-foreground">{app.namespaces.map((item) => item.namespace).join(", ") || "—"}</span> },
                  { id: "revision", title: "Revision", size: 145, cell: (app) => <span title={app.lastSyncedRevision || app.revision} className="block max-w-32 truncate font-mono text-[11px] text-muted-foreground">{app.lastSyncedRevision || app.revision}</span> },
                  { id: "health", title: "Health", size: 150, cell: (app) => <StatusBadge status={app.health} /> },
                  { id: "policy", title: "Policy", size: 105, cell: (app) => <span className="text-[11px] capitalize text-muted-foreground">{app.syncPolicy.replace("-", " ")}</span> },
                ]}
                empty="No applications in this workspace yet."
              />
            </div>
          ) : <EmptyState title="Nothing deployed yet" description="Create a project, connect a Git source, then add your first application." href="/projects/new" action="Create project" />}
        </Panel>

        <div className="space-y-5">
          <Panel title="Your projects" description="Projects you can access" action={<Link href="/projects" className="text-xs text-primary hover:underline">View all</Link>}>
            {loading ? <div className="px-5 py-8 text-xs text-muted-foreground">Loading projects…</div> : projects.length ? (
              <div className="divide-y">
                {projects.slice(0, 5).map((project) => <Link key={project.id} href={`/projects/${project.id}`} className="flex items-center gap-3 px-5 py-3.5 transition-colors hover:bg-muted/40"><span className="grid size-8 place-items-center rounded-lg border bg-background text-xs font-semibold text-primary">{project.name.slice(0, 1).toUpperCase()}</span><span className="min-w-0 flex-1"><span className="block truncate text-xs font-medium">{project.name}</span><span className="block truncate text-[10px] text-muted-foreground">{project.description || "No description"}</span></span><span className="text-[10px] capitalize text-muted-foreground">{project.role}</span></Link>)}
              </div>
            ) : <EmptyState title="No projects" description="Projects keep credentials, sources, and access scopes together." href="/projects/new" action="Create first project" />}
          </Panel>
          <div className="overflow-hidden rounded-xl bg-[#15243b] p-5 text-white">
            <span className="inline-flex rounded-full border border-white/15 bg-white/5 px-2 py-1 text-[9px] font-medium uppercase tracking-wider text-blue-200">Safe by default</span>
            <h2 className="mt-3 text-sm font-semibold">Review before it changes</h2>
            <p className="mt-2 text-xs leading-5 text-slate-300">Every sync starts with an immutable plan. Deletions and cluster-wide changes stay paused until an owner explicitly approves them.</p>
            <div className="mt-4 flex items-center gap-2 text-[10px] text-slate-400"><span className="size-1.5 rounded-full bg-emerald-400" /> No CRDs, controllers, or operators installed</div>
          </div>
        </div>
      </div>
    </>
  )
}
