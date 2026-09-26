import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  Folder01Icon,
  ArrowRight01Icon,
  Layers01Icon,
  GitBranchIcon,
  Search01Icon,
  Settings02Icon,
  Shield01Icon,
  ServerStack01Icon,
} from "@hugeicons/core-free-icons"
import { buttonVariants } from "@/components/ui/button"
import { ErrorDetailsButton, ErrorGuidance } from "@/components/error-details"
import { errorMessage } from "@/lib/api"
import type { Project } from "@/lib/types"

const icons = {
  folder: Folder01Icon,
  arrow: ArrowRight01Icon,
  app: Layers01Icon,
  branch: GitBranchIcon,
  search: Search01Icon,
  settings: Settings02Icon,
  shield: Shield01Icon,
  server: ServerStack01Icon,
}
export function WorkspaceIcon({
  name,
  className = "size-5",
}: {
  name: keyof typeof icons
  className?: string
}) {
  return (
    <HugeiconsIcon
      icon={icons[name]}
      strokeWidth={1.7}
      className={className}
      aria-hidden="true"
    />
  )
}

export function ActionLink({
  href,
  children,
  secondary = false,
}: {
  href: string
  children: React.ReactNode
  secondary?: boolean
}) {
  return (
    <Link
      href={href}
      className={buttonVariants({
        variant: secondary ? "outline" : "default",
        className: "h-9 gap-2 px-3.5 text-xs",
      })}
    >
      {children}
    </Link>
  )
}

export function CollectionSkeleton() {
  return (
    <div
      role="status"
      aria-label="Loading workspace"
      className="grid gap-4 md:grid-cols-2 xl:grid-cols-3"
    >
      {[0, 1, 2].map((item) => (
        <div
          key={item}
          className="h-56 animate-pulse rounded-2xl border bg-muted/50 motion-reduce:animate-none"
        />
      ))}
    </div>
  )
}

export function LoadError({
  error,
  retry,
}: {
  error: unknown
  retry: () => void
}) {
  return (
    <div
      role="alert"
      className="mb-6 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive"
    >
      <span className="min-w-0 flex-1">{errorMessage(error)}</span>
      <div className="flex items-center gap-2">
        <ErrorDetailsButton error={error} />
        <button
          type="button"
          onClick={retry}
          className="rounded-md px-2 py-1 font-medium underline underline-offset-4"
        >
          Try again
        </button>
      </div>
      <div className="basis-full">
        <ErrorGuidance error={error} />
      </div>
    </div>
  )
}

export function ErrorNotice({ error }: { error: unknown }) {
  return (
    <div
      role="alert"
      className="mb-5 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-destructive"
    >
      <span className="min-w-0 flex-1">{errorMessage(error)}</span>
      <ErrorDetailsButton error={error} />
      <div className="basis-full">
        <ErrorGuidance error={error} />
      </div>
    </div>
  )
}

export function ProjectCard({
  project,
  total,
  synced,
  attention,
}: {
  project: Project
  total: number
  synced: number
  attention: number
}) {
  return (
    <Link
      href={`/projects/${project.id}`}
      className="workspace-card group flex min-w-0 flex-col rounded-2xl border bg-card p-5"
    >
      <div className="flex items-center justify-between">
        <span className="grid size-10 place-items-center rounded-xl border bg-muted/40 text-muted-foreground">
          <WorkspaceIcon name="folder" />
        </span>
        <span className="rounded-md bg-muted px-2 py-1 text-[11px] text-muted-foreground capitalize">
          {project.role}
        </span>
      </div>
      <h3 className="mt-5 truncate text-base font-semibold tracking-tight">
        {project.name}
      </h3>
      <p className="mt-1.5 line-clamp-2 min-h-10 text-xs leading-5 text-muted-foreground">
        {project.description ||
          "Applications, connections, and team access in one place."}
      </p>
      <div className="mt-6 flex items-center justify-between text-xs">
        <span>
          <strong className="font-semibold tabular-nums">{total}</strong>{" "}
          <span className="text-muted-foreground">
            application{total === 1 ? "" : "s"}
          </span>
        </span>
        <WorkspaceIcon
          name="arrow"
          className="size-4 text-muted-foreground group-hover:text-primary"
        />
      </div>
      <div
        className="mt-3 flex h-1 overflow-hidden rounded-full bg-muted"
        aria-label={`${synced} in sync, ${attention} need attention, ${total - synced - attention} other`}
      >
        {total > 0 && (
          <>
            <span
              className="bg-emerald-500"
              style={{ width: `${(synced / total) * 100}%` }}
            />
            <span
              className="bg-amber-500"
              style={{ width: `${(attention / total) * 100}%` }}
            />
          </>
        )}
      </div>
      <p className="mt-2 text-[11px] text-muted-foreground">
        {total === 0
          ? "Ready for your first application"
          : `${synced} in sync · ${attention} need attention`}
      </p>
    </Link>
  )
}
