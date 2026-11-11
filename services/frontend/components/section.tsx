"use client"

import { useId, type ReactNode } from "react"
import { Card } from "@/components/panelui/card"
import { cn } from "@/lib/utils"

/**
 * A titled part of a page.
 *
 * Open (default): the heading sits on the canvas and the content chooses its own surface. Two open
 * sections stacked directly after each other are ruled off automatically, so a reader can always
 * tell where one ends; pass `divided={false}` when the content already ends in a surface of its own
 * (settings sections, a stack of record Cards).
 *
 * `surface`: the whole section is one Card, heading inside. Use it for data that would otherwise
 * float on the canvas: a toolbar with a table, a filtered list, a summary beside other Cards.
 */
export function Section({ id, title, description, action, children, className, surface = false, divided = true, heading = "default" }: {
  id?: string
  title?: ReactNode
  description?: ReactNode
  action?: ReactNode
  children: ReactNode
  className?: string
  /** Draw the section as one Card with the heading in its header. */
  surface?: boolean
  /** Rule this open section off from an open section directly before it. Default true. */
  divided?: boolean
  /** `large` for the one heading that titles a whole wizard step. */
  heading?: "default" | "large"
}) {
  const generatedId = useId()
  const headingId = `${id ?? generatedId}-heading`
  const hasHeading = Boolean(title || description || action)
  if (surface) {
    return <Card as="section" size="sm" id={id} aria-labelledby={title ? headingId : undefined} data-section="surface" className={cn("min-w-0", className)}>
      {hasHeading && <Card.Header>
        {title && <Card.Title as="h2" id={headingId}>{title}</Card.Title>}
        {description && <Card.Description>{description}</Card.Description>}
        {action && <Card.Action className="flex max-w-full flex-wrap items-center gap-2">{action}</Card.Action>}
      </Card.Header>}
      <Card.Content className="min-w-0 space-y-4">{children}</Card.Content>
    </Card>
  }
  return <section
    id={id}
    aria-labelledby={title ? headingId : undefined}
    data-section={divided ? "open" : "open-undivided"}
    className={cn("min-w-0 space-y-4", divided && "[[data-section=open]+&]:border-t [[data-section=open]+&]:pt-6", className)}
  >
    {hasHeading && <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
      <div className="min-w-0 flex-1 basis-56 space-y-1">
        {title && <h2 id={headingId} className={heading === "large" ? "text-lg font-semibold tracking-tight" : "text-sm font-semibold"}>{title}</h2>}
        {description && <div className="text-sm leading-5 text-muted-foreground">{description}</div>}
      </div>
      {action && <div className="flex max-w-full flex-wrap items-center gap-2">{action}</div>}
    </div>}
    {children}
  </section>
}
