"use client"

import Link from "next/link"
import { useRouter } from "next/navigation"
import { useState } from "react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { DataGridList } from "@/components/data-grid-table"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { EmptyState, StatusBadge } from "@/components/ui-kit"
import { WorkspaceIcon } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import {
  needsAttention,
  type WorkspaceApplication,
} from "@/hooks/use-workspace"
import type { Project } from "@/lib/types"
import { api } from "@/lib/api"

function ApplicationActions({ app, canManage, onDeleted }: { app: WorkspaceApplication; canManage: boolean; onDeleted?: (id: string) => void }) {
  const router = useRouter()
  const toast = useToast()
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [policy, setPolicy] = useState("keep")
  async function remove() {
    const result = await api<{ deleted: boolean }>(`/api/v1/applications/${encodeURIComponent(app.id)}?resources=${policy}`, { method: "DELETE" })
    if (result.deleted) {
      toast.success(`${app.name} deleted.`)
      onDeleted?.(app.id)
    } else {
      toast.info("Deletion plan created. Review it before applying.")
      router.push(`/applications/${app.id}?tab=changes`)
    }
  }
  return <>
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="icon" aria-label={`Actions for ${app.name}`} title={`Actions for ${app.name}`} />}><span className="text-lg leading-none" aria-hidden="true">⋯</span></DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-36">
        <DropdownMenuItem onClick={() => router.push(`/applications/${app.id}`)}>View application</DropdownMenuItem>
        {canManage && <><DropdownMenuItem onClick={() => router.push(`/applications/${app.id}/edit`)}>Edit application</DropdownMenuItem><DropdownMenuItem variant="destructive" onClick={() => setDeleteOpen(true)}>Delete application</DropdownMenuItem></>}
      </DropdownMenuContent>
    </DropdownMenu>
    <ConfirmDisclosure open={deleteOpen} onOpenChange={setDeleteOpen} title={`Delete ${app.name}?`} description="Choose whether JustCD keeps the managed Kubernetes resources or prepares a deletion plan for review." confirmLabel="Continue" onConfirm={remove}>
      <FormSelect ariaLabel="Managed cluster resources" value={policy} onValueChange={setPolicy} items={[{ value: "keep", label: "Keep resources in Kubernetes" }, { value: "delete", label: "Delete through a reviewed plan" }]} />
      {policy === "delete" && <p className="mt-2 text-xs text-muted-foreground">Resources are not deleted immediately. Review and approve the deletion plan on the application page.</p>}
    </ConfirmDisclosure>
  </>
}

export function ApplicationCard({ app, canManage, onDeleted }: { app: WorkspaceApplication; canManage?: boolean; onDeleted?: (id: string) => void }) {
  const namespaces = app.namespaces.map((item) => item.namespace).join(", ")
  const revision = /^[a-f0-9]{40}$|^[a-f0-9]{64}$/i.test(app.revision)
    ? app.revision.slice(0, 8)
    : app.revision

  return (
    <article className="flex min-w-0 flex-col rounded-xl border bg-card p-5">
      <header className="flex items-start justify-between gap-3">
        <div className="min-w-0 pt-1">
          <h3 className="truncate text-base font-semibold tracking-tight" title={app.name}>
            <Link href={`/applications/${app.id}`} className="rounded-sm hover:underline underline-offset-4">
              {app.name}
            </Link>
          </h3>
          <p className="mt-1 truncate text-xs text-muted-foreground" title={app.projectName || app.projectId}>
            {app.projectName || app.projectId}
          </p>
        </div>
        <ApplicationActions app={app} canManage={Boolean(canManage)} onDeleted={onDeleted} />
      </header>

      <div className="mt-4">
        <StatusBadge status={app.health} />
        {app.statusIssues?.length > 0 && <p className="mt-2 text-xs leading-5 text-destructive" title={app.statusIssues.map((issue) => issue.summary).join("\n")}>{app.statusIssues.map((issue) => issue.source === "git" ? "Git" : issue.source === "cluster" ? "Cluster" : issue.source).join(" + ")} check failed · <Link href={`/applications/${app.id}`} className="underline underline-offset-2">Details</Link></p>}
      </div>

      <dl className="my-5 grid grid-cols-[70px_minmax(0,1fr)] items-baseline gap-x-3 gap-y-2.5 text-xs">
        <dt className="text-muted-foreground">Path</dt>
        <dd className="truncate font-mono" title={app.manifestPath || "."}>
          {app.manifestPath || "."}
        </dd>
        <dt className="text-muted-foreground">Revision</dt>
        <dd className="truncate font-mono" title={app.revision}>
          {revision}
        </dd>
        <dt className="text-muted-foreground">Namespace</dt>
        <dd className="truncate" title={namespaces || "No namespace"}>
          {namespaces || "No namespace"}
        </dd>
      </dl>

      <footer className="mt-auto flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t pt-3 text-xs text-muted-foreground">
        <span>{app.renderer === "helm" ? "Helm" : app.renderer === "kustomize" ? "Kustomize" : app.renderer}</span>
        <span>{app.syncPolicy === "auto-safe" ? "Auto-safe sync" : "Manual sync"}</span>
      </footer>
    </article>
  )
}

