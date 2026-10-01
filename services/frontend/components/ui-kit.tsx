import Link from "next/link"
import type { ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Layers01Icon } from "@hugeicons/core-free-icons"
import { IconStack } from "@/components/reui/icon-stack"

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
  icon = "·",
}: {
  label: string
  value: string | number
  note: string
  accent?: string
  icon?: string
}) {
  return (
    <div className="rounded-xl border bg-card p-4 sm:p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="text-sm font-medium text-muted-foreground">{label}</p>
          <p className="mt-3 text-2xl font-semibold tracking-tight">{value}</p>
        </div>
        <span aria-hidden="true" className={`grid size-9 shrink-0 place-items-center rounded-lg text-base leading-none ${accent}`}>{icon}</span>
      </div>
      <p className="mt-3 text-sm text-muted-foreground">{note}</p>
    </div>
  )
}

export function StatusBadge({ status }: { status: string }) {
  const normalized = status.toLowerCase().replaceAll("_", " ")
  const styles = normalized.includes("synced") || normalized.includes("succeeded") || normalized === "healthy" || normalized === "true"
    ? "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-300"
    : normalized.includes("delet") || normalized.includes("failed") || normalized.includes("degrad") || normalized.includes("error") || normalized === "missing" || normalized === "false"
      ? "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-900 dark:bg-rose-950/50 dark:text-rose-300"
      : normalized.includes("sync") || normalized.includes("pending") || normalized.includes("running") || normalized.includes("out of") || normalized.includes("progress") || normalized === "partial" || normalized === "suspended" || normalized.includes("paused")
        ? "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950/50 dark:text-amber-300"
        : "border-border bg-muted/50 text-muted-foreground"
  return <span className={`inline-flex w-fit shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-2 py-0.5 text-sm font-medium capitalize ${styles}`}><span className="size-1.5 shrink-0 rounded-full bg-current opacity-70" />{normalized}</span>
}

export function EmptyState({
  title,
  description,
  href,
  action = "Get started",
}: {
  title: string
  description: string
  href?: string
  action?: string
}) {
  return (
    <div className="flex flex-col items-center justify-center px-5 py-12 text-center">
      <IconStack className="empty-state-icon-stack text-primary"><HugeiconsIcon icon={Layers01Icon} className="size-6" /></IconStack>
      <h3 className="mt-4 text-sm font-semibold">{title}</h3>
      <p className="mt-1 max-w-sm text-sm leading-5 text-muted-foreground">{description}</p>
      {href && <Link href={href} className="mt-4 text-sm font-medium text-primary hover:underline">{action} <span aria-hidden="true">→</span></Link>}
    </div>
  )
}

export function FormField({
  label,
  htmlFor,
  hint,
  children,
}: {
  label: string
  htmlFor: string
  hint?: string
  children: ReactNode
}) {
  return (
    <div className="space-y-1.5">
      <label htmlFor={htmlFor} className="block text-sm font-medium">{label}</label>
      {children}
      {hint && <p className="text-sm leading-4 text-muted-foreground">{hint}</p>}
    </div>
  )
}

export function InlineLink({ href, children }: { href: string; children: ReactNode }) {
  return <Link href={href} className="text-sm font-medium text-primary hover:underline">{children}</Link>
}
