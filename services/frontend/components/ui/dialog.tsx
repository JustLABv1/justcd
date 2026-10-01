"use client"

import type { ReactNode } from "react"
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog"
import { HugeiconsIcon } from "@hugeicons/react"
import { Cancel01Icon } from "@hugeicons/core-free-icons"
import { cn } from "cn"
import { Button } from "@/components/ui/button"

const sizes = { sm: "max-w-md", md: "max-w-xl", lg: "max-w-2xl", xl: "max-w-3xl" } as const

/**
 * Shared modal. `form` dialogs close through the footer's Cancel button (see
 * DialogFooter); `info` dialogs have no footer and close through the header X.
 * A dialog never shows both, so there is exactly one obvious way out.
 */
function AppDialog({ open, onOpenChange, title, description, busy = false, variant = "form", size = "lg", children }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  busy?: boolean
  variant?: "form" | "info"
  size?: keyof typeof sizes
  children: ReactNode
}) {
  return <DialogPrimitive.Root open={open} onOpenChange={(next) => { if (!busy) onOpenChange(next) }}>
    <DialogPrimitive.Portal>
      <DialogPrimitive.Backdrop className="fixed inset-0 z-50 bg-black/55 backdrop-blur-[2px] data-open:animate-in data-open:fade-in-0 motion-reduce:animate-none" />
      <DialogPrimitive.Popup className={cn("fixed left-1/2 top-1/2 z-50 max-h-[min(88dvh,900px)] w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-2xl border bg-card p-5 text-card-foreground shadow-2xl outline-none data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 motion-reduce:animate-none sm:p-6", sizes[size])}>
        <div className="flex items-start justify-between gap-4 border-b pb-4">
          <div className="min-w-0">
            <DialogPrimitive.Title className="text-base font-semibold">{title}</DialogPrimitive.Title>
            {description && <DialogPrimitive.Description className="mt-1 text-sm leading-6 text-muted-foreground">{description}</DialogPrimitive.Description>}
          </div>
          {variant === "info" && <DialogPrimitive.Close render={<Button type="button" size="icon-sm" variant="ghost" aria-label="Close dialog" disabled={busy} />}>
            <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} aria-hidden="true" />
          </DialogPrimitive.Close>}
        </div>
        <div className="pt-5">{children}</div>
      </DialogPrimitive.Popup>
    </DialogPrimitive.Portal>
  </DialogPrimitive.Root>
}

/** Right-aligned `[Cancel] [Primary]` footer. Put it as the last child of the dialog form. */
function DialogFooter({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("flex flex-wrap justify-end gap-2 border-t pt-4", className)}>{children}</div>
}

/** Cancel button for DialogFooter; must render inside an AppDialog. */
function DialogCancel({ disabled, children = "Cancel" }: { disabled?: boolean; children?: ReactNode }) {
  return <DialogPrimitive.Close render={<Button type="button" variant="outline" disabled={disabled} />}>{children}</DialogPrimitive.Close>
}

export { AppDialog, DialogFooter, DialogCancel }
