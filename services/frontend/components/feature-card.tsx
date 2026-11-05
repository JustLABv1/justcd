"use client"

import { useState, type ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Edit02Icon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"

/**
 * A compact setting: title, one-line summary, optional on/off switch and an
 * edit icon. Selecting the card (or the icon) opens a side sheet with the
 * detailed controls, which keeps the surrounding page short.
 *
 * Without `enabled` the card is always on and only offers configuration.
 * Without `children` it is a plain switch card with nothing to configure.
 */
export function FeatureCard({
  id,
  title,
  description,
  summary,
  enabled,
  onEnabledChange,
  switchDisabled,
  attention,
  configured,
  open: controlledOpen,
  onOpenChange,
  sheetTitle,
  sheetDescription,
  sheetFooter,
  children,
}: {
  id: string
  title: ReactNode
  description?: ReactNode
  /** Short read-only status such as the current choices. */
  summary?: ReactNode
  enabled?: boolean
  onEnabledChange?: (enabled: boolean) => void
  switchDisabled?: boolean
  /** Shown as a badge when the configuration needs the user's input. */
  attention?: string
  /** Shown as an informational badge when the item holds non-default values. */
  configured?: boolean
  open?: boolean
  onOpenChange?: (open: boolean) => void
  sheetTitle?: string
  sheetDescription?: ReactNode
  /** Extra footer actions, shown beside "Done". */
  sheetFooter?: ReactNode
  children?: ReactNode
}) {
  const [innerOpen, setInnerOpen] = useState(false)
  const open = controlledOpen ?? innerOpen
  const setOpen = onOpenChange ?? setInnerOpen
  const active = enabled !== false
  const configurable = children != null && active
  const label = typeof title === "string" ? title : "setting"
  const body = (
    <>
      <span className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium">{title}</span>
        {attention && (
          <Badge size="sm" radius="full" variant="warning-light">
            {attention}
          </Badge>
        )}
        {!attention && configured && (
          <Badge size="sm" radius="full" variant="info-light">
            Configured
          </Badge>
        )}
      </span>
      {description && (
        <span className="mt-1 block text-xs leading-5 text-muted-foreground">
          {description}
        </span>
      )}
      {active && summary && (
        <span className="mt-2 block truncate text-xs text-foreground/80">
          {summary}
        </span>
      )}
    </>
  )
  return (
    <div
      data-slot="feature-card"
      className={`flex items-start gap-3 rounded-2xl p-4 ring-1 ring-border transition-colors dark:bg-muted/45 dark:ring-0 ${active ? "bg-card" : "bg-muted/20"} ${configurable ? "hover:bg-muted/40" : ""}`}
    >
      {configurable ? (
        <button
          type="button"
          onClick={() => setOpen(true)}
          aria-haspopup="dialog"
          aria-label={`Configure ${label}`}
          className="min-w-0 flex-1 rounded-md text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          {body}
        </button>
      ) : (
        <div className="min-w-0 flex-1">{body}</div>
      )}
      <div className="flex shrink-0 items-center gap-2">
        {configurable && (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={`Edit ${label}`}
            onClick={() => setOpen(true)}
          >
            <HugeiconsIcon icon={Edit02Icon} strokeWidth={2} />
          </Button>
        )}
        {enabled !== undefined && (
          <Switch
            id={id}
            checked={enabled}
            disabled={switchDisabled}
            aria-label={`Enable ${label}`}
            onCheckedChange={(next) => {
              onEnabledChange?.(next)
              // Offer the details right after turning on a configurable item.
              if (next && children != null) setOpen(true)
            }}
          />
        )}
      </div>
      {configurable && (
        <Sheet open={open} onOpenChange={setOpen}>
          <SheetContent className="gap-0 data-[side=right]:w-full motion-reduce:transition-none sm:my-3 sm:mr-3 sm:rounded-xl sm:border sm:data-[side=right]:h-[calc(100%-1.5rem)] data-[side=right]:sm:max-w-xl">
            <SheetHeader className="shrink-0 border-b p-5 pr-12">
              <SheetTitle>{sheetTitle ?? label}</SheetTitle>
              {sheetDescription && (
                <SheetDescription>{sheetDescription}</SheetDescription>
              )}
            </SheetHeader>
            <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5">
              {children}
            </div>
            <SheetFooter className="shrink-0 flex-row justify-between gap-2 border-t p-4">
              <div>{sheetFooter}</div>
              <SheetClose render={<Button type="button" />}>Done</SheetClose>
            </SheetFooter>
          </SheetContent>
        </Sheet>
      )}
    </div>
  )
}
