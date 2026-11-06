"use client"

import Link from "next/link"
import type { ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Building03Icon, Shield01Icon, UserIcon, Folder01Icon, InformationCircleIcon, Alert02Icon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { PageHeading } from "@/components/ui-kit"

export type SettingsNavItem = { id: string; label: string; href?: string; onSelect?: () => void; badge?: ReactNode }
export type SettingsNavGroup = { label?: string; items: SettingsNavItem[] }

const navItemClass = (active: boolean) => `flex w-full items-center justify-between gap-2 rounded-lg px-3 py-2 text-left text-sm whitespace-nowrap transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring ${active ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:bg-muted/60 hover:text-foreground"}`

/** Left navigation of a settings area. A scrollable strip below `lg`, a sticky column above. */
export function SettingsNav({ groups, activeId, label }: { groups: SettingsNavGroup[]; activeId: string; label: string }) {
  return <nav aria-label={label} className="min-w-0 lg:sticky lg:top-6 lg:self-start">
    <div className="-mx-1 flex gap-1 overflow-x-auto px-1 pb-1 lg:mx-0 lg:block lg:space-y-5 lg:overflow-visible lg:px-0 lg:pb-0">
      {groups.map((group, index) => <div key={group.label ?? index} className="flex shrink-0 gap-1 lg:block lg:space-y-0.5">
        {group.label && <p className="hidden px-3 pb-1 text-[11px] font-semibold tracking-wider text-muted-foreground uppercase lg:block">{group.label}</p>}
        {group.items.map((item) => {
          const active = item.id === activeId
          const content = <><span>{item.label}</span>{item.badge}</>
          return item.href
            ? <Link key={item.id} href={item.href} aria-current={active ? "page" : undefined} className={navItemClass(active)}>{content}</Link>
            : <button key={item.id} type="button" aria-current={active ? "page" : undefined} onClick={item.onSelect} className={navItemClass(active)}>{content}</button>
        })}
      </div>)}
    </div>
  </nav>
}

const scopeIcons = { instance: Shield01Icon, workspace: Folder01Icon, account: UserIcon, application: Building03Icon } as const

/** Says who a setting affects, replacing the full-width "these settings apply to…" banners. */
export function ScopeBadge({ scope, name }: { scope: keyof typeof scopeIcons; name?: string }) {
  const label = scope === "instance" ? "Whole instance" : scope === "account" ? "Your account" : name ?? (scope === "workspace" ? "Workspace" : "Application")
  return <Badge variant="outline" className="gap-1.5"><HugeiconsIcon icon={scopeIcons[scope]} strokeWidth={1.8} className="size-3.5" aria-hidden="true" /><span className="sr-only">Applies to: </span>{label}</Badge>
}

/** Inline notice for a settings page. Use sparingly; the scope badge covers "who does this affect". */
export function SettingsNotice({ tone = "info", title, children }: { tone?: "info" | "warning"; title?: string; children?: ReactNode }) {
  return <div role={tone === "warning" ? "alert" : "note"} className={`flex items-start gap-3 rounded-xl border px-4 py-3 text-sm ${tone === "warning" ? "border-warning/40 bg-warning/10" : "bg-muted/30"}`}>
    <HugeiconsIcon icon={tone === "warning" ? Alert02Icon : InformationCircleIcon} strokeWidth={1.8} className={`mt-0.5 size-4 shrink-0 ${tone === "warning" ? "text-warning-foreground dark:text-warning" : "text-muted-foreground"}`} aria-hidden="true" />
    <div className="min-w-0">{title && <p className="font-medium">{title}</p>}{children && <div className={title ? "mt-0.5 text-muted-foreground" : "text-muted-foreground"}>{children}</div>}</div>
  </div>
}

/**
 * The shell of every settings page: heading with scope, a left navigation when the area has more
 * than one section, and a content column of sections. All settings areas use this one layout.
 */
export function SettingsLayout({ title, description, scope, actions, nav, activeId, navLabel = "Settings sections", children }: {
  title: string
  description?: ReactNode
  scope?: ReactNode
  actions?: ReactNode
  nav?: SettingsNavGroup[]
  activeId?: string
  navLabel?: string
  children: ReactNode
}) {
  const hasNav = (nav?.reduce((total, group) => total + group.items.length, 0) ?? 0) > 1
  return <div className="min-w-0">
    <PageHeading title={title} description={description} badge={scope} actions={actions} />
    {hasNav
      ? <div className="grid items-start gap-6 lg:grid-cols-[176px_minmax(0,1fr)] lg:gap-8">
          <SettingsNav groups={nav!} activeId={activeId ?? ""} label={navLabel} />
          <div className="min-w-0 max-w-5xl space-y-8">{children}</div>
        </div>
      : <div className="max-w-4xl space-y-8">{children}</div>}
  </div>
}
