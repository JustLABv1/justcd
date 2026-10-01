import Link from "next/link"
import type { ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Layers01Icon } from "@hugeicons/core-free-icons"
import { IconStack } from "@/components/reui/icon-stack"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Switch } from "@/components/ui/switch"

export function PageHeading({
  title,
  description,
  actions,
  badge,
}: {
  title: string
  description?: string
  actions?: ReactNode
  badge?: ReactNode
}) {
  return (
    <header className="mb-5 flex shrink-0 flex-wrap items-center justify-between gap-x-6 gap-y-4">
      <div className="min-w-0 flex-1 basis-64">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <h1 className="min-w-0 break-words text-2xl font-semibold leading-tight tracking-tight sm:text-[28px]">{title}</h1>
          {badge}
        </div>
        {description && <p className="mt-1.5 max-w-2xl break-words text-sm leading-6 text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex max-w-full flex-wrap items-center gap-2">{actions}</div>}
    </header>
  )
}

export function Panel({
  title,
  description,
  action,
  children,
  className = "",
  surface = "card",
}: {
  title?: string
  description?: string
  action?: ReactNode
  children: ReactNode
  className?: string
  surface?: "card" | "flat"
}) {
  return (
    <section className={`${surface === "flat" ? "min-w-0" : "rounded-xl border bg-card"} ${className}`}>
      {(title || action) && (
        <div className={`flex items-start justify-between gap-4 ${surface === "flat" ? "pb-4" : "border-b px-5 py-4"}`}>
          <div className="min-w-0">
            {title && <h2 className="text-sm font-semibold">{title}</h2>}
            {description && <p className="mt-1 text-sm leading-5 text-muted-foreground">{description}</p>}
          </div>
          <div className="shrink-0">{action}</div>
        </div>
      )}
      {children}
    </section>
  )
}

export function StatCard({
  label,
  value,
  note,
  accent = "bg-primary/10 text-primary",
  icon,
}: {
  label: string
  value: string | number
  note: string
  accent?: string
  icon?: ReactNode
}) {
  return (
    <div className="rounded-xl border bg-card p-4 sm:p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="text-sm font-medium text-muted-foreground">{label}</p>
          <p className="mt-3 text-2xl font-semibold tracking-tight">{value}</p>
        </div>
        {icon && <span aria-hidden="true" className={`grid size-9 shrink-0 place-items-center rounded-lg text-base leading-none ${accent}`}>{icon}</span>}
      </div>
      <p className="mt-3 text-sm text-muted-foreground">{note}</p>
    </div>
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

const statusBadgeVariant = { success: "success-light", destructive: "destructive-light", warning: "warning-light", neutral: "secondary" } as const

export function StatusBadge({ status }: { status: string }) {
  const normalized = status.toLowerCase().replaceAll("_", " ")
  return <Badge variant={statusBadgeVariant[statusTone(status)]} radius="full" className="capitalize"><span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-current opacity-70" />{normalized}</Badge>
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
      <IconStack className="empty-state-icon-stack text-primary"><HugeiconsIcon icon={Layers01Icon} className="size-6" aria-hidden="true" /></IconStack>
      <h3 className="mt-4 text-sm font-semibold">{title}</h3>
      <p className="mt-1 max-w-sm text-sm leading-5 text-muted-foreground">{description}</p>
      {href ? <Button render={<Link href={href} />} nativeButton={false} variant={actionVariant} className="mt-4">{action}</Button>
        : onAction ? <Button type="button" variant={actionVariant} className="mt-4" onClick={onAction}>{action}</Button> : null}
    </div>
  )
}

export function FormField({
  label,
  htmlFor,
  hint,
  error,
  children,
}: {
  label: string
  htmlFor: string
  hint?: string
  error?: string
  children: ReactNode
}) {
  return (
    <div className="space-y-1.5">
      <label htmlFor={htmlFor} className="block text-sm font-medium">{label}</label>
      {children}
      {error ? <p role="alert" className="text-sm leading-4 text-destructive">{error}</p> : hint && <p className="text-sm leading-4 text-muted-foreground">{hint}</p>}
    </div>
  )
}

export function InlineLink({ href, children }: { href: string; children: ReactNode }) {
  return <Link href={href} className="text-sm font-medium text-primary hover:underline">{children}</Link>
}

/** A single list entry for connection-style inventories (credentials, clusters, Git sources, …). */
export function ConnectionRow({ icon, title, subtitle, badges, meta, actions, children }: {
  icon?: ReactNode
  title: ReactNode
  subtitle?: ReactNode
  badges?: ReactNode
  meta?: ReactNode
  actions?: ReactNode
  children?: ReactNode
}) {
  return (
    <li className="px-5 py-4">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-3">
        {icon && <span aria-hidden="true" className="grid size-9 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">{icon}</span>}
        <div className="min-w-0 flex-1 basis-56">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h3 className="truncate text-sm font-medium">{title}</h3>
            {badges}
          </div>
          {subtitle && <p className="mt-0.5 truncate font-mono text-xs text-muted-foreground">{subtitle}</p>}
          {meta && <div className="mt-1 text-xs text-muted-foreground">{meta}</div>}
        </div>
        {actions}
      </div>
      {children && <div className="mt-3">{children}</div>}
    </li>
  )
}

/** Separated destructive area for deleting a resource. */
export function DangerZone({ title = "Danger zone", description, children }: { title?: string; description?: string; children: ReactNode }) {
  return (
    <section className="rounded-xl border border-destructive/30 bg-destructive/5">
      <div className="border-b border-destructive/20 px-5 py-3"><h2 className="text-sm font-semibold text-destructive">{title}</h2>{description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}</div>
      <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">{children}</div>
    </section>
  )
}

/** Bordered checkbox with a title and optional help text; the whole card toggles. */
export function CheckboxCard({ checked, onCheckedChange, title, description, disabled, id }: {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  title: ReactNode
  description?: ReactNode
  disabled?: boolean
  id?: string
}) {
  return (
    <label className={`flex items-start gap-3 rounded-lg border p-3 text-sm ${disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer hover:bg-muted/40"}`}>
      <Checkbox id={id} checked={checked} disabled={disabled} onCheckedChange={(next) => onCheckedChange(Boolean(next))} className="mt-0.5" />
      <span className="min-w-0"><span className="block font-medium">{title}</span>{description && <span className="mt-0.5 block text-muted-foreground">{description}</span>}</span>
    </label>
  )
}

/** Boolean setting with a Switch, label and help text on one row. */
export function SwitchField({ checked, onCheckedChange, label, description, disabled, id, tone = "default" }: {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  label: string
  description?: string
  disabled?: boolean
  id: string
  tone?: "default" | "warning"
}) {
  return (
    <div className={`flex items-start justify-between gap-4 rounded-lg border p-3 ${tone === "warning" ? "border-warning/30 bg-warning/10" : ""}`}>
      <div className="min-w-0"><label htmlFor={id} className="block text-sm font-medium">{label}</label>{description && <p className="mt-0.5 text-sm text-muted-foreground">{description}</p>}</div>
      <Switch id={id} checked={checked} disabled={disabled} onCheckedChange={onCheckedChange} aria-label={label} />
    </div>
  )
}
