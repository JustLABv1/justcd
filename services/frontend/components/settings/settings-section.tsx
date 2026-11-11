"use client"

import { createContext, useContext, type ReactNode } from "react"
import { Card } from "@/components/panelui/card"
import { Section } from "@/components/section"

/**
 * Building blocks of every settings page.
 *
 *   SettingsSection   heading (title, description, action) above one card
 *   SettingsGroup     the card: rows divided by hairlines
 *   SettingsRow       label + description on the left, value and control on the right
 *   SettingsPanel     a section whose card holds arbitrary content (lists, forms, tables)
 *   SettingsDangerZone  destructive actions, always last on the page
 */

const InSettingsCard = createContext(false)

/** Settings headings sit on the canvas above a single Card. */
function SettingsSurface({ title, description, action, children, className = "", tone }: { title?: ReactNode; description?: ReactNode; action?: ReactNode; children: ReactNode; className?: string; tone?: "danger" }) {
  return <Section divided={false} title={title} description={description} action={action} className={className}>
    <InSettingsCard.Provider value>
      <Card className={tone === "danger" ? "min-w-0 border-destructive/30 dark:border-destructive/30" : "min-w-0"}>{children}</Card>
    </InSettingsCard.Provider>
  </Section>
}

export function SettingsSection({ title, description, action, children, className = "" }: { title: ReactNode; description?: ReactNode; action?: ReactNode; children: ReactNode; className?: string }) {
  return <SettingsSurface title={title} description={description} action={action} className={className}>{children}</SettingsSurface>
}

/** Rows divided by hairlines. Inside a section it is just the rows; on its own it gets a Card. */
export function SettingsGroup({ children, className = "", bare }: { children: ReactNode; className?: string; /** Never draw a Card, even outside a section. */ bare?: boolean }) {
  const inside = useContext(InSettingsCard)
  const rows = <div className={`min-w-0 divide-y ${className}`}>{children}</div>
  if (inside || bare) return rows
  return <Card className="min-w-0">{rows}</Card>
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
 * A section whose body holds arbitrary content. Drop-in for the generic `Panel` inside settings
 * pages: same title, description and action props.
 */
export function SettingsPanel({ title, description, action, children, className = "", surface = "card", padded = false }: { title?: ReactNode; description?: ReactNode; action?: ReactNode; children: ReactNode; className?: string; /** `flat` drops the Card, for a panel nested inside another one. */ surface?: "card" | "flat"; /** Pad the content, for free-form content such as fields (rows and lists bring their own padding). */ padded?: boolean }) {
  if (surface === "flat") return <Section divided={false} title={title} description={description} action={action} className={className}>{children}</Section>
  return <SettingsSurface title={title} description={description} action={action} className={className}>{padded ? <div className="min-w-0 p-5">{children}</div> : children}</SettingsSurface>
}

/** Destructive actions. Rows inside use the destructive button variant. */
export function SettingsDangerZone({ title = "Danger zone", description, children }: { title?: ReactNode; description?: ReactNode; children: ReactNode }) {
  return <SettingsSurface tone="danger" title={<span className="text-destructive">{title}</span>} description={description}><div className="min-w-0 divide-y divide-destructive/20">{children}</div></SettingsSurface>
}
