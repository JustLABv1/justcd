"use client"

import Link from "next/link"
import { useState, type ReactNode } from "react"
import { HugeiconsIcon, type IconSvgElement } from "@hugeicons/react"
import { ArrowDown01Icon, MoreHorizontalIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"

export type ActionItem = {
  label: string
  icon?: IconSvgElement
  onSelect?: () => void
  href?: string
  disabled?: boolean
  destructive?: boolean
  /** When set, selecting the item opens a confirmation dialog before `onConfirm` runs. */
  confirm?: { title: string; description: string; confirmLabel: string; onConfirm: () => Promise<void>; children?: ReactNode; confirmDisabled?: boolean }
  /** Draw a separator above this item. Destructive items get one automatically. */
  separatorBefore?: boolean
}

function MenuItems({ items, onConfirm }: { items: ActionItem[]; onConfirm: (index: number) => void }) {
  return <>{items.map((item, index) => {
    const separator = index > 0 && (item.separatorBefore || (item.destructive && !items[index - 1].destructive))
    const content = <>{item.icon && <HugeiconsIcon icon={item.icon} strokeWidth={1.8} aria-hidden="true" />}{item.label}</>
    return <span key={item.label} className="contents">
      {separator && <DropdownMenuSeparator />}
      {item.href
        ? <DropdownMenuItem render={<Link href={item.href} />} disabled={item.disabled} variant={item.destructive ? "destructive" : "default"}>{content}</DropdownMenuItem>
        : <DropdownMenuItem disabled={item.disabled} variant={item.destructive ? "destructive" : "default"} onClick={() => item.confirm ? onConfirm(index) : item.onSelect?.()}>{content}</DropdownMenuItem>}
    </span>
  })}</>
}

function useConfirmState(items: ActionItem[]) {
  const [confirming, setConfirming] = useState<number | null>(null)
  const active = confirming === null ? null : items[confirming]
  const dialog = active?.confirm ? <ConfirmDisclosure
    open
    onOpenChange={(next) => { if (!next) setConfirming(null) }}
    title={active.confirm.title}
    description={active.confirm.description}
    confirmLabel={active.confirm.confirmLabel}
    confirmDisabled={active.confirm.confirmDisabled}
    confirmVariant={active.destructive ? "destructive" : "default"}
    onConfirm={active.confirm.onConfirm}
  >{active.confirm.children}</ConfirmDisclosure> : null
  return { dialog, ask: setConfirming }
}

/**
 * Row-level actions: at most one visible `primary` control plus a "⋯" menu for
 * the rest. `label` names the row so each menu trigger is distinguishable for
 * assistive technology.
 */
export function RowActions({ label, primary, items }: { label: string; primary?: ReactNode; items: ActionItem[] }) {
  const { dialog, ask } = useConfirmState(items)
  const visible = items.filter((item) => item.label)
  return <div className="flex shrink-0 items-center gap-1.5">
    {primary}
    {visible.length > 0 && <DropdownMenu>
      <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="icon" aria-label={`Actions for ${label}`} />}>
        <HugeiconsIcon icon={MoreHorizontalIcon} strokeWidth={2} aria-hidden="true" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-auto min-w-48">
        <MenuItems items={visible} onConfirm={ask} />
      </DropdownMenuContent>
    </DropdownMenu>}
    {dialog}
  </div>
}

/** Labelled menu button for page headers ("New", "More actions", "View"). */
export function ActionMenu({ label, items, variant = "outline", size = "default", icon, align = "end" }: {
  label: ReactNode
  items: ActionItem[]
  variant?: "default" | "outline" | "secondary" | "ghost"
  size?: "default" | "sm" | "xs"
  icon?: IconSvgElement
  align?: "start" | "end"
}) {
  const { dialog, ask } = useConfirmState(items)
  return <>
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button type="button" variant={variant} size={size} />}>
        {icon && <HugeiconsIcon icon={icon} strokeWidth={1.8} aria-hidden="true" />}
        {label}
        <HugeiconsIcon icon={ArrowDown01Icon} strokeWidth={2} aria-hidden="true" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-auto min-w-52">
        <MenuItems items={items} onConfirm={ask} />
      </DropdownMenuContent>
    </DropdownMenu>
    {dialog}
  </>
}
