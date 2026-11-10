"use client"

import { useState, type ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Edit02Icon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/panelui/badge"
import { Card } from "@/components/panelui/card"
import { SheetBody } from "@/components/panelui/sheet"
import { Button } from "@/components/panelui/button"
import { Checkbox } from "@/components/panelui/checkbox"
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/panelui/sheet"

/**
 * A compact setting: title, one-line summary, optional checkbox saved with the form and an
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
    <span className="block min-w-0 w-full">
      <span className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium">{title}</span>
        {attention && (
          <Badge size="sm"  variant="warning">
            {attention}
          </Badge>
        )}
        {!attention && configured && (
          <Badge size="sm"  variant="info">
            Configured
          </Badge>
        )}
      </span>
      {description && (
        <span className="mt-1 block text-xs font-normal leading-5 text-muted-foreground">
          {description}
        </span>
      )}
      {active && summary && (
        <span className="mt-2 block truncate text-xs font-normal text-foreground/80">
          {summary}
        </span>
      )}
    </span>
  )
  return (
    <Card size="sm" data-slot="feature-card" className={`min-w-0 ${active ? "bg-card" : "bg-muted/20"}`}>
      <Card.Content className="flex items-start gap-3">
      {configurable ? (
        <Button variant="ghost"
          type="button"
          onClick={() => setOpen(true)}
          aria-haspopup="dialog"
          aria-label={`Configure ${label}`}
          className="h-auto min-w-0 flex-1 justify-start p-0 text-left whitespace-normal outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          {body}
        </Button>
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
          <Checkbox
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
        <Sheet side="right" open={open} onOpenChange={setOpen}>
          <SheetContent className="w-full max-w-xl">
            <SheetHeader className="shrink-0">
              <SheetTitle>{sheetTitle ?? label}</SheetTitle>
              {sheetDescription && (
                <SheetDescription>{sheetDescription}</SheetDescription>
              )}
            </SheetHeader>
            <SheetBody className="space-y-4">{children}</SheetBody>
            <SheetFooter className="shrink-0 flex-row justify-between gap-2">
              <div>{sheetFooter}</div>
              <SheetClose render={<Button type="button" />}>Done</SheetClose>
            </SheetFooter>
          </SheetContent>
        </Sheet>
      )}
      </Card.Content>
    </Card>
  )
}
