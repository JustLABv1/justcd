"use client"

import { useState } from "react"
import { Input } from "@/components/ui/input"
import { EmptyState, PageHeading } from "@/components/ui-kit"
import {
  ActionLink,
  CollectionSkeleton,
  LoadError,
  ProjectCard,
  WorkspaceIcon,
} from "@/components/workspace-ui"
import { needsAttention, useWorkspace } from "@/hooks/use-workspace"

export default function ProjectsPage() {
  const { projects, applications, loading, error, refresh } = useWorkspace()
  const [query, setQuery] = useState("")
  const visible = projects.filter((project) =>
    `${project.name} ${project.description}`
      .toLowerCase()
      .includes(query.trim().toLowerCase())
  )
  return (
    <>
      <p className="mb-2 text-[10px] font-semibold tracking-[0.18em] text-muted-foreground uppercase">
        A home for every deployment
      </p>
      <PageHeading
        title="Projects"
        description="Bring applications, connections, and people together."
        actions={
          <ActionLink href="/projects/new">
            <span aria-hidden="true">＋</span> New project
          </ActionLink>
        }
      />
      {error ? (
        <LoadError error={error} retry={refresh} />
      ) : loading ? (
        <CollectionSkeleton />
      ) : (
        <>
          <div className="mb-7 grid gap-6 rounded-2xl border bg-card p-6 sm:grid-cols-[1fr_auto] sm:items-center">
            <div className="flex items-start gap-4">
              <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-primary/7 text-primary">
                <WorkspaceIcon name="folder" />
              </span>
              <div>
                <h2 className="text-sm font-semibold">
                  Your workspace, organized.
                </h2>
                <p className="mt-1 max-w-lg text-xs leading-5 text-muted-foreground">
                  Each project keeps its own Git sources, deployment targets,
                  and team permissions together.
                </p>
              </div>
            </div>
            <div className="flex gap-8 sm:border-l sm:pl-8">
              <div>
                <p className="text-2xl font-semibold tabular-nums">
                  {projects.length}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">Projects</p>
              </div>
              <div>
                <p className="text-2xl font-semibold tabular-nums">
                  {applications.length}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  Applications
                </p>
              </div>
            </div>
          </div>
          {projects.length ? (
            <>
              <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
                <h2 className="text-sm font-semibold">
                  All projects{" "}
                  <span className="ml-2 font-normal text-muted-foreground">
                    {projects.length}
                  </span>
                </h2>
                <div className="relative w-full sm:w-72">
                  <WorkspaceIcon
                    name="search"
                    className="pointer-events-none absolute top-2.5 left-3 size-4 text-muted-foreground"
                  />
                  <Input
                    className="h-9 bg-card pl-9"
                    aria-label="Search projects"
                    placeholder="Find a project…"
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                  />
                </div>
              </div>
              {visible.length ? (
                <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                  {visible.map((project) => {
                    const apps = applications.filter(
                      (app) => app.projectId === project.id
                    )
                    return (
                      <ProjectCard
                        key={project.id}
                        project={project}
                        total={apps.length}
                        synced={
                          apps.filter((app) => app.health === "synced").length
                        }
                        attention={apps.filter(needsAttention).length}
                      />
                    )
                  })}
                </div>
              ) : (
                <EmptyState
                  title="No projects found"
                  description="Try another project name or description."
                />
              )}
            </>
          ) : (
            <div className="rounded-2xl border border-dashed bg-card">
              <EmptyState
                title="Make room for your first application"
                description="Create a project to connect repositories, choose deployment targets, and work with your team."
                href="/projects/new"
                action="Create project"
              />
            </div>
          )}
        </>
      )}
    </>
  )
}
