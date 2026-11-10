import Link from "next/link"
import type { ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Layers01Icon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Card } from "@/components/panelui/card"

export function PageHeading({
  title,
  description,
  actions,
  badge,
  metadata,
}: {
  title: string
  description?: ReactNode
  actions?: ReactNode
  badge?: ReactNode
  metadata?: ReactNode
}) {
  return (
    <header className="mb-5 flex shrink-0 flex-wrap items-start justify-between gap-x-6 gap-y-4">
      <div className="min-w-0 flex-1 basis-64">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <h1 className="min-w-0 break-words text-2xl font-semibold leading-tight tracking-tight sm:text-[28px]">{title}</h1>
          {badge}
        </div>
        {description && <p className="mt-1.5 max-w-2xl break-words text-sm leading-6 text-muted-foreground">{description}</p>}
        {metadata && <div className="mt-2">{metadata}</div>}
      </div>
      {actions && <div className="flex max-w-full flex-wrap items-center gap-2">{actions}</div>}
    </header>
  )
}

export type StatusTone = "success" | "destructive" | "warning" | "neutral"

/** Maps a free-form status string (sync, health, check result, …) to a semantic tone. Matching is substring based on purpose. */
export function statusTone(status: string): StatusTone {
  const normalized = status.toLowerCase().replaceAll("_", " ")
  if (normalized.includes("synced") || normalized.includes("succeeded") || normalized === "healthy" || normalized === "true") return "success"
  if (normalized.includes("delet") || normalized.includes("failed") || normalized.includes("degrad") || normalized.includes("error") || normalized === "missing" || normalized === "false") return "destructive"
  if (normalized.includes("sync") || normalized.includes("pending") || normalized.includes("running") || normalized.includes("out of") || normalized.includes("progress") || normalized === "partial" || normalized === "suspended" || normalized.includes("paused")) return "warning"
  return "neutral"
}

const statusBadgeVariant = { success: "success", destructive: "destructive", warning: "warning", neutral: "secondary" } as const

export function StatusBadge({ status, tone }: { status: string; tone?: StatusTone }) {
  const normalized = status.toLowerCase().replaceAll("_", " ")
  return <Badge variant={statusBadgeVariant[tone ?? statusTone(status)]} dot className="capitalize">{normalized}</Badge>
}

export function EmptyState({
  title,
  description,
  href,
  action = "Get started",
  onAction,
  actionVariant = "default",
}: {
  title: string
  description: string
  href?: string
  action?: string
  onAction?: () => void
  actionVariant?: "default" | "outline"
}) {
  return (
    <div className="flex flex-col items-center justify-center px-5 py-12 text-center">
      <span aria-hidden="true" className="inline-flex shrink-0 items-center justify-center rounded-2xl bg-primary/10 p-3 empty-state-icon-stack text-primary"><HugeiconsIcon icon={Layers01Icon} className="size-6" aria-hidden="true" /></span>
      <h3 className="mt-4 text-sm font-semibold">{title}</h3>
      <p className="mt-1 max-w-sm text-sm leading-5 text-muted-foreground">{description}</p>
      {href ? <Button render={<Link href={href} />} nativeButton={false} variant={actionVariant} className="mt-4">{action}</Button>
        : onAction ? <Button type="button" variant={actionVariant} className="mt-4" onClick={onAction}>{action}</Button> : null}
    </div>
  )
}

export { FormField } from "@/components/form-field"

export function InlineLink({ href, children }: { href: string; children: ReactNode }) {
  return <Button render={<Link href={href} />} nativeButton={false} variant="ghost" size="sm" className="-mx-2 text-primary hover:text-primary">{children}</Button>
}

/** A single list entry for connection-style inventories (credentials, clusters, Git sources, …). */
export function ConnectionRow({ icon, media, title, subtitle, badges, meta, actions, children }: {
  icon?: ReactNode
  media?: ReactNode
  title: ReactNode
  subtitle?: ReactNode
  badges?: ReactNode
  meta?: ReactNode
  actions?: ReactNode
  children?: ReactNode
}) {
  return (
    <Card as="li" size="sm">
      <Card.Content className="px-5 py-4">
      <div className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-4 gap-y-3 sm:flex sm:flex-wrap">
        {media ?? (icon && <span aria-hidden="true" className="grid size-9 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">{icon}</span>)}
        <div className={`min-w-0 sm:flex-1 sm:basis-56 ${icon || media ? "" : "col-span-2"}`}>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h3 className="truncate text-sm font-medium">{title}</h3>
            {badges}
          </div>
          {subtitle && <p className="mt-0.5 truncate font-mono text-xs text-muted-foreground">{subtitle}</p>}
          {meta && <div className="mt-1 text-xs text-muted-foreground">{meta}</div>}
        </div>
        {actions && <div className="col-span-2 justify-self-end sm:contents">{actions}</div>}
      </div>
      {children && <div className="mt-3">{children}</div>}
      </Card.Content>
    </Card>
  )
}

export { CheckboxCard, CheckboxField, SwitchField } from "@/components/boolean-field"
