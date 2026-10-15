"use client"

import type { ComponentProps } from "react"
import { Toggle as TogglePrimitive } from "@base-ui/react/toggle"
import { ToggleGroup as ToggleGroupPrimitive } from "@base-ui/react/toggle-group"
import { cn } from "cn"

function ToggleGroup({ className, ...props }: ToggleGroupPrimitive.Props) {
  return <ToggleGroupPrimitive data-slot="toggle-group" className={cn("inline-flex w-fit items-center gap-0.5 rounded-lg border bg-background p-0.5", className)} {...props} />
}

function ToggleGroupItem({ className, ...props }: TogglePrimitive.Props) {
  return <TogglePrimitive data-slot="toggle-group-item" className={cn("group/toggle relative inline-flex h-7 shrink-0 items-center justify-center gap-2 rounded-md px-3 whitespace-nowrap text-sm font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 data-pressed:bg-primary data-pressed:text-primary-foreground", className)} {...props} />
}

function ToggleGroupCount({ className, ...props }: ComponentProps<"span">) {
  return <span data-slot="toggle-group-count" className={cn("min-w-5 rounded bg-muted px-1.5 py-0.5 text-xs leading-none text-foreground tabular-nums group-aria-pressed/toggle:bg-primary-foreground/15 group-aria-pressed/toggle:text-primary-foreground", className)} {...props} />
}

export { ToggleGroup, ToggleGroupItem, ToggleGroupCount }
