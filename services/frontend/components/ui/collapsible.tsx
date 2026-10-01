"use client"

import type { ReactNode } from "react"
import { Collapsible as CollapsiblePrimitive } from "@base-ui/react/collapsible"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowDown01Icon } from "@hugeicons/core-free-icons"
import { cn } from "cn"

function Collapsible(props: CollapsiblePrimitive.Root.Props) {
  return <CollapsiblePrimitive.Root data-slot="collapsible" {...props} />
}

function CollapsibleTrigger({ className, children, ...props }: CollapsiblePrimitive.Trigger.Props) {
  return (
    <CollapsiblePrimitive.Trigger
      data-slot="collapsible-trigger"
      className={cn("group/trigger flex w-fit items-center gap-1.5 rounded-md text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring", className)}
      {...props}
    >
      {children}
      <HugeiconsIcon icon={ArrowDown01Icon} strokeWidth={2} aria-hidden="true" className="size-4 text-muted-foreground transition-transform group-data-panel-open/trigger:rotate-180" />
    </CollapsiblePrimitive.Trigger>
  )
}

function CollapsibleContent({ className, ...props }: CollapsiblePrimitive.Panel.Props) {
  return <CollapsiblePrimitive.Panel data-slot="collapsible-content" className={cn("overflow-hidden", className)} {...props} />
}

/** Disclosure with a styled summary; replaces native <details>. */
function Disclosure({ summary, children, defaultOpen, className }: { summary: ReactNode; children: ReactNode; defaultOpen?: boolean; className?: string }) {
  return <Collapsible defaultOpen={defaultOpen} className={className}>
    <CollapsibleTrigger>{summary}</CollapsibleTrigger>
    <CollapsibleContent><div className="pt-3">{children}</div></CollapsibleContent>
  </Collapsible>
}

export { Collapsible, CollapsibleTrigger, CollapsibleContent, Disclosure }