export function ApplicationCollection({
  applications,
  projects,
  initialFilter = "all",
  createHref = "/applications/new",
  canCreate = true,
  projectRole,
  onDeleted,
}: {
  applications: WorkspaceApplication[]
  projects?: Project[]
  initialFilter?: string
  createHref?: string
  canCreate?: boolean
  projectRole?: Project["role"]
  onDeleted?: (id: string) => void
}) {
  const [query, setQuery] = useState("")
  const [filter, setFilter] = useState(initialFilter)
  const [projectId, setProjectId] = useState("all")
  const [view, setView] = useState<"cards" | "table">("cards")
  const [sort, setSort] = useState("attention")
  const [removedIds, setRemovedIds] = useState<string[]>([])
  const handleDeleted = (id: string) => { setRemovedIds((current) => [...current, id]); onDeleted?.(id) }
  const available = applications.filter((app) => !removedIds.includes(app.id))
  const counts = {
    all: available.length,
    attention: available.filter(needsAttention).length,
    synced: available.filter((app) => app.health === "synced").length,
    other: available.filter(
      (app) => !needsAttention(app) && app.health !== "synced"
    ).length,
  }
  const visible = available
    .filter((app) => {
      const matchesStatus =
        filter === "all" ||
        (filter === "attention"
          ? needsAttention(app)
          : filter === "synced"
            ? app.health === "synced"
            : !needsAttention(app) && app.health !== "synced")
      return (
        matchesStatus &&
        (projectId === "all" || app.projectId === projectId) &&
        `${app.name} ${app.projectName ?? ""} ${app.namespaces.map((item) => item.namespace).join(" ")} ${app.revision}`
          .toLowerCase()
          .includes(query.trim().toLowerCase())
      )
    })
    .sort((a, b) =>
      sort === "name"
        ? a.name.localeCompare(b.name)
        : sort === "newest"
          ? new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
          : Number(needsAttention(b)) - Number(needsAttention(a)) ||
            a.name.localeCompare(b.name)
    )

  if (!available.length)
    return (
      <div className="rounded-2xl border border-dashed bg-card">
        <EmptyState
          title="Your next deployment starts here"
          description="Connect a Git repository and a Kubernetes target to bring your first application online."
          href={canCreate ? createHref : undefined}
          action="Create application"
        />
      </div>
    )
  return (
    <div>
      <div className="mb-5 flex flex-col justify-between gap-4 border-b pb-4 min-[1100px]:flex-row min-[1100px]:items-center">
        <div
          className="flex flex-wrap gap-1"
          role="group"
          aria-label="Filter by health"
        >
          {(
            [
              ["all", "All apps"],
              ["attention", "Needs attention"],
              ["synced", "In sync"],
              ["other", "Other"],
            ] as const
          ).map(([value, label]) => (
            <button
              key={value}
              type="button"
              aria-pressed={filter === value}
              onClick={() => setFilter(value)}
              className={`flex min-h-9 items-center gap-2 rounded-lg px-3 text-xs font-medium transition-colors ${filter === value ? "border border-foreground bg-foreground text-background" : "border border-border bg-card text-muted-foreground hover:bg-muted hover:text-foreground"}`}
            >
              {label}
              <span
                className={`rounded px-1.5 py-0.5 text-[10px] tabular-nums ${filter === value ? "bg-background/15" : "bg-muted"}`}
              >
                {counts[value]}
              </span>
            </button>
          ))}
        </div>
        <div
          className="flex w-fit gap-1 rounded-lg border bg-card p-1"
          role="group"
          aria-label="Application view"
        >
          {(["cards", "table"] as const).map((value) => (
            <button
              type="button"
              key={value}
              aria-pressed={view === value}
              onClick={() => setView(value)}
              className={`rounded-md px-3 py-1.5 text-xs capitalize ${view === value ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground"}`}
            >
              {value}
            </button>
          ))}
        </div>
      </div>
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <div className="relative min-w-48 flex-1">
          <WorkspaceIcon
            name="search"
            className="pointer-events-none absolute top-2.5 left-3 size-4 text-muted-foreground"
          />
          <Input
            aria-label="Search applications"
            placeholder="Search applications, namespaces, revisions…"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            className="h-9 bg-card pl-9"
          />
        </div>
        {projects && (
          <FormSelect
            ariaLabel="Filter by project"
            value={projectId}
            onValueChange={setProjectId}
            className="w-44"
            items={[
              { value: "all", label: "All projects" },
              ...projects.map((project) => ({
                value: project.id,
                label: project.name,
              })),
            ]}
          />
        )}
        <FormSelect
          ariaLabel="Sort applications"
          value={sort}
          onValueChange={setSort}
          className="w-40"
          items={[
            { value: "attention", label: "Attention first" },
            { value: "name", label: "Name A–Z" },
            { value: "newest", label: "Newest first" },
          ]}
        />
      </div>
      <p role="status" className="mb-3 text-xs text-muted-foreground">
        {visible.length} of {available.length} applications
      </p>
      {visible.length ? (
        view === "cards" ? (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,280px),1fr))] gap-4">
            {visible.map((app) => (
              <ApplicationCard key={app.id} app={app} canManage={projectRole === "owner" || projects?.some((project) => project.id === app.projectId && project.role === "owner")} onDeleted={handleDeleted} />
            ))}
          </div>
        ) : (
          <DataGridList
            rows={visible}
            columns={[
              {
                id: "name",
                title: "Application",
                size: 230,
                cell: (app) => (
                  <Link
                    href={`/applications/${app.id}`}
                    className="block truncate font-medium hover:text-primary"
                  >
                    {app.name}
                    <span className="block truncate text-xs font-normal text-muted-foreground">
                      {app.projectName || app.manifestPath}
                    </span>
                  </Link>
                ),
              },
              {
                id: "health",
                title: "Health",
                size: 150,
                cell: (app) => <div className="space-y-1"><StatusBadge status={app.health} />{app.statusIssues?.length > 0 && <span className="block text-xs text-destructive">{app.statusIssues.map((issue) => issue.source === "git" ? "Git" : issue.source === "cluster" ? "Cluster" : issue.source).join(" + ")} check failed</span>}</div>,
              },
              {
                id: "target",
                title: "Namespace",
                size: 160,
                cell: (app) => (
                  <span className="block truncate text-xs">
                    {app.namespaces.map((item) => item.namespace).join(", ") ||
                      "—"}
                  </span>
                ),
              },
              {
                id: "revision",
                title: "Revision",
                size: 130,
                cell: (app) => (
                  <span className="block truncate font-mono text-xs">
                    {app.revision}
                  </span>
                ),
              },
              {
                id: "policy",
                title: "Sync policy",
                size: 120,
                cell: (app) => (
                  <span className="text-xs">{app.syncPolicy}</span>
                ),
              },
              {
                id: "actions",
                title: "",
                size: 64,
                cell: (app) => <ApplicationActions app={app} canManage={projectRole === "owner" || Boolean(projects?.some((project) => project.id === app.projectId && project.role === "owner"))} onDeleted={handleDeleted} />,
              },
            ]}
          />
        )
      ) : (
        <div className="rounded-2xl border border-dashed bg-card pb-6 text-center">
          <EmptyState
            title="No matching applications"
            description="Try another search or clear the filters to see all applications."
          />
          <Button
            variant="outline"
            onClick={() => {
              setQuery("")
              setFilter("all")
              setProjectId("all")
            }}
          >
            Clear filters
          </Button>
        </div>
      )}
    </div>
  )
}
