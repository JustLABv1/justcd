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
import { Button, buttonVariants } from "@/components/panelui/button"
import { Alert } from "@/components/panelui/alert"
import { Card } from "@/components/panelui/card"
import { Badge } from "@/components/panelui/badge"
import { ErrorDetailsButton, ErrorGuidance } from "@/components/error-details"
import { errorMessage } from "@/lib/api"
import type { Workspace } from "@/lib/types"

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
        className: "h-9 gap-2 px-3.5 text-xs [&_svg:not([class*='size-'])]:size-4",
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
  return <Alert tone="destructive" variant="plain" className="mb-6">
    <Alert.Icon /><Alert.Title>{errorMessage(error)}</Alert.Title>
    <Alert.Description><ErrorGuidance error={error} /></Alert.Description>
    <Alert.Actions><ErrorDetailsButton error={error} /><Button type="button" size="sm" variant="outline" onClick={retry}>Try again</Button></Alert.Actions>
  </Alert>
}

/** Page-level errors get an Alert; inside an existing surface use the unframed variant. */
export function ErrorNotice({ error, inline = false }: { error: unknown; inline?: boolean }) {
  return <Alert tone="destructive" variant={inline ? "inline" : "plain"} className="mb-5">
    <Alert.Icon /><Alert.Title>{errorMessage(error)}</Alert.Title>
    <Alert.Description><ErrorGuidance error={error} /></Alert.Description>
    <Alert.Actions><ErrorDetailsButton error={error} /></Alert.Actions>
  </Alert>
}

export function WorkspaceCard({
  workspace,
  total,
  synced,
  attention,
}: {
  workspace: Workspace
  total: number
  synced: number
  attention: number
}) {
  return (
    <Card render={<Link href={`/workspaces/${workspace.id}`} />} className="workspace-card group min-w-0">
      <Card.Content className="flex flex-col p-5">

      <div className="flex items-center justify-between">
        <span className="grid size-10 place-items-center rounded-xl border bg-muted/40 text-muted-foreground">
          <WorkspaceIcon name="folder" />
        </span>
        <Badge variant="secondary" className="capitalize">
          {workspace.role}
        </Badge>
      </div>
      <h3 className="mt-5 truncate text-base font-semibold tracking-tight">
        {workspace.name}
      </h3>
      <p className="mt-1.5 line-clamp-2 min-h-10 text-sm leading-5 text-muted-foreground">
        {workspace.description ||
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
        role="img"
        aria-label={`${synced} in sync, ${attention} need attention, ${total - synced - attention} other`}
      >
        {total > 0 && (
          <>
            <span
              className="bg-success"
              style={{ width: `${(synced / total) * 100}%` }}
            />
            <span
              className="bg-warning"
              style={{ width: `${(attention / total) * 100}%` }}
            />
          </>
        )}
      </div>
      <p className="mt-2 text-sm text-muted-foreground">
        {total === 0
          ? "Ready for your first application"
          : `${synced} in sync · ${attention} need attention`}
      </p>
      </Card.Content>
    </Card>
  )
}
