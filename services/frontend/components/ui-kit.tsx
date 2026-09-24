import Link from "next/link"
import type { ReactNode } from "react"

export function PageHeading({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow?: string
  title: string
  description?: string
  actions?: ReactNode
}) {
  return (
    <div className="mb-7 flex flex-col justify-between gap-4 sm:flex-row sm:items-end">
      <div>
        {eyebrow && <p className="mb-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-primary">{eyebrow}</p>}
        <h1 className="text-2xl font-semibold tracking-tight sm:text-[28px]">{title}</h1>
        {description && <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  )
}

export function Panel({
  title,
  description,
  action,
  children,
  className = "",
}: {
  title?: string
  description?: string
  action?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={`rounded-xl border bg-card ${className}`}>
      {(title || action) && (
        <div className="flex items-start justify-between gap-4 border-b px-5 py-4">
          <div>
            {title && <h2 className="text-sm font-semibold">{title}</h2>}
            {description && <p className="mt-1 text-xs leading-5 text-muted-foreground">{description}</p>}
          </div>
          {action}
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
          <p className="text-xs font-medium text-muted-foreground">{label}</p>
          <p className="mt-3 text-2xl font-semibold tracking-tight">{value}</p>
        </div>
        <span className={`grid size-9 place-items-center rounded-lg text-base ${accent}`}>{icon}</span>
      </div>
      <p className="mt-3 text-[11px] text-muted-foreground">{note}</p>
    </div>
  )
}

export function StatusBadge({ status }: { status: string }) {
  const normalized = status.toLowerCase().replaceAll("_", " ")
  const styles = normalized.includes("synced") || normalized.includes("succeeded") || normalized === "healthy"
    ? "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-300"
    : normalized.includes("delet") || normalized.includes("failed") || normalized.includes("degrad") || normalized.includes("error")
      ? "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-900 dark:bg-rose-950/50 dark:text-rose-300"
      : normalized.includes("sync") || normalized.includes("pending") || normalized.includes("running") || normalized.includes("out of")
        ? "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950/50 dark:text-amber-300"
        : "border-border bg-muted/50 text-muted-foreground"
  return <span className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[10px] font-medium capitalize ${styles}`}><span className="size-1.5 rounded-full bg-current opacity-70" />{normalized}</span>
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
      <span className="grid size-11 place-items-center rounded-xl border bg-muted/50 text-lg text-muted-foreground">◇</span>
      <h3 className="mt-4 text-sm font-semibold">{title}</h3>
      <p className="mt-1 max-w-sm text-xs leading-5 text-muted-foreground">{description}</p>
      {href && <Link href={href} className="mt-4 text-xs font-medium text-primary hover:underline">{action} <span aria-hidden="true">→</span></Link>}
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
      <label htmlFor={htmlFor} className="block text-xs font-medium">{label}</label>
      {children}
      {hint && <p className="text-[11px] leading-4 text-muted-foreground">{hint}</p>}
    </div>
  )
}

export function InlineLink({ href, children }: { href: string; children: ReactNode }) {
  return <Link href={href} className="text-xs font-medium text-primary hover:underline">{children}</Link>
}
