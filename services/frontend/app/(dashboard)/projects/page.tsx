"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { DataGridList } from "@/components/data-grid-table"
import { EmptyState, PageHeading, Panel } from "@/components/ui-kit"
import { api } from "@/lib/api"
import type { ListResponse, Project } from "@/lib/types"

export default function ProjectsPage() {
  const [items, setItems] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    api<ListResponse<Project>>("/api/v1/projects").then((result) => setItems(result.items)).finally(() => setLoading(false))
  }, [])
  return <>
    <PageHeading title="Projects" description="Keep delivery scopes, Git sources, namespace access, and team permissions organized." actions={<Link href="/projects/new"><Button><span aria-hidden="true">＋</span> New project</Button></Link>} />
    <Panel surface="flat" title="All projects" description={`${items.length} project${items.length === 1 ? "" : "s"}`}>
      {loading ? <div className="p-8 text-sm text-muted-foreground">Loading projects…</div> : items.length ? <div className="min-w-0"><DataGridList rows={items} columns={[
        { id: "name", title: "Project", cell: (project) => <Link href={`/projects/${project.id}`} className="font-medium hover:text-primary">{project.name}<span className="mt-0.5 block max-w-[440px] truncate text-[10px] font-normal text-muted-foreground">{project.description || "No description"}</span></Link> },
        { id: "role", title: "Your role", cell: (project) => <span className="text-xs capitalize text-muted-foreground">{project.role}</span> },
        { id: "created", title: "Created", cell: (project) => <span className="text-xs text-muted-foreground">{new Date(project.createdAt).toLocaleDateString()}</span> },
        { id: "open", title: "", cell: (project) => <Link href={`/projects/${project.id}`} className="text-xs text-primary">Open →</Link> },
      ]} empty="No projects yet." /></div> : <EmptyState title="Start with a project" description="A project groups the people, repositories, and namespace credentials for one delivery scope." href="/projects/new" action="Create project" />}
    </Panel>
  </>
}
