import { useId, type ReactNode } from "react"

/**
 * Building blocks of every settings page.
 *
 *   SettingsSection   heading (title, description, action) above one card
 *   SettingsGroup     the card: rows divided by hairlines
 *   SettingsRow       label + description on the left, value and control on the right
 *   SettingsPanel     a section whose card holds arbitrary content (lists, forms, tables)
 *   SettingsDangerZone  destructive actions, always last on the page
 */

/** Heading block shared by sections and panels, so the two can never drift apart. */
function SectionHeading({ id, title, description, action }: { id?: string; title?: ReactNode; description?: ReactNode; action?: ReactNode }) {
  if (!title && !action) return null
  return <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
    <div className="min-w-0 flex-1 basis-72 space-y-1">
      {title && <h2 id={id} className="text-base font-semibold tracking-tight">{title}</h2>}
      {description && <p className="max-w-2xl text-sm leading-5 text-muted-foreground">{description}</p>}
    </div>
    {action && <div className="flex shrink-0 flex-wrap items-center gap-2">{action}</div>}
  </div>
}

const cardClass = "min-w-0 overflow-hidden rounded-xl border bg-card"

export function SettingsSection({ title, description, action, children, className = "" }: { title: ReactNode; description?: ReactNode; action?: ReactNode; children: ReactNode; className?: string }) {
  const id = useId()
  return <section aria-labelledby={id} className={`min-w-0 space-y-3 ${className}`}>
    <SectionHeading id={id} title={title} description={description} action={action} />
    {children}
  </section>
}

/** The card for a stack of rows. */
export function SettingsGroup({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`${cardClass} divide-y ${className}`}>{children}</div>
}

export function SettingsRow({ label, description, value, action, className = "" }: { label: ReactNode; description?: ReactNode; value?: ReactNode; action?: ReactNode; className?: string }) {
  return <div className={`flex min-w-0 flex-wrap items-center gap-x-5 gap-y-3 px-5 py-4 sm:flex-nowrap ${className}`}>
    <div className="min-w-0 flex-1 basis-40 space-y-1">
      <h3 className="text-sm font-medium">{label}</h3>
      {description && <div className="text-sm leading-5 text-muted-foreground">{description}</div>}
    </div>
    {value && <div className="min-w-0 text-sm sm:max-w-[45%]">{value}</div>}
    {action && <div className="shrink-0">{action}</div>}
  </div>
}

/**
 * A section whose card holds arbitrary content. Drop-in for the generic `Panel` inside settings
 * pages: same title, description and action props, but the heading sits above the card.
 */
export function SettingsPanel({ title, description, action, children, className = "", surface = "card" }: { title?: ReactNode; description?: ReactNode; action?: ReactNode; children: ReactNode; className?: string; /** `flat` drops the card, for a panel nested inside another card. */ surface?: "card" | "flat" }) {
  const id = useId()
  return <section aria-labelledby={title ? id : undefined} className={`min-w-0 space-y-3 ${className}`}>
    <SectionHeading id={id} title={title} description={description} action={action} />
    {surface === "flat" ? <div className="min-w-0">{children}</div> : <div className={cardClass}>{children}</div>}
  </section>
}

/** Destructive actions. Rows inside use the destructive button variant. */
export function SettingsDangerZone({ title = "Danger zone", description, children }: { title?: ReactNode; description?: ReactNode; children: ReactNode }) {
  const id = useId()
  return <section aria-labelledby={id} className="min-w-0 space-y-3">
    <SectionHeading id={id} title={<span className="text-destructive">{title}</span>} description={description} />
    <div className="min-w-0 divide-y divide-destructive/20 overflow-hidden rounded-xl border border-destructive/30 bg-destructive/5">{children}</div>
  </section>
}
