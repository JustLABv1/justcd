"use client"

import type { ReactNode } from "react"
import { Dialog } from "@/components/panelui/dialog"
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog"
import { cn } from "cn"
import { Button } from "@/components/panelui/button"

const sizes = { sm: "sm", md: "md", lg: "lg", xl: "lg" } as const

/**
 * Shared modal, drawn with the PanelUI Dialog. `form` dialogs close through the footer's Cancel
 * button (see DialogFooter); `info` dialogs have no footer and close through the header X.
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
  return <Dialog open={open} onOpenChange={(next) => { if (!busy) onOpenChange(next) }} dismissible={!busy}>
    <Dialog.Content variant="plain" size={sizes[size]} showClose={variant === "info" && !busy}>
      <Dialog.Header title={title} description={description} />
      <Dialog.Body>{children}</Dialog.Body>
    </Dialog.Content>
  </Dialog>
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
