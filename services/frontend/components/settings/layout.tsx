"use client"

import { Button } from "@/components/panelui/button"

import Link from "next/link"
import { useEffect, useRef, type ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Building03Icon, Shield01Icon, UserIcon, Folder01Icon } from "@hugeicons/core-free-icons"
import { Alert } from "@/components/panelui/alert"
import { Badge } from "@/components/panelui/badge"
import { PageHeading } from "@/components/ui-kit"

export type SettingsNavItem = { id: string; label: string; href?: string; onSelect?: () => void; badge?: ReactNode }
export type SettingsNavGroup = { label?: string; items: SettingsNavItem[] }

const navItemClass = (active: boolean) => `flex w-full items-center justify-between gap-2 rounded-lg px-3 py-2 text-left text-sm whitespace-nowrap transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring ${active ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:bg-muted/60 hover:text-foreground"}`

/** Left navigation of a settings area. A scrollable strip below `lg`, a sticky column above. */
export function SettingsNav({ groups, activeId, label }: { groups: SettingsNavGroup[]; activeId: string; label: string }) {
  const rail = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const container = rail.current
    if (!container) return
    const reveal = () => {
      const active = container.querySelector<HTMLElement>('[aria-current="page"]')
      if (!active || container.scrollWidth <= container.clientWidth) return
      const bounds = container.getBoundingClientRect()
      const item = active.getBoundingClientRect()
      if (item.left < bounds.left || item.right > bounds.right) container.scrollLeft += item.left - bounds.left - (bounds.width - item.width) / 2
    }
    reveal()
    const observer = new ResizeObserver(reveal)
    observer.observe(container)
    return () => observer.disconnect()
  }, [activeId])
  return <nav aria-label={label} className="min-w-0 lg:sticky lg:top-6 lg:self-start">
    <div ref={rail} className="-mx-1 flex gap-1 overflow-x-auto px-1 pb-1 lg:mx-0 lg:block lg:space-y-5 lg:overflow-visible lg:px-0 lg:pb-0">
      {groups.map((group, index) => <div key={group.label ?? index} className="flex shrink-0 gap-1 lg:block lg:space-y-0.5">
        {group.label && <p className="hidden px-3 pb-1 text-[11px] font-semibold tracking-wider text-muted-foreground uppercase lg:block">{group.label}</p>}
        {group.items.map((item) => {
          const active = item.id === activeId
          const content = <><span>{item.label}</span>{item.badge}</>
          return item.href
            ? <Link key={item.id} href={item.href} aria-current={active ? "page" : undefined} className={navItemClass(active)}>{content}</Link>
            : <Button variant="ghost" key={item.id} type="button" aria-current={active ? "page" : undefined} onClick={item.onSelect} className={navItemClass(active)}>{content}</Button>
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
export function SettingsNotice({ tone = "info", title, children, inline = false }: { tone?: "info" | "warning" | "success" | "destructive"; title?: string; children?: ReactNode; inline?: boolean }) {
  return <Alert tone={tone} variant={inline ? "inline" : "plain"} role={tone === "warning" || tone === "destructive" ? "alert" : tone === "success" ? "status" : "note"}>
    <Alert.Icon />{title && <Alert.Title>{title}</Alert.Title>}{children && <Alert.Description>{children}</Alert.Description>}
  </Alert>
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
      // From `lg` up the sections are children of their destination in the app's navigation rail; the strip is for narrow screens.
      ? <div className="min-w-0 space-y-6 lg:space-y-0">
          <div className="lg:hidden"><SettingsNav groups={nav!} activeId={activeId ?? ""} label={navLabel} /></div>
          <div className="min-w-0 space-y-8">{children}</div>
        </div>
      : <div className="max-w-4xl space-y-8">{children}</div>}
  </div>
}
